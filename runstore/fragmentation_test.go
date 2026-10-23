package runstore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

const fragSource100 = "/paths/strict-send USDC/NGNC source_amount=100"

// fragMetric builds a stored fragmentation.paths reading the way
// checks.Findings.ToJSON writes one into a record.
func fragMetric(determined bool, value, source, reason string) checks.MetricJSON {
	m := checks.MetricJSON{
		ID:         checks.FragmentationMetricID,
		Scope:      "corridor",
		Subject:    "USDC -> NGNC",
		Determined: determined,
		Reason:     reason,
		Value:      value,
		Unit:       "ratio",
		Venue:      "pathfinding",
		Summary:    "fixture",
		Evidence:   []checks.EvidenceJSON{},
		ObservedAt: "2026-08-21T22:30:40Z",
	}
	if source != "" {
		m.Evidence = append(m.Evidence, checks.EvidenceJSON{
			Source:     source,
			Observed:   "fixture",
			ObservedAt: "2026-08-21T22:30:40Z",
		})
	}
	return m
}

func fragRecord(seq int64, at time.Time, metrics ...checks.MetricJSON) *Record {
	r := measurabilityRecord(seq, at, "DIRECT", nil)
	r.Metrics = metrics
	return r
}

func fragAt(hour int) time.Time { return time.Date(2026, 8, 21, hour, 0, 0, 0, time.UTC) }

// recordedFragmentation measures the metric against the repository's
// recorded USDC -> NGNC ladder and renders it exactly as a sweep would store
// it, so the reader is exercised on a stored figure that came from recorded
// upstream bytes rather than on a hand-typed string.
func recordedFragmentation(t *testing.T, size int64) checks.MetricJSON {
	t.Helper()
	m, err := snapshot.Load(filepath.Join("..", "testdata", "snapshots", "usdc-ngnc-20260821T223040Z"))
	if err != nil {
		t.Fatalf("loading recorded ladder: %v", err)
	}
	metric := checks.FragmentationMetric{
		DEX:  &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()},
		Size: decimal.NewFromInt(size),
	}
	f := &checks.Findings{}
	f.AddMetric(checks.RunMetric(context.Background(), metric,
		checks.Subject{Send: asset.USDC(), Receive: asset.NGNC()}))
	return f.ToJSON().Metrics[0]
}

func TestFragmentationOfRecord(t *testing.T) {
	other := checks.MetricJSON{ID: "spread.bid-ask", Determined: true, Value: "0.8", Unit: "percent"}

	tests := []struct {
		name       string
		metrics    []checks.MetricJSON
		determined bool
		hhi        string
		source     string
		reason     string
	}{
		{
			name:       "determined reading",
			metrics:    []checks.MetricJSON{other, fragMetric(true, "0.3877487606078083", fragSource100, "")},
			determined: true,
			hhi:        "0.3877487606078083",
			source:     fragSource100,
		},
		{
			// Every record written before the metric existed looks like
			// this; it is unknown, not a zero.
			name:    "metric absent",
			metrics: []checks.MetricJSON{other},
			reason:  "not measured",
		},
		{
			name:    "no findings at all",
			metrics: nil,
			reason:  "not measured",
		},
		{
			name:    "measured and undetermined keeps the stored reason",
			metrics: []checks.MetricJSON{fragMetric(false, "", fragSource100, "pathfinding failed: timeout")},
			source:  fragSource100,
			reason:  "pathfinding failed: timeout",
		},
		{
			name:    "two readings in one record",
			metrics: []checks.MetricJSON{fragMetric(true, "0.5", fragSource100, ""), fragMetric(true, "0.6", fragSource100, "")},
			reason:  "choosing one would be a guess",
		},
		{
			name:    "value that is not a decimal",
			metrics: []checks.MetricJSON{fragMetric(true, "high", fragSource100, "")},
			source:  fragSource100,
			reason:  "not a decimal",
		},
		{
			name:    "zero is not an HHI",
			metrics: []checks.MetricJSON{fragMetric(true, "0", fragSource100, "")},
			source:  fragSource100,
			reason:  "outside (0, 1]",
		},
		{
			name:    "above one is not an HHI",
			metrics: []checks.MetricJSON{fragMetric(true, "1.2", fragSource100, "")},
			source:  fragSource100,
			reason:  "outside (0, 1]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FragmentationOfRecord(fragRecord(4, fragAt(1), tc.metrics...))
			if got.Seq != 4 || !got.RecordedAt.Equal(fragAt(1)) {
				t.Errorf("seq/recorded_at = %d/%s, want 4/%s", got.Seq, got.RecordedAt, fragAt(1))
			}
			if got.Determined != tc.determined {
				t.Fatalf("determined = %v, want %v (reason %q)", got.Determined, tc.determined, got.Reason)
			}
			if got.HHI != tc.hhi {
				t.Errorf("hhi = %q, want %q", got.HHI, tc.hhi)
			}
			if got.Source != tc.source {
				t.Errorf("source = %q, want %q", got.Source, tc.source)
			}
			if tc.reason == "" && got.Reason != "" {
				t.Errorf("reason = %q, want none on a determined reading", got.Reason)
			}
			if !strings.Contains(got.Reason, tc.reason) {
				t.Errorf("reason = %q, want it to contain %q", got.Reason, tc.reason)
			}
		})
	}
}

