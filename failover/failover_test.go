package failover_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/failover"
)

// Every test here counts the requests each host actually received, because most
// of what this package promises is about a host that stays silent: "never fail
// over on a 404" is invisible in the response a caller gets, and only a counter
// proves the second Horizon was never dialed.

// TestMain discards the default logger. Every failover in these tests would
// otherwise print a switch line to stderr, and the two tests that assert on the
// log install their own handler on the transport.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestNewValidatesTheFailoverAddress(t *testing.T) {
	tests := []struct {
		name string
		url  string
		// wantErr is the wording an operator reads at startup, so it is
		// asserted rather than merely "some error".
		wantErr string
	}{
		{name: "plain host", url: "https://horizon.example.org"},
		{name: "with a port", url: "http://127.0.0.1:8000"},
		{name: "trailing slash", url: "https://horizon.example.org/"},
		{
			name:    "a path",
			url:     "https://horizon.example.org/paths/strict-send",
			wantErr: "bare origin without a path",
		},
		{
			name:    "not http",
			url:     "grpc://horizon.example.org:8000",
			wantErr: "must be http or https",
		},
		{
			name:    "no scheme",
			url:     "horizon.example.org",
			wantErr: "must be http or https",
		},
		{
			name:    "no host",
			url:     "https://",
			wantErr: "must name a host",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := failover.New("https://horizon.example.org", tc.url, nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("New(%q) = %v, want it accepted", tc.url, err)
				}
				if tr.Secondary != tc.url {
					t.Errorf("Secondary = %q, want the configured value %q kept for reporting", tr.Secondary, tc.url)
				}
				return
			}
			if err == nil {
				t.Fatalf("New(%q) accepted a bad address", tc.url)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("New(%q) error = %v, want it to say %q", tc.url, err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.url) {
				t.Errorf("New(%q) error = %v, want it to name the offending value", tc.url, err)
			}
		})
	}
}

// TestNewValidatesThePrimaryAddress: the primary is only compared against, never
// dialled from here, which makes a mistyped one exactly as dangerous as a
// mistyped failover address — it switches the failover off, silently.
func TestNewValidatesThePrimaryAddress(t *testing.T) {
	for _, bad := range []string{"", "horizon.example.org", "https://horizon.example.org/api"} {
		tr, err := failover.New(bad, "https://secondary.example", nil)
		if err == nil {
			t.Errorf("New(%q, …) accepted a primary that can never match a request URL", bad)
			continue
		}
		if tr != nil {
			t.Errorf("New(%q, …) returned a transport alongside the error, want nil", bad)
		}
		if !strings.Contains(err.Error(), "primary Horizon") {
			t.Errorf("New(%q, …) error = %v, want it to name which of the two addresses is wrong", bad, err)
		}
	}
}

// TestEmptySecondaryDisablesFailover: the flag is optional, and an operator who
// runs one Horizon must not get a second one's behaviour by accident.
func TestEmptySecondaryDisablesFailover(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusServiceUnavailable))
	_, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, "", nil)}
	ctx, pin := failover.WithPin(context.Background())

	body := get(t, client, ctx, primary.URL)
	if body != "" {
		t.Errorf("body = %q, want the primary's empty answer kept", body)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests with failover disabled, want 0", got)
	}
	// Nothing was ever asked of a failover transport, so there is no second
	// host to report and the manifest must not claim one.
	if got := pin.Host(); got != "" {
		t.Errorf("Host() = %q with no secondary configured, want empty", got)
	}
}

// TestZeroValueIsInert: the struct is usable without New, so a client built in a
// test fixture or an ad-hoc script gets a plain transport rather than a panic.
func TestZeroValueIsInert(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusServiceUnavailable))
	_, secondaryHits := stubHost(t, answer("from the failover host"))

	tr := &failover.Transport{}
	ctx, pin := failover.WithPin(context.Background())

	resp, err := tr.RoundTrip(mustGet(t, primary.URL, ctx))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	status := resp.StatusCode
	resp.Body.Close()

	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want the primary's answer unchanged", status)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("secondary saw %d requests, want 0", got)
	}
	if got := pin.Host(); got != "" {
		t.Errorf("Host() = %q, want empty for a transport with no secondary", got)
	}
}

