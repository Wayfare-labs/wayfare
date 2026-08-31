package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// trendSeed is one stored run, customised where the trend reads it.
type trendSeed struct {
	at        time.Time
	integrity string
	mid       string
	source    string
	loss      string
	verdict   string

	// divergencePct is the reference cross-check divergence to store, left
	// empty for the common case (a SINGLE-provider run has none to report).
	divergencePct string
}

// seedTrendStore builds a store holding the given runs for USDC-NGNC, in the
// order given (oldest first).
func seedTrendStore(t *testing.T, seeds []trendSeed) runstore.Store {
	t.Helper()
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range seeds {
		rec := &runstore.Record{
			RecordedAt: s.at,
			Corridor:   "USDC-NGNC",
			Integrity:  s.integrity,
			Reference: runstore.Reference{
				Mid:           s.mid,
				Source:        s.source,
				AsOf:          s.at.UTC().Format(time.RFC3339),
				ScoredAgainst: s.source,
				DivergencePct: s.divergencePct,
			},
			FloorLossPct: s.loss, FloorSize: "0.1",
			WorstLossPct: s.loss, WorstSize: "0.1",
			Recommended: nil,
			Finding:     "seeded run",
			Rungs: []runstore.Rung{{
				SendAmount: "0.1", Priced: true, Integrity: s.integrity,
				ReceiveAmount: "100", EffectiveRate: "1000",
				LossPct: s.loss, Verdict: s.verdict, Path: "USDC -> NGNC",
			}},
		}
		if err := st.Append(context.Background(), rec); err != nil {
			t.Fatalf("seeding run %d: %v", i, err)
		}
	}
	return st
}

// trendServer stands up the API with a store and no live engine: the trend
// endpoint must answer from storage alone, and a nil engine proves it does
// not secretly measure.
func trendServer(t *testing.T, store runstore.Store) *httptest.Server {
	t.Helper()
	s := &Server{Store: store}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

// TestTrendServesHistoryOldestFirst is the core of the trend contract: the
// runs come back as a timeline (oldest first), and the two axes besides loss
// — integrity state and the reference each run was scored against — come
// through on every run.
func TestTrendServesHistoryOldestFirst(t *testing.T) {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	seeds := []trendSeed{
		{at: base, integrity: "DIRECT", mid: "1350.00", source: "exchangerate-api", loss: "25.00", verdict: "UNUSABLE"},
		{at: base.Add(6 * time.Hour), integrity: "DERIVATIVE", mid: "1351.00", source: "currency-api", loss: "26.50", verdict: "UNUSABLE"},
		{at: base.Add(12 * time.Hour), integrity: "DIRECT", mid: "1349.00", source: "exchangerate-api", loss: "24.75", verdict: "UNUSABLE"},
	}
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	if body["corridor"] != "USDC-NGNC" {
		t.Errorf("corridor = %v, want USDC-NGNC", body["corridor"])
	}
	if body["count"] != float64(3) {
		t.Errorf("count = %v, want 3", body["count"])
	}

	runs, _ := body["runs"].([]any)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}

	wantAt := []time.Time{base, base.Add(6 * time.Hour), base.Add(12 * time.Hour)}
	wantIntegrity := []string{"DIRECT", "DERIVATIVE", "DIRECT"}
	wantSource := []string{"exchangerate-api", "currency-api", "exchangerate-api"}
	for i, runAny := range runs {
		run := runAny.(map[string]any)
		if run["recorded_at"] != wantAt[i].Format(time.RFC3339) {
			t.Errorf("run %d recorded_at = %v, want %s (the timeline must read oldest first)",
				i, run["recorded_at"], wantAt[i].Format(time.RFC3339))
		}
		if run["integrity"] != wantIntegrity[i] {
			t.Errorf("run %d integrity = %v, want %s", i, run["integrity"], wantIntegrity[i])
		}
		ref, _ := run["reference"].(map[string]any)
		if ref == nil {
			t.Fatalf("run %d carries no reference; the trend cannot show which mid it was scored against", i)
		}
		if ref["scored_against"] != wantSource[i] {
			t.Errorf("run %d scored_against = %v, want %s", i, ref["scored_against"], wantSource[i])
		}
		rungs, _ := run["rungs"].([]any)
		if len(rungs) != 1 {
			t.Fatalf("run %d rungs = %d, want 1", i, len(rungs))
		}
		rung := rungs[0].(map[string]any)
		if rung["send_amount"] != "0.1" || rung["loss_pct"] != seeds[i].loss {
			t.Errorf("run %d rung = %v, want 0.1 at %s%%", i, rung, seeds[i].loss)
		}
	}
}

// TestTrendWithNoHistoryIsEmptyNotError covers both halves of the empty case:
// an opened-but-empty store, and no store at all. Both must answer 200 with
// an empty array, because a missing history is the answer, not a failure,
// and the first day of a deployment is exactly when a monitor is most read.
func TestTrendWithNoHistoryIsEmptyNotError(t *testing.T) {
	empty, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for name, store := range map[string]runstore.Store{"empty store": empty, "nil store": nil} {
		t.Run(name, func(t *testing.T) {
			srv := trendServer(t, store)
			status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 for an empty history: %v", status, body)
			}
			if body["count"] != float64(0) {
				t.Errorf("count = %v, want 0", body["count"])
			}
			runs, _ := body["runs"].([]any)
			if runs == nil {
				t.Fatal("runs must be an empty array, not null, so a client can iterate it without a nil check")
			}
			if len(runs) != 0 {
				t.Errorf("runs = %d, want 0", len(runs))
			}
		})
	}
}

