package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/metrics"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// Issue #705: the counters a scraper reads at /metrics. The tests here are the
// handler's half of that contract — what the endpoint promises a scraper about
// its shape, and that the ways a live request can end are each counted exactly
// once. The arithmetic of the exposition itself belongs to metrics_test.go.

// metricsServer wires a Server with the given collector over a horizon chosen
// by the caller. timeout is the -timeout ceiling; zero leaves the server on its
// default. store may be nil, which is the "no history configured" shape.
func metricsServer(t *testing.T, collector *metrics.Collector, horizonURL string, timeout time.Duration, store runstore.Store) *httptest.Server {
	t.Helper()

	s := &Server{
		Engine:  newEngine(horizonURL, "1500"),
		Store:   store,
		Timeout: timeout,
		Metrics: collector,
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

// closedHorizonURL returns a URL no request can reach: the listener was closed
// the moment it opened, which is the shape of an upstream outage.
func closedHorizonURL(t *testing.T) string {
	t.Helper()
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close()
	return url
}

// pricedHorizonURL answers every request with a priced NGNC corridor, so a
// live measurement through it succeeds.
func pricedHorizonURL(t *testing.T) string {
	t.Helper()
	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(liveNGNCPaths))
	}))
	t.Cleanup(horizon.Close)
	return horizon.URL
}

// slowHorizonURL accepts requests and never answers them, so the only thing
// that ends a measurement is a deadline.
func slowHorizonURL(t *testing.T) string {
	t.Helper()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)
	return slow.URL
}

// scrape reads /metrics from an API under test.
func scrape(t *testing.T, api *httptest.Server) string {
	t.Helper()
	resp, err := httpGet(api.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading /metrics: %v", err)
	}
	return string(body)
}

// waitForToken pauses until a limiter at the given rate has refilled one
// token. It exists because /metrics shares that bucket with every other route:
// a scrape refused with 429 is a real rejection and lands in the counter, so a
// test that polled its way to a 200 would be reading a number it had itself
// raised.
func waitForToken(rate float64) {
	time.Sleep(time.Duration(float64(time.Second)/rate) + 100*time.Millisecond)
}

// TestMetricsServesEveryFamilyWithNothingObserved pins the answer a scraper
// gets from a process that has recorded nothing: valid text, all five families
// declared, and exactly one sample line.
//
// That sample is the label-free rejection counter, which the metrics package
// documents as the one exception to "absence is not zero". A server built
// without a collector must answer this way rather than 404: a wrong port and a
// process that has observed nothing are different problems, and only one of
// them is a missing endpoint.
func TestMetricsServesEveryFamilyWithNothingObserved(t *testing.T) {
	api := metricsServer(t, nil, closedHorizonURL(t), 0, nil)
	text := scrape(t, api)

	for _, family := range []string{
		metrics.MetricLiveDuration,
		metrics.MetricRejections,
		metrics.MetricSweepOutcomes,
		metrics.MetricUpstreamErrors,
		metrics.MetricUpstreamDuration,
	} {
		if !strings.Contains(text, "# HELP "+family+" ") {
			t.Errorf("exposition is missing the HELP line for %s", family)
		}
		if !strings.Contains(text, "# TYPE "+family+" ") {
			t.Errorf("exposition is missing the TYPE line for %s", family)
		}
	}

	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line != "wayfare_rate_limit_rejections_total 0" {
			t.Errorf("a process that observed nothing emitted the sample %q", line)
		}
	}
}

