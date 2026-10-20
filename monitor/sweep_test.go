package monitor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// failHandler wraps a snapshot replayer and returns HTTP 500 for requests
// whose query contains the given receive-asset code.  This simulates a
// corridor whose Horizon pathfinding is unreachable without touching the
// network.
func failHandler(m *snapshot.Manifest, failAssetCode string) http.HandlerFunc {
	replay := m.Replay()
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "destination_assets="+failAssetCode) {
			http.Error(w, "simulated upstream failure", http.StatusInternalServerError)
			return
		}
		resp, err := replay.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// multiCorridorHandler serves different snapshot replays for different
// corridors, failing with HTTP 500 for the corridor named by failAssetCode.
func multiCorridorHandler(t *testing.T, failAssetCode string) http.HandlerFunc {
	t.Helper()
	snaps := map[string]*snapshot.Replayer{}
	for _, prefix := range []string{"usdc-ngnc", "usdc-ghsc"} {
		m := loadSnapshot(t, prefix)
		snaps[strings.TrimPrefix(strings.TrimPrefix(prefix, "usdc-"), "")] = m.Replay()
	}

	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.RawQuery
		switch {
		case strings.Contains(q, "destination_assets=KESC"):
			http.Error(w, "simulated upstream failure", http.StatusInternalServerError)
		case strings.Contains(q, "destination_assets=NGNC"):
			resp, err := snaps["ngnc"].RoundTrip(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		case strings.Contains(q, "destination_assets=GHSC"):
			resp, err := snaps["ghsc"].RoundTrip(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		default:
			http.NotFound(w, r)
		}
	}
}

// loadSnapshot finds and loads a snapshot matching the given prefix from
// testdata/snapshots.  It calls t.Skip if no matching snapshot exists.
func loadSnapshot(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Skipf("no snapshot matching %q", prefix)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot %s: %v", matches[0], err)
	}
	return m
}

// TestFailingSweepDoesNotBreakChain verifies the core invariant of issue #135:
// when one corridor's DEX pathfinding fails (returns HTTP 500), the other
// corridor's chain remains valid and the gap is visible — the failing corridor
// is still recorded with an error finding rather than silently dropped.
//
// The monitor's contract is that a corridor whose upstream is unreachable is
// itself information worth recording.  An unavailable quantity is "unknown",
// never zero and never a default, and the gap must be visible in the chain
// so a later reader can tell "nothing was measured" from "measured and found
// nothing".
func TestFailingSweepDoesNotBreakChain(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Load a snapshot and build a handler that fails for KESC but replays
	// the snapshot for NGNC.
	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "KESC"))
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
		"USD/KES": decimal.RequireFromString("129.4263"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Logger: quiet(),
	}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// --- the successful corridor must have a valid record and chain ---
	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record for the successful corridor USDC-NGNC")
	}
	if rec.Seq != 1 {
		t.Errorf("Seq = %d, want 1", rec.Seq)
	}
	if rec.Integrity != "DIRECT" {
		t.Errorf("Integrity = %s, want DIRECT", rec.Integrity)
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain verification failed for successful corridor: %v", err)
	}

	// --- the failing corridor must be recorded with a visible gap ---
	// When the DEX returns 500, every rung errors but Ladder still returns
	// a result.  measure records it with Integrity=UNKNOWN and a Finding
	// that names the failure — the gap is visible in the chain.
	kescRec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if kescRec == nil {
		t.Fatal("failing corridor USDC-KESC was not recorded; the gap must be visible, not silently dropped")
	}
	if kescRec.Integrity != "UNKNOWN" {
		t.Errorf("USDC-KESC Integrity = %s, want UNKNOWN (all rungs errored)", kescRec.Integrity)
	}
	if kescRec.Finding == "" {
		t.Error("USDC-KESC Finding is empty; the gap should be explained in prose")
	}
	if err := store.Verify(context.Background(), "USDC-KESC"); err != nil {
		t.Errorf("chain verification failed for failing corridor: %v", err)
	}
}

