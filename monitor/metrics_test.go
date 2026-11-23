package monitor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/metrics"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// Issue #705 from the scheduler's side: a sweep is the only thing that writes
// history, and wayfare_sweep_outcomes_total is how an operator learns a sweep
// went wrong. The three outcomes are documented as mutually exclusive, so these
// tests pin both halves of that: each failure mode raises its own counter, and
// raises it exactly once per corridor.
//
// Every Scheduler built here without a collector would panic on a nil pointer
// if the field were not inert, which is how the nil case stays covered without
// a test of its own.

// usdcNgnc is the corridor the snapshot fixtures can actually price.
var usdcNgnc = Corridor{
	Send: asset.USDC(), Receive: asset.NGNC(),
	ReferenceBase: "USD", ReferenceQuote: "NGN",
}

func ngnMid() decimal.Decimal { return decimal.RequireFromString("1350.2568") }

func kesMid() decimal.Decimal { return decimal.RequireFromString("129.4263") }

// sweepCounters renders a collector and returns the sample lines for one
// corridor. Reading the exposition rather than a getter is deliberate: the
// document is what an alert is built from.
func sweepCounters(t *testing.T, c *metrics.Collector, corridor string) []string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := c.WriteTo(&buf); err != nil {
		t.Fatalf("rendering the exposition: %v", err)
	}
	prefix := `wayfare_sweep_outcomes_total{corridor="` + corridor + `"`
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestSweepCountsACorridorAsRecorded is the ordinary case: one corridor
// measured and appended, one series, one observation, and it says `ok` rather
// than nothing at all.
func TestSweepCountsACorridorAsRecorded(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	counters := metrics.New()

	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.Metrics = counters

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	lines := sweepCounters(t, counters, "USDC-NGNC")
	if len(lines) != 1 {
		t.Fatalf("sweep counters for USDC-NGNC = %v, want one line", lines)
	}
	if want := `wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} 1`; lines[0] != want {
		t.Errorf("got %q, want %q", lines[0], want)
	}
}

// TestSweepCountsARecordTheStoreRefusedAsUnrecorded covers the outcome that
// only exists because its remedy differs: the measurement happened and the disk
// said no, so history has a hole the corridor did not cause. Folding it into
// `failed` would point an operator at the upstream when they should be looking
// at the volume.
//
// RunOnce keeps answering nil in this case, so the counter is the only signal
// that the sweep degraded — which is exactly why it needs its own label value.
func TestSweepCountsARecordTheStoreRefusedAsUnrecorded(t *testing.T) {
	realStore, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	counters := metrics.New()

	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568",
		&failingStore{Store: realStore, failCorridor: "USDC-NGNC"})
	s.Metrics = counters

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v (a storage failure must not fail the sweep for the caller)", err)
	}

	lines := sweepCounters(t, counters, "USDC-NGNC")
	if len(lines) != 1 {
		t.Fatalf("sweep counters for USDC-NGNC = %v, want one line", lines)
	}
	if want := `wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="unrecorded"} 1`; lines[0] != want {
		t.Errorf("got %q, want %q", lines[0], want)
	}

	// The other half of the same claim: `ok` was not also counted, because the
	// two outcomes are mutually exclusive by construction, not by convention.
	if rec, err := realStore.Latest(context.Background(), "USDC-NGNC"); err != nil {
		t.Fatalf("reading the store back: %v", err)
	} else if rec != nil {
		t.Errorf("a refused Append still recorded seq %d", rec.Seq)
	}
}

// TestSweepCountsARefusedIncompleteSnapshotAsFailed is the failure an operator
// alerts on, reached through the shape a real outage takes: snapshots are on,
// the upstream answers nothing usable, and the scheduler refuses to write a
// hollow record. Nothing was measured and nothing was recorded, so the count
// says `failed`.
func TestSweepCountsARefusedIncompleteSnapshotAsFailed(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	counters := metrics.New()

	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "NGNC"))
	t.Cleanup(srv.Close)

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": ngnMid(),
			}),
		},
		Store:     store,
		Corridors: []Corridor{usdcNgnc},
		Logger:    quiet(),
		Metrics:   counters,
	}
	// Snapshots on, so an unpriceable corridor is refused rather than
	// recorded. Without a recorder the same ladder lands in history as a
	// measured-looking empty run, which is the other test's subject.
	s.SnapshotDir = t.TempDir()
	s.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
		recorder.Transport = srv.Client().Transport
		return &route.Engine{
			DEX: &dex.Client{
				HorizonURL: srv.URL,
				HTTPClient: &http.Client{Transport: recorder},
			},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": ngnMid(),
			}),
		}
	}

	if err := s.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce: want an error when the only corridor failed to measure")
	}

	lines := sweepCounters(t, counters, "USDC-NGNC")
	if len(lines) != 1 {
		t.Fatalf("sweep counters for USDC-NGNC = %v, want one line", lines)
	}
	if want := `wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="failed"} 1`; lines[0] != want {
		t.Errorf("got %q, want %q", lines[0], want)
	}
}

// TestSweepCountsEachCorridorOncePerRun is the anti-double-count check. A
// corridor that fails is reported by measure and counted by RunOnce; a corridor
// that succeeds passes through the same single call site. Two sweeps must
// produce two observations of one series, never two series and never four.
func TestSweepCountsEachCorridorOncePerRun(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	counters := metrics.New()

	snap := loadSnapshot(t, "usdc-ngnc")
	srv := httptest.NewServer(failHandler(snap, "KESC"))
	t.Cleanup(srv.Close)

	corridors := []Corridor{
		usdcNgnc,
		{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
	}

	s := &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": ngnMid(),
				"USD/KES": kesMid(),
			}),
		},
		Store:     store,
		Corridors: corridors,
		Logger:    quiet(),
		Metrics:   counters,
	}

	for i := 0; i < 2; i++ {
		if err := s.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}

	var buf bytes.Buffer
	if _, err := counters.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} 2`,
		// A corridor whose upstream returns 500 is still a completed sweep:
		// the record exists and says UNKNOWN. `failed` is reserved for a sweep
		// with nothing to record, which is the distinction the two label
		// values exist to carry.
		`wayfare_sweep_outcomes_total{corridor="USDC-KESC",outcome="ok"} 2`,
	} {
		if !strings.Contains(buf.String(), want+"\n") {
			t.Errorf("exposition is missing %q:\n%s", want, buf.String())
		}
	}
	if lines := sweepCounters(t, counters, "USDC-NGNC"); len(lines) != 1 {
		t.Errorf("USDC-NGNC has %d counter lines after two sweeps, want 1: %v", len(lines), lines)
	}
}
