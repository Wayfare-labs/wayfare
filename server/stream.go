package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/Wayfare-labs/wayfare/route"
)

// maxConcurrentStreams bounds how many live measurement streams one Server
// serves at once. Streaming holds a connection open for the whole ladder — up
// to the full measurement budget — so this is deliberately small: the free
// deployment must not be pinned by a few curious clients, and a caller over
// the limit gets an immediate, machine-readable refusal rather than a
// connection that silently stalls.
const maxConcurrentStreams = 8

// rungEventJSON is one rung event's payload. Index and Total carry the rung's
// position and the ladder's size so a client can order the rungs and count
// progress even though the ladder prices concurrently and rungs do not arrive
// in index order.
type rungEventJSON struct {
	Index int            `json:"index"`
	Total int            `json:"total"`
	Rung  route.RungJSON `json:"rung"`
}

// sseWriter serialises Server-Sent Events onto one response.
//
// route.Ladder prices rungs concurrently, so OnRung is invoked from several
// goroutines at once; the handler must not let two of them interleave a
// partial frame into the same byte stream. Every write goes through here under
// one mutex, and a write that fails (the client has gone) is remembered so the
// handler stops trying.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher

	mu     sync.Mutex
	broken bool
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	f, _ := w.(http.Flusher)
	return &sseWriter{w: w, f: f}
}

func (s *sseWriter) writeEvent(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.writeRaw(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
}

func (s *sseWriter) writeComment(text string) {
	s.writeRaw(": " + text + "\n\n")
}

func (s *sseWriter) writeRaw(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return
	}
	if _, err := fmt.Fprint(s.w, frame); err != nil {
		s.broken = true
		return
	}
	if s.f != nil {
		s.f.Flush()
	}
}

// handleCorridorStream is the streaming counterpart of handleCorridor: it
// prices the same corridor with the same validation, budget and failure
// policy, but reports each rung as it resolves instead of blocking until the
// ladder is finished (issue #639). A twelve-rung live measurement is a dozen
// round trips to Horizon, so the JSON endpoint can sit silent for tens of
// seconds; this reports progress instead.
//
// It emits Server-Sent Events. A `rung` event carries one priced rung in the
// exact wire shape the JSON response uses for that rung (route.ToRungJSON).
// The `final` event carries the same body the JSON endpoint would have
// returned — the very same measureLive call produces it — so a client that
// prefers not to stream can ignore the rung events and read `final`. A failure
// sends `error` with the same body and code the JSON endpoint uses. `done`
// always ends the stream.
func (s *Server) handleCorridorStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET is supported")
		return
	}

	// Bound concurrent streams before doing any work: a refusal must be cheap.
	if !s.acquireStream() {
		writeError(w, r, http.StatusServiceUnavailable, codeTooManyStreams,
			"too many live measurement streams in progress; try again shortly")
		return
	}
	defer s.releaseStream()

	req, ok := s.parseCorridorRequest(w, r, "from", "to", "sizes", "pretty")
	if !ok {
		return
	}

	rungs := len(req.sizes)
	if rungs == 0 {
		rungs = len(route.DefaultSizes)
	}
	budget := s.budgetFor(rungs)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Ask any buffering reverse proxy not to hold the events back. Without
	// this, nginx (and some platforms' edge) would deliver the whole stream at
	// once and defeat the point of streaming it.
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Measurement-Budget", budget.String())
	w.WriteHeader(http.StatusOK)

	sw := newSSEWriter(w)

	// A comment opens the stream immediately so the client's connection state
	// flips as soon as the ladder starts rather than after the first rung,
	// which can be several seconds away.
	sw.writeComment("wayfare live measurement")
	sw.writeEvent("start", map[string]any{
		"corridor":       req.from + "-" + req.to,
		"total_rungs":    rungs,
		"budget":         budget.String(),
		"reference_pair": req.pair(),
	})

	// The ladder's context is the request's, so a client that navigates away
	// or cancels the fetch — which the UI does on #640 and on any navigation —
	// drops the connection, cancels this context, and stops the ladder instead
	// of leaving it to run to completion for a reader who has gone.
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()

	req.onRung = func(index int, rung route.Rung) {
		sw.writeEvent("rung", rungEventJSON{
			Index: index,
			Total: rungs,
			Rung:  route.ToRungJSON(rung),
		})
	}

	body, status, _ := s.measureLive(ctx, req, budget)
	if status == http.StatusOK {
		sw.writeEvent("final", body)
	} else {
		sw.writeEvent("error", body)
	}
	sw.writeEvent("done", map[string]any{"status": status})
}

// acquireStream takes one stream slot without blocking, so the refusal path
// costs no latency. The semaphore is built once; see Server.streamOnce.
func (s *Server) acquireStream() bool {
	s.streamOnce.Do(func() { s.streamSem = make(chan struct{}, maxConcurrentStreams) })
	select {
	case s.streamSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseStream() {
	<-s.streamSem
}