// TestWithoutAPinThePrimaryAloneAnswers: an unwired call site is a bug, and the
// safe failure is no redundancy — never a ladder split across two providers.
func TestWithoutAPinThePrimaryAloneAnswers(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusInternalServerError))
	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}

	resp, err := client.Get(primary.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	status := resp.StatusCode
	resp.Body.Close()

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want the primary's 500 served to an unscoped request", status)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests for a pin-less request, want 0", got)
	}
}

// TestOnlyThePrimaryHostsTrafficMoves is the shared-recorder rule. In a recorded
// run one transport sits beneath every upstream, so it sees the reference-rate
// and counterparty-check requests too; those must reach the host they were aimed
// at, and must not settle the ladder's host choice either.
func TestOnlyThePrimaryHostsTrafficMoves(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusServiceUnavailable))
	secondary, secondaryHits := stubHost(t, answer("from the failover host"))
	feed, feedHits := stubHost(t, answer("the reference rate"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())

	if body := get(t, client, ctx, feed.URL+"/latest"); body != "the reference rate" {
		t.Errorf("body = %q, want the feed's own answer", body)
	}
	if got := feedHits.Load(); got != 1 {
		t.Errorf("reference feed saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests aimed at another origin, want 0", got)
	}
	// Leaving the decision unclaimed is the other half of the promise: a feed
	// that answers first would otherwise decide which Horizon the rungs go to.
	if got := pin.Host(); got != "" {
		t.Errorf("Host() = %q after a foreign-origin request, want no decision claimed", got)
	}

	// The same transport on the same context still fails over for its own host.
	if body := get(t, client, ctx, primary.URL); body != "from the failover host" {
		t.Errorf("body = %q, want the Horizon request to fail over as usual", body)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := pin.Host(); got != secondary.URL {
		t.Errorf("Host() = %q, want the failover host %q", got, secondary.URL)
	}
}

// TestPrimaryComparisonIgnoresCase: host names are case-insensitive, and a
// configured address that differed from the client's only in spelling would
// disable failover in a way nobody notices until the first outage.
func TestPrimaryComparisonIgnoresCase(t *testing.T) {
	spy := &spyRoundTripper{
		statusOf: map[string]int{
			"primary.example":   http.StatusServiceUnavailable,
			"secondary.example": http.StatusOK,
		},
	}
	tr, err := failover.New("https://PRIMARY.example", "https://secondary.example", spy)
	if err != nil {
		t.Fatalf("failover.New: %v", err)
	}

	ctx, pin := failover.WithPin(context.Background())
	resp, err := tr.RoundTrip(mustGet(t, "https://primary.example/order_book", ctx))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	if got := strings.Join(spy.hosts(), " "); got != "primary.example secondary.example" {
		t.Errorf("base transport dialled %q, want the differently-spelled primary to be recognised", got)
	}
	if !pin.FailedOver() {
		t.Error("FailedOver() = false for a primary that only differed in case")
	}
}

// TestOutageFailsOver covers the shapes that mean "this Horizon is down": a 5xx
// of any kind, and no HTTP response at all.
func TestOutageFailsOver(t *testing.T) {
	refused := func(t *testing.T) string {
		t.Helper()
		// A closed server leaves the port refusing connections, which is the
		// upstream being absent rather than slow.
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		return url
	}

	tests := []struct {
		name    string
		primary func(t *testing.T) string
	}{
		{name: "server error", primary: failing(http.StatusInternalServerError)},
		{name: "bad gateway", primary: failing(http.StatusBadGateway)},
		{name: "service unavailable", primary: failing(http.StatusServiceUnavailable)},
		{name: "gateway timeout", primary: failing(http.StatusGatewayTimeout)},
		{name: "connection refused", primary: refused},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primaryURL := tc.primary(t)
			secondary, secondaryHits := stubHost(t, answer("from the failover host"))

			client := &http.Client{Transport: mustTransport(t, primaryURL, secondary.URL, nil)}
			ctx, pin := failover.WithPin(context.Background())

			body := get(t, client, ctx, primaryURL)
			if body != "from the failover host" {
				t.Errorf("body = %q, want the failover host's answer", body)
			}
			if got := secondaryHits.Load(); got != 1 {
				t.Errorf("failover host saw %d requests, want 1", got)
			}
			if got := pin.Host(); got != secondary.URL {
				t.Errorf("Host() = %q, want the failover host %q", got, secondary.URL)
			}
			if !pin.FailedOver() {
				t.Error("FailedOver() = false on a ladder the failover host answered")
			}
		})
	}
}

