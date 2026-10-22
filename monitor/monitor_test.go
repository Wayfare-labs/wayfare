package monitor

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
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

func TestSchedulerLoadsIssuerDriftBaselineFromLatestRecord(t *testing.T) {
	ctx := context.Background()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := checks.IssuerDriftBaseline{
		AuthRequired:  true,
		AuthImmutable: true,
		TOMLHash:      strings.Repeat("a", 64),
	}
	record := &runstore.Record{
		Corridor: "USDC-NGNC",
		Checks: []checks.CheckJSON{{
			ID: "issuer.drift",
			Evidence: []checks.EvidenceJSON{{
				Source:   "https://horizon.stellar.org/accounts/issuer → drift-check",
				Observed: "auth_required=true auth_revocable=false auth_clawback_enabled=false auth_immutable=true toml_sha256=" + want.TOMLHash,
			}},
		}},
	}
	if err := store.Append(ctx, record); err != nil {
		t.Fatal(err)
	}

	scheduler := &Scheduler{Store: store, Logger: quiet()}
	got := scheduler.issuerDriftBaseline(ctx, "USDC-NGNC")
	if got == nil || *got != want {
		t.Fatalf("baseline = %#v, want %#v", got, want)
	}
}

func TestRunOnceStoresSnapshotAndLinksItsHash(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", "usdc-ngnc-*"))
	if err != nil || len(matches) == 0 {
		t.Skip("no USDC-NGNC snapshot fixture")
	}
	snap, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot: %v", err)
	}
	artifactDir := t.TempDir()
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.SnapshotDir = artifactDir
	s.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
		recorder.Transport = snap.Replay()
		client := &http.Client{Transport: recorder}
		return &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: client,
			},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1350.2568"),
			}),
		}
	}
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || !strings.HasPrefix(rec.Snapshot, "sha256:") {
		t.Fatalf("run record snapshot = %q, want a content hash", rec.Snapshot)
	}
	path := filepath.Join(artifactDir, strings.TrimPrefix(rec.Snapshot, "sha256:"))
	if got, err := snapshot.ContentHash(path); err != nil || got != rec.Snapshot {
		t.Errorf("stored artifact hash = %q, %v; want %q", got, err, rec.Snapshot)
	}
	if err := store.VerifySnapshots(context.Background(), "USDC-NGNC", artifactDir); err != nil {
		t.Errorf("VerifySnapshots: %v", err)
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

func TestSchedulerAppendConcurrentWithFileStoreReaders(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scheduler := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	const readerCount = 4
	const sweepCount = 4
	start := make(chan struct{})
	ready := make(chan struct{}, readerCount)
	done := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(readerCount)
	for i := 0; i < readerCount; i++ {
		go func() {
			defer readers.Done()
			<-start
			assertReadableRunstore(t, store)
			ready <- struct{}{}
			for {
				select {
				case <-done:
					return
				default:
					assertReadableRunstore(t, store)
				}
			}
		}()
	}
	close(start)
	for i := 0; i < readerCount; i++ {
		<-ready
	}
	for i := 0; i < sweepCount; i++ {
		if err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	close(done)
	readers.Wait()

	all, err := store.All(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != sweepCount {
		t.Fatalf("All returned %d records, want %d", len(all), sweepCount)
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain after concurrent scheduler writes does not verify: %v", err)
	}
}

func assertReadableRunstore(t *testing.T, store runstore.Store) {
	t.Helper()
	ctx := context.Background()

	latest, err := store.Latest(ctx, "USDC-NGNC")
	if err != nil {
		t.Errorf("Latest: %v", err)
	} else if latest != nil {
		if err := latest.VerifySelf(); err != nil {
			t.Errorf("Latest returned a partial record: %v", err)
		}
	}

	recent, err := store.Recent(ctx, "USDC-NGNC", 3)
	if err != nil {
		t.Errorf("Recent: %v", err)
	} else {
		for i, record := range recent {
			if err := record.VerifySelf(); err != nil {
				t.Errorf("Recent record %d is partial: %v", i, err)
			}
			if i > 0 && (recent[i-1].Seq != record.Seq+1 || recent[i].Hash != recent[i-1].PrevHash) {
				t.Errorf("Recent records are not a complete descending chain: seq %d then %d",
					recent[i-1].Seq, record.Seq)
			}
		}
	}

	all, err := store.All(ctx, "USDC-NGNC")
	if err != nil {
		t.Errorf("All: %v", err)
		return
	}
	for i, record := range all {
		if err := record.VerifySelf(); err != nil {
			t.Errorf("All record %d is partial: %v", i, err)
		}
		if i > 0 && record.PrevHash != all[i-1].Hash {
			t.Errorf("All has a broken link between seq %d and %d", all[i-1].Seq, record.Seq)
		}
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
	s.Jitter = time.Hour

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

func TestSchedulerJitterIsDeterministicAndBounded(t *testing.T) {
	seed := int64(42)
	scheduler := &Scheduler{
		Interval:   6 * time.Hour,
		Jitter:     5 * time.Minute,
		JitterSeed: &seed,
	}
	first := scheduler.randomSource()
	second := scheduler.randomSource()
	var varied bool
	for i := 0; i < 10; i++ {
		got := scheduler.nextInterval(first)
		replay := scheduler.nextInterval(second)
		if got != replay {
			t.Fatalf("seeded interval %d = %s, replay = %s", i, got, replay)
		}
		if got < scheduler.Interval || got >= scheduler.Interval+scheduler.Jitter {
			t.Errorf("interval %s is outside [%s, %s)", got, scheduler.Interval, scheduler.Interval+scheduler.Jitter)
		}
		varied = varied || got > scheduler.Interval
	}
	if !varied {
		t.Fatal("seeded jitter never added an offset")
	}
}

func TestSchedulerZeroJitterKeepsBaseInterval(t *testing.T) {
	scheduler := &Scheduler{Interval: 3 * time.Hour}
	if got := scheduler.nextInterval(rand.New(rand.NewSource(1))); got != scheduler.Interval {
		t.Errorf("interval with zero jitter = %s, want %s", got, scheduler.Interval)
	}
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
