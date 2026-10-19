package runstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// measurabilityRecord builds a record shaped like the ones FromCorridorJSON
// and the scheduler write, with the fields the measurability reader consumes.
// The priced fixtures carry a full rung shape, so the count gate's second
// half (an effective rate must be present) is exercised by construction: a
// Priced=true rung with no rate must never count as a price.
func measurabilityRecord(seq int64, at time.Time, integrity string, rungs []Rung) *Record {
	if rungs == nil {
		rungs = []Rung{{SendAmount: "0.1", Priced: true, Integrity: integrity, EffectiveRate: "1350", LossPct: "3.57", Verdict: "UNUSABLE"}}
	}
	return &Record{
		Version:    Version,
		Seq:        seq,
		RecordedAt: at,
		Corridor:   "USDC-NGNC",
		Integrity:  integrity,
		DependsOn:  []string{},
		Reference: Reference{
			Mid:           "1400",
			Source:        "currency-api",
			ScoredAgainst: "currency-api",
		},
		FloorLossPct: "3.57",
		FloorSize:    "0.1",
		Rungs:        rungs,
	}
}

func pricedRung(amount string) Rung {
	return Rung{SendAmount: amount, Priced: true, Integrity: "DIRECT", EffectiveRate: "1350", LossPct: "3.57", Verdict: "UNUSABLE"}
}

func unpricedRung(amount string) Rung {
	return Rung{SendAmount: amount, Priced: false, Integrity: "NO-MARKET"}
}