// TestNeverFailsOverOnAClientAnswer is the acceptance rule: a 4xx is this
// request being wrong, which every Horizon agrees on, and re-asking somebody
// else's service turns a local bug into their load. 429 belongs to the refusal
// for the same reason — the quota is the cause, and a second host is only a way
// to ignore it.
func TestNeverFailsOverOnAClientAnswer(t *testing.T) {
	statuses := []int{
		http.StatusOK,
		http.StatusMovedPermanently,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusTeapot,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			primary, primaryHits := stubHost(t, statusBody(status, "from the primary"))
			secondary, secondaryHits := stubHost(t, answer("from the failover host"))

			client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
			ctx, pin := failover.WithPin(context.Background())

			resp, err := client.Do(mustGet(t, primary.URL, ctx))
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			got := resp.StatusCode
			body := readAll(t, resp.Body)
			resp.Body.Close()

			if got != status {
				t.Errorf("status = %d, want the primary's %d preserved", got, status)
			}
			// A redirect is resolved above this transport by http.Client, which
			// re-asks the same host, so only the non-redirect shapes carry the
			// primary's body back to here.
			if status < 300 && body != "from the primary" {
				t.Errorf("body = %q, want the primary's answer", body)
			}
			if n := primaryHits.Load(); n != 1 {
				t.Errorf("primary saw %d requests, want 1", n)
			}
			if n := secondaryHits.Load(); n != 0 {
				t.Errorf("failover host saw %d requests after a %d, want 0", n, status)
			}
			if host := pin.Host(); host != primary.URL {
				t.Errorf("Host() = %q, want the primary %q even for a refusal", host, primary.URL)
			}
			if pin.FailedOver() {
				t.Errorf("FailedOver() = true after the primary answered a %d itself", status)
			}
		})
	}
}

