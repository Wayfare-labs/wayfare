// Package runstore keeps a tamper-evident history of corridor measurements.
//
// This file implements measurability uptime: how often a corridor was
// measurable at all. Uptime of the measurement, not of the service — how
// many scheduled sweeps produced a priced ladder, regardless of whether that
// ladder was worth taking.
//
// The distinction is the corridor taxonomy itself. A DIRECT corridor that
// prices badly is measurable: Horizon answered and a ladder was priced. A
// NO-MARKET corridor is not: Horizon answered, but the answer is that no
// path exists, so there is no price and no loss figure to analyse — the
// sweep ran fine and produced nothing measurable. Reporting such a sweep as
// measurable would let a corridor with no market pose as a priced one;
// reporting the service's HTTP uptime as the corridor's would let a server
// that is up while Horizon is down pose as a working measurement. Neither
// figure answers the other's question.
package runstore

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// RunMeasurability is one stored run's measurability: whether that sweep
// produced a priced ladder, and how much of the ladder priced.
type RunMeasurability struct {
	// Seq is the record's position in the corridor's chain.
	Seq int64 `json:"seq"`

	// RecordedAt is when the sweep was recorded.
	RecordedAt time.Time `json:"recorded_at"`

	// Integrity is the record's stored integrity state — DIRECT,
	// DERIVATIVE, NO-MARKET or UNKNOWN — so a reader can tell the two
	// not-measurable shapes apart: NO-MARKET measured the corridor and
	// found no market; UNKNOWN learned nothing (an upstream outage).
	Integrity string `json:"integrity"`

	// TotalRungs is how many ladder sizes the sweep attempted.
	TotalRungs int `json:"total_rungs"`

	// PricedRungs is how many of those sizes produced a price. A run may
	// price some sizes and not others (a partial ladder), so this is
	// carried per run rather than folded into the boolean.
	PricedRungs int `json:"priced_rungs"`

	// Measurable is whether the sweep produced a priced ladder at all —
	// the issue's definition: at least one priced rung. A NO-MARKET sweep
	// ran fine and is still not measurable, because there was no price to
	// measure with.
	Measurable bool `json:"measurable"`
}

// MeasurabilitySummary is the uptime figure over a set of runs: how many
// scheduled sweeps produced a priced ladder.
//
// The counts are the primary figure. UptimePct is derived from them for
// convenience and is computed in decimal.Decimal; it is present only when
// the counts back it, never as a stand-in.
type MeasurabilitySummary struct {
	// Corridor is the corridor key (e.g. "USDC-NGNC").
	Corridor string `json:"corridor"`

	// TotalRuns is how many recorded runs the figure covers. It is the
	// denominator of the uptime figure and is always present: a reader
	// must be able to see what the percentage stands on.
	//
	// It counts recorded runs only. A sweep that died before writing a
	// record — the process crashed, the writer failed — left nothing in
	// the chain and cannot be counted; that absence is #118's
	// not-measured-versus-could-not-be distinction, and pretending a
	// missing record was a failed sweep would be synthesis.
	TotalRuns int `json:"total_runs"`

	// MeasurableRuns is how many of those runs produced a priced ladder.
	MeasurableRuns int `json:"measurable_runs"`

	// NotMeasurableRuns is TotalRuns − MeasurableRuns, carried so a
	// reader never has to subtract. A measured zero here is a finding:
	// every sweep recorded, and none produced a price.
	NotMeasurableRuns int `json:"not_measurable_runs"`

	// UptimePct is measurable_runs / total_runs × 100, a decimal string.
	// A measured "0" (no sweep ever priced) and a measured "100" are both
	// real figures. It is empty only when TotalRuns is zero — no recorded
	// history, so uptime is unknown, never zero: the corridor was not
	// measured, which is a different fact from never being measurable.
	UptimePct string `json:"uptime_pct,omitempty"`

	// FirstRunAt and LastRunAt bound the window the figure covers, so a
	// reader can tell "0% over three days" from "0% over an hour".
	FirstRunAt time.Time `json:"first_run_at"`
	LastRunAt  time.Time `json:"last_run_at"`
}

