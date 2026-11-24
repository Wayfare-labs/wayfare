// Package metrics counts what the monitor does while nobody is reading its log.
//
// # Why it exists next to the log
//
// Every fact recorded here is already in the log: which corridor failed to
// sweep, which upstream refused a request, how long a live measurement took,
// how often a client was told to slow down. A log answers "what happened at
// 04:12?" and cannot answer "how often in the last hour?" — and the second
// question is the one an alert has to be built from. So the counters sit
// beside the log lines rather than replacing them, and each keeps its own
// audience.
//
// # Why there is no client library
//
// The project runs exactly two direct dependencies (CONTRIBUTING.md, enforced
// by deps_test.go), and Prometheus's text exposition format is line-oriented
// plain text: five metric families are about a hundred lines of formatting.
// What a client library would add — a registry, reflection-based collectors,
// an HTTP handler — is machinery this package does not need, and every line of
// it would be surface a reviewer has to trust.
//
// # What a number here does not mean
//
// A counter is the count of observations made by this process since it
// started. It resets on restart, so it is not a total for the deployment: an
// alert should read rate() over a window, never the raw value. A labelled
// series that has never been observed is absent from the exposition, and
// absence is not zero — it says nothing was recorded, which is the same line
// this project draws everywhere between unknown and nil. The one exception is
// the label-free rejection counter, which always prints, because an absent
// line there would be indistinguishable from an endpoint that stopped working.
//
// # Bounds on the series
//
// Label values come from code, never from a request: corridors from the asset
// registry, upstreams from the wiring in cmd/wayfared, outcomes and kinds from
// the constants below. That is what keeps the exposition from growing with
// traffic, and docs/api.md documents the full value set for each label.
package metrics

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sweep outcome labels: what one corridor did in one sweep.
//
// The three are mutually exclusive by construction. A corridor either fails
// to measure (SweepFailed), measures and then fails to be written down
// (SweepUnrecorded), or is recorded (SweepOK), so the sum of the three is the
// number of corridor measurements attempted.
const (
	// SweepOK: measured and appended to the run store.
	SweepOK = "ok"
	// SweepFailed: the measurement did not happen, so there was nothing to
	// record. This is the upstream-or-engine failure an operator alerts on.
	SweepFailed = "failed"
	// SweepUnrecorded: the measurement succeeded and the record was refused
	// by the store. The history has a hole that the corridor did not cause,
	// which is why it is not folded into SweepFailed: the remedies are
	// different (disk and permissions versus upstream availability).
	SweepUnrecorded = "unrecorded"
)

// Upstream error kinds: how an upstream exchange failed.
//
// The vocabulary is the HTTP-level shape of the failure taxonomy the clients
// already distinguish (issues #121 and #127): did the upstream refuse (rate
// limit or client error), break (server error), or never answer (transport,
// timeout). See Transport for where the classification happens and why an
// upstream's own error type is not the source.
const (
	// UpstreamRateLimited: HTTP 429 — the request was well-formed and the
	// quota was not. Transient, and the only kind whose remedy is to wait.
	UpstreamRateLimited = "rate_limited"
	// UpstreamClientError: any other 4xx. The upstream answered and refused
	// this request specifically, which usually means the URL or the asset is
	// wrong rather than the upstream being ill.
	UpstreamClientError = "client_error"
	// UpstreamServerError: any 5xx. The upstream is unhealthy and no client
	// change fixes it.
	UpstreamServerError = "server_error"
	// UpstreamTransport: no HTTP response at all — DNS failure, connection
	// refused, TCP reset.
	UpstreamTransport = "transport"
	// UpstreamTimeout: the exchange was cut off by a deadline, either the
	// client's own timeout or the request context's measurement budget.
	UpstreamTimeout = "timeout"
)

// Live-request outcome labels: how one request that asked for a measurement
// ended. Only requests that actually reach the engine are observed; a
// history-first read served from the store is not a live request and does not
// appear here.
const (
	// LiveMeasured: the ladder priced, and the response is that measurement.
	LiveMeasured = "measured"
	// LiveStale: the measurement failed and the response is the most recent
	// stored run, labelled stale. The reader got a number; it is not current.
	LiveStale = "stale"
	// LiveTimeout: the measurement ran out of the budget derived from the
	// rung count, and there was no history to fall back on.
	LiveTimeout = "timeout"
	// LiveError: the measurement failed for another reason and there was no
	// history to fall back on, so the response is an error envelope.
	LiveError = "error"
)

