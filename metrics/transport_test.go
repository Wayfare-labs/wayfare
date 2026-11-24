package metrics_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/metrics"
)

// These tests drive a real net/http exchange against httptest servers, because
// the value of the series is entirely in the classification being right: a
// kind label that files a timeout under "transport" sends an operator to the
// wrong remedy, and no amount of unit-testing a pure function would catch
// which error shape net/http actually returns.

func TestTransportClassifiesEveryExchangeShape(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			_, _ = w.Write([]byte("eventually"))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	// A server that has been closed leaves the port refusing connections:
	// the shape of "the upstream is not there at all".
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	goneURL := gone.URL
	gone.Close()

	tests := []struct {
		name    string
		status  int
		url     func(srv *httptest.Server) string
		timeout time.Duration
		ctxKill bool
		// ctxCancel abandons the request from a goroutine, which leaves the
		// context cancelled with no deadline that ever passed.
		ctxCancel bool
		want      string
	}{
		{name: "answered", status: http.StatusOK, want: ""},
		{name: "no content", status: http.StatusNoContent, want: ""},
		{name: "moved", status: http.StatusMovedPermanently, want: ""},
		{name: "bad request", status: http.StatusBadRequest, want: metrics.UpstreamClientError},
		{name: "not found", status: http.StatusNotFound, want: metrics.UpstreamClientError},
		{name: "rate limited", status: http.StatusTooManyRequests, want: metrics.UpstreamRateLimited},
		{name: "server error", status: http.StatusInternalServerError, want: metrics.UpstreamServerError},
		{name: "unavailable", status: http.StatusServiceUnavailable, want: metrics.UpstreamServerError},
		{name: "gateway error", status: http.StatusBadGateway, want: metrics.UpstreamServerError},
		{
			name: "connection refused",
			url:  func(*httptest.Server) string { return goneURL },
			want: metrics.UpstreamTransport,
		},
		{
			name:    "client timeout",
			timeout: 20 * time.Millisecond,
			url:     func(*httptest.Server) string { return slow.URL },
			want:    metrics.UpstreamTimeout,
		},
		{
			name:    "request deadline",
			ctxKill: true,
			url:     func(*httptest.Server) string { return slow.URL },
			want:    metrics.UpstreamTimeout,
		},
		{
			// The reader hung up. There is a cancellation in flight and no
			// deadline that passed, so calling this a timeout would blame
			// the upstream for the client — and on a public endpoint those
			// two events have entirely different remedies.
			name:      "request cancelled",
			ctxCancel: true,
			url:       func(*httptest.Server) string { return slow.URL },
			want:      metrics.UpstreamTransport,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collector := metrics.New()
			target := tc.url
			if target == nil {
				target = func(srv *httptest.Server) string { return srv.URL }
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)
			url := target(srv)

			client := &http.Client{Transport: metrics.NewTransport(
				metrics.UpstreamHorizon, collector, nil)}
			if tc.timeout > 0 {
				client.Timeout = tc.timeout
			}

			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			if tc.ctxKill {
				ctx, cancel := context.WithTimeout(req.Context(), 20*time.Millisecond)
				defer cancel()
				req = req.WithContext(ctx)
			}
			if tc.ctxCancel {
				ctx, cancel := context.WithCancel(req.Context())
				defer cancel()
				req = req.WithContext(ctx)
				go func() {
					time.Sleep(20 * time.Millisecond)
					cancel()
				}()
			}

			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}

			got := errorKind(t, collector, metrics.UpstreamHorizon)
			if got != tc.want {
				t.Errorf("classified kind = %q, want %q (exchange error: %v)", got, tc.want, err)
			}
			// Every shape above is still one exchange: the latency series has
			// to hold the observation even when the request failed, or the
			// error ratio has no denominator on exactly the day it matters.
			if n := observations(t, collector, metrics.UpstreamHorizon); n != 1 {
				t.Errorf("upstream observations = %d, want 1", n)
			}
		})
	}
}