// TestDeadlineWinsOverFailover: a ladder that ran out of time is a timeout, and
// spending more time against a second host would report an outage as slowness.
func TestDeadlineWinsOverFailover(t *testing.T) {
	primary, primaryHits := stubHost(t, slow())
	secondary, secondaryHits := stubHost(t, answer("too late"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	pinned, pin := failover.WithPin(ctx)

	_, err := client.Do(mustGet(t, primary.URL, pinned))
	if err == nil {
		t.Fatal("the expired context did not fail the exchange")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("GET error = %v, want a deadline failure", err)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests after the deadline, want 0", got)
	}
	// The ladder resolved to the host it was asking, so the record of this run
	// says which Horizon it tried rather than nothing.
	if got := pin.Host(); got != primary.URL {
		t.Errorf("Host() = %q, want the primary %q", got, primary.URL)
	}
}

// TestCancellationWinsOverFailover is the same rule for a reader who hangs up:
// the second host would be dialled for a request nobody wants any more.
func TestCancellationWinsOverFailover(t *testing.T) {
	primary, _ := stubHost(t, slow())
	secondary, secondaryHits := stubHost(t, answer("nobody asked"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pinned, _ := failover.WithPin(ctx)

	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	resp, err := client.Do(mustGet(t, primary.URL, pinned))
	if err == nil {
		resp.Body.Close()
		t.Fatal("the cancelled context did not fail the exchange")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GET error = %v, want a cancellation", err)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests after cancellation, want 0", got)
	}
}

// TestLadderPinsOneHostForItsWholeLife is the mixing rule from one side: once a
// ladder is on a host it stays there, even when the primary is healthy again and
// would answer better.
func TestLadderPinsOneHostForItsWholeLife(t *testing.T) {
	var primaryHits atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if primaryHits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "from the primary")
	}))
	t.Cleanup(primary.Close)

	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())

	for i := 1; i <= 3; i++ {
		if body := get(t, client, ctx, primary.URL); body != "from the failover host" {
			t.Fatalf("request %d body = %q, want every rung on the failover host", i, body)
		}
	}

	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want only the one that settled the decision", got)
	}
	if got := secondaryHits.Load(); got != 3 {
		t.Errorf("failover host saw %d requests, want 3", got)
	}
	if got := pin.Host(); got != secondary.URL {
		t.Errorf("Host() = %q, want the failover host %q", got, secondary.URL)
	}
}

// TestHealthyPrimaryIsNotAbandonedMidLadder names the cost of the pinning rule
// so it stays a decision rather than a surprise: a host that breaks partway
// through a ladder keeps the ladder, and the later rungs fail with it. The
// alternative — switching mid-curve — would compare sizes priced by two
// providers and report the difference as a price curve.
func TestHealthyPrimaryIsNotAbandonedMidLadder(t *testing.T) {
	var primaryHits atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if primaryHits.Add(1) > 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "from the primary")
	}))
	t.Cleanup(primary.Close)

	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())

	if body := get(t, client, ctx, primary.URL); body != "from the primary" {
		t.Fatalf("first rung body = %q, want the primary", body)
	}
	resp, err := client.Do(mustGet(t, primary.URL, ctx))
	if err != nil {
		t.Fatalf("second rung: %v", err)
	}
	status := resp.StatusCode
	resp.Body.Close()

	if status != http.StatusServiceUnavailable {
		t.Errorf("second rung status = %d, want the pinned primary's 503", status)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests mid-ladder, want 0 — that is a mixed ladder", got)
	}
	if got := pin.Host(); got != primary.URL {
		t.Errorf("Host() = %q, want the primary %q", got, primary.URL)
	}
	// The record of this ladder must name the host it actually asked, which is
	// not the one whose outage it suffered.
	if pin.FailedOver() {
		t.Error("FailedOver() = true for a ladder that stayed on the primary")
	}
}

// TestConcurrentRungsShareOneDecision is the ladder as the engine builds it:
// several rungs in flight at once against one host. Only the first may touch the
// primary, because a decision made while the others are already in flight is the
// mixing this package exists to prevent.
func TestConcurrentRungsShareOneDecision(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusGatewayTimeout))
	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())

	const rungs = 12
	var wg sync.WaitGroup
	bodies := make([]string, rungs)
	errs := make([]error, rungs)
	wg.Add(rungs)
	for i := 0; i < rungs; i++ {
		go func(i int) {
			defer wg.Done()
			bodies[i], errs[i] = tryGet(client, ctx, primary.URL)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("rung %d: %v", i, err)
		}
		if bodies[i] != "from the failover host" {
			t.Errorf("rung %d body = %q, want every concurrent rung on the failover host", i, bodies[i])
		}
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1 (the exchange that settled the decision)", got)
	}
	if got := secondaryHits.Load(); got != rungs {
		t.Errorf("failover host saw %d requests, want %d", got, rungs)
	}
	if got := pin.Host(); got != secondary.URL {
		t.Errorf("Host() = %q, want the failover host", got)
	}
}