// liveBuckets bound the duration of one live corridor measurement.
//
// A measurement is one HTTP exchange per rung — twelve at the default sizes,
// up to twenty-four when sizes= is used — plus the reference-rate fetch and
// the checks, all inside the budget server.budgetFor derives from the rung
// count and caps at -timeout. So the top bucket is the default 90s ceiling,
// and the sub-second buckets exist because a corridor served from a warm
// connection is that fast, and "the whole endpoint got ten times slower"
// deserves to be visible.
var liveBuckets = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	40 * time.Second,
	60 * time.Second,
	90 * time.Second,
}

// upstreamBuckets bound the duration of one HTTP exchange with one upstream.
//
// Deliberately finer and shorter than liveBuckets: this is a single request,
// and the clients give it 10s (refrate) or 20s (Horizon) before giving up. A
// Horizon path search that takes 8s is normal; one that regularly takes 18s
// is the story the buckets exist to tell.
var upstreamBuckets = []time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
}

// Collector holds the counters and histograms one process serves at /metrics.
//
// It is safe for concurrent use: every observation takes one mutex, and the
// ladder prices up to four rungs at a time while the sweep and HTTP requests
// overlap.
//
// A nil *Collector is usable. Every observe method returns early on a nil
// receiver, which is what makes this an optional dependency in the honest
// sense: a Server, Scheduler or RateLimiter constructed without one behaves
// exactly as it did before this package existed, and no caller has to write a
// nil check.
type Collector struct {
	mu sync.Mutex

	// sweeps is corridor -> outcome -> count.
	sweeps map[string]map[string]uint64

	// upstreamErrors is upstream -> kind -> count, and upstreamDuration is
	// the matching latency histogram per upstream. Both are keyed by the
	// label the process was wired with, so a value appears only if some
	// client actually declares that upstream.
	upstreamErrors   map[string]map[string]uint64
	upstreamDuration map[string]*histogram

	// liveDuration histograms one live measurement attempt by outcome, so a
	// 40s timeout and a 0.4s success do not share a bucket.
	liveDuration map[string]*histogram

	// rejections counts requests the rate limiter refused. It carries no
	// label on purpose: the quota is the service's, and keying rejections by
	// client would publish client identities and grow with them.
	rejections uint64
}

// New returns a collector with nothing observed yet.
func New() *Collector {
	return &Collector{
		sweeps:           map[string]map[string]uint64{},
		upstreamErrors:   map[string]map[string]uint64{},
		upstreamDuration: map[string]*histogram{},
		liveDuration:     map[string]*histogram{},
	}
}

// Sweep records the outcome of measuring one corridor in one sweep.
// outcome is SweepOK, SweepFailed or SweepUnrecorded.
func (c *Collector) Sweep(corridor, outcome string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sweeps[corridor] == nil {
		c.sweeps[corridor] = map[string]uint64{}
	}
	c.sweeps[corridor][outcome]++
}

// Upstream records one completed HTTP exchange with one upstream: how long it
// took, and kind when it did not succeed. kind is one of the Upstream*
// constants, or empty when the upstream answered.
//
// One call is one observation of both the latency histogram and — when kind is
// non-empty — the error counter, so the two series cannot drift out of step or
// count the same failure twice.
func (c *Collector) Upstream(upstream string, took time.Duration, kind string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.histLocked(c.upstreamDuration, upstream, upstreamBuckets).observe(took)
	if kind == "" {
		return
	}
	if c.upstreamErrors[upstream] == nil {
		c.upstreamErrors[upstream] = map[string]uint64{}
	}
	c.upstreamErrors[upstream][kind]++
}

// LiveRequest records one attempt to measure a corridor on behalf of a
// request, with the outcome the request actually got. took is the handler's
// wall-clock duration for the attempt, not the response's staleness.
func (c *Collector) LiveRequest(outcome string, took time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.histLocked(c.liveDuration, outcome, liveBuckets).observe(took)
}

