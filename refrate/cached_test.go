package refrate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// countingProvider records how many times it was asked.
type countingProvider struct {
	calls atomic.Int64
	mid   string
	err   error
	delay time.Duration
}

func (c *countingProvider) Name() string { return "counting" }

func (c *countingProvider) Rate(ctx context.Context, base, quote string) (Rate, error) {
	c.calls.Add(1)
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return Rate{}, ctx.Err()
		}
	}
	if c.err != nil {
		return Rate{}, c.err
	}
	return Rate{
		Base: base, Quote: quote,
		Mid:    decimal.RequireFromString(c.mid),
		AsOf:   time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		Source: c.Name(),
	}, nil
}

// TestCacheCollapsesALadder is the reason the cache exists. A twelve-rung
// ladder must not become twelve identical upstream requests.
func TestCacheCollapsesALadder(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	c := &Cached{Inner: inner}

	for i := 0; i < 12; i++ {
		if _, err := c.Rate(context.Background(), "USD", "NGN"); err != nil {
			t.Fatalf("Rate %d: %v", i, err)
		}
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("upstream called %d times for one ladder, want 1", got)
	}
}

// TestExpiryRefetches pins that the cache is a burst collapser, not a way to
// avoid refetching. A monitor running every few hours must see a fresh rate.
func TestExpiryRefetches(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	c := &Cached{Inner: inner, TTL: time.Hour, Clock: func() time.Time { return now }}

	if _, err := c.Rate(context.Background(), "USD", "NGN"); err != nil {
		t.Fatal(err)
	}
	// Inside the bound: still one call.
	now = now.Add(59 * time.Minute)
	if _, err := c.Rate(context.Background(), "USD", "NGN"); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("calls = %d inside the TTL, want 1", got)
	}

	// Past the bound: refetched.
	now = now.Add(2 * time.Minute)
	if _, err := c.Rate(context.Background(), "USD", "NGN"); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Errorf("calls = %d past the TTL, want 2", got)
	}
}

// TestExpiredEntryPlusFailingProviderIsAnError is the invariant that matters
// most here.
//
// A stale rate must never be presented as current. When the cached rate is
// past its bound and the provider cannot be reached, the honest answer is an
// error — not the old figure served as though it were fresh.
func TestExpiredEntryPlusFailingProviderIsAnError(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	c := &Cached{Inner: inner, TTL: time.Hour, Clock: func() time.Time { return now }}

	first, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}

	// Provider goes down, and the cached entry ages out.
	inner.err = errors.New("connection refused")
	now = now.Add(2 * time.Hour)

	got, err := c.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatalf("expected an error; instead got mid %s, which is %s old and would "+
			"have been presented as current", got.Mid, now.Sub(first.FetchedAt))
	}
}

// TestFailedFetchIsNotCached checks a transient outage does not become a
// TTL-long refusal.
func TestFailedFetchIsNotCached(t *testing.T) {
	inner := &countingProvider{err: errors.New("timeout")}
	c := &Cached{Inner: inner}

	if _, err := c.Rate(context.Background(), "USD", "NGN"); err == nil {
		t.Fatal("expected the first call to fail")
	}

	// Provider recovers; the next call must try again rather than replay
	// the cached failure.
	inner.err = nil
	inner.mid = "1350"
	r, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("provider recovered but the cache still failed: %v", err)
	}
	if !r.Mid.Equal(decimal.RequireFromString("1350")) {
		t.Errorf("Mid = %s, want 1350", r.Mid)
	}
}

// TestConcurrentMissesCollapseIntoOneFetch covers the real read pattern:
// Ladder prices four sizes at once, so the first request after every expiry
// arrives four times simultaneously.
func TestConcurrentMissesCollapseIntoOneFetch(t *testing.T) {
	inner := &countingProvider{mid: "1350", delay: 50 * time.Millisecond}
	c := &Cached{Inner: inner}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Rate(context.Background(), "USD", "NGN")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("upstream called %d times for 8 concurrent misses, want 1", got)
	}
}

// TestConcurrentMissesAllSeeAnError checks the failure path under the same
// concurrency: every waiter must get the error, not a zero-value rate.
func TestConcurrentMissesAllSeeAnError(t *testing.T) {
	inner := &countingProvider{err: errors.New("boom"), delay: 20 * time.Millisecond}
	c := &Cached{Inner: inner}

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Rate(context.Background(), "USD", "NGN")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Errorf("goroutine %d got no error though the provider failed", i)
		}
	}
}

