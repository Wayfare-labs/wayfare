package server

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter bounds what a population of clients can cost the service.
//
// The motivation is issue #320: a live measurement is a dozen Horizon round
// trips (more when sizes= is used, up to the 24 the API accepts), the
// reference rate adds two more upstreams, and the public deployment runs on a
// free instance with no platform rate limiting in front of it. Nothing stopped
// one client from holding ?live=1 down in a loop and pricing ladders forever.
// A per-client token bucket turns that from unbounded into a number an
// operator chose.
//
// The limiter is deliberately dependency-free: the project runs exactly two
// direct dependencies (see CONTRIBUTING.md), and a token bucket is forty lines
// rather than a reason for a third.
//
// A bucket keyed by client is a fairness mechanism, not a security boundary.
// A client that spoofs its identity key gets its own bucket — which is all it
// would have had without spoofing. The point is that a fixed pool of tokens
// bounds the service's total upstream cost, so one abusive client cannot make
// the monitor unusable for everyone else.
type RateLimiter struct {
	// Rate is the sustained request rate per client, in requests per second.
	// One bucket refills continuously at this pace.
	Rate float64

	// Burst is the bucket depth: how many requests a client may make in an
	// instant above the sustained rate. It absorbs the small fan-out of a
	// normal page load (assets, corridor, trend) without the reader ever
	// seeing a 429.
	Burst int

	mu      sync.Mutex
	buckets map[string]*clientBucket

	// now is swapped in tests; nil means time.Now.
	now func() time.Time
}

// clientBucket is one client's token balance.
type clientBucket struct {
	tokens float64
	// last is when the bucket was last touched, for refill and for evicting
	// entries for clients that have gone away.
	last time.Time
}

// NewRateLimiter returns a limiter allowing each client rate requests per
// second with burst of instantaneous headroom.
//
// A non-positive rate or burst disables limiting: Allow then approves
// everything, so an operator who passes 0 gets the old behaviour on purpose
// rather than a limiter that silently denies the world.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		Rate:    rate,
		Burst:   burst,
		buckets: map[string]*clientBucket{},
	}
}

// enabled reports whether the limiter is configured to limit at all.
func (rl *RateLimiter) enabled() bool {
	return rl.Rate > 0 && rl.Burst > 0
}

// Allow reports whether the named client may proceed now, and if not, how
// long until a token is available again. The duration is exact — the caller
// decides how to round it — so Retry-After can be derived from it honestly.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	if !rl.enabled() {
		return true, 0
	}
	now := rl.clock()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		// A first-seen client starts with a full bucket: a client that has
		// never cost the service anything has nothing to be punished for.
		b = &clientBucket{tokens: float64(rl.Burst), last: now}
		rl.buckets[key] = b
		rl.evictLocked(now)
	}

	// Refill for the time this bucket sat idle, capped at the burst depth.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(float64(rl.Burst), b.tokens+elapsed*rl.Rate)
	}
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Time until the fractional balance reaches the next whole token.
	deficit := 1 - b.tokens
	return false, time.Duration(deficit / rl.Rate * float64(time.Second))
}

// evictLocked drops buckets idle past evictionIdle so the map cannot grow
// with the number of distinct spoofed or rotating client keys. Called only
// from Allow with the mutex held, and only on the allocation path, so a
// steady client population costs one map entry each and nothing grows
// unboundedly.
func (rl *RateLimiter) evictLocked(now time.Time) {
	if len(rl.buckets) < bucketEvictThreshold {
		return
	}
	for k, b := range rl.buckets {
		if now.Sub(b.last) > bucketIdleEviction {
			delete(rl.buckets, k)
		}
	}
}

const (
	// bucketEvictThreshold is the map size at which a sweep for idle buckets
	// is worth doing. Small enough to bound memory, large enough that the
	// sweep never runs during normal operation.
	bucketEvictThreshold = 4096
	// bucketIdleEviction is how long a client must be silent before its
	// bucket is forgettable. Longer than any real reader's pause between
	// clicks; shorter than forever.
	bucketIdleEviction = 10 * time.Minute
)

func (rl *RateLimiter) clock() time.Time {
	if rl.now != nil {
		return rl.now()
	}
	return time.Now()
}

// middleware returns an http.Handler that spends one token per request and
// answers 429 with the shared error envelope when the client's bucket is
// empty.
func (rl *RateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		ok, retryAfter := rl.Allow(key)
		if !ok {
			w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
			writeError(w, r, http.StatusTooManyRequests, codeRateLimited,
				"rate limit exceeded; retry after the delay in the Retry-After header")
			log().Info("request rate limited",
				"client", key,
				"path", r.URL.Path,
				"retry_after", retryAfter.Round(time.Millisecond).String())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// formatRetryAfter renders a wait as whole seconds, rounded up: telling a
// client one second when the bucket needs 1.2 would have it come back and be
// refused again.
func formatRetryAfter(d time.Duration) string {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// clientKey reduces a request to the identity the limiter buckets on: the
// connecting peer's host, or the forwarded-for client when the request
// demonstrably arrived through a reverse proxy.
//
// X-Forwarded-For is trusted only when the immediate peer is loopback or a
// private-range address, because that is the shape of "we are behind a proxy"
// on the deployments this project runs (Render terminates TLS and forwards
// from its own network). Trusting the header from a directly-connected public
// peer would let the client choose its bucket at will, which is not a
// boundary worth having. When the header is not trusted the peer host is
// used, which on such a deployment means one shared bucket: still bounded
// total cost — the point of the limiter — at the price of per-client
// fairness, and an honest failure mode to document rather than a spoofable
// key.
func clientKey(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = strings.TrimSpace(r.RemoteAddr)
	}
	if isProxyPeer(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The leftmost entry is the originating client; intermediaries
			// append. The rest of the chain is untrusted either way.
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	return peer
}

// isProxyPeer reports whether the connecting host looks like a reverse proxy
// in front of this process: loopback, or an RFC 1918 / RFC 4193 / link-local
// address. Public peers are their own client; their forwarding headers are
// ignored.
func isProxyPeer(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
