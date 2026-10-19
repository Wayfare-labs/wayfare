package runstore

import (
	"testing"
	"time"
)

// TestRunMeasurabilityOfClassifiesLikeHistory pins that the record-level
// constructor and the history reader classify a run identically (#638): the
// trend wire reads records with this constructor while the history reader
// walks the chain, so a disagreement would make the same stored run measurable
// in one view and not the other.
func TestRunMeasurabilityOfMatchesHistory(t *testing.T) {
	rec := &Record{
		Seq:        7,
		RecordedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Integrity:  "DERIVATIVE",
		Rungs: []Rung{
			{SendAmount: "10", Priced: true, EffectiveRate: "1300"},
			{SendAmount: "100", Priced: true, EffectiveRate: ""}, // priced claim with no rate: not a price
			{SendAmount: "500", Priced: false},
		},
	}

	got := RunMeasurabilityOf(rec)
	if got.Seq != 7 {
		t.Errorf("seq = %d, want 7", got.Seq)
	}
	if !got.RecordedAt.Equal(rec.RecordedAt) {
		t.Errorf("recorded_at = %v, want %v", got.RecordedAt, rec.RecordedAt)
	}
	if got.Integrity != "DERIVATIVE" {
		t.Errorf("integrity = %q, want %q", got.Integrity, "DERIVATIVE")
	}
	if got.TotalRungs != 3 {
		t.Errorf("total_rungs = %d, want 3", got.TotalRungs)
	}
	if got.PricedRungs != 1 {
		t.Errorf("priced_rungs = %d, want 1 (the empty rate must not count)", got.PricedRungs)
	}
	if !got.Measurable {
		t.Error("a record with one genuinely priced rung must be measurable")
	}
}

// TestRunMeasurabilityOfNoMarketIsNotMeasurable pins the taxonomy the issue
// turns on: a NO-MARKET sweep ran and found no price, so it is recorded — and
// still not measurable. Uptime of the measurement is not "did the sweep run".
func TestRunMeasurabilityOfNoMarketIsNotMeasurable(t *testing.T) {
	got := RunMeasurabilityOf(&Record{
		Seq:       1,
		Integrity: "NO-MARKET",
		Rungs: []Rung{
			{SendAmount: "10", Priced: false},
			{SendAmount: "100", Priced: false},
		},
	})
	if got.Measurable {
		t.Error("a NO-MARKET record must not be measurable")
	}
	if got.TotalRungs != 2 || got.PricedRungs != 0 {
		t.Errorf("total=%d priced=%d, want 2 and 0", got.TotalRungs, got.PricedRungs)
	}
}
