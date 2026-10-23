package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Recorder captures upstream responses as an http.RoundTripper.
//
// Recording is opt-in and off by default. Nothing writes a snapshot during a
// wayfared request: a monitor that scribbles to disk on every page view is a
// different program with different failure modes, and the capture path is a
// deliberate act by someone who intends to publish the result.
//
// Requests are deduplicated by key. A ladder prices each size once and then
// re-prices some of them as slippage probes, so the same key genuinely recurs
// within a run; storing one copy keeps the directory readable and makes the
// replay of a repeated request trivially consistent.
type Recorder struct {
	// Transport is the real transport to record through. Nil means
	// http.DefaultTransport.
	Transport http.RoundTripper

	// Manifest metadata the recorder cannot infer. Fill these before use.
	Corridor    Corridor
	Sizes       []string
	Sources     Sources
	GitRevision string
	Notes       []string

	// Dirty marks a snapshot recorded from a modified working tree, where
	// GitRevision names a tree that did not produce these bytes. It is
	// carried into the manifest so the caveat lives in the artifact rather
	// than only in the shell that made it.
	Dirty bool

	// Classify names the upstream a request went to. Nil uses a default that
	// distinguishes Horizon by its paths.
	Classify func(*http.Request) string

	mu      sync.Mutex
	seen    map[string]bool
	entries []Interaction
	bodies  [][]byte
	started time.Time
}

// defaultClassify distinguishes Horizon, reference-rate, and check requests.
//
// Horizon's endpoints are a known, small set. The two rate feeds and their
// diagnostic are also known; other hosts are published check endpoints.
func defaultClassify(req *http.Request) string {
	p := req.URL.EscapedPath()
	host := strings.ToLower(req.URL.Hostname())
	switch {
	case strings.HasPrefix(p, "/paths/"),
		strings.HasPrefix(p, "/order_book"),
		strings.HasPrefix(p, "/accounts"),
		strings.HasPrefix(p, "/assets"):
		return KindHorizon
	case strings.HasSuffix(host, "open.er-api.com"),
		strings.HasSuffix(host, "currency-api.pages.dev"),
		strings.HasSuffix(host, "floatrates.com"):
		return KindReference
	default:
		return KindCheck
	}
}

// RoundTrip implements http.RoundTripper, recording as it proxies.
//
// The response body is read in full and replaced, so the caller sees exactly
// the bytes that were stored. Anything else would let a snapshot and the run
// that produced it disagree.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := r.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("snapshot: reading response for recording: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("snapshot: closing recorded response: %w", closeErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	r.add(req, resp, body)
	return resp, nil
}

// add stores one interaction, ignoring a key already captured.
func (r *Recorder) add(req *http.Request, resp *http.Response, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.seen == nil {
		r.seen = map[string]bool{}
		// Truncated to the second because the directory name is derived
		// from this value at second resolution. Keeping sub-second digits
		// here would make a snapshot's own manifest disagree with the
		// directory holding it.
		r.started = time.Now().UTC().Truncate(time.Second)
	}
	key := Key(req.Method, req.URL)
	if r.seen[key] {
		return
	}
	r.seen[key] = true

	classify := r.Classify
	if classify == nil {
		classify = defaultClassify
	}

	seq := len(r.entries) + 1
	r.entries = append(r.entries, Interaction{
		Kind:        classify(req),
		Method:      strings.ToUpper(req.Method),
		Key:         key,
		URL:         req.URL.String(),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		RecordedAt:  time.Now().UTC().Truncate(time.Second),
		BodyFile:    fmt.Sprintf("%s/%03d-%s.json", responsesDir, seq, slugFor(req)),
		BodySHA256:  bodyHash(body),
	})
	r.bodies = append(r.bodies, body)
}

