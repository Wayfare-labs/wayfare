package metrics_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/metrics"
)

// The counters are the product here: an operator alerts on these numbers, and
// nothing else in the build checks that the text they parse means what the code
// intended. So the assertions are made against the exposition itself rather
// than against collector internals, and every family is named — a series that
// quietly stopped being emitted has to fail a test.

func TestExpositionOfAFreshCollector(t *testing.T) {
	got := exposition(t, metrics.New())

	// A process that has observed nothing still declares every family, and
	// still prints the one label-free counter. What it must not print is
	// zero-valued samples for label sets nobody used: that reads as "swept
	// every corridor successfully, upstream never failed", which is the most
	// misleading shape an empty monitor could take.
	if !strings.Contains(got, "wayfare_rate_limit_rejections_total 0\n") {
		t.Errorf("exposition =\n%s\nwant the rejection counter printed at zero", got)
	}
	if strings.Contains(got, "wayfare_sweep_outcomes_total{") {
		t.Error("a fresh collector emitted a labelled sweep sample; unobserved series must be absent")
	}
	for _, want := range []string{
		"# TYPE wayfare_sweep_outcomes_total counter",
		"# TYPE wayfare_upstream_errors_total counter",
		"# TYPE wayfare_upstream_request_duration_seconds histogram",
		"# TYPE wayfare_live_request_duration_seconds histogram",
		"# TYPE wayfare_rate_limit_rejections_total counter",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q", want)
		}
	}
	assertParsable(t, got)
}

