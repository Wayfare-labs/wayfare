package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// measurabilityTrendStore seeds runs whose rungs are priced or not, so a test
// can build a history with a known mix of measurable and unmeasurable runs.
// seedTrendStore always prices its one rung; measurability is exactly the
// question of whether it priced, so this builder varies it.
func measurabilityTrendStore(t *testing.T, at []time.Time, priced []bool, integrity []string) *runstore.FileStore {
	t.Helper()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := range at {
		rung := runstore.Rung{
			SendAmount: "0.1", Priced: priced[i], Integrity: integrity[i],
			LossPct: "5", Verdict: "FAIR",
		}
		if priced[i] {
			rung.EffectiveRate = "1000"
			rung.ReceiveAmount = "100"
		}
		rec := &runstore.Record{
			RecordedAt: at[i], Corridor: "USDC-NGNC", Integrity: integrity[i],
			Reference: runstore.Reference{
				Mid: "1350", Source: "currency-api", AsOf: at[i].UTC().Format(time.RFC3339),
				ScoredAgainst: "currency-api",
			},
			FloorLossPct: "5", FloorSize: "0.1", WorstLossPct: "5", WorstSize: "0.1",
			Finding: "seeded run",
			Rungs:   []runstore.Rung{rung},
		}
		if err := store.Append(context.Background(), rec); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	return store
}

// TestTrendMeasurabilityBlock pins the wire (#638): a corridor's trend carries
// an uptime figure in the same window as its runs, with the denominator always
// present so a rate is never read without its sample, low_sample derived
// server-side from the project's one threshold, and the window bounds.
func TestTrendMeasurabilityBlock(t *testing.T) {
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	at := []time.Time{base, base.Add(6 * time.Hour), base.Add(12 * time.Hour)}
	srv := trendServer(t, measurabilityTrendStore(t, at, []bool{true, false, true}, []string{"DIRECT", "NO-MARKET", "DIRECT"}))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	m, ok := body["measurability"].(map[string]any)
	if !ok {
		t.Fatalf("measurability = %v, want an object", body["measurability"])
	}
	if m["total_runs"] != float64(3) {
		t.Errorf("total_runs = %v, want 3 (the denominator must always be present)", m["total_runs"])
	}
	if m["measurable_runs"] != float64(2) || body["measurability"].(map[string]any)["not_measurable_runs"] != float64(1) {
		t.Errorf("measurable/not = %v/%v, want 2/1", m["measurable_runs"], m["not_measurable_runs"])
	}
	if want := "66.6666666666666667"; body["measurability"].(map[string]any)["uptime_pct"] != want {
		t.Errorf("uptime_pct = %v, want %s", body["measurability"].(map[string]any)["uptime_pct"], want)
	}
	if body["measurability"].(map[string]any)["low_sample"] != true {
		t.Error("low_sample = false with three runs; the threshold is 30")
	}
	block := body["measurability"].(map[string]any)
	if block["first_run_at"] != base.Format(time.RFC3339) {
		t.Errorf("first_run_at = %v, want %s", block["first_run_at"], base.Format(time.RFC3339))
	}
	if block["last_run_at"] != at[2].Format(time.RFC3339) {
		t.Errorf("last_run_at = %v, want %s", block["last_run_at"], at[2].Format(time.RFC3339))
	}
}

// TestTrendMeasurabilityAbsentWithoutHistory pins the unknown rule at this
// boundary: no recorded runs means no uptime figure at all — the block is
// omitted, never a 0% of 0 runs that reads as a measured failure.
func TestTrendMeasurabilityAbsentWithoutHistory(t *testing.T) {
	srv := trendServer(t, nil)
	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	if _, ok := body["measurability"]; ok {
		t.Errorf("measurability = %v, want it absent with no history", body["measurability"])
	}
}
