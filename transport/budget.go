package transport

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Priority says what a request is for, which decides who waits.
//
// A scheduled sweep exists to keep the committed chain complete, and a live
// request exists to answer one caller right now. When they compete for the same
// Horizon quota the sweep has to win: dropping one live reading costs a user a
// retry, while dropping a sweep run leaves a hole that nothing later fills.
type Priority int

const (
	// Live is an on-demand request, served from a warm process and expected to
	// finish inside a few seconds.
	Live Priority = iota

	// Sweep is a scheduled measurement run.
	Sweep
)

func (p Priority) String() string {
	if p == Sweep {
		return "sweep"
	}
	return "live"
}

// ExhaustedError reports that a caller gave up waiting for budget.
//
// It is a typed error rather than a wrapped deadline because the caller's
// remedy depends on which it was: a caller that ran out of budget should be
// told to try again shortly, while a caller whose request timed out has a
// different problem. Both are also distinguishable from a 429, which is
// Horizon's own refusal and arrives as ErrRateLimited in package dex.
type ExhaustedError struct {
	Priority Priority

	// Waited is how long the caller held its place in the queue.
	Waited time.Duration

	// Available is the token count when the caller gave up, between zero and
	// the bucket's capacity.
	Available float64
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf(
		"transport: outbound budget exhausted for a %s request after %s waiting (%.2f tokens available)",
		e.Priority, e.Waited.Round(time.Millisecond), e.Available,
	)
}

// BudgetStats is a snapshot for /healthz. It is JSON-shaped on purpose: the
// numbers are what an operator needs to tell "slow because Horizon is slow"
// from "slow because we are rationing ourselves".
type BudgetStats struct {
	Capacity        float64 `json:"capacity"`
	RefillPerSecond float64 `json:"refill_per_second"`
	Available       float64 `json:"available"`
	LiveWaiting     int     `json:"live_waiting"`
	SweepWaiting    int     `json:"sweep_waiting"`
	GrantedSweep    uint64  `json:"granted_sweep"`
	GrantedLive     uint64  `json:"granted_live"`
	RefusedLive     uint64  `json:"refused_live"`
}

// Budget is one token bucket shared by every outbound Horizon request.
//
// Horizon's public limit is per IP and applies to the sum of everything this
// process sends, so a per-caller limiter is not enough: a sweep and a live
// ladder that each stay under the limit on their own still exceed it together,
// and the result is 429s for both. One bucket with a known rate is the only
// thing that keeps the sum under the limit.
//
// A sweep may go into debt — it takes a token it does not have and sleeps off
// the difference — so it is never refused. Live requests never borrow: they
// wait for a real token and give up with ExhaustedError when their context
// ends. That is what makes sweep priority real rather than advisory: by the
// time a token appears, the sweep has already claimed it.
//
// The zero value is not usable; call NewBudget. A nil *Budget is valid and
// means "no budget", which is what every existing caller has.
type Budget struct {
	mu sync.Mutex

	capacity float64
	rate     float64
	tokens   float64
	last     time.Time

	// now is time.Now except in tests, which need to move the clock rather
	// than sleep for it.
	now func() time.Time

	liveWaiting  int
	sweepWaiting int

	grantedSweep uint64
	grantedLive  uint64
	refusedLive  uint64
}