// TestMetricsCarriesTheScrapersHeaders checks the three headers a scrape
// depends on. The version parameter is part of the exposition format: a server
// that cannot parse the body has to know which grammar it was served. And
// no-store is the difference between a stale reading and a wrong one — the
// counters only go up, so a cached body reports a process that has stopped
// being scraped as a process that has stopped seeing failures.
func TestMetricsCarriesTheScrapersHeaders(t *testing.T) {
	api := metricsServer(t, metrics.New(), closedHorizonURL(t), 0, nil)

	resp, err := httpGet(api.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	for _, want := range [][2]string{
		{"Content-Type", "text/plain; version=0.0.4; charset=utf-8"},
		{"X-Content-Type-Options", "nosniff"},
		{"Cache-Control", "no-store"},
	} {
		if got := resp.Header.Get(want[0]); got != want[1] {
			t.Errorf("%s = %q, want %q", want[0], got, want[1])
		}
	}
}

// TestMetricsHeadMatchesGet extends the issue #612 HEAD contract to the
// exposition: a monitor may probe with HEAD, and the answer must be this
// document's headers rather than a 405.
//
// Both requests are sent without Accept-Encoding. The document is larger than
// server.minGzipSize, so a gzip-accepting GET is chunked while Go answers the
// HEAD with the uncompressed length, and the comparison would be about the
// transfer encoding instead of the exposition.
func TestMetricsHeadMatchesGet(t *testing.T) {
	api := metricsServer(t, metrics.New(), closedHorizonURL(t), 0, nil)
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	getReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, api.URL+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	getBody, err := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if err != nil {
		t.Fatalf("reading GET /metrics: %v", err)
	}

	headReq, err := http.NewRequestWithContext(context.Background(), http.MethodHead, api.URL+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	headResp, err := client.Do(headReq)
	if err != nil {
		t.Fatalf("HEAD /metrics: %v", err)
	}
	headBody, err := io.ReadAll(headResp.Body)
	headResp.Body.Close()
	if err != nil {
		t.Fatalf("reading HEAD /metrics: %v", err)
	}

	if headResp.StatusCode != getResp.StatusCode {
		t.Errorf("HEAD status = %d, want GET status %d", headResp.StatusCode, getResp.StatusCode)
	}
	if len(headBody) != 0 {
		t.Errorf("HEAD returned a %d-byte body; HEAD must carry no body", len(headBody))
	}
	compareHeaders(t, "/metrics", getResp.Header, headResp.Header)

	// A probe must not spend anything. The counters are read, not drained, so
	// the GET after a HEAD says exactly what the GET before it said.
	if got := scrape(t, api); got != string(getBody) {
		t.Errorf("GET after HEAD differs from GET before HEAD:\n%s\n---\n%s", getBody, got)
	}
}

// TestMetricsRejectsMethodsThatWrite: the exposition is read-only, and a write
// verb is answered with the same error shape as every other route.
func TestMetricsRejectsMethodsThatWrite(t *testing.T) {
	api := metricsServer(t, metrics.New(), closedHorizonURL(t), 0, nil)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			code, status := errorCode(t, method, api.URL+"/metrics")
			if code != codeMethodNotAllowed {
				t.Errorf("%s /metrics code = %q, want %q", method, code, codeMethodNotAllowed)
			}
			if status != http.StatusMethodNotAllowed {
				t.Errorf("%s /metrics status = %d, want 405", method, status)
			}
		})
	}
}

// TestEveryLiveOutcomeIsCountedOnce is the heart of #705 for the server: each
// way a request that asked for a measurement can end lands in exactly one
// series, and lands there once.
//
// The count is read back through /metrics rather than from a getter because
// what an operator alerts on is the document this endpoint serves: a getter
// could be right while the exposition is wrong.
func TestEveryLiveOutcomeIsCountedOnce(t *testing.T) {
	tests := []struct {
		name    string
		horizon func(t *testing.T) string
		timeout time.Duration
		history bool
		// wantOutcome is the label the request must produce; wantStatus is the
		// status the request itself gets, so a counting bug cannot hide behind
		// a response that never reached the branch under test.
		wantOutcome string
		wantStatus  int
	}{
		{
			name:        "measured",
			horizon:     pricedHorizonURL,
			wantOutcome: metrics.LiveMeasured,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "stale",
			horizon:     closedHorizonURL,
			history:     true,
			wantOutcome: metrics.LiveStale,
			wantStatus:  http.StatusOK,
		},
		{
			// The budget is the server's own -timeout, so a ceiling below the
			// time an unanswered Horizon takes is a deterministic timeout.
			name:        "timeout",
			horizon:     slowHorizonURL,
			timeout:     50 * time.Millisecond,
			wantOutcome: metrics.LiveTimeout,
			wantStatus:  http.StatusGatewayTimeout,
		},
		{
			name:        "error",
			horizon:     closedHorizonURL,
			wantOutcome: metrics.LiveError,
			wantStatus:  http.StatusBadGateway,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collector := metrics.New()
			var store runstore.Store
			if tc.history {
				store = storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
			}
			api := metricsServer(t, collector, tc.horizon(t), tc.timeout, store)

			resp, err := httpGet(api.URL + "/api/corridor?to=NGNC&sizes=100")
			if err != nil {
				t.Fatalf("GET /api/corridor: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.StatusCode, tc.wantStatus, body)
			}

			text := scrape(t, api)
			sample := `wayfare_live_request_duration_seconds_count{outcome="` + tc.wantOutcome + `"}`
			if got := countLinesWithPrefix(text, sample+" "); got != 1 {
				t.Fatalf("%s appears on %d lines, want exactly 1:\n%s", sample, got, text)
			}
			if !strings.Contains(text, sample+" 1") {
				t.Errorf("%s is not 1 after one request that got %s:\n%s",
					sample, tc.wantOutcome, text)
			}

			// One request, one outcome: the other series must be absent rather
			// than present and zero, which is how this project reports
			// "did not happen" everywhere else.
			for _, other := range []string{
				metrics.LiveMeasured, metrics.LiveStale, metrics.LiveTimeout, metrics.LiveError,
			} {
				if other == tc.wantOutcome {
					continue
				}
				if strings.Contains(text, `outcome="`+other+`"`) {
					t.Errorf("one %s request also produced a %s series", tc.wantOutcome, other)
				}
			}
		})
	}
}