// TestPartialSweepRecordsSuccessfulCorridors checks that when one of three
// corridors fails, the other two are still recorded and their chains verify.
func TestPartialSweepRecordsSuccessfulCorridors(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(multiCorridorHandler(t, "KESC"))
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
		"USD/GHS": decimal.RequireFromString("11.0912"),
		"USD/KES": decimal.RequireFromString("129.4263"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.GHSC(), ReferenceBase: "USD", ReferenceQuote: "GHS"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Logger: quiet(),
	}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// --- the two successful corridors must have records ---
	for _, want := range []struct {
		key   string
		integ string
	}{
		{"USDC-NGNC", "DIRECT"},
		{"USDC-GHSC", "DERIVATIVE"},
	} {
		rec, err := store.Latest(context.Background(), want.key)
		if err != nil {
			t.Fatalf("Latest(%s): %v", want.key, err)
		}
		if rec == nil {
			t.Errorf("no record written for %s", want.key)
			continue
		}
		if rec.Integrity != want.integ {
			t.Errorf("%s Integrity = %s, want %s", want.key, rec.Integrity, want.integ)
		}
		if err := store.Verify(context.Background(), want.key); err != nil {
			t.Errorf("chain verification failed for %s: %v", want.key, err)
		}
	}

	// --- the failing corridor must be recorded with a visible gap ---
	kescRec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if kescRec == nil {
		t.Error("failing corridor USDC-KESC was not recorded; the gap must be visible")
	} else if kescRec.Integrity != "UNKNOWN" {
		t.Errorf("USDC-KESC Integrity = %s, want UNKNOWN", kescRec.Integrity)
	}
}

// failingStore wraps a runstore.Store and returns an error on Append for a
// specific corridor, simulating a disk failure during recording.
type failingStore struct {
	runstore.Store
	failCorridor string
}

func (fs *failingStore) Append(ctx context.Context, r *runstore.Record) error {
	if strings.EqualFold(r.Corridor, fs.failCorridor) {
		return fmt.Errorf("simulated storage failure for %s", r.Corridor)
	}
	return fs.Store.Append(ctx, r)
}

// TestStorageFailureDoesNotCorruptChain checks that when store.Append fails
// for one corridor, the other corridor's measurement is still recorded and its
// chain verifies.  The storage failure is logged but does not fail the sweep.
func TestStorageFailureDoesNotCorruptChain(t *testing.T) {
	dir := t.TempDir()
	realStore, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := &failingStore{Store: realStore, failCorridor: "USDC-KESC"}

	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "KESC"))
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
		"USD/KES": decimal.RequireFromString("129.4263"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Logger: quiet(),
	}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// NGNC must be recorded and its chain valid.
	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record for USDC-NGNC")
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain verification failed for USDC-NGNC after storage failure on other corridor: %v", err)
	}

	// KESC has no record — the storage failure is visible as a gap.
	kescRec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if kescRec != nil {
		t.Errorf("USDC-KESC unexpectedly has a record despite storage failure (seq %d)", kescRec.Seq)
	}
}

// TestRunOnceReturnsErrorWhenAllCorridorsFail verifies that when every
// corridor's Ladder call itself returns an error (not merely rung-level
// errors), RunOnce returns an error — a monitor that silently succeeded on
// a total failure would mask a broken deployment.
//
// We achieve this by cancelling the context, which causes Ladder to return
// ctx.Err().  This is the only reliable way to make Ladder itself fail:
// rung-level errors (e.g. DEX 500) are recorded in the result rather than
// propagated.
func TestRunOnceReturnsErrorWhenAllCorridorsFail(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "NGNC"))
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Logger: quiet(),
	}

	// Cancel the context so Ladder returns ctx.Err() for every corridor.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if err := s.RunOnce(ctx); err == nil {
		t.Error("RunOnce should return an error when context is cancelled")
	}
}

// TestRunContinuesAfterSweepFailure verifies that the ticker loop in Run
// survives a failed initial sweep and keeps running.  This is the "a monitor
// that exits on one bad sweep stops recording precisely when a corridor is
// misbehaving" scenario from the code comment.
func TestRunContinuesAfterSweepFailure(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	snap := loadSnapshot(t, "usdc-ngnc")
	// Both corridors fail — the handler returns 500 for everything.
	handler := failHandler(snap, "NGNC")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Interval: 50 * time.Millisecond,
		Logger:   quiet(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Run should not return immediately after the first failing sweep.
	// Ladder returns a result (not an error) when all rungs fail, so the
	// sweep succeeds and Run keeps ticking.
	err = s.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Run returned %v, want context.DeadlineExceeded (meaning it kept running after failure)", err)
	}
}

// TestRecordedGapIsVerifiable checks that a corridor recorded after a
// partial sweep carries enough information for a later reader to tell the
// gap from a genuine no-market finding.
func TestRecordedGapIsVerifiable(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "KESC"))
	t.Cleanup(srv.Close)

	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
		"USD/KES": decimal.RequireFromString("129.4263"),
	})

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: srv.Client(),
			},
			RefRate: staticRef,
		},
		Store: store,
		Corridors: []Corridor{
			{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
			{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		},
		Logger: quiet(),
	}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Both corridors should have records (one valid, one gap).
	corridors, err := store.Corridors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(corridors) != 2 {
		t.Fatalf("expected 2 corridors in store, got %d: %v", len(corridors), corridors)
	}

	// The KESC record should have no rungs (nothing was priced) but a
	// Finding that names the failure.  This is what makes the gap visible:
	// a reader can distinguish "all rungs errored" from "no market at all"
	// and from "measured and found nothing".
	kescRec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if kescRec == nil {
		t.Fatal("USDC-KESC not recorded")
	}
	// When the DEX returns 500, every rung errors but Ladder still creates
	// them (one per size).  The rungs are present with Priced=false, and
	// the Finding names the failure.  A reader can tell "all rungs errored"
	// from "no market" because the Integrity is UNKNOWN rather than
	// NO-MARKET.
	for _, rung := range kescRec.Rungs {
		if rung.Priced {
			t.Errorf("USDC-KESC rung %s is priced; all rungs should be unpriced after a DEX failure",
				rung.SendAmount)
		}
	}
	if kescRec.Finding == "" {
		t.Error("USDC-KESC Finding is empty; a gap must be explained")
	}
}

