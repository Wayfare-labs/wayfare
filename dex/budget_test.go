package dex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/transport"
)

// countingHorizon is a fake Horizon that records how many requests reached it.
// The count is the point: a refused request must never get this far, because a
// request that reached Horizon has already spent the quota the budget exists to
// protect.
func countingHorizon(t *testing.T, status int) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestEveryHorizonCallSpendsTheBudget(t *testing.T) {
	horizon, hits := countingHorizon(t, http.StatusOK)
	c := &Client{HorizonURL: horizon.URL, Budget: transport.NewBudget(1, 2)}
	ctx := context.Background()

	var out map[string]any
	for i := 0; i < 2; i++ {
		if err := c.get(ctx, "/ledgers", url.Values{}, &out); err != nil {
			t.Fatalf("request %d inside the burst: %v", i, err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	err := c.get(waitCtx, "/ledgers", url.Values{}, &out)

	var exhausted *transport.ExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("request past the budget: want *transport.ExhaustedError, got %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("Horizon saw %d requests, want 2: a refused call still went out", got)
	}
}

func TestSweepPrioritySurvivesAnEmptyBudget(t *testing.T) {
	horizon, hits := countingHorizon(t, http.StatusOK)
	// Ten tokens a second with a burst of one: after the first call a live
	// request waits 100ms, which its 20ms context does not allow, while the
	// sweep's debt is the same 100ms and its context does.
	c := &Client{HorizonURL: horizon.URL, Budget: transport.NewBudget(10, 1)}
	ctx := context.Background()

	var out map[string]any
	if err := c.get(ctx, "/ledgers", url.Values{}, &out); err != nil {
		t.Fatalf("priming the bucket: %v", err)
	}

	sweepCtx, cancel := context.WithTimeout(transport.WithPriority(ctx, transport.Sweep), time.Second)
	defer cancel()

	if err := c.get(sweepCtx, "/ledgers", url.Values{}, &out); err != nil {
		t.Fatalf("sweep on an empty bucket: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("Horizon saw %d requests, want 2", got)
	}
}

func TestNoBudgetMeansNoRationing(t *testing.T) {
	horizon, hits := countingHorizon(t, http.StatusOK)
	c := &Client{HorizonURL: horizon.URL}
	ctx := context.Background()

	var out map[string]any
	for i := 0; i < 5; i++ {
		if err := c.get(ctx, "/ledgers", url.Values{}, &out); err != nil {
			t.Fatalf("unbudgeted request %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(hits); got != 5 {
		t.Fatalf("Horizon saw %d requests, want 5", got)
	}
}

func TestBudgetRefusalIsNotAnHTTPFailure(t *testing.T) {
	horizon, _ := countingHorizon(t, http.StatusOK)
	c := &Client{HorizonURL: horizon.URL, Budget: transport.NewBudget(1, 1)}
	ctx := context.Background()

	var out map[string]any
	if err := c.get(ctx, "/ledgers", url.Values{}, &out); err != nil {
		t.Fatalf("priming the bucket: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	err := c.get(waitCtx, "/ledgers", url.Values{}, &out)

	// A 429 means Horizon said no; a budget refusal means we never asked. The
	// two carry different remedies, so they must not be the same error type.
	var rateLimited *ErrRateLimited
	if errors.As(err, &rateLimited) {
		t.Fatal("budget refusal surfaced as ErrRateLimited")
	}
}