// TestWaiterGivesUpWithItsContext: a primary that never answers must cost one
// stalled exchange per ladder, not one per rung.
func TestWaiterGivesUpWithItsContext(t *testing.T) {
	var primaryHits atomic.Int64
	release := make(chan struct{})
	var unblock sync.Once
	done := func() { unblock.Do(func() { close(release) }) }

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(func() {
		done()
		primary.Close()
	})

	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pinned, pin := failover.WithPin(ctx)

	deciderDone := make(chan struct{})
	go func() {
		defer close(deciderDone)
		resp, err := client.Do(mustGet(t, primary.URL, pinned))
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait until the decider is provably inside the primary's handler, so the
	// second request is a waiter rather than a racing decider.
	for primaryHits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	waiterCtx, waitCancel := context.WithTimeout(pinned, 50*time.Millisecond)
	defer waitCancel()
	if _, err := client.Do(mustGet(t, primary.URL, waiterCtx)); err == nil {
		t.Fatal("the waiter returned an answer while the decision was unresolved")
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests before the primary was known dead, want 0", got)
	}

	done()
	<-deciderDone
	if got := pin.Host(); got != secondary.URL {
		t.Errorf("Host() = %q once the decider settled, want the failover host %q", got, secondary.URL)
	}
}

// TestRequestBodyIsNotReissued: a body has one reader, so a second exchange
// would send an empty or truncated payload to a host that cannot tell it was
// meant to. The primary's answer is the only honest thing left to return.
func TestRequestBodyIsNotReissued(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusServiceUnavailable))
	secondary, secondaryHits := stubHost(t, answer("from the failover host"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())
	req, err := http.NewRequest(http.MethodPost, primary.URL, strings.NewReader("a body"))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	status := resp.StatusCode
	resp.Body.Close()

	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want the primary's answer rather than a transport error", status)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 0 {
		t.Errorf("failover host saw %d requests carrying a replayed body, want 0", got)
	}
	if got := pin.Host(); got != primary.URL {
		t.Errorf("Host() = %q, want the primary %q", got, primary.URL)
	}
}

// TestRequestIsNotModified is a RoundTripper contract, not a preference:
// http.Client may keep using the request after the transport returns, and a
// rewritten URL would silently retarget whatever it does next.
func TestRequestIsNotModified(t *testing.T) {
	primary, _ := stubHost(t, always(http.StatusInternalServerError))
	secondary, _ := stubHost(t, answer("from the failover host"))

	tr := mustTransport(t, primary.URL, secondary.URL, nil)
	ctx, _ := failover.WithPin(context.Background())
	req, err := http.NewRequest(http.MethodGet, primary.URL+"/paths/strict-send?destination_asset=NGNC", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	wantURL := req.URL.String()

	resp, err := tr.RoundTrip(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	body := readAll(t, resp.Body)
	resp.Body.Close()

	if body != "from the failover host" {
		t.Fatalf("body = %q, want the failover answer", body)
	}
	if got := req.URL.String(); got != wantURL {
		t.Errorf("caller's request URL = %q, want it unchanged from %q", got, wantURL)
	}
}

// TestQueryAndHostReachTheFailoverHost: rewriting the origin must carry the
// whole request. A Horizon query that arrived without its parameters would be a
// different question, and the ladder would be priced on a bad answer.
func TestQueryAndHostReachTheFailoverHost(t *testing.T) {
	primary, _ := stubHost(t, always(http.StatusServiceUnavailable))
	var seen struct {
		path, query, host, method string
	}
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.query = r.URL.RawQuery
		seen.host = r.Host
		seen.method = r.Method
		_, _ = io.WriteString(w, "answered")
	}))
	t.Cleanup(secondary.Close)

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, _ := failover.WithPin(context.Background())
	req, err := http.NewRequest(http.MethodGet,
		primary.URL+"/paths/strict-send?destination_asset=NGNC%3Aissuer&source_amount=100", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	readAll(t, resp.Body)
	resp.Body.Close()

	if seen.path != "/paths/strict-send" {
		t.Errorf("failover host saw path %q", seen.path)
	}
	if seen.query != "destination_asset=NGNC%3Aissuer&source_amount=100" {
		t.Errorf("failover host saw query %q, want the original query preserved", seen.query)
	}
	if want := strings.TrimPrefix(secondary.URL, "http://"); seen.host != want {
		t.Errorf("Host header = %q, want %q so a virtual-hosted provider routes the request", seen.host, want)
	}
	if seen.method != http.MethodGet {
		t.Errorf("failover host saw method %q, want GET", seen.method)
	}
}

