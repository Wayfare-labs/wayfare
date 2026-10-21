package server

import (
	"strings"
	"testing"
)

// TestUITrendShowsDivergenceStats guards issue #755: the trend response
// already carries divergence_stats, and the trend view must render a compact
// summary of it with the sample size. The vocabulary is the glossary's
// agreement bands (AGREE ≤2%, DISAGREE 2–10%, MALFUNCTION >10%), not a new
// verdict for the corridor.
//
// Like the other UI tests this is a source-text assertion: CI has no browser
// harness, so it pins the contract the renderer must implement rather than
// proving pixels. docs/qa/README.md is where the rendered states are checked.
func TestUITrendShowsDivergenceStats(t *testing.T) {
	page := uiSource(t)

	for _, want := range []string{
		// The trend panel actually reads the field.
		"divergenceSummary(d.divergence_stats, runs.length)",
		// Older servers have no such field, and the renderer must omit the
		// line rather than print a zero or crash on undefined.
		"function divergenceSummary(stats, runCount)",
		"if (!stats) return '';",
		// The sample size is always stated, including which runs were
		// excluded because they had only one provider to compare.
		"stats.observation_count",
		"stored run(s) carried a divergence figure",
		// The undetermined state says so; it never invents a mean.
		"stats.determined",
		"stats.mean_pct",
		"stats.reason",
		// The optional trend, present only when the server had enough
		// observations to compute one.
		"stats.trend_direction",
		"stats.trend_magnitude_pct",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("trend view is missing %q; divergence_stats would not be rendered with its sample size", want)
		}
	}

	// The agreement-band vocabulary and its thresholds come from the
	// glossary. A change that renamed or re-scaled a band must fail here,
	// because readers use those words to interpret every loss figure.
	start := strings.Index(page, "function divergenceBand(meanPct)")
	end := strings.Index(page, "function divergenceSummary(stats, runCount)")
	if start < 0 || end < 0 || end < start {
		t.Fatal("the divergence band classifier is missing from the trend view")
	}
	band := page[start:end]
	for _, want := range []string{
		"AGREE", "DISAGREE", "MALFUNCTION",
		"<= 2", "<= 10",
	} {
		if !strings.Contains(band, want) {
			t.Errorf("divergenceBand is missing %q; the summary would not speak the glossary's agreement vocabulary", want)
		}
	}
}
