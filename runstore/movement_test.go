package runstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// movementRecord builds a record shaped like the ones FromCorridorJSON and
// the scheduler write, with the figures a movement comparison reads. The
// loss/rate/mid figures obey the measurement's own identity — the effective
// rate is what the corridor delivered per send unit, and the loss percentage
// is the shortfall against the mid it was scored against — so the fixtures
// are the same shape real storage produces, not figures derived from the
// code under test.
func movementRecord(seq int64, at time.Time, mid, rate, loss string) *Record {
	rec := &Record{
		Version:    Version,
		Seq:        seq,
		RecordedAt: at,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		DependsOn:  []string{},
		Reference: Reference{
			Mid:           mid,
			Source:        "currency-api",
			ScoredAgainst: "currency-api",
		},
		FloorLossPct: loss,
		FloorSize:    "0.1",
		Rungs: []Rung{{
			SendAmount:    "0.1",
			Priced:        true,
			Integrity:     "DIRECT",
			EffectiveRate: rate,
			LossPct:       loss,
			Verdict:       "UNUSABLE",
		}},
	}
	return rec
}

// seedMovement builds a store with the given runs appended in order.
func seedMovement(t *testing.T, recs ...*Record) *FileStore {
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

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("parsing %q as decimal: %v", s, err)
	}
	return d
}

// TestBenchmarkMovementCorridorOnly pins the case the issue exists for, one
// side of it: the benchmark held still while the corridor's delivered rate
// changed. All of the change must land on the corridor, none of it on the
// benchmark.
func TestBenchmarkMovementCorridorOnly(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	// The corridor's rate moved 1350 → 1300 (a 3.70…% fall in what it
	// delivers per unit sent); the benchmark mid held at 1400.
	store := seedMovement(t,
		movementRecord(1, at(20, 12), "1400", "1350", "3.57"),
		movementRecord(2, at(21, 12), "1400", "1300", "7.14"),
	)

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a movement between the two runs")
	}

	if m.BenchmarkPct != "0" {
		t.Errorf("BenchmarkPct = %q, want \"0\": both runs were scored and the benchmark measured still — zero is the measured figure, not a placeholder", m.BenchmarkPct)
	}
	wantCorridor := decimal.RequireFromString("-50").Div(decimal.RequireFromString("1350")).Mul(decimal.NewFromInt(100))
	if got := mustDecimal(t, m.CorridorPct); !got.Equal(wantCorridor) {
		t.Errorf("CorridorPct = %s, want %s", got, wantCorridor)
	}
	wantChange := decimal.RequireFromString("3.57")
	if got := mustDecimal(t, m.LossPctChange); !got.Equal(wantChange) {
		t.Errorf("LossPctChange = %s, want %s", got, wantChange)
	}
	if m.CorridorFrom != "1350" || m.CorridorTo != "1300" {
		t.Errorf("corridor rates = %q → %q, want 1350 → 1300", m.CorridorFrom, m.CorridorTo)
	}
	if m.BenchmarkFrom != "1400" || m.BenchmarkTo != "1400" {
		t.Errorf("benchmark mids = %q → %q, want 1400 → 1400", m.BenchmarkFrom, m.BenchmarkTo)
	}
	if m.PreviousRunAt != at(20, 12) || m.CurrentRunAt != at(21, 12) {
		t.Errorf("run stamps = %s → %s, want the two records' RecordedAt values",
			m.PreviousRunAt, m.CurrentRunAt)
	}
}

// TestBenchmarkMovementBenchmarkOnly pins the other side: the corridor's
// delivered rate held still while the benchmark moved. The headline changed
// because the yardstick did, and the figures must say exactly that.
func TestBenchmarkMovementBenchmarkOnly(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	// The corridor delivered 1300 both days; the benchmark moved
	// 1350 → 1400, a 3.70…% rise.
	store := seedMovement(t,
		movementRecord(1, at(20, 12), "1350", "1300", "3.70"),
		movementRecord(2, at(21, 12), "1400", "1300", "7.14"),
	)

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a movement between the two runs")
	}

	if m.CorridorPct != "0" {
		t.Errorf("CorridorPct = %q, want \"0\": both runs priced and the corridor's rate measured still — zero is the measured figure, not a placeholder", m.CorridorPct)
	}
	wantBench := decimal.RequireFromString("50").Div(decimal.RequireFromString("1350")).Mul(decimal.NewFromInt(100))
	if got := mustDecimal(t, m.BenchmarkPct); !got.Equal(wantBench) {
		t.Errorf("BenchmarkPct = %s, want %s", got, wantBench)
	}
	wantChange := decimal.RequireFromString("3.44")
	if got := mustDecimal(t, m.LossPctChange); !got.Equal(wantChange) {
		t.Errorf("LossPctChange = %s, want %s", got, wantChange)
	}
}

