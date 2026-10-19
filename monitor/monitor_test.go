package monitor

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// schedulerOver builds a scheduler measuring one corridor from a snapshot, so
// the whole test runs offline.
func schedulerOver(t *testing.T, prefix string, recv asset.Asset, quote, mid string, store runstore.Store) *Scheduler {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Skipf("no snapshot matching %q", prefix)
	}
	snap, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot: %v", err)
	}

	return &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: snap.HTTPClient(),
			},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/" + quote: decimal.RequireFromString(mid),
			}),
		},
		Store: store,
		Corridors: []Corridor{{
			Send: asset.USDC(), Receive: recv,
			ReferenceBase: "USD", ReferenceQuote: quote,
		}},
		Logger: quiet(),
	}
}

// TestRunOnceWritesOneRecordPerCorridor is the scheduler's basic contract.
func TestRunOnceWritesOneRecordPerCorridor(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record")
	}
	if rec.Seq != 1 {
		t.Errorf("Seq = %d, want 1", rec.Seq)
	}
	if rec.Integrity != "DIRECT" {
		t.Errorf("Integrity = %s, want DIRECT", rec.Integrity)
	}
	if rec.Recommended != nil {
		t.Error("a corridor measured at a heavy loss must record no recommendation")
	}
	if rec.FloorLossPct == "" || rec.WorstLossPct == "" {
		t.Error("the record should carry the floor and worst figures")
	}
	if rec.Reference.ScoredAgainst == "" {
		t.Error("the record must name which mid produced its verdicts")
	}
}

// TestRunOnceIsIdempotentUnderReRun checks that running twice appends two
// records that chain, rather than rewriting or duplicating history.
func TestRunOnceIsIdempotentUnderReRun(t *testing.T) {
	dir := t.TempDir()
	store, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	for i := 0; i < 3; i++ {
		if err := s.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}

	recent, err := store.Recent(context.Background(), "USDC-NGNC", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 {
		t.Fatalf("got %d records after 3 runs, want 3", len(recent))
	}
	// Same corridor, same snapshot: the measurements must agree.
	for _, r := range recent {
		if r.Integrity != recent[0].Integrity || r.FloorLossPct != recent[0].FloorLossPct {
			t.Errorf("re-running the same snapshot produced a different measurement: %s/%s vs %s/%s",
				r.Integrity, r.FloorLossPct, recent[0].Integrity, recent[0].FloorLossPct)
		}
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain does not verify after repeated runs: %v", err)
	}
}

// TestNoMarketCorridorIsRecorded checks that a corridor with no market still
// produces a record. It is a finding, and a history that skipped it would show
// a gap where a real result belongs.
func TestNoMarketCorridorIsRecorded(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("no record written for a no-market corridor")
	}
	if rec.Integrity != "NO-MARKET" {
		t.Errorf("Integrity = %s, want NO-MARKET", rec.Integrity)
	}
}

// TestDerivativeDependencyIsRecorded pins that the dependency survives into
// history, since #24 compares it across runs.
func TestDerivativeDependencyIsRecorded(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ghsc", asset.GHSC(), "GHS", "11.0912", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-GHSC")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Integrity != "DERIVATIVE" {
		t.Fatalf("Integrity = %s, want DERIVATIVE", rec.Integrity)
	}
	if len(rec.DependsOn) != 1 || rec.DependsOn[0] != "NGNC" {
		t.Errorf("DependsOn = %v, want [NGNC]; #24 needs this to detect a "+
			"dependency change that an integrity comparison alone would miss",
			rec.DependsOn)
	}
}

// TestSchedulerRunsWithoutAStore pins that history is optional. The scheduler
// must be runnable with nothing configured, which is also how it behaves if a
// volume fails to mount.
func TestSchedulerRunsWithoutAStore(t *testing.T) {
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", nil)
	if err := s.RunOnce(context.Background()); err != nil {
		t.Errorf("RunOnce with no store: %v", err)
	}
}

// TestRunStopsOnContextCancel checks the shutdown path, so a deploy does not
// hang waiting for a ticker.
func TestRunStopsOnContextCancel(t *testing.T) {
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", nil)
	s.Interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// The first sweep runs immediately, so a freshly started monitor has
	// data without waiting a full interval.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run should return the context error on cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// TestRunSweepsImmediately checks the first measurement does not wait for one
// full interval, which on a six-hour cadence would mean a freshly deployed
// instance served nothing all morning.
func TestRunSweepsImmediately(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.Interval = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, _ := store.Latest(context.Background(), "USDC-NGNC")
		if rec != nil {
			return // measured well inside the one-hour interval
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("no measurement recorded; the first sweep waited for the interval")
}

// TestSchedulerNeedsAnEngine guards the obvious misconfiguration.
func TestSchedulerNeedsAnEngine(t *testing.T) {
	s := &Scheduler{Logger: quiet()}
	if err := s.RunOnce(context.Background()); err == nil {
		t.Error("expected an error with no engine configured")
	}
	if err := s.Run(context.Background()); err == nil {
		t.Error("expected Run to refuse with no engine configured")
	}
}

// TestDefaultCorridorsCoverEveryState documents why the sweep includes the
// case-study trio: each corridor exercises a different integrity state, so a
// deployment measuring them continuously exercises the whole taxonomy. The
// sweep now covers every registered corridor destination; this test guards the
// trio that anchors it.
func TestDefaultCorridorsCoverEveryState(t *testing.T) {
	got := DefaultCorridors()
	if len(got) == 0 {
		t.Fatal("no default corridors; the sweep must measure at least the case-study trio")
	}
	// The case-study trio is still the spine of the sweep: one corridor per
	// integrity state (NGNC DIRECT, GHSC DERIVATIVE, KESC NO-MARKET).
	wantKeys := map[string]bool{"USDC-NGNC": false, "USDC-GHSC": false, "USDC-KESC": false}
	for _, c := range got {
		if _, ok := wantKeys[c.Key()]; ok {
			wantKeys[c.Key()] = true
		}
		if c.ReferenceQuote == "" || c.ReferenceBase == "" {
			t.Errorf("%s has no reference pair; it could not be scored", c.Key())
		}
	}
	for key, seen := range wantKeys {
		if !seen {
			t.Errorf("default corridors are missing %s", key)
		}
	}
}