// slugFor builds the human-readable half of a body filename. It carries no
// meaning to the loader, which resolves files only through the manifest.
func slugFor(req *http.Request) string {
	s := strings.Trim(req.URL.EscapedPath(), "/")
	s = strings.ReplaceAll(s, "/", "-")
	// Some upstreams end their path in .json already; the recorder adds its
	// own extension, and two of them reads as a mistake in a committed fixture.
	s = strings.TrimSuffix(s, ".json")
	if amt := req.URL.Query().Get("source_amount"); amt != "" {
		s += "-" + amt
	}
	if s == "" {
		s = "root"
	}
	return sanitiseSlug(s)
}

// sanitiseSlug keeps a filename to characters that behave the same on every
// filesystem a contributor might check the repo out on.
func sanitiseSlug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// DirName is the conventional directory name for a snapshot recorded now:
// <send>-<recv>-<RFC3339 basic, UTC>, e.g. "usdc-ngnc-20260821T140355Z".
//
// Corridor first so a listing groups by corridor; timestamp second so it sorts
// chronologically within one.
func DirName(c Corridor, at time.Time) string {
	return c.Slug() + "-" + at.UTC().Format("20060102T150405Z")
}

// RecordedAt is the moment the first interaction was captured, which is the
// manifest's recorded_at.
//
// Callers naming a directory must use this rather than reading the clock
// again: the directory name and the manifest inside it are the same fact, and
// a second clock read makes them disagree by however long the run took.
func (r *Recorder) RecordedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		return time.Now().UTC().Truncate(time.Second)
	}
	return r.started
}

// Save writes the captured run to dir, creating it.
//
// It refuses to write into a directory that already holds a manifest: a
// snapshot is a record of one moment, and overwriting one silently would
// destroy the provenance of anything already derived from it.
func (r *Recorder) Save(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.entries) == 0 {
		return fmt.Errorf("snapshot: nothing was recorded, refusing to write an empty snapshot to %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
		return fmt.Errorf("snapshot: %s already contains a %s; snapshots are never overwritten", dir, ManifestFile)
	}
	if err := os.MkdirAll(filepath.Join(dir, responsesDir), 0o755); err != nil {
		return fmt.Errorf("snapshot: creating %s: %w", dir, err)
	}

	for i, in := range r.entries {
		path := filepath.Join(dir, filepath.FromSlash(in.BodyFile))
		if err := os.WriteFile(path, r.bodies[i], 0o644); err != nil {
			return fmt.Errorf("snapshot: writing %s: %w", in.BodyFile, err)
		}
	}

	m := Manifest{
		Format:       Format,
		Version:      Version,
		RecordedAt:   r.started,
		GitRevision:  r.GitRevision,
		Dirty:        r.Dirty,
		Corridor:     r.Corridor,
		Sizes:        r.Sizes,
		Sources:      r.Sources,
		Notes:        r.Notes,
		Interactions: r.entries,
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot: encoding manifest: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), buf, 0o644); err != nil {
		return fmt.Errorf("snapshot: writing manifest: %w", err)
	}
	return nil
}

// SaveContentAddressed stores a run under its content hash and returns that
// hash for the run record. Snapshot bytes remain outside the run-store chain.
func SaveContentAddressed(parent string, r *Recorder) (string, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("snapshot: creating artifact directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".capture-")
	if err != nil {
		return "", fmt.Errorf("snapshot: creating temporary artifact: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if err := r.Save(tempDir); err != nil {
		return "", err
	}
	hash, err := ContentHash(tempDir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(parent, strings.TrimPrefix(hash, "sha256:"))
	if _, err := os.Stat(dest); err == nil {
		if _, err := VerifyContentHash(dest, hash); err != nil {
			return "", err
		}
		return hash, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("snapshot: inspecting content-addressed artifact: %w", err)
	}
	if err := os.Rename(tempDir, dest); err != nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			if _, verifyErr := VerifyContentHash(dest, hash); verifyErr == nil {
				return hash, nil
			}
		}
		return "", fmt.Errorf("snapshot: storing content-addressed artifact: %w", err)
	}
	return hash, nil
}

// Count reports how many distinct interactions have been captured.
func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