func TestSweepCounterCountsPerCorridorAndOutcome(t *testing.T) {
	c := metrics.New()
	c.Sweep("USDC-NGNC", metrics.SweepOK)
	c.Sweep("USDC-NGNC", metrics.SweepOK)
	c.Sweep("USDC-NGNC", metrics.SweepFailed)
	c.Sweep("USDC-GHSC", metrics.SweepUnrecorded)
	got := exposition(t, c)

	for _, want := range []string{
		`wayfare_sweep_outcomes_total{corridor="USDC-GHSC",outcome="unrecorded"} 1`,
		`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="failed"} 1`,
		`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} 2`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
	// One line per corridor-and-outcome pair actually seen, and no others: a
	// stray line here would read as a corridor outcome that never happened.
	if n := countPrefixed(got, "wayfare_sweep_outcomes_total{"); n != 3 {
		t.Errorf("sweep samples = %d, want 3\ngot:\n%s", n, got)
	}
	assertParsable(t, got)
}

// TestUpstreamObservationCountsOnce: the latency histogram and the error
// counter are written by the same call, and the two series have to agree about
// how many exchanges happened. Counting an error twice would make the error
// ratio exceed 100% on a bad afternoon, which is the failure mode a reviewer
// should be able to rule out by reading one test.
func TestUpstreamObservationCountsOnce(t *testing.T) {
	c := metrics.New()
	c.Upstream(metrics.UpstreamHorizon, 100*time.Millisecond, "")
	c.Upstream(metrics.UpstreamHorizon, 200*time.Millisecond, "")
	c.Upstream(metrics.UpstreamHorizon, 30*time.Second, metrics.UpstreamRateLimited)
	got := exposition(t, c)

	for _, want := range []string{
		`wayfare_upstream_errors_total{kind="rate_limited",upstream="horizon"} 1`,
		`wayfare_upstream_request_duration_seconds_count{upstream="horizon"} 3`,
		`wayfare_upstream_request_duration_seconds_sum{upstream="horizon"} 30.3`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
	// A successful exchange contributes latency and no error sample, so the
	// only error kind present is the one that failed.
	if n := countPrefixed(got, "wayfare_upstream_errors_total{"); n != 1 {
		t.Errorf("upstream error samples = %d, want 1 (only the 429 failed)\ngot:\n%s", n, got)
	}
	// The +Inf bucket is the count by construction, so the two can never
	// disagree about how many exchanges were observed.
	if !strings.Contains(got, `wayfare_upstream_request_duration_seconds_bucket{le="+Inf",upstream="horizon"} 3`) {
		t.Errorf("+Inf bucket must equal the observation count\ngot:\n%s", got)
	}
	assertParsable(t, got)
}

func TestUpstreamKindsAreSeparateSeries(t *testing.T) {
	c := metrics.New()
	for _, kind := range []string{
		metrics.UpstreamRateLimited, metrics.UpstreamClientError,
		metrics.UpstreamServerError, metrics.UpstreamTransport, metrics.UpstreamTimeout,
	} {
		c.Upstream(metrics.UpstreamHorizon, time.Second, kind)
	}
	c.Upstream(metrics.UpstreamExchangeRateAPI, time.Second, metrics.UpstreamTimeout)
	got := exposition(t, c)

	for _, want := range []string{
		`wayfare_upstream_errors_total{kind="rate_limited",upstream="horizon"} 1`,
		`wayfare_upstream_errors_total{kind="client_error",upstream="horizon"} 1`,
		`wayfare_upstream_errors_total{kind="server_error",upstream="horizon"} 1`,
		`wayfare_upstream_errors_total{kind="transport",upstream="horizon"} 1`,
		`wayfare_upstream_errors_total{kind="timeout",upstream="horizon"} 1`,
		`wayfare_upstream_errors_total{kind="timeout",upstream="refrate.exchangerate-api"} 1`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestLiveHistogramIsCumulativeAndBucketed pins the bucket arithmetic: the
// counts must be non-decreasing, an observation lands in the bucket it fits
// and every wider one, and a duration exactly equal to a boundary is inside
// that boundary. A histogram whose buckets are not cumulative is read as a
// probability distribution by every tool that consumes it.
func TestLiveHistogramIsCumulativeAndBucketed(t *testing.T) {
	c := metrics.New()
	// 250ms is exactly the first documented live bucket.
	for _, d := range []time.Duration{
		50 * time.Millisecond,
		250 * time.Millisecond,
		249 * time.Millisecond,
		4 * time.Second,
		31 * time.Second,
		3 * time.Minute,
	} {
		c.LiveRequest(metrics.LiveMeasured, d)
	}
	got := exposition(t, c)

	wantBuckets := []struct {
		le    string
		count uint64
	}{
		{"0.25", 3}, // 50ms, 249ms, and 250ms exactly on the bound
		{"0.5", 3},
		{"1", 3},
		{"2.5", 3},
		{"5", 4}, // plus 4s
		{"10", 4},
		{"20", 4},
		{"40", 5}, // plus 31s
		{"60", 5},
		{"90", 5},
		{"+Inf", 6}, // plus the 3-minute request, past the ceiling
	}
	for _, b := range wantBuckets {
		want := fmt.Sprintf(
			`wayfare_live_request_duration_seconds_bucket{le=%q,outcome="measured"} %d`, b.le, b.count)
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
	for _, want := range []string{
		`wayfare_live_request_duration_seconds_count{outcome="measured"} 6`,
		`wayfare_live_request_duration_seconds_sum{outcome="measured"} 215.549`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
	assertParsable(t, got)
}

// TestLiveOutcomesDoNotShareBuckets: a 40s timeout and a 0.4s success are both
// live requests and both belong in the same family, but not in the same
// number — otherwise the median latency of this endpoint would silently mean
// "of the requests that worked".
func TestLiveOutcomesDoNotShareBuckets(t *testing.T) {
	c := metrics.New()
	c.LiveRequest(metrics.LiveMeasured, 400*time.Millisecond)
	c.LiveRequest(metrics.LiveTimeout, 40*time.Second)
	c.LiveRequest(metrics.LiveStale, 8*time.Second)
	c.LiveRequest(metrics.LiveError, 2*time.Second)
	got := exposition(t, c)

	for _, want := range []string{
		`wayfare_live_request_duration_seconds_count{outcome="error"} 1`,
		`wayfare_live_request_duration_seconds_count{outcome="measured"} 1`,
		`wayfare_live_request_duration_seconds_count{outcome="stale"} 1`,
		`wayfare_live_request_duration_seconds_count{outcome="timeout"} 1`,
		`wayfare_live_request_duration_seconds_bucket{le="0.25",outcome="measured"} 0`,
		`wayfare_live_request_duration_seconds_bucket{le="0.5",outcome="measured"} 1`,
		`wayfare_live_request_duration_seconds_bucket{le="20",outcome="timeout"} 0`,
		`wayfare_live_request_duration_seconds_bucket{le="40",outcome="timeout"} 1`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q\ngot:\n%s", want, got)
		}
	}
	assertParsable(t, got)
}

func TestRateLimitRejectionsCount(t *testing.T) {
	c := metrics.New()
	c.RateLimitRejection()
	c.RateLimitRejection()
	got := exposition(t, c)

	if !strings.Contains(got, "wayfare_rate_limit_rejections_total 2\n") {
		t.Errorf("exposition =\n%s\nwant the rejection counter at 2", got)
	}
	// Unlabelled by design: the quota belongs to the service, and keying it
	// by client would publish client identities as metric labels.
	if strings.Contains(got, "wayfare_rate_limit_rejections_total{") {
		t.Errorf("the rejection counter grew a label\ngot:\n%s", got)
	}
}

// TestEscapingKeepsTheExpositionParsable: every label value this package
// publishes today comes from a fixed vocabulary, so nothing reaches the
// escaper. That is exactly why the escaper is tested directly — the next
// caller passes a corridor key containing a quote, and an unescaped quote
// silently corrupts every line after it.
func TestEscapingKeepsTheExpositionParsable(t *testing.T) {
	c := metrics.New()
	c.Sweep(`USDC-"NGNC"`, "line1\nline2")
	got := exposition(t, c)

	if !strings.Contains(got, `wayfare_sweep_outcomes_total{corridor="USDC-\"NGNC\"",outcome="line1\nline2"} 1`) {
		t.Errorf("label values are not escaped as the format requires\ngot:\n%q", got)
	}
	assertParsable(t, got)
}

// TestOutputIsIndependentOfMapOrder: the maps behind the exposition are keyed
// by corridor, upstream and outcome, and Go randomises map iteration. A
// payload that changed between scrapes for no reason is a payload nobody can
// diff or grep reliably.
func TestOutputIsIndependentOfMapOrder(t *testing.T) {
	build := func() string {
		c := metrics.New()
		c.Sweep("USDC-NGNC", metrics.SweepOK)
		c.Sweep("USDC-GHSC", metrics.SweepOK)
		c.Sweep("USDC-KESC", metrics.SweepFailed)
		c.Upstream(metrics.UpstreamHorizon, time.Second, metrics.UpstreamServerError)
		c.Upstream(metrics.UpstreamFloatRates, time.Second, "")
		c.LiveRequest(metrics.LiveMeasured, time.Second)
		c.LiveRequest(metrics.LiveStale, time.Second)
		return exposition(t, c)
	}
	first := build()
	for i := 0; i < 20; i++ {
		if got := build(); got != first {
			t.Fatalf("run %d differs from run 0:\n%s\n---\n%s", i+1, got, first)
		}
	}
}

// TestConcurrentObservationsLoseNothing: the collector is written from the
// ladder's goroutines and from overlapping HTTP requests at once. Under -race
// this test is also the proof that the mutex covers every map.
func TestConcurrentObservationsLoseNothing(t *testing.T) {
	const goroutines, each = 8, 200
	c := metrics.New()

	var wg sync.WaitGroup
	wg.Add(goroutines * 4)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				c.Sweep("USDC-NGNC", metrics.SweepOK)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				c.Upstream(metrics.UpstreamHorizon, time.Millisecond, metrics.UpstreamRateLimited)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				c.LiveRequest(metrics.LiveMeasured, time.Millisecond)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				c.RateLimitRejection()
			}
		}(i)
	}
	wg.Wait()

	got := exposition(t, c)
	total := uint64(goroutines * each)
	for _, want := range []string{
		fmt.Sprintf(`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} %d`, total),
		fmt.Sprintf(`wayfare_upstream_errors_total{kind="rate_limited",upstream="horizon"} %d`, total),
		fmt.Sprintf(`wayfare_live_request_duration_seconds_count{outcome="measured"} %d`, total),
		fmt.Sprintf("wayfare_rate_limit_rejections_total %d", total),
		fmt.Sprintf(`wayfare_upstream_request_duration_seconds_count{upstream="horizon"} %d`, total),
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("exposition is missing %q — a concurrent observation was lost", want)
		}
	}
}

// TestNilCollectorIsInert: the fields on Server, Scheduler and RateLimiter are
// optional, and "optional" has to mean a nil value costs nothing and panics
// nowhere rather than every call site needing a guard.
func TestNilCollectorIsInert(t *testing.T) {
	var c *metrics.Collector
	c.Sweep("USDC-NGNC", metrics.SweepOK)
	c.Upstream(metrics.UpstreamHorizon, time.Second, metrics.UpstreamTimeout)
	c.LiveRequest(metrics.LiveMeasured, time.Second)
	c.RateLimitRejection()

	// A nil collector also renders nothing rather than failing: /metrics with
	// no metrics configured answers empty text, which a scraper reads as
	// "no data", not as an outage.
	var buf bytes.Buffer
	n, err := c.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo on a nil collector: %v", err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("nil collector wrote %d bytes (%q), want none", n, buf.String())
	}
}