// seedMeasurability builds a store with the given runs appended in order.
func seedMeasurability(t *testing.T, recs ...*Record) *FileStore {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, rec := range recs {
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// TestMeasurableNeedsOnePricedRung pins the issue's definition: measurable
// means the sweep produced a priced ladder, and a priced ladder means at
// least one priced rung — not all of them.
func TestMeasurableNeedsOnePricedRung(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	if !Measurable(measurabilityRecord(1, at, "DIRECT", []Rung{
		unpricedRung("0.1"), unpricedRung("1"), pricedRung("10"),
		unpricedRung("5000"),
	})) {
		t.Error("a ladder with one priced rung is measurable; the rest failing does not undo the price")
	}
	if Measurable(measurabilityRecord(2, at, "DIRECT", []Rung{
		unpricedRung("0.1"), unpricedRung("1"),
	})) {
		t.Error("a sweep with no priced rung is not measurable")
	}
	if !Measurable(measurabilityRecord(3, at, "DIRECT", nil)) {
		t.Error("a fully priced ladder is measurable")
	}
}

// TestMeasurableRejectsFlagWithoutRate pins the "an unavailable quantity is
// unknown, never zero" gate at the rung level: a rung flagged Priced whose
// effective rate is missing has no price, and counting it would let a
// corrupt record pose as a priced sweep.
func TestMeasurableRejectsFlagWithoutRate(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	rec := measurabilityRecord(1, at, "DIRECT", []Rung{{
		SendAmount: "0.1", Priced: true, Integrity: "DIRECT",
		// EffectiveRate deliberately absent: the flag lies.
	}})
	if Measurable(rec) {
		t.Error("a Priced rung with no effective rate is not a price; measurable must be false")
	}
}

// TestMeasurabilityHistoryClassifiesEveryRun pins the per-run shape, oldest
// first, with the NO-MARKET case the issue exists for: the sweep ran fine
// (it measured the corridor and found no market) and is still not
// measurable, because there was no price to measure with.
func TestMeasurabilityHistoryClassifiesEveryRun(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	store := seedMeasurability(t,
		measurabilityRecord(1, at(20, 12), "DIRECT", nil),
		// The case-study NO-MARKET sweep: prices nothing, yet is a
		// successful measurement of "no market exists".
		measurabilityRecord(2, at(20, 18), "NO-MARKET", []Rung{
			unpricedRung("0.1"), unpricedRung("5000"),
		}),
		// A partial ladder: first sizes failed, later ones priced.
		measurabilityRecord(3, at(21, 0), "DIRECT", []Rung{
			unpricedRung("0.1"), pricedRung("100"), pricedRung("5000"),
		}),
	)

	runs, err := MeasurabilityHistory(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}

	want := []struct {
		seq        int64
		integrity  string
		total      int
		priced     int
		measurable bool
	}{
		{1, "DIRECT", 1, 1, true},
		{2, "NO-MARKET", 2, 0, false},
		{3, "DIRECT", 3, 2, true},
	}
	for i, w := range want {
		got := runs[i]
		if got.Seq != w.seq {
			t.Errorf("run %d seq = %d, want %d (oldest first)", i, got.Seq, w.seq)
		}
		if got.Integrity != w.integrity {
			t.Errorf("run %d integrity = %q, want %q", i, got.Integrity, w.integrity)
		}
		if got.TotalRungs != w.total || got.PricedRungs != w.priced {
			t.Errorf("run %d rungs = %d/%d priced, want %d/%d", i, got.PricedRungs, got.TotalRungs, w.priced, w.total)
		}
		if got.Measurable != w.measurable {
			t.Errorf("run %d measurable = %v, want %v", i, got.Measurable, w.measurable)
		}
	}
}

// TestMeasurabilitySummaryUptime pins the uptime arithmetic and, with it,
// the difference between a measured zero and an unknown: three sweeps, one
// priced → 33.3…% uptime, with the window bounds carried so a reader can
// see what the figure stands on.
func TestMeasurabilitySummaryUptime(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	store := seedMeasurability(t,
		measurabilityRecord(1, at(20, 12), "DIRECT", nil),
		measurabilityRecord(2, at(20, 18), "NO-MARKET", []Rung{unpricedRung("0.1")}),
		measurabilityRecord(3, at(21, 0), "DIRECT", []Rung{unpricedRung("0.1"), pricedRung("10")}),
	)

	s, err := MeasurabilityOf(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected a summary over three recorded runs")
	}
	if s.Corridor != "USDC-NGNC" {
		t.Errorf("corridor = %q, want USDC-NGNC", s.Corridor)
	}
	if s.TotalRuns != 3 || s.MeasurableRuns != 2 || s.NotMeasurableRuns != 1 {
		t.Errorf("counts = %d/%d (+%d not), want 2/3 (+1 not)",
			s.MeasurableRuns, s.TotalRuns, s.NotMeasurableRuns)
	}
	// 200/3 = 66.6̄, repeating — exact decimal.Decimal division at the
	// package's default precision (16 digits), the same arithmetic the
	// movement reader uses; never a float64 rounding.
	if s.UptimePct != "66.6666666666666667" {
		t.Errorf("uptime_pct = %q, want the decimal quotient 66.6666666666666667, not a float rounding", s.UptimePct)
	}
	if !s.FirstRunAt.Equal(at(20, 12)) || !s.LastRunAt.Equal(at(21, 0)) {
		t.Errorf("window = %s…%s, want 20th 12:00…21st 00:00",
			s.FirstRunAt, s.LastRunAt)
	}
}

// TestMeasurabilityZeroIsMeasured pins the other half of the unknown rule:
// when every recorded sweep failed to price, uptime is a measured 0 — a
// finding about the corridor — not an unknown to be excused.
func TestMeasurabilityZeroIsMeasured(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	store := seedMeasurability(t,
		measurabilityRecord(1, at(20, 12), "NO-MARKET", []Rung{unpricedRung("0.1")}),
		measurabilityRecord(2, at(20, 18), "NO-MARKET", []Rung{unpricedRung("0.1")}),
	)

	s, err := MeasurabilityOf(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected a summary")
	}
	if s.UptimePct != "0" {
		t.Errorf("uptime_pct = %q, want \"0\": recorded sweeps with no price is a measured zero, not an unknown", s.UptimePct)
	}
	if s.MeasurableRuns != 0 || s.NotMeasurableRuns != 2 {
		t.Errorf("counts = %d measurable / %d not, want 0/2", s.MeasurableRuns, s.NotMeasurableRuns)
	}
}

// TestMeasurabilityUnknownWhenNoHistory pins the gap rule: with no recorded
// runs there is no uptime figure at all — nil, not zero. A corridor that
// was not measured is not a corridor that was never measurable.
func TestMeasurabilityUnknownWhenNoHistory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	s, err := MeasurabilityOf(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatalf("an empty history is not an error: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil summary with no history, got %+v", s)
	}

	runs, err := MeasurabilityHistory(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if runs == nil || len(runs) != 0 {
		t.Errorf("expected an empty (non-nil) run list, got %+v", runs)
	}
}

// TestSummarizeMeasurabilityIgnoresOrderAndBlankCorridorOk is a pure-function
// check: the summary depends only on the run series, so an out-of-order
// input still yields correct counts.
func TestSummarizeMeasurabilityIgnoresOrderAndBlankCorridorOk(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	runs := []RunMeasurability{
		{Seq: 2, RecordedAt: at, Measurable: false},
		{Seq: 1, RecordedAt: at.Add(-time.Hour), Measurable: true},
	}

	s := SummarizeMeasurability("USDC-NGNC", runs)
	if s == nil {
		t.Fatal("expected a summary")
	}
	if s.TotalRuns != 2 || s.MeasurableRuns != 1 || s.NotMeasurableRuns != 1 {
		t.Errorf("counts = %d/%d (+%d), want 1/2 (+1)", s.MeasurableRuns, s.TotalRuns, s.NotMeasurableRuns)
	}
	if s.UptimePct != "50" {
		t.Errorf("uptime_pct = %q, want \"50\"", s.UptimePct)
	}
}

// TestMeasurabilityWireShapePinned freezes the JSON keys of both wire
// shapes. A consumer reading uptime_pct or measurable must not be broken by
// a quiet tag rename — the same pin the movement and divergence readers
// carry.
func TestMeasurabilityWireShapePinned(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	raw, err := json.Marshal(RunMeasurability{
		Seq: 7, RecordedAt: at, Integrity: "NO-MARKET",
		TotalRungs: 12, PricedRungs: 0, Measurable: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantRun = `{"seq":7,"recorded_at":"2026-08-22T12:00:00Z","integrity":"NO-MARKET",` +
		`"total_rungs":12,"priced_rungs":0,"measurable":false}`
	if string(raw) != wantRun {
		t.Errorf("RunMeasurability wire shape changed:\n got  %s\n want %s", raw, wantRun)
	}

	raw, err = json.Marshal(&MeasurabilitySummary{
		Corridor:  "USDC-NGNC",
		TotalRuns: 4, MeasurableRuns: 3, NotMeasurableRuns: 1,
		UptimePct:  "75",
		FirstRunAt: at, LastRunAt: at.Add(18 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantSummary = `{"corridor":"USDC-NGNC","total_runs":4,"measurable_runs":3,` +
		`"not_measurable_runs":1,"uptime_pct":"75",` +
		`"first_run_at":"2026-08-22T12:00:00Z","last_run_at":"2026-08-23T06:00:00Z"}`
	if string(raw) != wantSummary {
		t.Errorf("MeasurabilitySummary wire shape changed:\n got  %s\n want %s", raw, wantSummary)
	}
}
