// Package runstore keeps a tamper-evident history of corridor measurements.
//
// This file reads path concentration over time: whether a corridor's
// executable value is concentrating into fewer paths, from the
// fragmentation.paths metric each run recorded in its metrics block.
//
// Nothing here measures anything. The metric was measured when the run was
// taken (checks.FragmentationMetric) and stored word-for-word with it; this
// reader only lines the stored figures up oldest-first and compares the
// first comparable one with the last. A run that did not record the metric,
// or recorded it as undetermined, stays in the series as unknown with its
// reason — it is never dropped to make the series look continuous, and never
// read as a zero.
package runstore

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/checks"
)

// Fragmentation directions. They name the sign of the change in the stored
// HHI between the first and the last comparable reading, and nothing more:
// no threshold, no trend fit, no claim about anything between the two.
const (
	// FragmentationConcentrating means the HHI rose: the same alternatives
	// pay more unevenly, or fewer of them pay at all.
	FragmentationConcentrating = "CONCENTRATING"

	// FragmentationDispersing means the HHI fell.
	FragmentationDispersing = "DISPERSING"

	// FragmentationUnchanged means the two readings are equal.
	FragmentationUnchanged = "UNCHANGED"
)

// RunFragmentation is one stored run's path-concentration reading.
type RunFragmentation struct {
	// Seq is the record's position in the corridor's chain.
	Seq int64 `json:"seq"`

	// RecordedAt is when the sweep was recorded.
	RecordedAt time.Time `json:"recorded_at"`

	// Determined is whether this run holds a usable HHI. False covers a run
	// that never measured it (no fragmentation.paths metric in its record —
	// every record written before the metric existed, or by a sweep that
	// did not register it) and a run that measured it and could not
	// determine it; Reason says which.
	Determined bool `json:"determined"`

	// HHI is the stored value as a decimal string, present only when
	// Determined. It is the figure as written, not re-derived.
	HHI string `json:"hhi,omitempty"`

	// Source is the stored evidence source — endpoint, pair and probed
	// size. Two readings are comparable only when their sources match: a
	// reading at 100 USDC and one at 5000 answer different questions.
	Source string `json:"source,omitempty"`

	// Reason explains an undetermined reading. Empty when Determined.
	Reason string `json:"reason,omitempty"`
}

// FragmentationSummary compares the first and last comparable readings in a
// run series.
//
// The counts are always present so a reader can see what the comparison
// stands on. Every comparison field is empty, never zero, when fewer than two
// comparable readings exist: a change that could not be computed is unknown,
// and a "0" there would read as a measured lack of change.
type FragmentationSummary struct {
	// Corridor is the corridor key (e.g. "USDC-NGNC").
	Corridor string `json:"corridor"`

	// TotalRuns is how many recorded runs the series covers.
	TotalRuns int `json:"total_runs"`

	// DeterminedRuns is how many of them hold a usable HHI, from any source.
	DeterminedRuns int `json:"determined_runs"`

	// UnknownRuns is TotalRuns − DeterminedRuns, carried so a reader never
	// has to subtract.
	UnknownRuns int `json:"unknown_runs"`

	// Source is the observation the comparison is made on: the latest
	// determined reading's source. Empty when no run is determined.
	Source string `json:"source,omitempty"`

	// ComparableRuns is how many determined readings share Source. Readings
	// from another source (a different size or pair) are counted in
	// DeterminedRuns and excluded here.
	ComparableRuns int `json:"comparable_runs"`

	// FirstSeq, FirstAt and FirstHHI are the earliest comparable reading.
	// All three are absent, never zero-valued, when no reading is
	// determined: a 0001-01-01 timestamp would be a date nobody measured.
	FirstSeq int64     `json:"first_seq,omitempty"`
	FirstAt  time.Time `json:"first_at,omitzero"`
	FirstHHI string    `json:"first_hhi,omitempty"`

	// LastSeq, LastAt and LastHHI are the latest comparable reading.
	LastSeq int64     `json:"last_seq,omitempty"`
	LastAt  time.Time `json:"last_at,omitzero"`
	LastHHI string    `json:"last_hhi,omitempty"`

	// ChangeHHI is LastHHI − FirstHHI as a decimal string. Positive is
	// concentrating. Empty when fewer than two comparable readings exist.
	ChangeHHI string `json:"change_hhi,omitempty"`

	// Direction names the sign of ChangeHHI, one of the Fragmentation*
	// constants. Empty when ChangeHHI is.
	Direction string `json:"direction,omitempty"`

	// Reason explains an empty Direction.
	Reason string `json:"reason,omitempty"`
}

