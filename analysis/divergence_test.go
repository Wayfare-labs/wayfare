package analysis

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// divRecord builds a minimal record carrying only what DivergencePctSeries
// reads, at a given sequence number so ordering is easy to assert.
func divRecord(seq int64, divergencePct string) *runstore.Record {
	return &runstore.Record{
		Seq:      seq,
		Corridor: "USDC-NGNC",
		Reference: runstore.Reference{
			DivergencePct: divergencePct,
		},
	}
}

func TestDivergencePctSeriesSkipsRunsWithNoDivergence(t *testing.T) {
	recs := []*runstore.Record{
		divRecord(1, "1.50"),
		divRecord(2, ""), // SINGLE agreement: nothing to diverge from
		divRecord(3, "2.25"),
	}

	got, err := DivergencePctSeries(recs)
	if err != nil {
		t.Fatalf("DivergencePctSeries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (the empty entry must be skipped, not zero-filled)", len(got))
	}
	if got[0].String() != "1.5" || got[1].String() != "2.25" {
		t.Errorf("got %v, want [1.5 2.25] in order", got)
	}
}

func TestDivergencePctSeriesPreservesOrder(t *testing.T) {
	recs := []*runstore.Record{
		divRecord(1, "0.10"),
		divRecord(2, "0.20"),
		divRecord(3, "0.30"),
	}
	got, err := DivergencePctSeries(recs)
	if err != nil {
		t.Fatalf("DivergencePctSeries: %v", err)
	}
	want := []string{"0.1", "0.2", "0.3"}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestDivergencePctSeriesEmptyInputIsEmptyNotNil(t *testing.T) {
	got, err := DivergencePctSeries(nil)
	if err != nil {
		t.Fatalf("DivergencePctSeries(nil): %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want an empty (non-nil) slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestDivergencePctSeriesRejectsMalformedValue pins that a stored figure that
// fails to parse is reported as an error, not silently skipped like an
// absent one. A corrupt record and a missing observation are different
// failures, and collapsing them would hide the corruption.
func TestDivergencePctSeriesRejectsMalformedValue(t *testing.T) {
	recs := []*runstore.Record{
		divRecord(1, "1.50"),
		divRecord(7, "not-a-number"),
	}
	_, err := DivergencePctSeries(recs)
	if err == nil {
		t.Fatal("expected an error for an unparseable divergence_pct")
	}
	for _, want := range []string{"USDC-NGNC", "7", "not-a-number"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q, which a reader needs to find the bad record", err, want)
		}
	}
}

// TestDivergencePctSeriesRejectsNegativeValue covers the same defect class as
// TestDivergencePctSeriesRejectsMalformedValue: DivergencePct is a magnitude
// (see refrate.reconcile, which computes it as hi.Sub(lo) of the larger mid
// minus the smaller), so a negative figure cannot be a legitimate
// observation and must not be allowed to quietly pull a reported mean down.
func TestDivergencePctSeriesRejectsNegativeValue(t *testing.T) {
	recs := []*runstore.Record{
		divRecord(1, "1.50"),
		divRecord(9, "-0.50"),
	}
	_, err := DivergencePctSeries(recs)
	if err == nil {
		t.Fatal("expected an error for a negative divergence_pct")
	}
	for _, want := range []string{"USDC-NGNC", "9", "-0.50"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q, which a reader needs to find the bad record", err, want)
		}
	}
}

// TestDivergenceHistoryRejectsNegativeValue pins that DivergenceHistory
// propagates the same rejection rather than letting a negative figure reach
// AnalyzeDecimal and skew the mean.
func TestDivergenceHistoryRejectsNegativeValue(t *testing.T) {
	recs := []*runstore.Record{divRecord(1, "-1.00")}
	if _, err := DivergenceHistory(recs); err == nil {
		t.Fatal("expected DivergenceHistory to reject a negative divergence_pct")
	}
}

// TestDivergenceHistoryUndeterminedBelowMinimumSample covers the documented
// minimum-sample-size discipline this package already enforces for every
// other series: a handful of runs must not produce a mean and stddev that
// look precise but are not meaningful.
func TestDivergenceHistoryUndeterminedBelowMinimumSample(t *testing.T) {
	recs := make([]*runstore.Record, 0, 10)
	for i := int64(1); i <= 10; i++ {
		recs = append(recs, divRecord(i, "1.00"))
	}

	stats, err := DivergenceHistory(recs)
	if err != nil {
		t.Fatalf("DivergenceHistory: %v", err)
	}
	if !stats.Undetermined {
		t.Error("expected undetermined with only 10 divergence observations")
	}
	if stats.ObservationCount != 10 {
		t.Errorf("ObservationCount = %d, want 10", stats.ObservationCount)
	}
	if stats.Mean != nil {
		t.Error("Mean should be nil when undetermined")
	}
}

// TestDivergenceHistoryCountsOnlyDivergenceBearingRuns is the sharpest case:
// a corridor with plenty of total runs but few where divergence was actually
// measured must be judged on the smaller number. An unmeasured divergence is
// unknown, never zero, so it must not pad the sample toward the threshold.
func TestDivergenceHistoryCountsOnlyDivergenceBearingRuns(t *testing.T) {
	recs := make([]*runstore.Record, 0, 40)
	for i := int64(1); i <= 25; i++ {
		recs = append(recs, divRecord(i, "1.00")) // only 25 with divergence
	}
	for i := int64(26); i <= 40; i++ {
		recs = append(recs, divRecord(i, "")) // 15 SINGLE-provider runs
	}

	stats, err := DivergenceHistory(recs)
	if err != nil {
		t.Fatalf("DivergenceHistory: %v", err)
	}
	if stats.ObservationCount != 25 {
		t.Errorf("ObservationCount = %d, want 25 (40 total runs, only 25 carry a divergence)", stats.ObservationCount)
	}
	// 25 < MinSampleSizeForMeanStdDev (30), even though 40 total runs exist.
	if !stats.Undetermined {
		t.Error("expected undetermined: 25 divergence observations is below the mean/stddev minimum of 30")
	}
}

// TestDivergenceHistoryDeterminedAboveMinimumSample exercises the case with
// enough observations for a mean, stddev and trend, using a plainly
// increasing series so a widening benchmark disagreement is reported as
// worsening — the scenario the issue names explicitly.
func TestDivergenceHistoryDeterminedAboveMinimumSample(t *testing.T) {
	recs := make([]*runstore.Record, 0, 60)
	for i := int64(1); i <= 60; i++ {
		// 0.5, 1.0, ... 30.0 -- slope ~0.5 per observation, well past the
		// 0.1-per-observation dead zone computeTrend applies.
		pct := decimal.NewFromInt(i).Div(decimal.NewFromInt(2)).StringFixed(2)
		recs = append(recs, divRecord(i, pct))
	}

	stats, err := DivergenceHistory(recs)
	if err != nil {
		t.Fatalf("DivergenceHistory: %v", err)
	}
	if stats.Undetermined {
		t.Fatalf("expected a determined result with 60 observations, reason: %s", stats.Reason)
	}
	if stats.Mean == nil || stats.StdDev == nil {
		t.Fatal("expected Mean and StdDev to be populated")
	}
	if stats.Trend == nil {
		t.Fatal("expected a trend with 60 observations (the trend minimum)")
	}
	if stats.Trend.Direction != TrendWorsening {
		t.Errorf("Trend.Direction = %s, want %s for a steadily increasing divergence series",
			stats.Trend.Direction, TrendWorsening)
	}
}

func TestDivergenceHistoryPropagatesParseError(t *testing.T) {
	recs := []*runstore.Record{divRecord(1, "garbage")}
	if _, err := DivergenceHistory(recs); err == nil {
		t.Fatal("expected DivergenceHistory to propagate the parse error rather than swallow it")
	}
}

// TestDivergenceHistoryNoObservationsIsUndeterminedNotError distinguishes
// "no divergence was ever measured" (every run was SINGLE) from a parse
// failure: the former is a legitimate, reportable state, not an error.
func TestDivergenceHistoryNoObservationsIsUndeterminedNotError(t *testing.T) {
	recs := []*runstore.Record{divRecord(1, ""), divRecord(2, "")}
	stats, err := DivergenceHistory(recs)
	if err != nil {
		t.Fatalf("DivergenceHistory: %v", err)
	}
	if !stats.Undetermined || stats.ObservationCount != 0 {
		t.Errorf("stats = %+v, want Undetermined with ObservationCount 0", stats)
	}
}