// TestUsesTheBaseTransportItWasGiven: the real wiring hands this transport the
// instrumented one as Base, so a failover that dialled the network itself would
// silently drop every counter. The spy also shows which host each attempt was
// actually aimed at.
func TestUsesTheBaseTransportItWasGiven(t *testing.T) {
	spy := &spyRoundTripper{
		statusOf: map[string]int{
			"primary.example":   http.StatusServiceUnavailable,
			"secondary.example": http.StatusOK,
		},
	}
	tr, err := failover.New("https://primary.example", "https://secondary.example", spy)
	if err != nil {
		t.Fatalf("failover.New: %v", err)
	}

	ctx, pin := failover.WithPin(context.Background())
	req, err := http.NewRequest(http.MethodGet, "https://primary.example/order_book", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := tr.RoundTrip(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	body := readAll(t, resp.Body)
	resp.Body.Close()

	if body != "secondary.example" {
		t.Errorf("body = %q, want the answer from the host the base transport was told to reach", body)
	}
	if got := strings.Join(spy.hosts(), " "); got != "primary.example secondary.example" {
		t.Errorf("base transport dialled %q, want the primary then the failover host", got)
	}
	if got := pin.Host(); got != "https://secondary.example" {
		t.Errorf("Host() = %q, want the failover origin", got)
	}
}

// TestSwitchIsLoggedOncePerLadder: a stopgap in silent use is a stopgap nobody
// replaces, and one that logged per exchange would bury the rest of the log.
func TestSwitchIsLoggedOncePerLadder(t *testing.T) {
	primary, _ := stubHost(t, always(http.StatusServiceUnavailable))
	secondary, _ := stubHost(t, answer("answered"))

	var log bytes.Buffer
	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, func(tr *failover.Transport) {
		tr.Logger = slog.New(slog.NewTextHandler(&log, nil))
	})}

	ctx, _ := failover.WithPin(context.Background())
	for i := 0; i < 3; i++ {
		get(t, client, ctx, primary.URL+"/paths/strict-send")
	}

	lines := logLines(log.String())
	if len(lines) != 1 {
		t.Fatalf("failover logged %d lines for one ladder, want 1:\n%s", len(lines), log.String())
	}
	for _, want := range []string{
		`msg="switched to the horizon failover host"`,
		"from=" + primary.URL,
		"to=" + secondary.URL,
		"path=/paths/strict-send",
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("log line %q lacks %s", lines[0], want)
		}
	}
}

