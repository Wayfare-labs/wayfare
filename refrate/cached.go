package refrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultCacheTTL is how long a fetched rate is reused.
//
// One hour, chosen against two facts. Both providers publish roughly daily, so
// an hour never serves a rate the upstream has meaningfully moved past. And a
// single ladder is twelve Quote calls that would otherwise each fetch the same
// mid — a burst of identical requests that can exhaust a free-tier quota in
// one page view.
//
// It is deliberately far shorter than the publishing cadence. The cache exists
// to collapse a burst, not to avoid refetching: a monitor running every few
// hours should still see a fresh rate on every run.
const DefaultCacheTTL = time.Hour

// Cached memoises a provider for a bounded time.
//
// # Why a cached rate is allowed at all
//
// The project's rule is that no rate is displayed unless it came from a live
// source. A cached rate did come from a live source — the question is only
// whether it is still current. So the rule this implements is narrower and
// checkable: a cached rate is served only inside a documented age bound, and
// the age is carried to the caller rather than hidden.
//
// What is never allowed is serving a rate past its bound because a refetch
// failed. That would present a stale figure as current, which is the one thing
// the invariant exists to prevent. A miss plus a failing provider is an error.
//
// # Concurrency
//
// route.Engine.Ladder prices four sizes at once, so this is the read path
// under genuine concurrency. Misses for the same pair collapse into one
// upstream fetch: without that, the first ladder after every expiry would fire
// four simultaneous requests for the same number, which is exactly the burst
// the cache is meant to remove.
type Cached struct {
	Inner Provider

	// TTL bounds how long a fetched rate is reused. Zero means
	// DefaultCacheTTL. Negative disables caching entirely, which is useful
	// in tests that want to observe every upstream call.
	TTL time.Duration

	// Clock is the time source, for tests. Nil means time.Now.
	Clock func() time.Time

	mu      sync.Mutex
	entries map[string]*cacheEntry
}

// cacheEntry holds one pair's rate and the in-flight fetch for it.
type cacheEntry struct {
	// ready is closed when a fetch completes. A goroutine finding an entry
	// with a non-nil ready waits on it rather than starting its own fetch.
	ready chan struct{}

	rate      Rate
	err       error
	fetchedAt time.Time
}

func (c *Cached) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

func (c *Cached) ttl() time.Duration {
	if c.TTL == 0 {
		return DefaultCacheTTL
	}
	return c.TTL
}

// Name identifies the underlying provider. The cache is not a source and does
// not rename what it wraps.
func (c *Cached) Name() string { return c.Inner.Name() }

// Rate returns a cached rate if one is within the TTL, otherwise fetches.
func (c *Cached) Rate(ctx context.Context, base, quote string) (Rate, error) {
	if c.Inner == nil {
		return Rate{}, errors.New("refrate: Cached requires an inner provider")
	}
	if c.ttl() < 0 {
		return c.fetch(ctx, base, quote)
	}

	key := base + "/" + quote

	for {
		c.mu.Lock()
		if c.entries == nil {
			c.entries = map[string]*cacheEntry{}
		}
		e, ok := c.entries[key]

		// A completed entry inside the TTL is a hit.
		if ok && e.ready == nil && c.now().Sub(e.fetchedAt) < c.ttl() {
			rate := e.rate
			c.mu.Unlock()
			rate.FetchedAt = e.fetchedAt
			return rate, nil
		}

		// Another goroutine is already fetching this pair; wait for it
		// rather than starting a second identical request.
		if ok && e.ready != nil {
			ready := e.ready
			c.mu.Unlock()
			select {
			case <-ready:
				continue // re-evaluate; the result may still be an error
			case <-ctx.Done():
				return Rate{}, ctx.Err()
			}
		}

		// Miss, or expired. Claim the fetch.
		e = &cacheEntry{ready: make(chan struct{})}
		c.entries[key] = e
		c.mu.Unlock()

		rate, err := c.fetch(ctx, base, quote)

		c.mu.Lock()
		e.rate, e.err = rate, err
		e.fetchedAt = c.now()
		ready := e.ready
		e.ready = nil
		if err != nil {
			// A failed fetch is not cached. Caching it would turn one
			// transient outage into a TTL-long refusal, and — worse — a
			// later reader could not tell a provider that is down now
			// from one that was down an hour ago.
			delete(c.entries, key)
		}
		c.mu.Unlock()
		close(ready)

		if err != nil {
			return Rate{}, err
		}
		rate.FetchedAt = e.fetchedAt
		return rate, nil
	}
}

func (c *Cached) fetch(ctx context.Context, base, quote string) (Rate, error) {
	r, err := c.Inner.Rate(ctx, base, quote)
	if err != nil {
		// A rate limit is surfaced as itself rather than flattened, because
		// the remedy differs: "ask again later" is not "this provider is
		// broken", and under a schedule the first is the likely failure.
		var limited *ErrRateLimited
		if errors.As(err, &limited) {
			return Rate{}, fmt.Errorf("refrate: %s is rate-limited and no cached rate is within the "+
				"age bound; a stale rate will not be presented as current: %w", c.Inner.Name(), err)
		}
		return Rate{}, err
	}
	return r, nil
}

// Invalidate drops any cached rate for a pair. Used by tests and by an
// operator who knows a provider has corrected a published figure.
func (c *Cached) Invalidate(base, quote string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, base+"/"+quote)
}
