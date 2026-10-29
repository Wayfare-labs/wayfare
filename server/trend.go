package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/analysis"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// Wire shape for a corridor's stored history.
//
// These are explicit wire types for the same reason route.CorridorJSON
// exists: runstore.Record is a storage format — versioned, hash-sealed, with
// a field order that is part of the hash preimage — and marshalling it
// directly would make the API a function of how records happen to be stored
// on disk. The conversion below is the only place the two shapes meet.
//
// Runs come back oldest first: a timeline reads left to right. The store's
// own Recent() returns newest first because its callers want the latest
// record, so the handler reverses before converting.

const (
	// defaultTrendLimit is roughly twenty-five days of 6-hourly runs.
	defaultTrendLimit = 100
	// maxTrendLimit bounds one request's read, not the history itself.
	maxTrendLimit = 500
)

// TrendJSON is a corridor's stored history.
type TrendJSON struct {
	Corridor      string          `json:"corridor"`
	SendAsset     route.AssetJSON `json:"send_asset"`
	ReceiveAsset  route.AssetJSON `json:"receive_asset"`
	ReferencePair string          `json:"reference_pair"`

	// Count is len(Runs), carried separately so a reader can tell an empty
	// history from a failed request without inspecting the array.
	Count int `json:"count"`

	// Next is the sequence cursor to fetch the next older page of runs
	// via ?before=seq. Omitted when there are no older runs.
	Next int64 `json:"next,omitempty"`

	// Runs is oldest first. An array, never null, so a client can iterate
	// it without a nil check when the history is empty.
	Runs []TrendRunJSON `json:"runs"`

	// IntegrityTransitions is the ordered series of determined structural
	// changes between consecutive runs in Runs. Transitions involving UNKNOWN
	// are omitted by runstore's integrity-transition contract.
	IntegrityTransitions []IntegrityTransitionJSON `json:"integrity_transitions"`

	// LatestTransition is the most recent structural change in the window
	// above — the same event as the last element of IntegrityTransitions,
	// named on its own because "this corridor changed kind" is the one fact
	// a reader should not have to diff two records to find.
	//
	// Null when the window establishes no transition: a history of one run
	// has nothing to compare, and two runs that agree are not a change. It
	// is null rather than absent, so a client can tell "no transition" from
	// "this build does not report transitions". It never alters a run's
	// integrity state or verdict.
	LatestTransition *IntegrityTransitionJSON `json:"latest_transition"`

	// Movements is the benchmark-vs-corridor movement decomposition over the
	// same window (issue #637): one entry per consecutive pair that
	// established any movement, oldest pair first. Empty array, never null,
	// so a client can iterate without a nil check. Empty means no pair
	// established a movement — fewer than two runs, or no pair with both a
	// scorable benchmark and a priced rung — never "no movement happened".
	Movements []MovementJSON `json:"movements"`

	// DivergenceStats summarises how far the corridor's two reference
	// providers have disagreed across the runs above — a fact about the
	// benchmark, not the corridor. Always present, never omitted: a benchmark
	// that has never diverged (or a history too short to say) is reported
	// explicitly rather than left for a client to infer from an absent key.
	// Never feeds back into any run's verdict or integrity state above.
	DivergenceStats DivergenceStatsJSON `json:"divergence_stats"`
}

// IntegrityTransitionJSON is one structural change in a trend window.
type IntegrityTransitionJSON struct {
	PreviousIntegrity string   `json:"previous_integrity"`
	CurrentIntegrity  string   `json:"current_integrity"`
	PreviousRunAt     string   `json:"previous_run_at"`
	CurrentRunAt      string   `json:"current_run_at"`
	PreviousDependsOn []string `json:"previous_depends_on"`
	CurrentDependsOn  []string `json:"current_depends_on"`
	TransitionType    string   `json:"transition_type"`
}