// TestBenchmarkMovementUnscoredBenchmark pins the unknown-not-zero rule where
// it bites hardest: the first run's reference was never scorable, so there is
// no benchmark yardstick for the pair and BenchmarkPct must be the explicit
// unknown, never zero, never a default.
func TestBenchmarkMovementUnscoredBenchmark(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	unscorable := movementRecord(1, at(20, 12), "", "1300", "")
	unscorable.Reference = Reference{Mid: "1350", Source: "currency-api"}
	unscorable.Rungs[0].LossPct = ""
	unscorable.FloorLossPct = ""
	unscorable.FloorSize = ""

	store := seedMovement(t, unscorable, movementRecord(2, at(21, 12), "1400", "1300", "7.14"))

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a movement between the two runs")
	}

	if m.BenchmarkPct != "" {
		t.Errorf("BenchmarkPct = %q, want empty: the earlier run was scored against nothing, so the benchmark's move is unknown, never zero", m.BenchmarkPct)
	}
	if m.BenchmarkFrom != "" {
		t.Errorf("BenchmarkFrom = %q, want empty: an unscored rate is not a benchmark", m.BenchmarkFrom)
	}
	if m.BenchmarkTo != "1400" {
		t.Errorf("BenchmarkTo = %q, want 1400", m.BenchmarkTo)
	}
	if m.CorridorPct == "" {
		t.Error("CorridorPct must still be reported: both runs priced")
	}
	if m.LossPctChange != "" {
		t.Errorf("LossPctChange = %q, want empty: the unpriced first run has no loss figure", m.LossPctChange)
	}
}

// TestBenchmarkMovementNoPrices pins that unpriced runs contribute no
// corridor figure: there is no price, so there is nothing to compare, and
// synthesising one from an unpriced curve is the one thing this must never
// do. The benchmark's measured stillness is still reported — both runs were
// scored, and a held benchmark is a fact.
func TestBenchmarkMovementNoPrices(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	noMarket := func(seq int64, when time.Time) *Record {
		rec := movementRecord(seq, when, "1400", "", "")
		rec.Rungs[0] = Rung{SendAmount: "0.1", Priced: false, Integrity: "NO-MARKET"}
		return rec
	}
	store := seedMovement(t, noMarket(1, at(20, 12)), noMarket(2, at(21, 12)))

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a movement: the benchmark was measured on both runs")
	}
	if m.BenchmarkPct != "0" {
		t.Errorf("BenchmarkPct = %q, want \"0\" (measured, both runs scored)", m.BenchmarkPct)
	}
	if m.CorridorPct != "" {
		t.Errorf("CorridorPct = %q, want empty: neither run priced, so the corridor's movement is unknown", m.CorridorPct)
	}
	if m.CorridorFrom != "" || m.CorridorTo != "" {
		t.Errorf("corridor rates = %q → %q, want both empty: an unpriced run has no rate", m.CorridorFrom, m.CorridorTo)
	}
	if m.LossPctChange != "" {
		t.Errorf("LossPctChange = %q, want empty: no loss was measured on an unpriced curve", m.LossPctChange)
	}
}