func TestSummarizeFragmentation(t *testing.T) {
	const source5000 = "/paths/strict-send USDC/NGNC source_amount=5000"
	det := func(seq int64, hhi, source string) RunFragmentation {
		return RunFragmentation{Seq: seq, RecordedAt: fragAt(int(seq)), Determined: true, HHI: hhi, Source: source}
	}
	unknown := func(seq int64) RunFragmentation {
		return RunFragmentation{Seq: seq, RecordedAt: fragAt(int(seq)), Reason: "not measured"}
	}

	tests := []struct {
		name       string
		runs       []RunFragmentation
		determined int
		unknown    int
		comparable int
		firstSeq   int64
		lastSeq    int64
		change     string
		direction  string
		reason     string
	}{
		{
			name:       "concentrating",
			runs:       []RunFragmentation{det(1, "0.25", fragSource100), unknown(2), det(3, "0.3877", fragSource100)},
			determined: 2,
			unknown:    1,
			comparable: 2,
			firstSeq:   1,
			lastSeq:    3,
			change:     "0.1377",
			direction:  FragmentationConcentrating,
		},
		{
			name:       "dispersing",
			runs:       []RunFragmentation{det(1, "0.7675", fragSource100), det(2, "0.5", fragSource100)},
			determined: 2,
			comparable: 2,
			firstSeq:   1,
			lastSeq:    2,
			change:     "-0.2675",
			direction:  FragmentationDispersing,
		},
		{
			// A measured lack of change is a finding and reads "0".
			name:       "unchanged",
			runs:       []RunFragmentation{det(1, "0.5", fragSource100), det(2, "0.50", fragSource100)},
			determined: 2,
			comparable: 2,
			firstSeq:   1,
			lastSeq:    2,
			change:     "0",
			direction:  FragmentationUnchanged,
		},
		{
			// The latest reading probed 5000; the earlier ones at 100 are
			// a different question and are not compared with it.
			name:       "only same-source readings are compared",
			runs:       []RunFragmentation{det(1, "0.25", fragSource100), det(2, "0.39", fragSource100), det(3, "0.77", source5000)},
			determined: 3,
			comparable: 1,
			firstSeq:   3,
			lastSeq:    3,
			reason:     "1 comparable reading(s)",
		},
		{
			name:       "one reading is not a change",
			runs:       []RunFragmentation{unknown(1), det(2, "0.39", fragSource100)},
			determined: 1,
			unknown:    1,
			comparable: 1,
			firstSeq:   2,
			lastSeq:    2,
			reason:     "a change needs two",
		},
		{
			name:    "nothing determined",
			runs:    []RunFragmentation{unknown(1), unknown(2)},
			unknown: 2,
			reason:  "no run holds a determined",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := SummarizeFragmentation("USDC-NGNC", tc.runs)
			if s == nil {
				t.Fatal("summary is nil for a non-empty series")
			}
			if s.TotalRuns != len(tc.runs) || s.DeterminedRuns != tc.determined || s.UnknownRuns != tc.unknown {
				t.Errorf("counts = %d total / %d determined / %d unknown, want %d / %d / %d",
					s.TotalRuns, s.DeterminedRuns, s.UnknownRuns, len(tc.runs), tc.determined, tc.unknown)
			}
			if s.ComparableRuns != tc.comparable {
				t.Errorf("comparable = %d, want %d", s.ComparableRuns, tc.comparable)
			}
			if s.FirstSeq != tc.firstSeq || s.LastSeq != tc.lastSeq {
				t.Errorf("first/last seq = %d/%d, want %d/%d", s.FirstSeq, s.LastSeq, tc.firstSeq, tc.lastSeq)
			}
			if s.ChangeHHI != tc.change {
				t.Errorf("change = %q, want %q", s.ChangeHHI, tc.change)
			}
			if s.Direction != tc.direction {
				t.Errorf("direction = %q, want %q", s.Direction, tc.direction)
			}
			if !strings.Contains(s.Reason, tc.reason) {
				t.Errorf("reason = %q, want it to contain %q", s.Reason, tc.reason)
			}
			if tc.direction != "" && s.Reason != "" {
				t.Errorf("reason = %q alongside a direction; want none", s.Reason)
			}
		})
	}
}

