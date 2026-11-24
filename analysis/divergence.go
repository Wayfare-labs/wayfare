package analysis

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// DivergencePctSeries extracts the reference cross-check divergence recorded
// on each run, in the order given.
//
// A run's Reference.DivergencePct is empty exactly when the corridor was
// scored against a single provider (SINGLE agreement) — there was only one
// mid, so there was nothing to diverge from. That is a different fact from
// "the two providers agreed exactly" (0%), and folding an absent observation
// into a zero would understate how often, and how far, the benchmark has
// actually disagreed. So a run with no divergence recorded is omitted from
// the series rather than counted as zero — the project's unknown-is-never-
// zero rule, applied to a benchmark's own history rather than to a corridor's.
//
// A non-empty value that fails to parse as a decimal is a defect in the
// stored record, not a missing observation, and is reported as an error
// rather than silently dropped: a corrupt figure and an absent one are not
// the same failure, and treating the former as the latter would hide data
// corruption behind an ordinary "not enough history yet" result.
//
// A parsed value that is negative is the same class of defect. DivergencePct
// is produced by refrate.reconcile as hi.Sub(lo) of the two candidate mids —
// a magnitude, never signed — so a negative figure cannot be a legitimate
// observation. Accepting it would let a single corrupt record quietly pull
// the reported mean down, which is a worse failure than refusing to answer.
func DivergencePctSeries(recs []*runstore.Record) ([]decimal.Decimal, error) {
	points, err := DivergencePointSeries(recs)
	if err != nil {
		return nil, err
	}
	out := make([]decimal.Decimal, 0, len(points))
	for _, point := range points {
		out = append(out, point.Value)
	}
	return out, nil
}

// DivergencePointSeries extracts timestamped reference cross-check
// divergences, omitting runs where only one provider supplied a mid.
func DivergencePointSeries(recs []*runstore.Record) ([]Point, error) {
	out := make([]Point, 0, len(recs))
	for _, r := range recs {
		raw := r.Reference.DivergencePct
		if raw == "" {
			continue
		}
		v, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, fmt.Errorf(
				"analysis: corridor %s, run seq %d: parsing divergence_pct %q: %w",
				r.Corridor, r.Seq, raw, err)
		}
		if v.IsNegative() {
			return nil, fmt.Errorf(
				"analysis: corridor %s, run seq %d: divergence_pct %q is negative; "+
					"divergence is a magnitude and cannot be signed",
				r.Corridor, r.Seq, raw)
		}
		out = append(out, Point{At: r.RecordedAt, Value: v})
	}
	return out, nil
}

// DivergenceHistory summarises how far a corridor's two reference providers
// have disagreed across recs, given oldest first.
//
// This is a fact about the benchmark that produced a corridor's verdicts, not
// about the corridor's own execution — see docs/backlog.md, initiative D2,
// "track reference divergence over time". Like every analysis result it is
// undetermined below the documented minimum sample sizes and never
// backfilled with a zero for a run that had no divergence to report.
//
// The returned MetricStats.Regime is not meaningful for this series and
// should be ignored by callers: DefaultRegimeThresholds is calibrated to
// loss percentages (see AnalyzeDecimal), a completely different scale from
// reference-provider divergence, and applying it here would misclassify a
// benchmark on thresholds nobody chose for that purpose.
func DivergenceHistory(recs []*runstore.Record) (*MetricStats, error) {
	points, err := DivergencePointSeries(recs)
	if err != nil {
		return nil, err
	}
	return AnalyzeSeries(points, nil), nil
}