// TestEveryPublishedSeriesIsDocumented binds the exposition to docs/api.md.
//
// A series that ships undocumented is worse than no series: the operator
// builds an alert on a name they guessed, and the label vocabulary — which is
// the whole meaning of a counter — is nowhere to read. This mirrors
// route/wire_scale_test.go's rule that a documented claim and the thing that
// renders it must be checked against each other, not each against its own
// memory.
func TestEveryPublishedSeriesIsDocumented(t *testing.T) {
	c := metrics.New()
	c.Sweep("USDC-NGNC", metrics.SweepOK)
	c.Upstream(metrics.UpstreamHorizon, time.Second, metrics.UpstreamServerError)
	c.LiveRequest(metrics.LiveMeasured, time.Second)
	c.RateLimitRejection()
	got := exposition(t, c)

	documented := documentedSeries(t)
	for _, family := range []string{
		metrics.MetricSweepOutcomes,
		metrics.MetricUpstreamErrors,
		metrics.MetricUpstreamDuration,
		metrics.MetricLiveDuration,
		metrics.MetricRejections,
	} {
		if !strings.Contains(got, family) {
			t.Errorf("collector never published %s; the test fixture no longer exercises it", family)
		}
		if !documented[family] {
			t.Errorf("%s is served at /metrics but docs/api.md never names it", family)
		}
	}
	if len(documented) != 5 {
		t.Errorf("docs/api.md documents %d series names, want the 5 this package publishes: %v",
			len(documented), keysOf(documented))
	}
}

