package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// sseEvent is one parsed Server-Sent Event: its name and the JSON payload of
// its data line.
type sseEvent struct {
	Name string
	Data map[string]any
}

// parseSSE splits a complete SSE body into events, skipping comments and
// keeping only the named events and their data. It mirrors what a browser's
// EventSource does, which is the reader the endpoint is written for.
func parseSSE(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, frame := range strings.Split(raw, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" || strings.HasPrefix(frame, ":") {
			continue
		}
		var name string
		var data string
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if name == "" {
			t.Fatalf("SSE frame has no event name: %q", frame)
		}
		ev := sseEvent{Name: name}
		if data != "" {
			if err := json.Unmarshal([]byte(data), &ev.Data); err != nil {
				t.Fatalf("event %s: data is not JSON: %v (%q)", name, err, data)
			}
		}
		events = append(events, ev)
	}
	return events
}

// TestCorridorStreamReportsRungsThenFinal pins the streaming contract (#639):
// the response is an event stream, it announces the ladder, it reports each
// rung as it resolves, and it ends with a `final` carrying the same document
// the JSON endpoint sends plus a `done`. A client that reads only `final` must
// get a complete measurement.
func TestCorridorStreamReportsRungsThenFinal(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	resp, err := http.Get(srv.URL + "/api/corridor/stream?to=NGNC&sizes=10,100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ctype := resp.Header.Get("Content-Type"); !strings.HasPrefix(ctype, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ctype)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no so a proxy does not buffer the stream", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	events := parseSSE(t, string(body))

	if len(events) == 0 || events[0].Name != "start" {
		t.Fatalf("first event = %+v, want start", events)
	}
	if events[0].Data["corridor"] != "USDC-NGNC" {
		t.Errorf("start corridor = %v, want USDC-NGNC", events[0].Data["corridor"])
	}
	if events[0].Data["total_rungs"] != float64(2) {
		t.Errorf("start total_rungs = %v, want 2", events[0].Data["total_rungs"])
	}

	var rungEvents int
	for _, ev := range events {
		if ev.Name != "rung" {
			continue
		}
		rungEvents++
		rung, _ := ev.Data["rung"].(map[string]any)
		if rung == nil {
			t.Errorf("a rung event carries no rung object: %+v", ev.Data)
		}
		if ev.Data["total"] != float64(2) {
			t.Errorf("rung total = %v, want 2", ev.Data["total"])
		}
	}
	if rungEvents != 2 {
		t.Errorf("rung events = %d, want 2", rungEvents)
	}

	// The last two events are final then done.
	if n := len(events); n < 2 || events[n-2].Name != "final" || events[n-1].Name != "done" {
		t.Fatalf("last two events = %+v, want final then done", events)
	}
	final := events[len(events)-2].Data
	rungs, _ := final["rungs"].([]any)
	if len(rungs) != 2 {
		t.Errorf("final rungs = %d, want 2", len(rungs))
	}
	if _, ok := final["integrity"]; !ok {
		t.Error("final carries no integrity; it must be the same document the JSON endpoint sends")
	}
}

// TestCorridorStreamFinalMatchesJSON is the equivalence the design rests on:
// the stream's `final` event is produced by the very same measureLive call as
// the JSON endpoint, so the two documents describe one measurement in one
// shape. The clock fields differ between two independent measurements and are
// set aside, as the ?pretty test does.
func TestCorridorStreamFinalMatchesJSON(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	_, plainBody := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")

	resp, err := http.Get(srv.URL + "/api/corridor/stream?to=NGNC&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	events := parseSSE(t, string(raw))
	var final map[string]any
	for _, ev := range events {
		if ev.Name == "final" {
			final = ev.Data
		}
	}
	if final == nil {
		t.Fatal("stream carried no final event")
	}

	for _, doc := range []map[string]any{final, plainBody} {
		for _, field := range []string{"measured_at", "reference_fetched_at"} {
			delete(doc, field)
		}
		for _, field := range []string{"recommended", "recommended_size", "integrity", "finding"} {
			if _, ok := doc[field]; !ok {
				t.Fatalf("document is missing %q; the stream and JSON shapes have drifted", field)
			}
		}
	}
	if final["recommended_size"] != plainBody["recommended_size"] {
		t.Errorf("final recommended_size = %v, JSON = %v", final["recommended_size"], plainBody["recommended_size"])
	}
}

// TestCorridorStreamRefusesTooManyStreams pins the bound (#639): once the
// server is serving its maximum live streams, another caller gets an immediate
// machine-readable 503 rather than a connection that silently stalls.
func TestCorridorStreamRefusesTooManyStreams(t *testing.T) {
	s := &Server{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: "http://127.0.0.1:0"},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1500"),
			}),
		},
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for i := 0; i < maxConcurrentStreams; i++ {
		if !s.acquireStream() {
			t.Fatalf("acquireStream failed after %d acquires", i)
		}
	}
	defer func() {
		for i := 0; i < maxConcurrentStreams; i++ {
			s.releaseStream()
		}
	}()

	status, body := getJSON(t, srv.URL+"/api/corridor/stream?to=NGNC")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when the stream limit is reached", status)
	}
	if body["code"] != codeTooManyStreams {
		t.Errorf("code = %v, want %q", body["code"], codeTooManyStreams)
	}
}

// TestCorridorStreamRejectsBadRequestsBeforeStreaming pins that validation
// stays ahead of the stream: a refused request gets the ordinary JSON error
// (status and code), not a 200 stream that opens and then errors, so a client
// can treat a non-2xx as the JSON error it is.
func TestCorridorStreamRejectsBadRequestsBeforeStreaming(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	cases := []struct {
		name     string
		path     string
		wantCode string
	}{
		{"unknown param", "/api/corridor/stream?to=NGNC&debug=1", codeInvalidQuery},
		{"unknown asset", "/api/corridor/stream?to=SCAMC", codeUnknownReceiveAsset},
		{"bad sizes", "/api/corridor/stream?to=NGNC&sizes=abc", codeInvalidSizes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := getJSON(t, srv.URL+tc.path)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", status)
			}
			if body["code"] != tc.wantCode {
				t.Errorf("code = %v, want %q", body["code"], tc.wantCode)
			}
		})
	}
}

// TestCorridorStreamRejectsOtherMethods pins that the endpoint is a read, so a
// POST is a 405 like every other read endpoint.
func TestCorridorStreamRejectsOtherMethods(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	resp, err := http.Post(srv.URL+"/api/corridor/stream?to=NGNC", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}
}