// DivergenceStatsJSON is a corridor benchmark's own longitudinal divergence:
// how far its two reference providers have disagreed, across the same window
// of runs the trend response carries. See analysis.DivergenceHistory.
type DivergenceStatsJSON struct {
	// ObservationCount is how many of the runs above actually carried a
	// divergence figure. A run scored against a single provider (SINGLE
	// agreement) has no divergence to report and is not counted — an
	// unmeasured divergence is unknown, never zero, so it must not pad this
	// count toward the minimum sample size below.
	ObservationCount int `json:"observation_count"`

	// Determined is false below the documented minimum sample size (see
	// analysis.MinSampleSizeForMeanStdDev); Reason then explains why, and
	// MeanPct/StdDevPct/trend are absent rather than a plausible-looking
	// zero.
	Determined bool   `json:"determined"`
	Reason     string `json:"reason,omitempty"`

	MeanPct   string `json:"mean_pct,omitempty"`
	StdDevPct string `json:"stddev_pct,omitempty"`

	// TrendDirection and TrendMagnitudePct are present only with enough
	// observations for a trend (see analysis.MinSampleSizeForTrend), which
	// is a higher bar than Determined alone: a mean can be meaningful before
	// a trend direction is.
	TrendDirection    string `json:"trend_direction,omitempty"`
	TrendMagnitudePct string `json:"trend_magnitude_pct,omitempty"`
}

// toDivergenceStatsJSON renders an analysis.MetricStats for the wire.
//
// Regime is deliberately not rendered: analysis.DefaultRegimeThresholds is
// calibrated to loss percentages, not to reference-provider divergence, and
// publishing a regime label computed against the wrong scale would read as a
// verdict this project never issued.
func toDivergenceStatsJSON(s *analysis.MetricStats) DivergenceStatsJSON {
	out := DivergenceStatsJSON{
		ObservationCount: s.ObservationCount,
		Determined:       !s.Undetermined,
		Reason:           s.Reason,
	}
	if s.Mean != nil {
		out.MeanPct = s.Mean.StringFixed(4)
	}
	if s.StdDev != nil {
		out.StdDevPct = s.StdDev.StringFixed(4)
	}
	if s.Trend != nil {
		out.TrendDirection = string(s.Trend.Direction)
		out.TrendMagnitudePct = s.Trend.Magnitude.StringFixed(4)
	}
	return out
}

// TrendRunJSON is one stored measurement, reduced to what a trend needs:
// the state (integrity, depends_on), the headline figures, the reference the
// figures were scored against, and enough of each rung to plot loss by size.
// Hash linkage and full rung detail stay in storage.
type TrendRunJSON struct {
	Seq        int64  `json:"seq"`
	RecordedAt string `json:"recorded_at"`

	Integrity string   `json:"integrity"`
	DependsOn []string `json:"depends_on"`

	Reference TrendRefJSON `json:"reference"`

	FloorLossPct string `json:"floor_loss_pct"`
	FloorSize    string `json:"floor_size"`
	WorstLossPct string `json:"worst_loss_pct"`
	WorstSize    string `json:"worst_size"`

	// RecommendedSize is empty when no size produced an acceptable route,
	// which is the normal shape of a broken corridor and must survive into
	// the history rather than being promoted away.
	RecommendedSize string `json:"recommended_size,omitempty"`

	Finding string          `json:"finding"`
	Rungs   []TrendRungJSON `json:"rungs"`
}

// TrendRefJSON is the reference rate a run was scored against.
//
// Both mids are carried when both providers answered, exactly as storage
// does: a trend whose benchmark changed mid-series is a different question
// from a trend whose corridor moved, and the fields that tell them apart
// are source, secondary_source, divergence_pct and scored_against.
type TrendRefJSON struct {
	Mid    string `json:"mid"`
	Source string `json:"source"`
	AsOf   string `json:"as_of,omitempty"`

	SecondaryMid    string `json:"secondary_mid,omitempty"`
	SecondarySource string `json:"secondary_source,omitempty"`
	DivergencePct   string `json:"divergence_pct,omitempty"`

	// ScoredAgainst names which source produced the run's verdicts, so a
	// reader looking at the loss series can see when it was scored against
	// a different provider than the primary.
	ScoredAgainst string `json:"scored_against,omitempty"`
}

