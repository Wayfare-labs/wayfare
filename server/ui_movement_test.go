package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// TestUIMovementChart guards issue #637, the UI half: the trend view gains a
// benchmark-vs-corridor movement chart whose renderer paints only from the
// chart palette (never a verdict severity colour), whose legend names both
// series, and whose text alternatives — the section meta and the screen
// reader announcement — carry the same two components the chart draws.
func TestUIMovementChart(t *testing.T) {
	page := uiSource(t)

	for _, want := range []string{
		"function movementChart(d)",
		"id=\"movement-chart\"",
		"var(--chart-move-corridor)",
		"var(--chart-move-benchmark)",
		"Benchmark vs corridor movement",
		"corridor move",
		"benchmark move",
		"unknown",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; the movement decomposition would not render", want)
		}
	}

	// The chart section appears only when the runstore established a
	// movement, and its data comes from the trend payload.
	if !strings.Contains(page, "d.movements || []") {
		t.Error("UI never reads movements from the trend payload")
	}

	// Text alternative: the announcement names both components.
	start := strings.Index(page, "const announceTrend")
	if start < 0 {
		t.Fatal("could not find announceTrend")
	}
	// announceTrend is followed by other consts; bound it by its closing
	// brace instead.
	body := page[start:]
	end := strings.Index(body, "\n};")
	if end < 0 {
		t.Fatal("could not find the end of announceTrend")
	}
	body = body[:end]
	for _, want := range []string{"corridor moved", "benchmark moved", "unknown"} {
		if !strings.Contains(body, want) {
			t.Errorf("announceTrend text alternative missing %q; the chart has no accessible equivalent", want)
		}
	}

	// Verdict colours stay out of the movement renderer (same rule the
	// other chart renderers follow).
	mStart := strings.Index(page, "function movementChart(d)")
	mRel := strings.Index(page[mStart:], "function runsTable(")
	if mRel < 0 {
		t.Fatal("could not find the end of movementChart")
	}
	mBody := page[mStart : mStart+mRel]
	for _, forbidden := range []string{
		"var(--bad)", "var(--warn)", "var(--ok)",
		"var(--critical)", "var(--warning)", "var(--success)",
	} {
		if strings.Contains(mBody, forbidden) {
			t.Errorf("movementChart uses %q; chart data must not carry a verdict colour", forbidden)
		}
	}
}

// TestTrendServesMovements pins the wire half: consecutive runs with
// differing mids and priced rates produce a movements array on the trend
// endpoint — each entry naming the benchmark-free and corridor-free
// percentages of one pair — and the key is always present, empty array
// never null, so the UI can branch on presence without a null check.
func TestTrendServesMovements(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		mid   string
		rate  string
		loss  string
		at    time.Time
		price string
	}{
		// Oldest first. Mid 1500 → 1515 (+1%), rate 1000 → 1010 (+1%).
		{mid: "1500", rate: "1000", loss: "33.333333333333336", at: base},
		{mid: "1515", rate: "1010", loss: "33.29937210946759", at: base.Add(6 * time.Hour)},
	}

	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range seeds {
		rec := &runstore.Record{
			RecordedAt: s.at,
			Corridor:   "USDC-NGNC",
			Integrity:  "DIRECT",
			Reference: runstore.Reference{
				Mid:           s.mid,
				Source:        "provider",
				AsOf:          s.at.UTC().Format(time.RFC3339),
				ScoredAgainst: "provider",
			},
			FloorLossPct: s.loss, FloorSize: "0.1",
			WorstLossPct: s.loss, WorstSize: "0.1",
			Finding: "seeded run",
			Rungs: []runstore.Rung{{
				SendAmount: "0.1", Priced: true, Integrity: "DIRECT",
				ReceiveAmount: "100", EffectiveRate: s.rate,
				LossPct: s.loss, Verdict: "USABLE", Path: "USDC -> NGNC",
			}},
		}
		if err := st.Append(context.Background(), rec); err != nil {
			t.Fatalf("seeding run %d: %v", i, err)
		}
	}

	api := trendServer(t, st)
	res, err := http.Get(api.URL + "/api/corridor/trend?from=USDC&to=NGNC")
	if err != nil {
		t.Fatalf("GET trend: %v", err)
	}
	defer res.Body.Close()

	var doc struct {
		Movements []struct {
			PreviousRunAt string `json:"previous_run_at"`
			CurrentRunAt  string `json:"current_run_at"`
			BenchmarkPct  string `json:"benchmark_pct"`
			CorridorPct   string `json:"corridor_pct"`
			LossPctChange string `json:"loss_pct_change"`
		} `json:"movements"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		t.Fatalf("decoding trend: %v", err)
	}
	if len(doc.Movements) != 1 {
		t.Fatalf("movements = %d entries, want 1 pair", len(doc.Movements))
	}
	m := doc.Movements[0]
	if m.PreviousRunAt == "" || m.CurrentRunAt == "" {
		t.Error("movement missing run timestamps; the chart cannot place the pair on the time axis")
	}
	if m.CorridorPct == "" || m.BenchmarkPct == "" {
		t.Fatalf("movement percentages empty (corridor=%q benchmark=%q); the decomposition was not computed", m.CorridorPct, m.BenchmarkPct)
	}
	if m.LossPctChange == "" {
		t.Error("movement missing the headline loss change; attribution is impossible without it")
	}
	// The percentages must be the decomposed components, not the headline:
	// corridor-free against the previous rate, benchmark-free against the
	// previous mid.
	if !strings.HasPrefix(m.CorridorPct, "1") {
		t.Errorf("corridor_pct = %s, want ~1%% (1010 vs 1000)", m.CorridorPct)
	}
	if !strings.HasPrefix(m.BenchmarkPct, "1") {
		t.Errorf("benchmark_pct = %s, want ~1%% (1515 vs 1500)", m.BenchmarkPct)
	}

	// With a single run there is no pair: the key stays present, empty.
	st1, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.Append(context.Background(), &runstore.Record{
		RecordedAt:   base,
		Corridor:     "USDC-NGNC",
		Integrity:    "DIRECT",
		Reference:    runstore.Reference{Mid: "1500", Source: "provider", AsOf: base.Format(time.RFC3339), ScoredAgainst: "provider"},
		FloorLossPct: "1", FloorSize: "0.1", WorstLossPct: "1", WorstSize: "0.1", Finding: "solo",
		Rungs: []runstore.Rung{{SendAmount: "0.1", Priced: true, Integrity: "DIRECT", ReceiveAmount: "100", EffectiveRate: "1000", LossPct: "1", Verdict: "USABLE", Path: "USDC -> NGNC"}},
	}); err != nil {
		t.Fatal(err)
	}
	api1 := trendServer(t, st1)
	res1, err := http.Get(api1.URL + "/api/corridor/trend?from=USDC&to=NGNC")
	if err != nil {
		t.Fatalf("GET solo trend: %v", err)
	}
	defer res1.Body.Close()
	raw, err := io.ReadAll(res1.Body)
	if err != nil {
		t.Fatalf("reading solo trend: %v", err)
	}
	if !strings.Contains(string(raw), `"movements":[]`) {
		t.Error(`solo trend payload missing "movements":[]; the key must always be present, empty never null`)
	}
}