// TestSweepCoversEveryRegisteredCorridor pins the sweep set to the registry.
//
// Every verified corridor destination in asset must appear in
// DefaultCorridors(), so a newly registered corridor cannot be measured in
// asset and silently skipped by the monitor. Removing a corridor from the
// sweep makes this test fail.
func TestSweepCoversEveryRegisteredCorridor(t *testing.T) {
	sweep := make(map[string]Corridor)
	for _, c := range DefaultCorridors() {
		sweep[c.Receive.Code+":"+c.Receive.Issuer] = c
	}

	for _, e := range asset.Registry() {
		if e.Code == "USDC" || e.Peg == "" {
			// USDC is the settlement asset; every other registered entry
			// with a peg is a corridor destination.
			continue
		}
		key := e.Code + ":" + e.Issuer
		c, ok := sweep[key]
		if !ok {
			t.Errorf("registered corridor %s is not in the monitor's sweep set", runstore.CorridorKey("USDC", e.Code))
			continue
		}
		if c.ReferenceBase == "" || c.ReferenceQuote == "" {
			t.Errorf("%s has no reference pair; it could not be scored", c.Key())
		}
	}
}

// TestSweepSetEqualsRegistrySet pins the equality #615 asks for in both
// directions, which TestSweepCoversEveryRegisteredCorridor above does not: it
// walks the registry into the sweep, so a corridor that is swept but never
// registered passes it silently.
//
// That second direction is not hypothetical, and it is the more dangerous of
// the two. A swept-but-unregistered corridor is priced against a reference
// pair nothing has verified, while /api/corridor refuses the same code outright
// with unknown_receive_asset — so the monitor accumulates a history for a
// corridor the API says does not exist, and the two disagree about what the
// registry is. The fix is always registration (asset's own package comment:
// "the fix is registration, never guessing"), and this is the test that says
// so.
//
// It also pins the reference pair against the issuer's own declared peg rather
// than merely requiring it to be non-empty: the scoring contract says the
// benchmark is the peg a token claims to track, so a sweep entry scored against
// anything else grades a corridor against the wrong money.
func TestSweepSetEqualsRegistrySet(t *testing.T) {
	swept := make(map[string]Corridor, len(DefaultCorridors()))
	var duplicates []string
	for _, c := range DefaultCorridors() {
		key := c.Receive.Code + ":" + c.Receive.Issuer
		if _, seen := swept[key]; seen {
			duplicates = append(duplicates, c.Key())
			continue
		}
		swept[key] = c
	}
	if len(duplicates) > 0 {
		t.Errorf("the sweep lists %s twice; a duplicated corridor measures the same chain twice per run",
			strings.Join(duplicates, ", "))
	}

	registered := make(map[string]asset.Entry, len(swept))
	for _, e := range asset.Registry() {
		// USDC is the settlement asset every corridor starts from, not a
		// destination; an entry with no peg is not a corridor either.
		if e.Code == "USDC" || e.Peg == "" {
			continue
		}
		registered[e.Code+":"+e.Issuer] = e
	}

	for key, e := range registered {
		if _, ok := swept[key]; !ok {
			t.Errorf("registered corridor %s is not swept, so it never accumulates history",
				runstore.CorridorKey("USDC", e.Code))
		}
	}
	for key, c := range swept {
		e, ok := registered[key]
		if !ok {
			t.Errorf("swept corridor %s is not a registered destination; the API would answer it with unknown_receive_asset while the monitor records it",
				c.Key())
			continue
		}
		if c.Send.Code != "USDC" {
			t.Errorf("%s starts from %s; every registered corridor's send asset is the settlement asset USDC",
				c.Key(), c.Send.Code)
		}
		if c.ReferenceBase != "USD" {
			t.Errorf("%s reference base = %s, want USD", c.Key(), c.ReferenceBase)
		}
		if c.ReferenceQuote != e.Peg {
			t.Errorf("%s is scored against %s but its issuer declares a %s peg; the benchmark must be the peg the token claims to track",
				c.Key(), c.ReferenceQuote, e.Peg)
		}
	}
}