// TestHistoryServedReadIsNotALiveRequest is the negative half of the same
// boundary: a read answered from the store asked for no measurement, so
// counting it would dilute the series an operator watches.
func TestHistoryServedReadIsNotALiveRequest(t *testing.T) {
	store := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	s := &Server{
		Engine:       newEngine(closedHorizonURL(t), "1500"),
		Store:        store,
		HistoryFirst: true,
		Metrics:      metrics.New(),
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)

	resp, err := httpGet(api.URL + "/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatalf("GET /api/corridor: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 from a history-first read: %s", resp.StatusCode, body)
	}

	if text := scrape(t, api); strings.Contains(text, "wayfare_live_request_duration_seconds_") {
		t.Errorf("a history-first read was counted as a live request:\n%s", text)
	}
}

// TestMetricsCountsRateLimitRejections ties the rejection counter to the
// behaviour it counts: the number rises exactly when clients are refused, and
// requests that were allowed cost nothing.
//
// The limiter here is given no collector of its own, which is how cmd/wayfared
// builds it: the point of the test is that Server.Handler joins the two, so a
// deployment cannot ship a limiter whose refusals go uncounted.
func TestMetricsCountsRateLimitRejections(t *testing.T) {
	const rate = 1.0
	s := &Server{
		Engine:  newEngine(pricedHorizonURL(t), "1500"),
		Metrics: metrics.New(),
		// One token and a slow refill: the first hit is allowed and the next
		// two are refused.
		Limiter: NewRateLimiter(rate, 1),
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)

	for i := 0; i < 3; i++ {
		resp, err := httpGet(api.URL + "/healthz")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	waitForToken(rate)
	line := ""
	for _, l := range strings.Split(scrape(t, api), "\n") {
		if strings.HasPrefix(l, "wayfare_rate_limit_rejections_total ") {
			if line != "" {
				t.Fatalf("rejection counter emitted a second line: %q", l)
			}
			line = l
		}
	}
	if want := "wayfare_rate_limit_rejections_total 2"; line != want {
		t.Errorf("got %q, want %q (three requests, one token)", line, want)
	}
}

// TestSweepCountersReachTheServer is the wiring check for the deployed shape:
// the collector the server answers with is the same one the scheduler writes
// into, so an operator needs one endpoint rather than one per subsystem.
func TestSweepCountersReachTheServer(t *testing.T) {
	collector := metrics.New()
	collector.Sweep("USDC-NGNC", metrics.SweepOK)
	collector.Sweep("USDC-NGNC", metrics.SweepUnrecorded)

	api := metricsServer(t, collector, pricedHorizonURL(t), 0, nil)

	text := scrape(t, api)
	for _, want := range []string{
		`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} 1`,
		`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="unrecorded"} 1`,
	} {
		if !strings.Contains(text, want+"\n") {
			t.Errorf("exposition is missing %q:\n%s", want, text)
		}
	}
}

// TestClientHangupIsNotCountedAsATimeout keeps the two apart where it matters:
// a reader that disconnects mid-measurement ends the request context, and
// counting that as a measurement timeout would send an operator to the
// upstream that never caused it.
//
// The request must be in flight when the cancel lands, and the handler is still
// finishing its observation when the client gives up, so the exposition is
// polled rather than read once.
func TestClientHangupIsNotCountedAsATimeout(t *testing.T) {
	api := metricsServer(t, metrics.New(), slowHorizonURL(t), 0, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the request completed although its context was cancelled")
	}

	text := ""
	deadline := time.Now().Add(2 * time.Second)
	for {
		text = scrape(t, api)
		if strings.Contains(text, `outcome="error"`) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(text, `outcome="error"`) {
		t.Fatalf("the abandoned request was never recorded:\n%s", text)
	}
	if strings.Contains(text, `outcome="timeout"`) {
		t.Errorf("a cancelled request was counted as a measurement timeout:\n%s", text)
	}
}

func countLinesWithPrefix(text, prefix string) int {
	var n int
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}