// RateLimitRejection records one request the limiter refused with 429.
func (c *Collector) RateLimitRejection() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rejections++
}

// histLocked returns the histogram for key, creating it on first use. Callers
// must hold c.mu — the name is the repo's convention for that (see
// RateLimiter.evictLocked).
func (c *Collector) histLocked(m map[string]*histogram, key string, bounds []time.Duration) *histogram {
	h, ok := m[key]
	if !ok {
		h = newHistogram(bounds)
		m[key] = h
	}
	return h
}

// histogram is a cumulative duration histogram with fixed bucket boundaries.
type histogram struct {
	le []time.Duration
	// counts has len(le)+1 entries; counts[i] is the number of observations
	// at or below le[i], and the last is the +Inf bucket, so the slice is
	// monotonically non-decreasing and its tail equals observations.
	counts []uint64
	// sumNanos is the exact sum of observations. Durations add as integer
	// nanoseconds and are converted to seconds only when rendered, so no
	// rounding accumulates over a long-lived process.
	sumNanos int64
}

func newHistogram(le []time.Duration) *histogram {
	return &histogram{le: le, counts: make([]uint64, len(le)+1)}
}

func (h *histogram) observe(d time.Duration) {
	for i, bound := range h.le {
		if d <= bound {
			// Cumulative: this observation belongs in this bucket and every
			// wider one, including +Inf.
			for j := i; j < len(h.counts); j++ {
				h.counts[j]++
			}
			h.sumNanos += int64(d)
			return
		}
	}
	h.counts[len(h.counts)-1]++
	h.sumNanos += int64(d)
}

// observations is the count the +Inf bucket holds by construction.
func (h *histogram) observations() uint64 { return h.counts[len(h.counts)-1] }

// sumSeconds renders the accumulated duration as the Prometheus sample value.
func (h *histogram) sumSeconds() string {
	return formatSeconds(time.Duration(h.sumNanos))
}