// Measurable reports whether a single stored run produced a priced ladder —
// the issue's definition of measurable: at least one rung priced.
//
// The priced test gates on both the stored Priced flag and the presence of
// an effective rate, exactly as the movement reader does: a NO-MARKET
// record stores "0.00" figures for its unpriced curve, and those zeros must
// never read as a price.
func Measurable(r *Record) bool {
	return countPricedRungs(r) > 0
}

// MeasurabilityOf summarises how often a corridor was measurable across its
// whole recorded history.
//
// It reads the full chain rather than Recent's tail for the same reason
// DetectTransitions and BenchmarkMovement do: an uptime figure over "the
// last 100 runs" is a different figure from an uptime over the chain, and a
// caller that wants a bounded window can pass the runs it wants to
// SummarizeMeasurability directly.
//
// A nil result with no error means the corridor has no recorded history —
// uptime unknown, never zero. The result is derived on read and appends
// nothing: not a single hash in the chain depends on it.
func MeasurabilityOf(ctx context.Context, store Store, corridor string) (*MeasurabilitySummary, error) {
	runs, err := MeasurabilityHistory(ctx, store, corridor)
	if err != nil {
		return nil, err
	}
	return SummarizeMeasurability(corridor, runs), nil
}

// MeasurabilityHistory classifies every stored run for a corridor, oldest
// first, so a reader can see the measurable/unmeasurable pattern over time
// and not only the aggregate.
//
// An empty history returns an empty slice, not an error: a missing history
// is the answer, not a failure.
func MeasurabilityHistory(ctx context.Context, store Store, corridor string) ([]RunMeasurability, error) {
	records, err := store.All(ctx, corridor)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}
	runs := make([]RunMeasurability, 0, len(records))
	for _, rec := range records {
		priced := countPricedRungs(rec)
		runs = append(runs, RunMeasurability{
			Seq:         rec.Seq,
			RecordedAt:  rec.RecordedAt,
			Integrity:   rec.Integrity,
			TotalRungs:  len(rec.Rungs),
			PricedRungs: priced,
			Measurable:  priced > 0,
		})
	}
	return runs, nil
}

// SummarizeMeasurability reduces a run series to the uptime figure. It is
// pure: no I/O, no clock, so a caller can summarise any window — the whole
// chain, the last week, the runs since a deploy.
//
// A nil return means the input establishes no uptime at all: no runs, so
// the corridor's measurability is unknown, never zero.
func SummarizeMeasurability(corridor string, runs []RunMeasurability) *MeasurabilitySummary {
	if len(runs) == 0 {
		return nil
	}

	s := &MeasurabilitySummary{
		Corridor:   corridor,
		TotalRuns:  len(runs),
		FirstRunAt: runs[0].RecordedAt,
		LastRunAt:  runs[len(runs)-1].RecordedAt,
	}
	for i := range runs {
		if runs[i].Measurable {
			s.MeasurableRuns++
		} else {
			s.NotMeasurableRuns++
		}
	}

	// Decimal only: this is a percentage a reader may compare against the
	// verdict bands, and float64 has no place near those figures.
	num := decimal.NewFromInt(int64(s.MeasurableRuns)).Mul(decimal.NewFromInt(100))
	den := decimal.NewFromInt(int64(s.TotalRuns))
	s.UptimePct = num.Div(den).String()
	return s
}

// countPricedRungs counts the rungs of a record that produced a price. The
// double gate mirrors storedEffectiveRate: Priced alone is trusted only
// when the rate it claims is actually there.
func countPricedRungs(r *Record) int {
	n := 0
	for i := range r.Rungs {
		if r.Rungs[i].Priced && r.Rungs[i].EffectiveRate != "" {
			n++
		}
	}
	return n
}
