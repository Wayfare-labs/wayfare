package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock moves budget time without sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestBudget(rate, burst float64) (*Budget, *fakeClock) {
	c := newFakeClock()
	b := NewBudget(rate, burst)
	b.now = c.Now
	b.last = c.Now()
	return b, c
}

func TestBudgetGrantsUpToBurstThenRefusesLive(t *testing.T) {
	b, _ := newTestBudget(1, 2)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := b.Wait(ctx, Live); err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	err := b.Wait(waitCtx, Live)

	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("third live request: want *ExhaustedError, got %v", err)
	}
	if exhausted.Priority != Live {
		t.Errorf("priority: got %v, want live", exhausted.Priority)
	}

	if got := b.Stats().RefusedLive; got != 1 {
		t.Errorf("refused_live: got %d, want 1", got)
	}
}

func TestBudgetRefillsAtTheConfiguredRate(t *testing.T) {
	b, clock := newTestBudget(2, 2) // one token every 500ms
	ctx := context.Background()

	b.Wait(ctx, Live)
	b.Wait(ctx, Live)

	if got := b.Stats().Available; got > 0.01 {
		t.Fatalf("available after burst: got %.3f, want 0", got)
	}

	clock.Advance(500 * time.Millisecond)
	if got := b.Stats().Available; got < 0.99 {
		t.Fatalf("available after 500ms: got %.3f, want 1", got)
	}

	if err := b.Wait(ctx, Live); err != nil {
		t.Fatalf("request after refill: %v", err)
	}
}

func TestBudgetDoesNotRefillBeyondCapacity(t *testing.T) {
	b, clock := newTestBudget(10, 2)
	clock.Advance(time.Hour)
	if got := b.Stats().Available; got != 2 {
		t.Fatalf("available after a long idle: got %.2f, want capacity 2", got)
	}
}

func TestSweepBorrowsRatherThanBeingRefused(t *testing.T) {
	b, _ := newTestBudget(1000, 1) // 1ms per token, so the debt is short
	ctx := context.Background()

	if err := b.Wait(ctx, Live); err != nil {
		t.Fatalf("priming the bucket: %v", err)
	}

	started := time.Now()
	sweepCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := b.Wait(sweepCtx, Sweep); err != nil {
		t.Fatalf("sweep on an empty bucket: %v", err)
	}
	// It took the token it did not have, so it slept off the debt, which at
	// 1000/s is about a millisecond.
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("sweep waited %s, want a debt-sized pause", elapsed)
	}
	if got := b.Stats().GrantedSweep; got != 1 {
		t.Errorf("granted_sweep: got %d, want 1", got)
	}
}

func TestSweepIsNeverRefusedByAnExhaustedBucket(t *testing.T) {
	b, _ := newTestBudget(20, 1) // 50ms per token
	ctx := context.Background()
	b.Wait(ctx, Live)

	sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	// The bucket is empty and refills in 50ms; a live request would be
	// refused, and the sweep must still be granted.
	if err := b.Wait(sweepCtx, Sweep); err != nil {
		t.Fatalf("sweep with a 5ms context: %v", err)
	}
}

func TestLiveYieldsToAWaitingSweep(t *testing.T) {
	b, clock := newTestBudget(1, 1)
	ctx := context.Background()
	b.Wait(ctx, Live)

	// A sweep is queued behind the empty bucket, as it is during a run.
	b.enter(Sweep)
	defer b.leave(Sweep)

	// One token is now available. Without the yield the live request would take
	// it; with it, the live request waits for one more.
	clock.Advance(time.Second)

	liveCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err := b.Wait(liveCtx, Live)

	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("live request beside a waiting sweep: want *ExhaustedError, got %v", err)
	}
}

func TestNilBudgetGrantsImmediately(t *testing.T) {
	var b *Budget
	if err := b.Wait(context.Background(), Live); err != nil {
		t.Fatalf("nil budget: %v", err)
	}
	if got := b.Stats(); got != (BudgetStats{}) {
		t.Fatalf("nil budget stats: got %+v, want zero", got)
	}
}

func TestPriorityRoundTripsThroughContext(t *testing.T) {
	if got := PriorityOf(context.Background()); got != Live {
		t.Errorf("unset priority: got %v, want live", got)
	}
	ctx := WithPriority(context.Background(), Sweep)
	if got := PriorityOf(ctx); got != Sweep {
		t.Errorf("sweep priority: got %v, want sweep", got)
	}
}

func TestBudgetSerialisesConcurrentCallers(t *testing.T) {
	// Real time here: the point is that twenty goroutines share two tokens and
	// the ones that are refused are refused, not that any particular one wins.
	b := NewBudget(100, 2)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	granted, refused := 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			waitCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
			defer cancel()
			err := b.Wait(waitCtx, Live)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				var exhausted *ExhaustedError
				if !errors.As(err, &exhausted) {
					t.Errorf("unexpected error: %v", err)
				}
				refused++
				return
			}
			granted++
		}()
	}
	wg.Wait()

	if granted+refused != 20 {
		t.Fatalf("granted %d + refused %d != 20", granted, refused)
	}
	if granted > 3 { // two tokens plus whatever refilled in the window
		t.Fatalf("granted %d requests from a 2-token bucket", granted)
	}
	stats := b.Stats()
	if int(stats.GrantedLive) != granted {
		t.Errorf("granted_live: got %d, want %d", stats.GrantedLive, granted)
	}
	if stats.LiveWaiting != 0 || stats.SweepWaiting != 0 {
		t.Errorf("waiters left behind: %+v", stats)
	}
}
