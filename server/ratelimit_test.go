package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// newLimiterTestServer builds a Server whose limiter allows burst requests
// immediately and refills at rate per second, backed by the shared fixtures
// so a measurement succeeds without network. It exists so the middleware can
// be exercised end to end through Handler(), the way a real client meets it.
func newLimiterTestServer(t *testing.T, rate float64, burst int) *httptest.Server {
	t.Helper()
	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(liveNGNCPaths))
	}))
	t.Cleanup(horizon.Close)

	s := &Server{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: horizon.URL},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1500"),
			}),
		},
		Limiter: NewRateLimiter(rate, burst),
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

// TestRateLimiterFirstSeenClientIsAllowed pins the friendliest possible
// default: a client the service has never met starts with a full bucket, so
// the first visit can never be refused.
func TestRateLimiterFirstSeenClientIsAllowed(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	ok, retry := rl.Allow("10.0.0.1")
	if !ok {
		t.Fatalf("first request from a new client refused (retry in %v)", retry)
	}
}

// TestRateLimiterBurstThenRefuse is the core behaviour: burst requests pass
// at once, the next is refused, and the refusal names a wait that is at most
// one refill interval.
func TestRateLimiterBurstThenRefuse(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow("10.0.0.2"); !ok {
			t.Fatalf("request %d of the burst was refused; want %d allowed", i+1, 3)
		}
	}
	ok, retry := rl.Allow("10.0.0.2")
	if ok {
		t.Fatal("request past the burst was allowed; the bucket must bound it")
	}
	if retry <= 0 || retry > time.Second {
		t.Errorf("retry window = %v, want (0, 1s] for a 1/s limiter", retry)
	}
}

// TestRateLimiterRefillsOverTime proves the bucket recovers: after the burst
// is spent, one rate interval is enough to admit exactly one more request —
// and not two, so the refill is a rate, not a refill-to-full.
func TestRateLimiterRefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(2, 1) // one token per 500ms
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }

	if ok, _ := rl.Allow("10.0.0.3"); !ok {
		t.Fatal("first request refused; a fresh bucket must admit it")
	}
	if ok, _ := rl.Allow("10.0.0.3"); ok {
		t.Fatal("second immediate request allowed past a burst of 1")
	}

	now = now.Add(250 * time.Millisecond)
	if ok, _ := rl.Allow("10.0.0.3"); ok {
		t.Fatal("token granted after half a refill interval")
	}

	now = now.Add(250 * time.Millisecond)
	if ok, _ := rl.Allow("10.0.0.3"); !ok {
		t.Fatal("no token after one full refill interval at 2/s")
	}
	if ok, _ := rl.Allow("10.0.0.3"); ok {
		t.Fatal("second token granted without another refill interval passing")
	}
}

// TestRateLimiterBurstRefillsToCap checks the cap: idle time refills the
// bucket to burst and no further, so idle hours do not bank an unbounded
// allowance.
func TestRateLimiterBurstRefillsToCap(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		rl.Allow("10.0.0.4")
	}
	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow("10.0.0.4"); !ok {
			t.Fatalf("request %d after an idle hour refused; the bucket refills to burst, not to zero", i+1)
		}
	}
	if ok, _ := rl.Allow("10.0.0.4"); ok {
		t.Fatal("fourth request allowed after idle; refill must stop at burst")
	}
}

// TestRateLimiterDisabledAllowsEverything: rate 0 is the documented off
// switch, so an operator who opts out gets exactly the old behaviour.
func TestRateLimiterDisabledAllowsEverything(t *testing.T) {
	for _, cfg := range [][2]int{{0, 10}, {4, 0}, {0, 0}} {
		rl := NewRateLimiter(float64(cfg[0]), cfg[1])
		for i := 0; i < 100; i++ {
			if ok, retry := rl.Allow("10.0.0.5"); !ok {
				t.Fatalf("disabled limiter (rate=%d burst=%d) refused request %d (retry %v)",
					cfg[0], cfg[1], i+1, retry)
			}
		}
	}
}