// TestSummarizeFragmentationUnknownWithoutRuns: no runs is no history, so
// there is nothing to summarise — nil, not a zero-valued summary.
func TestSummarizeFragmentationUnknownWithoutRuns(t *testing.T) {
	if s := SummarizeFragmentation("USDC-NGNC", nil); s != nil {
		t.Errorf("summary over no runs = %+v, want nil", s)
	}
}

// TestFragmentationUnknownSummaryCarriesNoZeros pins the wire shape of a
// summary that could not compare anything: every comparison field is absent,
// so no consumer reads a "0" change or a year-one timestamp into it.
func TestFragmentationUnknownSummaryCarriesNoZeros(t *testing.T) {
	s := SummarizeFragmentation("USDC-NGNC", []RunFragmentation{{Seq: 1, RecordedAt: fragAt(1), Reason: "not measured"}})
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"first_at", "last_at", "first_hhi", "last_hhi", "change_hhi", "direction", "first_seq", "last_seq"} {
		if strings.Contains(string(b), `"`+absent+`"`) {
			t.Errorf("unknown summary carries %q: %s", absent, b)
		}
	}
	if !strings.Contains(string(b), `"total_runs":1`) || !strings.Contains(string(b), `"unknown_runs":1`) {
		t.Errorf("unknown summary must still carry its counts: %s", b)
	}
}

// TestFragmentationOfStoredChain is the end-to-end path: readings measured
// from recorded upstream bytes, stored in a hash-chained history alongside a
// run that predates the metric, and read back oldest first. Appending them
// changes nothing about the record layout — the metric rides in the existing
// metrics block — and the chain must still verify.
func TestFragmentationOfStoredChain(t *testing.T) {
	at100 := recordedFragmentation(t, 100)
	at5000 := recordedFragmentation(t, 5000)
	if !at100.Determined || !at5000.Determined {
		t.Fatalf("recorded readings undetermined: %q / %q", at100.Reason, at5000.Reason)
	}

	store := seedMeasurability(t,
		// A run written before the metric existed: no reading at all.
		fragRecord(1, fragAt(1)),
		fragRecord(2, fragAt(2), at100),
		fragRecord(3, fragAt(3), at100),
	)
	ctx := context.Background()
	if err := store.Verify(ctx, "USDC-NGNC"); err != nil {
		t.Fatalf("chain carrying fragmentation readings does not verify: %v", err)
	}

	runs, err := FragmentationHistory(ctx, store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}
	if runs[0].Determined || !strings.Contains(runs[0].Reason, "not measured") {
		t.Errorf("pre-metric run = %+v, want unknown and not measured", runs[0])
	}
	for _, r := range runs[1:] {
		if !r.Determined || r.HHI != at100.Value || r.Source != fragSource100 {
			t.Errorf("stored run = %+v, want determined %s from %q", r, at100.Value, fragSource100)
		}
	}

	s, err := FragmentationOf(ctx, store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if s.Direction != FragmentationUnchanged || s.ChangeHHI != "0" || s.UnknownRuns != 1 || s.ComparableRuns != 2 {
		t.Errorf("summary = %+v, want UNCHANGED over two comparable runs and one unknown", s)
	}

	// The same recorded corridor at 5000 is far more concentrated than at
	// 100; stored as a later run at the same size as an earlier 5000 run,
	// the comparison is made between those two only.
	store2 := seedMeasurability(t,
		fragRecord(1, fragAt(1), at5000),
		fragRecord(2, fragAt(2), at100),
		fragRecord(3, fragAt(3), at5000),
	)
	s2, err := FragmentationOf(ctx, store2, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if s2.ComparableRuns != 2 || s2.FirstSeq != 1 || s2.LastSeq != 3 || s2.DeterminedRuns != 3 {
		t.Errorf("summary = %+v, want runs 1 and 3 compared and run 2 (another size) excluded", s2)
	}
}

// TestFragmentationOfNoHistory: a corridor with no history has no summary.
func TestFragmentationOfNoHistory(t *testing.T) {
	store := seedMeasurability(t)
	s, err := FragmentationOf(context.Background(), store, "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Errorf("summary with no history = %+v, want nil", s)
	}
}