// TestEveryLabelValueIsDocumented checks the vocabularies, not just the names:
// a new outcome or kind that nobody documented is a label value an operator
// cannot decode.
func TestEveryLabelValueIsDocumented(t *testing.T) {
	text := documentation(t)
	for _, want := range []string{
		`"ok"`, `"failed"`, `"unrecorded"`,
		`"rate_limited"`, `"client_error"`, `"server_error"`, `"transport"`, `"timeout"`,
		`"measured"`, `"stale"`,
		`"horizon"`, `"refrate.exchangerate-api"`, `"refrate.currency-api"`,
		`"refrate.floatrates"`, `"checks"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("docs/api.md never names the label value %s", want)
		}
	}
}

// TestDocumentedExpositionIsTheRealOne re-renders the example docs/api.md
// prints and compares bytes.
//
// Without this the example is a transcription, and a transcription rots the
// first time a bucket is retuned. A wrong example in a metrics document is
// worse than no example: an operator writes the alert's label matcher from the
// line they can see, and the HELP text will not save them.
func TestDocumentedExpositionIsTheRealOne(t *testing.T) {
	const marker = "## `GET /metrics`"
	text := documentation(t)
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("docs/api.md has no %s section", marker)
	}
	fence := regexp.MustCompile("(?s)\n```text\n(.*?)\n```\n")
	m := fence.FindStringSubmatch(text[at:])
	if m == nil {
		t.Fatal("the /metrics section documents no ```text block to check")
	}
	documented := m[1] + "\n"

	// The fixture the document describes: two sweeps with different endings,
	// three Horizon exchanges of which two failed differently, one live
	// measurement, and two refused requests.
	c := metrics.New()
	c.Sweep("USDC-NGNC", metrics.SweepOK)
	c.Sweep("USDC-KESC", metrics.SweepFailed)
	c.Upstream(metrics.UpstreamHorizon, 410*time.Millisecond, metrics.UpstreamServerError)
	c.Upstream(metrics.UpstreamHorizon, 95*time.Millisecond, "")
	c.Upstream(metrics.UpstreamHorizon, 20*time.Second, metrics.UpstreamTimeout)
	c.LiveRequest(metrics.LiveMeasured, 1800*time.Millisecond)
	c.RateLimitRejection()
	c.RateLimitRejection()

	if got := exposition(t, c); got != documented {
		t.Errorf("the example in docs/api.md is not what the collector writes.\ngot from code:\n%s\ngot from docs:\n%s", got, documented)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func exposition(t *testing.T, c *metrics.Collector) string {
	t.Helper()
	var buf bytes.Buffer
	n, err := c.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if uint64(n) != uint64(buf.Len()) {
		t.Fatalf("WriteTo reported %d bytes and wrote %d", n, buf.Len())
	}
	return buf.String()
}

// assertParsable runs the format's own grammar over every emitted line. It is
// deliberately a validator rather than a set of substring checks: the checks
// above prove the numbers are right, and this proves the text is something a
// Prometheus server will accept at all.
func assertParsable(t *testing.T, text string) {
	t.Helper()
	// The format's own grammar, from the text-exposition specification: a
	// comment line is "# HELP|TYPE <metric> <rest>", and a sample line is
	// "<metric>[{<name>="<escaped value>"(,<same>)*}] <number>[ <timestamp>]".
	pair := `[a-zA-Z_][a-zA-Z0-9_]*="(\\.|[^"\\])*"`
	sample := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{` +
		pair + `(,` + pair + `)*\})? [-+]?\d+(\.\d+)?([eE][-+]?\d+)?( \d+)?$`)
	comment := regexp.MustCompile(`^# (HELP|TYPE) [a-zA-Z_:][a-zA-Z0-9_:]* .*$`)

	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if line == "" {
			t.Errorf("line %d is empty", i+1)
			continue
		}
		if strings.HasPrefix(line, "#") {
			if !comment.MatchString(line) {
				t.Errorf("line %d is not a valid comment: %q", i+1, line)
			}
			continue
		}
		if !sample.MatchString(line) {
			t.Errorf("line %d is not a valid sample: %q", i+1, line)
		}
	}
}

func countPrefixed(text, prefix string) int {
	var n int
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// documentedSeries reads the metric family names docs/api.md documents, from
// the backticked names in the metrics section's table.
func documentedSeries(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range regexp.MustCompile("`wayfare_[a-z_]+(?:_total|_seconds)`").FindAllString(documentation(t), -1) {
		out[strings.Trim(m, "`")] = true
	}
	if len(out) == 0 {
		t.Fatal("docs/api.md documents no wayfare_ metric series at all")
	}
	return out
}

func documentation(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "api.md"))
	if err != nil {
		t.Fatalf("reading docs/api.md: %v", err)
	}
	return string(raw)
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