// TestDoubleOutageIsSaidSo: "switched to the failover host" alone reads as a
// recovery. An operator has to be able to tell a working stopgap from a corridor
// with no Horizon at all.
func TestDoubleOutageIsSaidSo(t *testing.T) {
	primary, _ := stubHost(t, always(http.StatusServiceUnavailable))

	tests := []struct {
		name string
		// secondary returns the failover host's address. The two shapes differ
		// in what an operator can do about them: a reachable host answering
		// 5xx is a provider problem worth reporting by status, and a host that
		// cannot be dialled at all is a configuration or network problem whose
		// only witness is the transport error.
		secondary func(t *testing.T) string
		want      string
	}{
		{
			name:      "the failover host answers with a 5xx",
			secondary: failing(http.StatusBadGateway),
			want:      "status=502",
		},
		{
			name:      "the failover host is not there",
			secondary: refused,
			want:      "error=",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			secondaryURL := tc.secondary(t)

			var log bytes.Buffer
			client := &http.Client{Transport: mustTransport(t, primary.URL, secondaryURL, func(tr *failover.Transport) {
				tr.Logger = slog.New(slog.NewTextHandler(&log, nil))
			})}

			ctx, pin := failover.WithPin(context.Background())
			resp, err := client.Do(mustGet(t, primary.URL, ctx))
			if err == nil {
				status := resp.StatusCode
				resp.Body.Close()
				if status != http.StatusBadGateway {
					t.Errorf("status = %d, want the failover host's answer", status)
				}
			}

			lines := logLines(log.String())
			if len(lines) != 1 {
				t.Fatalf("logged %d lines, want 1:\n%s", len(lines), log.String())
			}
			line := lines[0]
			if !strings.Contains(line, `msg="horizon failover host also failed"`) {
				t.Errorf("log line %q does not say the failover host also failed", line)
			}
			if !strings.Contains(line, "level=ERROR") {
				t.Errorf("a double outage must not log below error level: %q", line)
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("log line %q does not carry %s", line, tc.want)
			}
			// Whichever way it failed, the ladder is recorded against the host
			// whose answer the caller got.
			if got := pin.Host(); got != secondaryURL {
				t.Errorf("Host() = %q, want the failover origin %q", got, secondaryURL)
			}
		})
	}
}

// TestLaddersDoNotShareDecisions: two corridors measured at once each get their
// own host choice, and neither may inherit the other's switch.
func TestLaddersDoNotShareDecisions(t *testing.T) {
	var primaryHits atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if primaryHits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "from the primary")
	}))
	t.Cleanup(primary.Close)

	secondary, _ := stubHost(t, answer("from the failover host"))
	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}

	// The first ladder fails over; the second starts on a primary that has
	// recovered, and must be answered by it.
	firstCtx, firstPin := failover.WithPin(context.Background())
	if body := get(t, client, firstCtx, primary.URL); body != "from the failover host" {
		t.Fatalf("first ladder body = %q, want the failover host", body)
	}
	secondCtx, secondPin := failover.WithPin(context.Background())
	if body := get(t, client, secondCtx, primary.URL); body != "from the primary" {
		t.Fatalf("second ladder body = %q, want its own primary", body)
	}

	if got := firstPin.Host(); got != secondary.URL {
		t.Errorf("first ladder Host() = %q, want the failover host", got)
	}
	if got := secondPin.Host(); got != primary.URL {
		t.Errorf("second ladder Host() = %q, want the primary", got)
	}
}

// TestInnerPinTakesPrecedence: a request built from a pinned context but given
// its own pin is its own ladder, and does not wait on the outer decision.
func TestInnerPinTakesPrecedence(t *testing.T) {
	primary, primaryHits := stubHost(t, always(http.StatusServiceUnavailable))
	secondary, secondaryHits := stubHost(t, answer("answered"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	outer, outerPin := failover.WithPin(context.Background())
	inner, innerPin := failover.WithPin(outer)

	get(t, client, inner, primary.URL)

	if got := innerPin.Host(); got != secondary.URL {
		t.Errorf("inner Host() = %q, want the failover host", got)
	}
	if got := outerPin.Host(); got != "" {
		t.Errorf("outer Host() = %q, want the inner ladder to leave it untouched", got)
	}
	if got := primaryHits.Load(); got != 1 {
		t.Errorf("primary saw %d requests, want 1", got)
	}
	if got := secondaryHits.Load(); got != 1 {
		t.Errorf("failover host saw %d requests, want 1", got)
	}
}

// Host is documented as safe to read from any goroutine, including while rungs
// are in flight, because the monitor reads it from the recording side.
func TestHostIsSafeToReadDuringTheExchange(t *testing.T) {
	primary, _ := stubHost(t, always(http.StatusServiceUnavailable))
	secondary, _ := stubHost(t, answer("answered"))

	client := &http.Client{Transport: mustTransport(t, primary.URL, secondary.URL, nil)}
	ctx, pin := failover.WithPin(context.Background())

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = pin.Host()
			}
		}
	}()

	for i := 0; i < 8; i++ {
		get(t, client, ctx, primary.URL)
	}
	close(stop)
	wg.Wait()

	if got := pin.Host(); got != secondary.URL {
		t.Errorf("Host() = %q after the ladder, want the failover host", got)
	}
}