// TestTransportLeavesTheExchangeAlone: the transport must not change what the
// clients see. dex wraps statuses and errors into its own taxonomy (issue
// #121) and refrate into its own (issue #127); if instrumenting the client
// altered either, the metrics would be paid for with the measurement.
func TestTransportLeavesTheExchangeAlone(t *testing.T) {
	const body = `{"bids":[],"asks":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Marker", "kept")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	collector := metrics.New()
	client := &http.Client{Transport: metrics.NewTransport(metrics.UpstreamHorizon, collector, nil)}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q — the transport consumed or rewrote it", got, body)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	if resp.Header.Get("X-Upstream-Marker") != "kept" {
		t.Error("response headers were altered")
	}
	if k := errorKind(t, collector, metrics.UpstreamHorizon); k != metrics.UpstreamClientError {
		t.Errorf("classified kind = %q, want %q", k, metrics.UpstreamClientError)
	}
}

// TestTransportReturnsTheBaseErrorUnchanged: dex's own rate-limit and
// transport error handling depends on the error it receives. Wrapping it here
// would break errors.As checks one package over.
func TestTransportReturnsTheBaseErrorUnchanged(t *testing.T) {
	sentinel := errors.New("base transport refused")
	collector := metrics.New()
	tr := metrics.NewTransport(metrics.UpstreamHorizon, collector, failingRoundTripper{err: sentinel})

	resp, err := tr.RoundTrip(mustGet(t, "https://horizon.example/paths/strict-send"))
	if err != nil {
		// errors.Is reaches the sentinel through whatever wrapping net/http
		// adds above this transport, and the collector still saw a failure.
		if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), sentinel.Error()) {
			t.Errorf("RoundTrip error = %v, want it to carry %v", err, sentinel)
		}
	} else {
		resp.Body.Close()
		t.Error("RoundTrip swallowed the base error")
	}
	if k := errorKind(t, collector, metrics.UpstreamHorizon); k != metrics.UpstreamTransport {
		t.Errorf("classified kind = %q, want %q", k, metrics.UpstreamTransport)
	}
}

// TestTransportCountsEachRedirectHop: http.Client resolves redirects itself,
// so a 302 that lands on a 500 is two exchanges and one failure. The docs say
// "per exchange" and this is what that means.
func TestTransportCountsEachRedirectHop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/final":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.Redirect(w, r, "/final", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)

	collector := metrics.New()
	client := &http.Client{Transport: metrics.NewTransport(metrics.UpstreamHorizon, collector, nil)}
	resp, err := client.Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if n := observations(t, collector, metrics.UpstreamHorizon); n != 2 {
		t.Errorf("upstream observations = %d, want 2 (the redirect hop and the final one)", n)
	}
	if k := errorKind(t, collector, metrics.UpstreamHorizon); k != metrics.UpstreamServerError {
		t.Errorf("classified kind = %q, want %q", k, metrics.UpstreamServerError)
	}
}

// TestTransportWithoutCollectorIsAPassThrough: the wiring must be able to
// leave metrics out — cmd/ladder and the snapshot replay paths build their own
// clients — without changing what those clients do.
func TestTransportWithoutCollectorIsAPassThrough(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	tr := &metrics.Transport{Upstream: metrics.UpstreamHorizon}
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", resp.StatusCode)
	}
	if hits != 1 {
		t.Errorf("upstream saw %d requests, want 1", hits)
	}
	// A nil collector receives nothing and, more importantly, never panics.
	tr.Collector.RateLimitRejection()
}

// TestTransportIsSafeUnderConcurrentLadders: Engine prices four rungs at a
// time and the server overlaps requests, so one transport instance is
// necessarily shared.
func TestTransportIsSafeUnderConcurrentLadders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	collector := metrics.New()
	client := &http.Client{Transport: metrics.NewTransport(metrics.UpstreamChecks, collector, nil)}

	const requests = 64
	var wg sync.WaitGroup
	wg.Add(requests)
	for i := 0; i < requests; i++ {
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Errorf("GET: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	if n := observations(t, collector, metrics.UpstreamChecks); n != requests {
		t.Errorf("upstream observations = %d, want %d", n, requests)
	}
	if got := sampleValue(t, exposition(t, collector),
		`wayfare_upstream_errors_total{kind="server_error",upstream="checks"}`); got != requests {
		t.Errorf("server_error counter = %d, want %d", got, requests)
	}
}

func TestUpstreamNamesAreTheWiredOnes(t *testing.T) {
	collector := metrics.New()
	tr := metrics.NewTransport(metrics.UpstreamExchangeRateAPI, collector, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	// The label is the one the process declared, not a host name scraped off
	// the URL: an operator's -horizon override must not publish an arbitrary
	// value as a metric label.
	if got := sampleValue(t, exposition(t, collector),
		`wayfare_upstream_errors_total{kind="rate_limited",upstream="refrate.exchangerate-api"}`); got != 1 {
		t.Errorf("counter = %d, want 1\n%s", got, exposition(t, collector))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type failingRoundTripper struct{ err error }

func (f failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func mustGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	return req
}

// errorKind reports the single error kind observed for upstream, or "" when
// nothing failed. It fails the test if more than one kind was recorded, since
// every caller here performs exactly one exchange.
func errorKind(t *testing.T, c *metrics.Collector, upstream string) string {
	t.Helper()
	const prefix = `wayfare_upstream_errors_total{kind="`
	target := `upstream="` + upstream + `"`

	var found []string
	for key := range samples(t, c) {
		if !strings.HasPrefix(key, prefix) || !strings.Contains(key, target) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		kind, _, _ := strings.Cut(rest, `"`)
		found = append(found, kind)
	}
	switch len(found) {
	case 0:
		return ""
	case 1:
		return found[0]
	default:
		t.Fatalf("upstream %s recorded %d error kinds %v, want at most one", upstream, len(found), found)
		return ""
	}
}

// observations reads the histogram count for one upstream. A missing series
// reads as 0 rather than passing silently, which is what makes "nothing was
// counted" a failure this test can see.
func observations(t *testing.T, c *metrics.Collector, upstream string) int {
	t.Helper()
	key := `wayfare_upstream_request_duration_seconds_count{upstream="` + upstream + `"}`
	return int(sampleValue(t, exposition(t, c), key))
}

// samples parses the exposition into "metric{labels}" -> numeric sample value.
func samples(t *testing.T, c *metrics.Collector) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, line := range strings.Split(exposition(t, c), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("unparsable exposition line %q", line)
		}
		v, err := strconv.ParseUint(fields[len(fields)-1], 10, 64)
		if err != nil {
			continue // a _sum sample may be fractional
		}
		out[fields[0]] = v
	}
	return out
}

func sampleValue(t *testing.T, text, key string) uint64 {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, key+" ") {
			fields := strings.Fields(line)
			v, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				t.Fatalf("sample %q is not a count: %v", line, err)
			}
			return v
		}
	}
	return 0
}