// TestDifferentPairsAreCachedSeparately guards the obvious keying bug, which
// would serve a naira rate for a cedi corridor.
func TestDifferentPairsAreCachedSeparately(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	c := &Cached{Inner: inner}

	ngn, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}
	ghs, err := c.Rate(context.Background(), "USD", "GHS")
	if err != nil {
		t.Fatal(err)
	}

	if inner.calls.Load() != 2 {
		t.Errorf("calls = %d for two distinct pairs, want 2", inner.calls.Load())
	}
	if ngn.Quote != "NGN" || ghs.Quote != "GHS" {
		t.Errorf("pairs crossed: got %s and %s", ngn.Pair(), ghs.Pair())
	}
}

// TestFetchedAtIsSurfaced pins that a caller can tell how old a served rate
// is. Hiding the age would make a cached figure indistinguishable from a fresh
// one, which is the whole risk of caching here.
func TestFetchedAtIsSurfaced(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	c := &Cached{Inner: inner, TTL: time.Hour, Clock: func() time.Time { return now }}

	first, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}
	if !first.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt = %s, want %s", first.FetchedAt, now)
	}

	now = now.Add(30 * time.Minute)
	second, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}

	// The served rate is half an hour old, and says so.
	if !second.FetchedAt.Equal(first.FetchedAt) {
		t.Errorf("FetchedAt = %s, want the original fetch time %s",
			second.FetchedAt, first.FetchedAt)
	}
	// AsOf is the upstream's own stamp and must not be rewritten by caching.
	if !second.AsOf.Equal(first.AsOf) {
		t.Error("caching changed AsOf; that is the upstream's stamp, not ours")
	}
}

// TestRateLimitIsSurfacedDistinctly covers the failure a scheduled monitor is
// most likely to hit on a free tier.
func TestRateLimitIsSurfacedDistinctly(t *testing.T) {
	inner := &countingProvider{err: &ErrRateLimited{
		Source: "counting", RetryAfter: 90 * time.Second, Message: "quota exceeded",
	}}
	c := &Cached{Inner: inner}

	_, err := c.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}

	var limited *ErrRateLimited
	if !errors.As(err, &limited) {
		t.Errorf("error %q does not unwrap to ErrRateLimited; the caller cannot "+
			"distinguish 'ask again later' from 'this provider is broken'", err)
	}
	if !strings.Contains(err.Error(), "will not be presented as current") {
		t.Errorf("error %q should say why no cached rate was substituted", err)
	}
}

// TestCachedServesRateWhenProviderTimesOutInsideBound covers the half of the
// age bound that makes it a bound rather than a trap: while a fetched rate is
// inside its TTL, an unreachable provider does not invalidate it.
//
// The provider timing out is not new information about the rate — the rate was
// obtained from a live source and is still within its documented age. Serving
// it is what the bound exists to permit; refusing would turn every upstream
// blip into a missing benchmark for the whole TTL.
func TestCachedServesRateWhenProviderTimesOutInsideBound(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	c := &Cached{Inner: inner, TTL: time.Hour, Clock: func() time.Time { return now }}

	first, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}

	inner.err = context.DeadlineExceeded
	now = now.Add(30 * time.Minute)

	got, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("provider timed out inside the age bound, but the cached rate was refused: %v", err)
	}
	if !got.Mid.Equal(first.Mid) {
		t.Errorf("Mid = %s, want the cached %s", got.Mid, first.Mid)
	}
	if !got.FetchedAt.Equal(first.FetchedAt) {
		t.Errorf("FetchedAt = %s, want the original fetch time so the age stays honest",
			got.FetchedAt)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("upstream called %d times, want 1 — the hit path must not re-probe a dead provider", got)
	}
}

