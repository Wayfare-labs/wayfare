package server

import (
	"net/http"

	"github.com/Wayfare-labs/wayfare/metrics"
)

// handleMetrics serves the process's counters in Prometheus text exposition
// format 0.0.4 (issue #705).
//
// The payload is produced by package metrics; this handler only decides what a
// scrape costs and how it is labelled. Every series this endpoint publishes,
// with its labels and vocabulary, is documented in docs/api.md — and a test in
// package metrics fails the build if a shipped series is missing from there.
//
// The exposition is the operational view of the same facts the log carries, so
// nothing here is a second source of truth about a corridor: no verdict, no
// price, no health claim. /healthz remains the endpoint a deployment probe
// should use.
//
// A Server built without a collector answers with the families declared and
// zero observations rather than an error. That is a deliberate reading of the
// project's rule about absence: an operator who points a scraper at the wrong
// port gets a "404", and one who points it at a process with metrics switched
// off gets an empty-but-valid document, which is the truthful answer of "this
// process has recorded nothing".
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			"only GET and HEAD are supported")
		return
	}

	collector := s.Metrics
	if collector == nil {
		collector = metrics.New()
	}

	// The version parameter is part of the format: a Prometheus server that
	// cannot parse the body must know which grammar it was served.
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Counters are monotonic within a process, so a cached body is not merely
	// stale, it is wrong: a scraper would read a plateau as "nothing is
	// happening" on exactly the instance that has stopped being scraped.
	w.Header().Set("Cache-Control", "no-store")

	if _, err := collector.WriteTo(w); err != nil {
		// The status line and headers are already on the wire, so the only
		// honest thing left is to say the payload was cut short rather than
		// let a scraper treat a truncated body as a complete one. It
		// validates what it parses, so it will reject the partial final line;
		// the log carries why.
		log().Warn("metrics exposition incomplete", "error", err)
	}
}