// WriteTo renders every observed series in Prometheus text exposition format
// 0.0.4 and writes it as one payload (io.WriterTo).
//
// Families are emitted in name order and label sets in label-value order, so
// two processes with the same counts produce byte-identical output. That is
// what makes the exposition testable against a literal expectation rather than
// against a parser that would accept anything.
//
// An empty collector writes the HELP and TYPE lines and the label-free
// rejection counter and nothing else: no observations is not the same claim as
// an observation of zero, and inventing zero-valued series for label sets
// nobody used would make a fresh process look like a busy quiet one.
func (c *Collector) WriteTo(w io.Writer) (int64, error) {
	if c == nil {
		return 0, nil
	}
	c.mu.Lock()
	buf := c.renderLocked()
	c.mu.Unlock()

	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

// The metric names, exported because docs/api.md documents these exact
// strings and a test can assert against them rather than a copy.
const (
	MetricSweepOutcomes    = "wayfare_sweep_outcomes_total"
	MetricUpstreamErrors   = "wayfare_upstream_errors_total"
	MetricUpstreamDuration = "wayfare_upstream_request_duration_seconds"
	MetricLiveDuration     = "wayfare_live_request_duration_seconds"
	MetricRejections       = "wayfare_rate_limit_rejections_total"
)

func (c *Collector) renderLocked() *bytes.Buffer {
	var buf bytes.Buffer

	// Live-request latency, by outcome.
	writeHeader(&buf, MetricLiveDuration, "histogram",
		"Duration of a request that asked for a live corridor measurement, by the outcome it got. Requests served from history are not live and never appear.")
	for _, outcome := range sortedKeys(c.liveDuration) {
		writeHistogram(&buf, MetricLiveDuration, labels{"outcome": outcome}, c.liveDuration[outcome])
	}

	// Rate-limit rejections: always present, see the package doc.
	writeHeader(&buf, MetricRejections, "counter",
		"Requests refused with HTTP 429 by this process's per-client rate limiter.")
	fmt.Fprintf(&buf, "%s %d\n", MetricRejections, c.rejections)

	// Sweep outcomes, by corridor and outcome.
	writeHeader(&buf, MetricSweepOutcomes, "counter",
		"Corridor measurements completed by the scheduler, by corridor and outcome. The three outcomes are mutually exclusive.")
	for _, corridor := range sortedKeys(c.sweeps) {
		for _, outcome := range sortedKeys(c.sweeps[corridor]) {
			fmt.Fprintf(&buf, "%s%s %d\n", MetricSweepOutcomes,
				labels{"corridor": corridor, "outcome": outcome}.render(), c.sweeps[corridor][outcome])
		}
	}

	// Upstream errors, by upstream and kind.
	writeHeader(&buf, MetricUpstreamErrors, "counter",
		"Failed HTTP exchanges with an upstream, by upstream and how it failed. One per HTTP exchange, so a redirect chain counts once per hop.")
	for _, upstream := range sortedKeys(c.upstreamErrors) {
		for _, kind := range sortedKeys(c.upstreamErrors[upstream]) {
			fmt.Fprintf(&buf, "%s%s %d\n", MetricUpstreamErrors,
				labels{"upstream": upstream, "kind": kind}.render(), c.upstreamErrors[upstream][kind])
		}
	}

	// Upstream latency, by upstream. Every exchange counts here, success or
	// not, so the _count sample is the denominator the error ratio needs.
	writeHeader(&buf, MetricUpstreamDuration, "histogram",
		"Duration of one HTTP exchange with an upstream, including failures and every redirect hop. The _count sample is the number of exchanges.")
	for _, upstream := range sortedKeys(c.upstreamDuration) {
		writeHistogram(&buf, MetricUpstreamDuration, labels{"upstream": upstream}, c.upstreamDuration[upstream])
	}

	return &buf
}

func writeHeader(buf *bytes.Buffer, name, typ, help string) {
	fmt.Fprintf(buf, "# HELP %s %s\n", name, help)
	fmt.Fprintf(buf, "# TYPE %s %s\n", name, typ)
}

// writeHistogram emits the cumulative buckets, the sum and the count for one
// label set. The label set is rendered on every sample, as the format
// requires, and le is appended to it.
func writeHistogram(buf *bytes.Buffer, name string, ls labels, h *histogram) {
	for i, bound := range h.le {
		merged := ls.clone()
		merged["le"] = formatSeconds(bound)
		fmt.Fprintf(buf, "%s_bucket%s %d\n", name, merged.render(), h.counts[i])
	}
	inf := ls.clone()
	inf["le"] = "+Inf"
	fmt.Fprintf(buf, "%s_bucket%s %d\n", name, inf.render(), h.counts[len(h.counts)-1])
	fmt.Fprintf(buf, "%s_sum%s %s\n", name, ls.render(), h.sumSeconds())
	fmt.Fprintf(buf, "%s_count%s %d\n", name, ls.render(), h.observations())
}

// formatSeconds renders a duration as Prometheus expects a base-unit sample:
// plain decimal seconds, no exponent, no trailing zeros.
func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// labels is a Prometheus label set. Iteration order is undefined, so every
// render sorts by name; that is the only reason the output is stable.
type labels map[string]string

func (l labels) clone() labels {
	out := make(labels, len(l)+1)
	for k, v := range l {
		out[k] = v
	}
	return out
}

func (l labels) render() string {
	if len(l) == 0 {
		return ""
	}
	names := make([]string, 0, len(l))
	for k := range l {
		names = append(names, k)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range names {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(k)
		buf.WriteString(`="`)
		buf.WriteString(escapeLabelValue(l[k]))
		buf.WriteString(`"`)
	}
	buf.WriteString("}")
	return buf.String()
}

// escapeLabelValue implements the format's quoting: backslash, double quote
// and newline are the three sequences a label value may not carry raw.
//
// Every value this package publishes today is a corridor key, an outcome or a
// declared upstream name, none of which contains those characters. The
// escaping is here because a label helper that happens to be safe with
// today's inputs is not safe: the next caller passes an error string, and the
// exposition silently becomes unparsable.
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	var buf strings.Builder
	buf.Grow(len(v) + 8)
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\n':
			buf.WriteString(`\n`)
		default:
			buf.WriteByte(v[i])
		}
	}
	return buf.String()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