// TestBothProvidersTimeOutOfTheirCachesServeTheCross is the deployment shape
// of the timeout question: Cross over two Cached providers, both inner
// providers timing out, both cached rates still inside their bounds.
//
// The cross-check must proceed on the cached figures exactly as it would on
// fresh ones — agreement, divergence and selection are properties of the two
// rates, not of how they were obtained. An implementation that propagated one
// provider's timeout into a hard error would take the whole benchmark down
// because one feed blinked.
func TestBothProvidersTimeOutOfTheirCachesServeTheCross(t *testing.T) {
	p1 := &countingProvider{mid: "1348"}
	p2 := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	stack := &Cross{
		Primary:   &Cached{Inner: p1, TTL: time.Hour, Clock: clock},
		Secondary: &Cached{Inner: p2, TTL: time.Hour, Clock: clock},
	}

	first, err := stack.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatal(err)
	}
	if first.Agreement != AgreementAgree {
		t.Fatalf("Agreement = %s on primed caches, want AGREE", first.Agreement)
	}

	// Both providers time out; both caches stay inside their bounds.
	p1.err = context.DeadlineExceeded
	p2.err = context.DeadlineExceeded
	now = now.Add(30 * time.Minute)

	r, err := stack.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("both caches were within their bounds yet the cross failed: %v", err)
	}
	if r.Agreement != AgreementAgree {
		t.Errorf("Agreement = %s, want AGREE computed from the two cached rates", r.Agreement)
	}
	if !r.Mid.Equal(decimal.RequireFromString("1348")) ||
		!r.SecondaryMid.Equal(decimal.RequireFromString("1350")) {
		t.Errorf("mids = %s/%s, want the cached 1348/1350", r.Mid, r.SecondaryMid)
	}
	if p1.calls.Load() != 1 || p2.calls.Load() != 1 {
		t.Errorf("upstream calls = %d/%d, want 1/1 — timeouts must not be re-probed on hits",
			p1.calls.Load(), p2.calls.Load())
	}
}

// TestOneExpiredCachePlusTimeoutDegradesToSingle is the boundary between the
// two rules above: the primary's cache is still good, but the secondary's has
// aged out at the same moment its provider times out.
//
// The secondary cannot answer and may not be substituted for, so the result
// degrades to SINGLE and says so — the primary's usable rate is not thrown
// away with it.
func TestOneExpiredCachePlusTimeoutDegradesToSingle(t *testing.T) {
	p1 := &countingProvider{mid: "1348"}
	p2 := &countingProvider{mid: "1350"}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	stack := &Cross{
		Primary:   &Cached{Inner: p1, TTL: time.Hour, Clock: clock},
		Secondary: &Cached{Inner: p2, TTL: time.Hour, Clock: clock},
	}
	if _, err := stack.Rate(context.Background(), "USD", "NGN"); err != nil {
		t.Fatal(err)
	}

	p2.err = context.DeadlineExceeded
	now = now.Add(2 * time.Hour) // past the secondary's bound only

	r, err := stack.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("the primary's cache was still valid yet the whole cross failed: %v", err)
	}
	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	if !r.Mid.Equal(decimal.RequireFromString("1348")) {
		t.Errorf("Mid = %s, want the primary's cached 1348", r.Mid)
	}
	if !strings.Contains(r.Note, "uncorroborated") {
		t.Errorf("Note = %q, want it to say the surviving rate is uncorroborated", r.Note)
	}
}

// TestNegativeTTLDisablesCaching keeps the escape hatch honest.
func TestNegativeTTLDisablesCaching(t *testing.T) {
	inner := &countingProvider{mid: "1350"}
	c := &Cached{Inner: inner, TTL: -1}

	for i := 0; i < 3; i++ {
		if _, err := c.Rate(context.Background(), "USD", "NGN"); err != nil {
			t.Fatal(err)
		}
	}
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("calls = %d with caching disabled, want 3", got)
	}
}

// TestComposesWithCheckedAndCross pins the layering the deployment uses:
// Cross over two Cached providers, each wrapped in Checked.
func TestComposesWithCheckedAndCross(t *testing.T) {
	primary := &countingProvider{mid: "1348"}
	secondary := &countingProvider{mid: "1350"}

	stack := &Cross{
		Primary:   &Checked{Inner: &Cached{Inner: primary}, MaxAge: 0},
		Secondary: &Checked{Inner: &Cached{Inner: secondary}, MaxAge: 0},
	}

	for i := 0; i < 5; i++ {
		r, err := stack.Rate(context.Background(), "USD", "NGN")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if r.Agreement != AgreementAgree {
			t.Errorf("Agreement = %s, want AGREE", r.Agreement)
		}
	}
	if primary.calls.Load() != 1 || secondary.calls.Load() != 1 {
		t.Errorf("upstream calls = %d/%d across five cross-checked reads, want 1/1",
			primary.calls.Load(), secondary.calls.Load())
	}
}