// NewBudget returns a bucket that refills at perSecond and holds at most burst
// tokens. Both must be positive; anything else would either deadlock every
// caller (a rate of zero) or remove the rationing entirely (a burst of zero).
func NewBudget(perSecond, burst float64) *Budget {
	if perSecond <= 0 {
		perSecond = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &Budget{
		capacity: burst,
		rate:     perSecond,
		tokens:   burst,
		now:      time.Now,
		last:     time.Now(),
	}
}

// Wait blocks until the caller may send one request, and returns
// *ExhaustedError when the context ends first.
//
// A nil budget grants immediately, so callers need no branch of their own.
func (b *Budget) Wait(ctx context.Context, p Priority) error {
	if b == nil {
		return nil
	}

	start := b.now()
	b.enter(p)
	defer b.leave(p)

	for {
		delay, ok := b.reserve(p)
		if ok && delay == 0 {
			b.record(p, true)
			return nil
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			// A sweep is admitted even here: it has already been charged for the
			// token, and the debt it left is what paces the next caller. Refusing
			// it would leave a hole in the chain that nothing fills later, which
			// is the failure this priority exists to prevent.
			if ok {
				b.record(p, true)
				return nil
			}
			b.record(p, false)
			return &ExhaustedError{Priority: p, Waited: b.now().Sub(start), Available: b.available()}
		case <-timer.C:
			if ok {
				b.record(p, true)
				return nil
			}
		}
	}
}

// Stats snapshots the bucket. Safe on a nil budget.
func (b *Budget) Stats() BudgetStats {
	if b == nil {
		return BudgetStats{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked()
	return BudgetStats{
		Capacity:        b.capacity,
		RefillPerSecond: b.rate,
		Available:       b.tokens,
		LiveWaiting:     b.liveWaiting,
		SweepWaiting:    b.sweepWaiting,
		GrantedSweep:    b.grantedSweep,
		GrantedLive:     b.grantedLive,
		RefusedLive:     b.refusedLive,
	}
}

// reserve takes a token when it can.
//
// It returns the delay to sleep and whether a token was already taken. A sweep
// always takes one, possibly by borrowing, and only reports how long to sleep
// so the debt is paid back. A live request takes one only if a whole token is
// there; otherwise it reports how long to wait for the next.
func (b *Budget) reserve(p Priority) (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked()

	if p == Sweep {
		b.tokens--
		// The debt is capped so a burst of sweeps cannot turn into a long
		// unthrottled spray followed by a long silence.
		if b.tokens < -b.capacity {
			b.tokens = -b.capacity
		}
		if b.tokens >= 0 {
			return 0, true
		}
		return b.delayFor(-b.tokens), true
	}

	// A live request beside a waiting sweep keeps one token in reserve for it:
	// without that, a steady stream of live traffic takes each token as it
	// appears and the sweep starves one token at a time.
	need := 1.0
	if b.sweepWaiting > 0 {
		need = 2
	}
	if b.tokens >= need {
		b.tokens--
		return 0, true
	}

	return b.delayFor(need - b.tokens), false
}

func (b *Budget) delayFor(tokens float64) time.Duration {
	if tokens <= 0 {
		return 0
	}
	seconds := tokens / b.rate
	return time.Duration(seconds * float64(time.Second))
}

func (b *Budget) refillLocked() {
	if b.now == nil {
		b.now = time.Now
	}
	now := b.now()
	if b.last.IsZero() {
		b.last = now
		return
	}
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.last = now
	b.tokens += elapsed.Seconds() * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
}

func (b *Budget) enter(p Priority) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p == Sweep {
		b.sweepWaiting++
	} else {
		b.liveWaiting++
	}
}

func (b *Budget) leave(p Priority) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p == Sweep {
		b.sweepWaiting--
	} else {
		b.liveWaiting--
	}
}

func (b *Budget) record(p Priority, granted bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p == Sweep {
		b.grantedSweep++
		return
	}
	if granted {
		b.grantedLive++
	} else {
		b.refusedLive++
	}
}

func (b *Budget) available() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillLocked()
	return b.tokens
}

// priorityKey is unexported so the only way to set a priority is WithPriority.
type priorityKey struct{}

// WithPriority marks a context's Horizon calls as sweep work.
//
// Priority is carried on the context rather than added to every call signature
// because the callers that need it (a scheduled run) sit several layers above
// the HTTP client, and threading a parameter through every intermediate
// function would touch code that has no other reason to know about rationing.
func WithPriority(ctx context.Context, p Priority) context.Context {
	return context.WithValue(ctx, priorityKey{}, p)
}

// PriorityOf reports the priority on a context, Live when unset.
func PriorityOf(ctx context.Context) Priority {
	if ctx == nil {
		return Live
	}
	if p, ok := ctx.Value(priorityKey{}).(Priority); ok {
		return p
	}
	return Live
}