// TestTrendRejectsUnknownAssets pins that the trend endpoint validates its
// corridor exactly as the measurement endpoint does: an unverified asset is
// an error rather than a guess at whose history to read.
func TestTrendRejectsUnknownAssets(t *testing.T) {
	srv := trendServer(t, nil)
	for name, q := range map[string]string{
		"unknown receive": "?to=SCAMC",
		"unknown send":    "?from=BOGUS",
		"no fiat peg":     "?to=USDC",
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := getJSON(t, srv.URL+"/api/corridor/trend"+q)
			if status != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", name, status)
			}
		})
	}
}

// TestTrendLimitBoundsTheRead checks that a limit keeps the most recent runs
// (the trend is about the present) and that a bad limit is a client error.
func TestTrendLimitBoundsTheRead(t *testing.T) {
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	seeds := make([]trendSeed, 0, 4)
	for i := 0; i < 4; i++ {
		seeds = append(seeds, trendSeed{
			at:        base.Add(time.Duration(i) * 6 * time.Hour),
			integrity: "DIRECT", mid: "1350", source: "currency-api",
			loss: "25.00", verdict: "UNUSABLE",
		})
	}
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC&limit=2")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	runs, _ := body["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("limit=2 returned %d runs, want 2", len(runs))
	}
	runs0 := runs[0].(map[string]any)
	runs1 := runs[1].(map[string]any)
	if runs0["seq"] != float64(3) || runs1["seq"] != float64(4) {
		t.Errorf("limit=2 kept seqs %v, %v, want the two most recent (3, 4)", runs0["seq"], runs1["seq"])
	}

	for name, q := range map[string]string{
		"not a number": "limit=abc",
		"zero":         "limit=0",
		"negative":     "limit=-1",
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC&"+q)
			if status != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", name, status)
			}
		})
	}
}

// buildTrendSeeds returns n runs, oldest first, each carrying divergencePct
// where wantDivergence[i] is true and empty otherwise — simulating a mix of
// cross-checked and SINGLE-provider runs.
func buildTrendSeeds(n int, divergencePct string, wantDivergence func(i int) bool) []trendSeed {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seeds := make([]trendSeed, 0, n)
	for i := 0; i < n; i++ {
		s := trendSeed{
			at:        base.Add(time.Duration(i) * 6 * time.Hour),
			integrity: "DIRECT", mid: "1350", source: "currency-api",
			loss: "5.00", verdict: "FAIR",
		}
		if wantDivergence(i) {
			s.divergencePct = divergencePct
		}
		seeds = append(seeds, s)
	}
	return seeds
}

// TestTrendDivergenceStatsIsAlwaysPresent covers the case with no store and
// the case with an empty store: divergence_stats must still appear, reported
// as undetermined rather than left off the response for a client to infer
// from its absence.
func TestTrendDivergenceStatsIsAlwaysPresent(t *testing.T) {
	empty, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]runstore.Store{"empty store": empty, "nil store": nil} {
		t.Run(name, func(t *testing.T) {
			srv := trendServer(t, store)
			status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200: %v", status, body)
			}
			stats, ok := body["divergence_stats"].(map[string]any)
			if !ok {
				t.Fatal("divergence_stats must always be present, even with no history")
			}
			if stats["determined"] != false {
				t.Errorf("determined = %v, want false with no runs", stats["determined"])
			}
			if stats["observation_count"] != float64(0) {
				t.Errorf("observation_count = %v, want 0", stats["observation_count"])
			}
			if stats["reason"] == nil || stats["reason"] == "" {
				t.Error("expected a reason explaining why divergence could not be determined")
			}
		})
	}
}

// TestTrendDivergenceStatsUndeterminedBelowMinimumSample pins that a handful
// of divergence-bearing runs is reported as undetermined rather than a mean
// that looks precise but is not meaningful — the same discipline
// analysis.AnalyzeDecimal already enforces, now reachable over the API.
func TestTrendDivergenceStatsUndeterminedBelowMinimumSample(t *testing.T) {
	seeds := buildTrendSeeds(10, "1.50", func(i int) bool { return true })
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	stats, ok := body["divergence_stats"].(map[string]any)
	if !ok {
		t.Fatal("expected a divergence_stats object")
	}
	if stats["observation_count"] != float64(10) {
		t.Errorf("observation_count = %v, want 10", stats["observation_count"])
	}
	if stats["determined"] != false {
		t.Error("expected undetermined with only 10 divergence observations")
	}
	if stats["mean_pct"] != nil {
		t.Error("mean_pct must be absent when undetermined, not a misleadingly precise figure")
	}
}