// TrendRungJSON is one size's stored result, reduced to what a trend plots.
type TrendRungJSON struct {
	SendAmount string `json:"send_amount"`
	Priced     bool   `json:"priced"`
	LossPct    string `json:"loss_pct,omitempty"`
	Verdict    string `json:"verdict,omitempty"`
}

// MovementJSON is one consecutive-pair movement decomposition on the wire
// (issue #637): how much of the headline change came from the benchmark and
// how much from the corridor itself. Empty percentage fields are the
// explicit unknown — a pair where neither side could be measured is absent
// from the movements array, never a zero.
type MovementJSON struct {
	PreviousRunAt string `json:"previous_run_at"`
	CurrentRunAt  string `json:"current_run_at"`

	// BenchmarkFrom/BenchmarkTo are the mids the two runs were scored
	// against; CorridorFrom/CorridorTo are their floor-size effective rates.
	BenchmarkFrom string `json:"benchmark_from,omitempty"`
	BenchmarkTo   string `json:"benchmark_to,omitempty"`
	CorridorFrom  string `json:"corridor_from,omitempty"`
	CorridorTo    string `json:"corridor_to,omitempty"`

	// Signed decimal strings in percent. BenchmarkPct is corridor-free;
	// CorridorPct is benchmark-free; LossPctChange is the headline move in
	// percentage points.
	BenchmarkPct  string `json:"benchmark_pct,omitempty"`
	CorridorPct   string `json:"corridor_pct,omitempty"`
	LossPctChange string `json:"loss_pct_change,omitempty"`
}

// toMovementJSON renders a runstore.Movement for the wire, dropping the
// corridor key the envelope already carries.
func toMovementJSON(m *runstore.Movement) MovementJSON {
	return MovementJSON{
		PreviousRunAt: m.PreviousRunAt.UTC().Format(time.RFC3339),
		CurrentRunAt:  m.CurrentRunAt.UTC().Format(time.RFC3339),
		BenchmarkFrom: m.BenchmarkFrom,
		BenchmarkTo:   m.BenchmarkTo,
		CorridorFrom:  m.CorridorFrom,
		CorridorTo:    m.CorridorTo,
		BenchmarkPct:  m.BenchmarkPct,
		CorridorPct:   m.CorridorPct,
		LossPctChange: m.LossPctChange,
	}
}

// toTrendJSON renders stored records in the wire shape, preserving their
// stored (oldest first) order.
func toTrendJSON(recs []*runstore.Record, key, pair string, send, recv asset.Asset, next int64) TrendJSON {
	out := TrendJSON{
		Corridor:             key,
		SendAsset:            route.ToAssetJSON(send),
		ReceiveAsset:         route.ToAssetJSON(recv),
		ReferencePair:        pair,
		Count:                len(recs),
		Next:                 next,
		Runs:                 make([]TrendRunJSON, 0, len(recs)),
		IntegrityTransitions: make([]IntegrityTransitionJSON, 0),
		Movements:            make([]MovementJSON, 0),
	}
	for _, rec := range recs {
		out.Runs = append(out.Runs, toTrendRunJSON(rec))
	}
	transitions := runstore.TransitionsFromRecords(recs)
	for _, transition := range transitions {
		out.IntegrityTransitions = append(out.IntegrityTransitions, toIntegrityTransitionJSON(transition))
	}
	// The last transition in the window is the corridor's latest one: the
	// window ends at the newest stored run, so — as long as it holds at
	// least two runs — the pair the detector itself compares (the two most
	// recent) is the pair this series ends with. A window of one run has
	// nothing to compare and reports no transition, which is why the field
	// is null rather than absent.
	if len(transitions) > 0 {
		latest := toIntegrityTransitionJSON(transitions[len(transitions)-1])
		out.LatestTransition = &latest
	}
	for _, movement := range runstore.MovementsFromRecords(recs) {
		out.Movements = append(out.Movements, toMovementJSON(movement))
	}