// FragmentationOfRecord reads one stored run's fragmentation.paths metric.
//
// It never computes a value: a record without the metric is reported as not
// measured, an undetermined metric carries its stored reason, and a stored
// value that does not parse as a ratio in (0, 1] is reported as unusable
// rather than repaired.
func FragmentationOfRecord(r *Record) RunFragmentation {
	out := RunFragmentation{Seq: r.Seq, RecordedAt: r.RecordedAt}

	var found []checks.MetricJSON
	for _, m := range r.Metrics {
		if m.ID == checks.FragmentationMetricID {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		out.Reason = "not measured: this run's record carries no " +
			checks.FragmentationMetricID + " metric"
		return out
	case 1:
	default:
		out.Reason = fmt.Sprintf(
			"this run's record carries %d %s readings; choosing one would be a guess",
			len(found), checks.FragmentationMetricID)
		return out
	}

	m := found[0]
	if len(m.Evidence) > 0 {
		out.Source = m.Evidence[0].Source
	}
	if !m.Determined {
		out.Reason = "measured and undetermined: " + m.Reason
		return out
	}
	v, err := decimal.NewFromString(m.Value)
	if err != nil {
		out.Reason = fmt.Sprintf("stored value %q is not a decimal", m.Value)
		return out
	}
	// An HHI is a sum of squared shares: above zero, at most one. A stored
	// figure outside that range is not a concentration, whatever wrote it.
	if !v.IsPositive() || v.GreaterThan(decimal.NewFromInt(1)) {
		out.Reason = fmt.Sprintf("stored value %s is outside (0, 1] and is not an HHI", m.Value)
		return out
	}
	out.Determined = true
	out.HHI = m.Value
	return out
}

// FragmentationHistory reads every stored run's path-concentration reading
// for a corridor, oldest first.
//
// An empty history returns an empty slice, not an error: a missing history
// is the answer, not a failure. Derived on read; appends nothing.
func FragmentationHistory(ctx context.Context, store Store, corridor string) ([]RunFragmentation, error) {
	records, err := store.All(ctx, corridor)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}
	runs := make([]RunFragmentation, 0, len(records))
	for _, rec := range records {
		runs = append(runs, FragmentationOfRecord(rec))
	}
	return runs, nil
}

// FragmentationOf summarises whether a corridor's executable value is
// concentrating into fewer paths across its whole recorded history.
//
// A nil result with no error means the corridor has no recorded history.
func FragmentationOf(ctx context.Context, store Store, corridor string) (*FragmentationSummary, error) {
	runs, err := FragmentationHistory(ctx, store, corridor)
	if err != nil {
		return nil, err
	}
	return SummarizeFragmentation(corridor, runs), nil
}

// SummarizeFragmentation compares the first and last comparable readings in
// a run series, which must be oldest first as FragmentationHistory returns
// it. It is pure: no I/O, no clock, so a caller can summarise any window.
//
// Comparable means determined and sharing the latest determined reading's
// source. The comparison is between two stored figures and is exactly their
// difference; it is not a trend, and says nothing about the runs between.
//
// A nil return means there are no runs at all.
func SummarizeFragmentation(corridor string, runs []RunFragmentation) *FragmentationSummary {
	if len(runs) == 0 {
		return nil
	}

	s := &FragmentationSummary{Corridor: corridor, TotalRuns: len(runs)}
	for i := range runs {
		if runs[i].Determined {
			s.DeterminedRuns++
			s.Source = runs[i].Source
		}
	}
	s.UnknownRuns = s.TotalRuns - s.DeterminedRuns
	if s.DeterminedRuns == 0 {
		s.Reason = "no run holds a determined " + checks.FragmentationMetricID + " reading"
		return s
	}

	var first, last *RunFragmentation
	for i := range runs {
		if !runs[i].Determined || runs[i].Source != s.Source {
			continue
		}
		s.ComparableRuns++
		if first == nil {
			first = &runs[i]
		}
		last = &runs[i]
	}

	s.FirstSeq, s.FirstAt, s.FirstHHI = first.Seq, first.RecordedAt, first.HHI
	s.LastSeq, s.LastAt, s.LastHHI = last.Seq, last.RecordedAt, last.HHI

	if s.ComparableRuns < 2 {
		s.Reason = fmt.Sprintf(
			"%d comparable reading(s); a change needs two determined readings from the same source",
			s.ComparableRuns)
		return s
	}

	// FragmentationOfRecord admitted these only after parsing them, so a
	// parse failure here would be a bug; it is still reported, not zeroed.
	a, errA := decimal.NewFromString(first.HHI)
	b, errB := decimal.NewFromString(last.HHI)
	if errA != nil || errB != nil {
		s.Reason = "a stored reading did not parse as a decimal"
		return s
	}
	change := b.Sub(a)
	s.ChangeHHI = change.String()
	switch change.Sign() {
	case 1:
		s.Direction = FragmentationConcentrating
	case -1:
		s.Direction = FragmentationDispersing
	default:
		s.Direction = FragmentationUnchanged
	}
	return s
}