// TestTrendDivergenceStatsSkipsRunsWithNoDivergence is the test named in the
// issue: a corridor whose providers increasingly disagree is a fact about
// the benchmark, and a run scored against a single provider has nothing to
// contribute to that fact. Mixing 30 divergence-bearing runs with 10
// SINGLE-provider runs must produce observation_count 30, not 40, and must
// cross the minimum-sample threshold to a determined result.
func TestTrendDivergenceStatsSkipsRunsWithNoDivergence(t *testing.T) {
	seeds := buildTrendSeeds(40, "2.00", func(i int) bool { return i < 30 })
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	stats, ok := body["divergence_stats"].(map[string]any)
	if !ok {
		t.Fatal("expected a divergence_stats object")
	}
	if stats["observation_count"] != float64(30) {
		t.Errorf("observation_count = %v, want 30 (40 runs, 10 with no divergence to report)",
			stats["observation_count"])
	}
	if stats["determined"] != true {
		t.Errorf("determined = %v, want true with 30 divergence observations", stats["determined"])
	}
	if stats["mean_pct"] != "2.0000" {
		t.Errorf("mean_pct = %v, want 2.0000 (every divergence-bearing run reported 2.00)", stats["mean_pct"])
	}
}

// TestTrendDivergenceStatsMalformedValueIsAnError pins the "nothing is ever
// synthesised to fill a gap" constraint at this boundary: a stored
// divergence_pct that fails to parse is a corrupt record, not a missing
// observation, and must fail the request rather than silently drop the run
// or report a plausible-looking number.
func TestTrendDivergenceStatsMalformedValueIsAnError(t *testing.T) {
	seeds := buildTrendSeeds(1, "not-a-decimal", func(i int) bool { return true })
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a corrupt stored divergence_pct: %v", status, body)
	}
	if body["error"] == nil {
		t.Error("expected an error message")
	}
}

// TestTrendDivergenceStatsNegativeValueIsAnError covers the same defect class
// as the malformed-value test above: DivergencePct is a magnitude and cannot
// legitimately be negative, so a stored negative figure must fail the
// request rather than be allowed to quietly pull the reported mean down.
func TestTrendDivergenceStatsNegativeValueIsAnError(t *testing.T) {
	seeds := buildTrendSeeds(1, "-2.50", func(i int) bool { return true })
	srv := trendServer(t, seedTrendStore(t, seeds))

	status, body := getJSON(t, srv.URL+"/api/corridor/trend?to=NGNC")
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a negative stored divergence_pct: %v", status, body)
	}
	if body["error"] == nil {
		t.Error("expected an error message")
	}
}

// TestTrendMoneyCrossesTheWireAsStrings guards the float64 invariant at the
// trend boundary, the same way the measurement endpoint is guarded.
func TestTrendMoneyCrossesTheWireAsStrings(t *testing.T) {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	srv := trendServer(t, seedTrendStore(t, []trendSeed{{
		at: base, integrity: "DIRECT", mid: "1350.00", source: "currency-api",
		loss: "25.00", verdict: "UNUSABLE",
	}}))

	resp, err := http.Get(srv.URL + "/api/corridor/trend?to=NGNC")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	var runs []map[string]json.RawMessage
	if err := json.Unmarshal(body["runs"], &runs); err != nil {
		t.Fatalf("decoding runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}

	var rungs []json.RawMessage
	if err := json.Unmarshal(runs[0]["rungs"], &rungs); err != nil {
		t.Fatalf("decoding rungs: %v", err)
	}
	var rung map[string]json.RawMessage
	_ = json.Unmarshal(rungs[0], &rung)

	var ref map[string]json.RawMessage
	_ = json.Unmarshal(runs[0]["reference"], &ref)

	for field, raw := range map[string]json.RawMessage{
		"floor_loss_pct": runs[0]["floor_loss_pct"],
		"worst_loss_pct": runs[0]["worst_loss_pct"],
		"reference.mid":  ref["mid"],
		"rung.loss_pct":  rung["loss_pct"],
	} {
		if !strings.HasPrefix(string(raw), `"`) {
			t.Errorf("%s = %s, want a quoted string so clients cannot parse it as a float", field, raw)
		}
	}
}

// TestTrendRejectsOtherMethods pins that the endpoint is a read.
func TestTrendRejectsOtherMethods(t *testing.T) {
	srv := trendServer(t, nil)
	resp, err := http.Post(srv.URL+"/api/corridor/trend?to=NGNC", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}
}

// TestUITrendIsSelfContained guards the trend view the way TestUIIsServed
// guards the page: it must read from this server only, and it must say what
// its data is. A chart that implied a continuous series out of irregular
// snapshots would be the one dishonesty this project exists to refuse, so
// the honesty caption is pinned too.
func TestUITrendIsSelfContained(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		"/api/corridor/trend", // the endpoint the trend reads
		"unusable above",      // the 20% threshold, as on the live curve
		"irregular snapshots, not a continuous series", // the honesty caption
		"scored_against",       // which mid a run was scored against
		"prefers-color-scheme", // the trend must live in the theme
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the trend view lacks %q; it would render incompletely or mislead", want)
		}
	}
}