// TestBenchmarkMovementBothMoved pins the arithmetic when both sides move:
// each percentage is measured against its own earlier figure, so neither can
// borrow the other's movement, and the stored wire shape round-trips.
func TestBenchmarkMovementBothMoved(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	// Corridor 1350 → 1320 (its own −2.22…%), benchmark 1400 → 1414
	// (its own +1%).
	store := seedMovement(t,
		movementRecord(1, at(20, 12), "1400", "1350", "3.57"),
		movementRecord(2, at(21, 12), "1414", "1320", "6.82"),
	)

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected a movement between the two runs")
	}

	wantCorridor := decimal.RequireFromString("-30").Div(decimal.RequireFromString("1350")).Mul(decimal.NewFromInt(100))
	if got := mustDecimal(t, m.CorridorPct); !got.Equal(wantCorridor) {
		t.Errorf("CorridorPct = %s, want %s", got, wantCorridor)
	}
	wantBench := decimal.RequireFromString("14").Div(decimal.RequireFromString("1400")).Mul(decimal.NewFromInt(100))
	if got := mustDecimal(t, m.BenchmarkPct); !got.Equal(wantBench) {
		t.Errorf("BenchmarkPct = %s, want %s (exactly 1, since 1414 rose 1%% over 1400)", got, wantBench)
	}
	if m.BenchmarkPct != "1" {
		t.Errorf("BenchmarkPct = %q, want \"1\" — the benchmark rose exactly 1%%", m.BenchmarkPct)
	}
}

// TestBenchmarkMovementFullHistory pins that BenchmarkMovement walks the
// whole chain oldest-pair-first and reports one movement per consecutive
// pair — the longitudinal shape the trend endpoint will serve.
func TestBenchmarkMovementFullHistory(t *testing.T) {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	store := seedMovement(t,
		movementRecord(1, at(20, 9), "1400", "1350", "3.57"),
		movementRecord(2, at(20, 15), "1414", "1350", "5.86"),
		movementRecord(3, at(20, 21), "1414", "1300", "9.53"),
	)

	movements, err := BenchmarkMovement(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(movements) != 2 {
		t.Fatalf("got %d movements, want 2 (one per consecutive pair)", len(movements))
	}

	// Oldest pair first.
	if movements[0].BenchmarkPct != "1" {
		t.Errorf("pair 1 BenchmarkPct = %q, want \"1\"", movements[0].BenchmarkPct)
	}
	if movements[0].CorridorPct != "0" {
		t.Errorf("pair 1 CorridorPct = %q, want \"0\": the corridor held at 1350 (measured)", movements[0].CorridorPct)
	}
	if movements[1].BenchmarkPct != "0" {
		t.Errorf("pair 2 BenchmarkPct = %q, want \"0\": the benchmark held at 1414 (measured)", movements[1].BenchmarkPct)
	}
	if movements[1].CorridorPct == "" {
		t.Error("pair 2 CorridorPct must be reported: the corridor moved 1350 → 1300")
	}
}

// TestBenchmarkMovedNeedsTwoRecords pins that a history too short to compare
// is the answer "nothing to compare yet", not an error.
func TestBenchmarkMovedNeedsTwoRecords(t *testing.T) {
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seedMovement(t, movementRecord(1, at, "1400", "1350", "3.57"))

	m, err := BenchmarkMoved(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatalf("a single record is not an error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil with one record, got %+v", m)
	}

	movements, err := BenchmarkMovement(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatalf("a single record is not an error: %v", err)
	}
	if movements != nil {
		t.Errorf("expected nil with one record, got %+v", movements)
	}
}

// TestMovementWireShapePinned freezes the JSON keys of Movement. A wire
// consumer reading benchmark_pct or corridor_pct must not be broken by a
// quiet tag rename.
func TestMovementWireShapePinned(t *testing.T) {
	m := &Movement{
		Corridor:      "USDC-NGNC",
		PreviousRunAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		CurrentRunAt:  time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		BenchmarkFrom: "1400",
		BenchmarkTo:   "1414",
		CorridorFrom:  "1350",
		CorridorTo:    "1320",
		BenchmarkPct:  "1",
		CorridorPct:   "-2.222222222222222222222222222222",
		LossPctChange: "3.25",
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"corridor":"USDC-NGNC",` +
		`"previous_run_at":"2026-08-20T12:00:00Z","current_run_at":"2026-08-21T12:00:00Z",` +
		`"benchmark_from":"1400","benchmark_to":"1414",` +
		`"corridor_from":"1350","corridor_to":"1320",` +
		`"benchmark_pct":"1","corridor_pct":"-2.222222222222222222222222222222",` +
		`"loss_pct_change":"3.25"}`
	if string(raw) != want {
		t.Errorf("Movement wire shape changed:\n got  %s\n want %s", raw, want)
	}
}