// TestRateLimiterClientsAreIndependent: one client exhausting its bucket
// must not cost another client its request — that is the fairness the
// limiter exists to provide.
func TestRateLimiterClientsAreIndependent(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	if ok, _ := rl.Allow("10.0.0.6"); !ok {
		t.Fatal("client A's first request refused")
	}
	if ok, _ := rl.Allow("10.0.0.7"); !ok {
		t.Fatal("client B refused because client A spent a token; buckets must be per-client")
	}
	if ok, _ := rl.Allow("10.0.0.6"); ok {
		t.Fatal("client A allowed a second request past a burst of 1")
	}
}

// TestRateLimiterEvictsIdleBuckets bounds memory: past the threshold, buckets
// that have been silent past the eviction age are dropped, so a rotating
// population of client keys cannot grow the map without limit.
func TestRateLimiterEvictsIdleBuckets(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }

	// Fill past the sweep threshold.
	for i := 0; i < bucketEvictThreshold+1; i++ {
		rl.Allow(clientKeyForTest(i))
	}
	// Advance well past the eviction age and admit one more client, which
	// triggers the sweep on the allocation path.
	now = now.Add(2 * bucketIdleEviction)
	rl.Allow("10.9.9.9")

	rl.mu.Lock()
	n := len(rl.buckets)
	_, oldPresent := rl.buckets[clientKeyForTest(0)]
	rl.mu.Unlock()

	if oldPresent {
		t.Error("a bucket idle past the eviction age survived the sweep")
	}
	if n > bucketEvictThreshold+1 {
		t.Errorf("bucket count = %d after eviction, want at most %d", n, bucketEvictThreshold+1)
	}
}

func clientKeyForTest(i int) string {
	return "10.1." + itoa(i/256) + "." + itoa(i%256)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [4]byte
	n := 0
	for i > 0 {
		b[n] = byte('0' + i%10)
		i /= 10
		n++
	}
	out := make([]byte, n)
	for j := 0; j < n; j++ {
		out[j] = b[n-1-j]
	}
	return string(out)
}

// TestRateLimiterConcurrentAccess exercises the mutex under -race: the
// limiter sits on every request path, so a data race here would be the
// service's race, not a test's.
func TestRateLimiterConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000, 1000)
	done := make(chan struct{})
	for w := 0; w < 8; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				rl.Allow("10.2.0." + itoa(w))
			}
		}(w)
	}
	for w := 0; w < 8; w++ {
		<-done
	}
}

// TestClientKeyFromDirectPublicPeer ignores X-Forwarded-For unless the
// request demonstrably came through a local proxy: a directly-connected
// public peer that sets the header must not get to choose its own bucket.
func TestClientKeyFromDirectPublicPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/corridor", nil)
	r.RemoteAddr = "203.0.113.7:4432"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientKey(r); got != "203.0.113.7" {
		t.Errorf("clientKey = %q, want the peer host %q; a public peer must not choose its own bucket", got, "203.0.113.7")
	}
}

// TestClientKeyBehindLoopbackProxy is the Render shape: TLS terminates on the
// same host, the app sees 127.0.0.1 and the header carries the real client.
func TestClientKeyBehindLoopbackProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/corridor", nil)
	r.RemoteAddr = "127.0.0.1:55555"
	r.Header.Set("X-Forwarded-For", "198.51.100.23, 10.0.0.1")
	if got := clientKey(r); got != "198.51.100.23" {
		t.Errorf("clientKey = %q, want the leftmost forwarded client %q", got, "198.51.100.23")
	}
}

// TestClientKeyBehindPrivateProxy covers a proxy on the RFC 1918 side, e.g. a
// container network front.
func TestClientKeyBehindPrivateProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:4432"
	r.Header.Set("X-Forwarded-For", "198.51.100.99")
	if got := clientKey(r); got != "198.51.100.99" {
		t.Errorf("clientKey = %q, want the forwarded client %q", got, "198.51.100.99")
	}
}

// TestClientKeyBehindProxyWithoutHeader: forwarded traffic with no header
// falls back to the peer, so every proxied client shares one bucket. Bounded
// total cost — not per-client fairness — is the guarantee there.
func TestClientKeyBehindProxyWithoutHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:55555"
	if got := clientKey(r); got != "127.0.0.1" {
		t.Errorf("clientKey = %q, want the peer host %q when no forwarding header exists", got, "127.0.0.1")
	}
}