// A caller that has no pin — a code path where the monitor never scoped the
// ladder — must be able to ask the question without branching on nil.
func TestNilPinReportsNothing(t *testing.T) {
	var pin *failover.Pin
	if got := pin.Host(); got != "" {
		t.Errorf("Host() = %q on a nil pin, want empty", got)
	}
	if pin.FailedOver() {
		t.Error("FailedOver() = true on a nil pin")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// stubHost starts a server that answers its n-th request with reply, and returns
// it with a counter of the requests it actually received.
func stubHost(t *testing.T, reply func(w http.ResponseWriter, r *http.Request, hit int64)) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	if reply == nil {
		reply = always(http.StatusOK)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, hits.Add(1))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// always answers with a status and nothing else.
func always(status int) func(http.ResponseWriter, *http.Request, int64) {
	return func(w http.ResponseWriter, _ *http.Request, _ int64) { w.WriteHeader(status) }
}

// statusBody answers with a status and a body that names which host replied.
func statusBody(status int, body string) func(http.ResponseWriter, *http.Request, int64) {
	return func(w http.ResponseWriter, _ *http.Request, _ int64) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// answer reports a fixed body with an implicit 200.
func answer(body string) func(http.ResponseWriter, *http.Request, int64) {
	return func(w http.ResponseWriter, _ *http.Request, _ int64) {
		_, _ = io.WriteString(w, body)
	}
}

// slow never answers on its own, which is what an unreachable Horizon looks
// like from inside a request that has to wait for it.
func slow() func(http.ResponseWriter, *http.Request, int64) {
	return func(_ http.ResponseWriter, r *http.Request, _ int64) {
		<-r.Context().Done()
	}
}

// failing and refused build the primary for a failover test, so each case names
// the outage it means instead of describing it inline.
func failing(status int) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		srv, _ := stubHost(t, always(status))
		return srv.URL
	}
}

func refused(t *testing.T) string {
	t.Helper()
	// A closed server leaves the port refusing connections, which is the
	// upstream being absent rather than slow.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

func mustTransport(t *testing.T, primary, secondary string, tweak func(*failover.Transport)) *failover.Transport {
	t.Helper()
	tr, err := failover.New(primary, secondary, nil)
	if err != nil {
		t.Fatalf("failover.New(%q, %q): %v", primary, secondary, err)
	}
	if tweak != nil {
		tweak(tr)
	}
	return tr
}

func mustGet(t *testing.T, url string, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	return req.WithContext(ctx)
}

func get(t *testing.T, client *http.Client, ctx context.Context, url string) string {
	t.Helper()
	body, err := tryGet(client, ctx, url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return body
}

func tryGet(client *http.Client, ctx context.Context, url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}
	return string(body)
}

func logLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// spyRoundTripper answers without touching the network, keyed by the host each
// attempt was aimed at.
type spyRoundTripper struct {
	mu       sync.Mutex
	statusOf map[string]int
	dialled  []string
}

func (s *spyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.dialled = append(s.dialled, req.URL.Host)
	status := s.statusOf[req.URL.Host]
	s.mu.Unlock()

	if status == 0 {
		status = http.StatusTeapot
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(req.URL.Host)),
		Request:    req,
	}, nil
}

func (s *spyRoundTripper) hosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.dialled...)
}