// TestClientKeyUnparseableRemoteAddr: RemoteAddr without a port (some test
// harnesses) must still yield a usable key rather than panicking.
func TestClientKeyUnparseableRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9"
	if got := clientKey(r); got != "203.0.113.9" {
		t.Errorf("clientKey = %q, want %q", got, "203.0.113.9")
	}
}

// TestHandlerAppliesRateLimit is the end-to-end contract (issue #320): past
// the configured allowance the service answers 429 in the shared error
// envelope, with a Retry-After header, and the code a client can switch on.
func TestHandlerAppliesRateLimit(t *testing.T) {
	// A slow refill (1/s) so the milliseconds between sequential requests
	// cannot mint a token; what is under test is the burst being spent, not
	// the refill racing the test.
	api := newLimiterTestServer(t, 1, 2)

	var last int
	var body map[string]any
	var retryAfter string
	for i := 0; i < 4; i++ {
		resp, err := http.Get(api.URL + "/api/corridor?sizes=1")
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		last = resp.StatusCode
		retryAfter = resp.Header.Get("Retry-After")
		body = map[string]any{}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d: decoding body: %v", i+1, err)
		}
	}

	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d after exhausting the burst, want 429", last)
	}
	if body["code"] != codeRateLimited {
		t.Errorf("error code = %v, want %q", body["code"], codeRateLimited)
	}
	if body["error"] == nil || body["error"] == "" {
		t.Error("429 must carry the shared error envelope with a message")
	}
	if retryAfter == "" {
		t.Error("429 must carry Retry-After so a well-behaved client can back off")
	}
}

// TestHandlerRateLimitHeadersOnlyOn429: Retry-After is a promise about a
// refusal; setting it on a 200 would tell caching layers and clients
// something false about every normal response.
func TestHandlerRateLimitHeadersOnlyOn429(t *testing.T) {
	api := newLimiterTestServer(t, 1000, 50)
	resp, err := http.Get(api.URL + "/api/corridor?sizes=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 within the burst", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "" {
		t.Error("Retry-After must not be set on a successful response")
	}
}

// TestHandlerRateLimitCoversEveryRoute: the limiter wraps the mux, not one
// handler, because the free instance's cost is CPU and upstream calls
// regardless of path.
func TestHandlerRateLimitCoversEveryRoute(t *testing.T) {
	paths := []string{
		"/api/corridor",
		"/api/corridor/trend",
		"/api/chain-heads",
		"/api/assets",
		"/healthz",
		"/",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			// One token and a slow refill, so the first hit spends the bucket
			// and the second must be refused whatever the path.
			api := newLimiterTestServer(t, 1, 1)

			// Spend the single-token bucket on the first hit of this path.
			resp1, err := http.Get(api.URL + path)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			resp1.Body.Close()

			resp2, err := http.Get(api.URL + path)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			defer resp2.Body.Close()

			if resp2.StatusCode != http.StatusTooManyRequests {
				t.Errorf("%s: second request status = %d, want 429; the limiter must cover every route", path, resp2.StatusCode)
			}
		})
	}
}

// TestHandlerWithoutLimiterIsUnlimited keeps the zero-value Server honest:
// nil Limiter means no limiting, which is what every existing constructor
// (and most tests) rely on.
func TestHandlerWithoutLimiterIsUnlimited(t *testing.T) {
	api := testServer(t, liveNGNCPaths, "1500")
	for i := 0; i < 30; i++ {
		resp, err := http.Get(api.URL + "/api/corridor?sizes=1")
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 with no limiter configured", i+1, resp.StatusCode)
		}
	}
}

// TestRetryAfterRoundsUp: telling a client 0 seconds when the bucket needs
// 400ms would have it come back and be refused again.
func TestRetryAfterRoundsUp(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{400 * time.Millisecond, "1"},
		{1 * time.Second, "1"},
		{1500 * time.Millisecond, "2"},
		{0, "1"},
	} {
		if got := formatRetryAfter(tc.d); got != tc.want {
			t.Errorf("formatRetryAfter(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
