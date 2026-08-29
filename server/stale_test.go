package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// deadServer builds an API whose upstream Horizon is unreachable, so every
// live measurement fails.
func deadServer(t *testing.T, store runstore.Store) *httptest.Server {
	t.Helper()

	// A server that closes immediately: any request to it fails at the
	// transport, which is what an upstream outage looks like.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	s := &Server{
		Engine: newEngine(deadURL, "1350"),
		Store:  store,
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

func newEngine(horizonURL, mid string) *route.Engine {
	return &route.Engine{
		DEX: dexClientAt(horizonURL),
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString(mid),
		}),
	}
}

// storedRun appends one record for USDC-NGNC, recorded at the given time.
func storedRun(t *testing.T, dir string, at time.Time) runstore.Store {
	t.Helper()
	st, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := &runstore.Record{
		RecordedAt: at,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api",
			AsOf: at.UTC().Format(time.RFC3339), ScoredAgainst: "currency-api",
		},
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Recommended: nil,
		Finding:     "No usable size.",
		Rungs: []runstore.Rung{{
			SendAmount: "0.1", Priced: true, Integrity: "DIRECT",
			ReceiveAmount: "102.78", EffectiveRate: "1027.84",
			LossPct: "24.65", Verdict: "UNUSABLE", Path: "USDC -> NGNC",
		}},
	}
	if err := st.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	return st
}

// TestStaleReadingIsLabelled is the core of the stale contract. A stored
// reading may be served when a live measurement fails, but it must be
// impossible to mistake for a fresh one.
func TestStaleReadingIsLabelled(t *testing.T) {
	recorded := time.Now().UTC().Add(-6 * time.Hour)
	srv := deadServer(t, storedRun(t, t.TempDir(), recorded))

	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 when history can be served: %v", status, body)
	}

	live, present := body["live"]
	if !present {
		t.Fatal("the live field must always be present, so its absence cannot be read as freshness")
	}
	if live != false {
		t.Errorf("live = %v, want false for a reading served from history", live)
	}

	stale, ok := body["stale"].(map[string]any)
	if !ok {
		t.Fatal("a non-live response must carry a stale envelope")
	}
	if stale["recorded_at"] == nil || stale["recorded_at"] == "" {
		t.Error("stale.recorded_at must name when the reading was taken")
	}
	age, _ := stale["age_seconds"].(float64)
	if age < 5*3600 || age > 7*3600 {
		t.Errorf("stale.age_seconds = %v, want roughly 6h", age)
	}
	if got, _ := stale["age_human"].(string); !strings.Contains(got, "h ago") {
		t.Errorf("stale.age_human = %q, want a human age like \"6h ago\"", got)
	}

	// The figures are the stored ones, not invented.
	if body["floor_loss_pct"] != "25.02" {
		t.Errorf("floor_loss_pct = %v, want the stored 25.02", body["floor_loss_pct"])
	}
	if body["recommended"] != nil {
		t.Error("a stored null recommendation must stay null on the stale path")
	}
}

// TestStaleServesReferenceFetchedAt covers backlog #7: a stored reading must
// carry reference_fetched_at so a reader can tell how old the benchmark was
// when the reading was taken — a rate reused from the cache is older than the
// measurement itself, and hiding that would make stored history look current.
func TestStaleServesReferenceFetchedAt(t *testing.T) {
	fetchedAt := time.Date(2026, 8, 21, 21, 0, 0, 0, time.UTC)
	recorded := fetchedAt.Add(2 * time.Hour)

	// A record that carries the fetch timestamp: the field must round-trip.
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := &runstore.Record{
		RecordedAt: recorded,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api",
			AsOf:      "2026-08-21T00:00:00Z",
			FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		},
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Finding: "No usable size.",
		Rungs:   []runstore.Rung{},
	}
	if err := st.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	srv := deadServer(t, st)
	_, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")

	want := fetchedAt.UTC().Format(time.RFC3339)
	if got, _ := body["reference_fetched_at"].(string); got != want {
		t.Errorf("reference_fetched_at = %q, want %q", got, want)
	}

	// A record without it (a pre-Version-3 chain) must omit the field, not
	// invent a timestamp — an absent benchmark age is unknown, and claiming
	// one would be the default-to-current failure in a new place.
	st2, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recNoFetch := *rec
	recNoFetch.Reference.FetchedAt = ""
	if err := st2.Append(context.Background(), &recNoFetch); err != nil {
		t.Fatal(err)
	}
	srv2 := deadServer(t, st2)
	_, body2 := getJSON(t, srv2.URL+"/api/corridor?to=NGNC&sizes=100")
	if _, present := body2["reference_fetched_at"]; present {
		t.Error("reference_fetched_at present on a record that never stored one; " +
			"an unknown benchmark age must stay absent")
	}
}

// TestStaleReconstructsReferenceRoundTrip covers the reason for the
// reference_agreement/scored gap on the stale path: staleJSON was leaving
// ReferenceAgreement at "" and Scored at its zero value, so every
// history-served corridor read as unscored even when the providers agreed.
// The record carries everything needed to reconstruct the state — ScoredAgainst
// records whether verdicts were derived at all, and DivergencePct plus the
// as-of stamps decide the band — so the round trip must reproduce AGREE,
// DISAGREE, SINGLE and MALFUNCTION as they were measured.
func TestStaleReconstructsReferenceRoundTrip(t *testing.T) {
	asOf := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

	// Each case is the reference that reconcile would have persisted for that
	// agreement state, through FromCorridorJSON into a stored record.
	base := func() runstore.Record {
		return runstore.Record{
			RecordedAt: asOf.Add(time.Hour),
			Corridor:   "USDC-NGNC",
			Integrity:  "DIRECT",
			FloorLossPct: "25.02", FloorSize: "0.1",
			WorstLossPct: "97.68", WorstSize: "5000",
			Finding: "No usable size.",
			Rungs:   []runstore.Rung{},
		}
	}

	cases := []struct {
		name        string
		ref         runstore.Reference
		wantAgree   string
		wantScored  bool
	}{
		{
			name: "AGREE",
			// Providers agreed within tolerance: divergence below the agree
			// ceiling, scored against the primary.
			ref: runstore.Reference{
				Mid: "1350.2568", Source: "currency-api", AsOf: asOf.Format(time.RFC3339),
				SecondaryMid: "1348.0585", SecondarySource: "exchangerate-api",
				SecondaryAsOf: asOf.Format(time.RFC3339),
				DivergencePct: "0.16", ScoredAgainst: "currency-api",
			},
			wantAgree:  "AGREE",
			wantScored: true,
		},
		{
			name: "DISAGREE",
			// Providers differ beyond the agree ceiling but below malfunction,
			// scored conservatively against one of them.
			ref: runstore.Reference{
				Mid: "1350.2568", Source: "currency-api", AsOf: asOf.Format(time.RFC3339),
				SecondaryMid: "1417.7696", SecondarySource: "exchangerate-api",
				SecondaryAsOf: asOf.Format(time.RFC3339),
				DivergencePct: "5.00", ScoredAgainst: "currency-api",
			},
			wantAgree:  "DISAGREE",
			wantScored: true,
		},
		{
			name: "SINGLE",
			// Only one provider answered: usable and uncorroborated, scored
			// against it.
			ref: runstore.Reference{
				Mid: "1350.2568", Source: "currency-api", AsOf: asOf.Format(time.RFC3339),
				ScoredAgainst: "currency-api",
			},
			wantAgree:  "SINGLE",
			wantScored: true,
		},
		{
			name: "MALFUNCTION",
			// Providers diverge beyond the malfunction threshold: a broken
			// feed, nothing scored against either.
			ref: runstore.Reference{
				Mid: "1350.2568", Source: "currency-api", AsOf: asOf.Format(time.RFC3339),
				SecondaryMid: "1620.3081", SecondarySource: "exchangerate-api",
				SecondaryAsOf: asOf.Format(time.RFC3339),
				DivergencePct: "20.00",
			},
			wantAgree:  "MALFUNCTION",
			wantScored: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := base()
			rec.Reference = tc.ref
			out := staleJSON(&rec, "USD/NGN", time.Now().UTC())
			if out.ReferenceAgreement != tc.wantAgree {
				t.Errorf("reference_agreement = %q, want %q", out.ReferenceAgreement, tc.wantAgree)
			}
			if out.Scored != tc.wantScored {
				t.Errorf("scored = %v, want %v for %s", out.Scored, tc.wantScored, tc.name)
			}
		})
	}
}

// TestStaleReferenceHasExplicitUnknownWhenUnrecordable pins the other half of
// the contract: a record that genuinely cannot answer (no divergence recorded)
// must report an explicit UNKNOWN, never "" and never a guessed band.
func TestStaleReferenceHasExplicitUnknownWhenUnrecordable(t *testing.T) {
	// A secondary exists but no divergence was recorded — an older record
	// predating the cross-check, or a corrupted one. The record cannot say
	// whether the providers agreed, so it says so.
	rec := runstore.Record{
		RecordedAt: time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC),
		Corridor:   "USDC-NGNC", Integrity: "DIRECT",
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Finding: "No usable size.", Rungs: []runstore.Rung{},
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api", AsOf: "2026-08-21T00:00:00Z",
			SecondaryMid: "1348.0585", SecondarySource: "exchangerate-api",
		},
	}

	out := staleJSON(&rec, "USD/NGN", time.Now().UTC())
	if out.ReferenceAgreement != "UNKNOWN" {
		t.Errorf("reference_agreement = %q, want explicit UNKNOWN for an unrecordable band", out.ReferenceAgreement)
	}
}

// TestNoHistoryIsAnErrorNotANumber is the other half, and the more important
// one. With nothing stored, a failed measurement must error rather than return
// a plausible figure.
func TestNoHistoryIsAnErrorNotANumber(t *testing.T) {
	empty, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := deadServer(t, empty)

	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if status == http.StatusOK {
		t.Fatalf("status = 200 with no history and no live measurement; body: %v", body)
	}
	if body["error"] == nil {
		t.Error("expected an error message rather than a measurement-shaped body")
	}
	for _, forbidden := range []string{"floor_loss_pct", "rungs", "finding"} {
		if _, present := body[forbidden]; present {
			t.Errorf("error response carries %q; it must not resemble a measurement", forbidden)
		}
	}
}

// TestNilStoreBehavesAsBefore pins that history is optional: a server without
// one errors on a failed measurement exactly as it did before the store
// existed.
func TestNilStoreBehavesAsBefore(t *testing.T) {
	srv := deadServer(t, nil)

	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if status == http.StatusOK {
		t.Errorf("status = 200 with no store configured; body: %v", body)
	}
}

// TestLiveMeasurementIsMarkedLive is the control. Without it, a server that
// always reported live:false would pass the tests above.
func TestLiveMeasurementIsMarkedLive(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	resp, err := http.Get(srv.URL + "/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body["live"] != true {
		t.Errorf("live = %v, want true for a fresh measurement", body["live"])
	}
	if _, present := body["stale"]; present {
		t.Error("a live measurement must not carry a stale envelope")
	}
}

// TestStaleShapeMatchesLiveShape checks the two documents parse the same way.
// A different shape would mean every consumer needed two parsers, and the one
// that forgot would render a six-hour-old reading as current.
func TestStaleShapeMatchesLiveShape(t *testing.T) {
	staleSrv := deadServer(t, storedRun(t, t.TempDir(), time.Now().UTC().Add(-time.Hour)))
	_, staleBody := getJSON(t, staleSrv.URL+"/api/corridor?to=NGNC&sizes=100")

	liveSrv := testServer(t, liveNGNCPaths, "1500")
	_, liveBody := getJSON(t, liveSrv.URL+"/api/corridor?to=NGNC&sizes=100")

	// Every field a client reads off a live document must exist on a stale
	// one, so switching between them needs no special casing.
	for _, field := range []string{
		"send_asset", "receive_asset", "integrity", "reference_mid",
		"reference_source", "floor_loss_pct", "worst_loss_pct",
		"recommended", "finding", "rungs", "measured_at", "live",
	} {
		if _, ok := staleBody[field]; !ok {
			t.Errorf("stale response is missing %q, which a live response carries", field)
		}
		if _, ok := liveBody[field]; !ok {
			t.Errorf("live response is missing %q", field)
		}
	}
}

// TestHumanAge covers the rendering directly, including the boundaries.
func TestHumanAge(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "just now",
		5 * time.Minute:  "5m ago",
		6 * time.Hour:    "6h ago",
		47 * time.Hour:   "47h ago",
		72 * time.Hour:   "3d ago",
	}
	for d, want := range cases {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%s) = %q, want %q", d, got, want)
		}
	}
}

// TestUIRendersAllThreeFindingStates guards the distinction in the surface a
// human actually reads.
//
// A UI that showed only pass and fail would render "this anchor publishes no
// SEP-10 endpoint" identically to "this anchor's SEP-10 endpoint is dead" —
// discarding, at the last step, the distinction the whole check contract
// exists to preserve.
func TestUIRendersAllThreeFindingStates(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{"UNKNOWN", "PASS", "FAIL", "f-unknown", "f-pass", "f-fail"} {
		if !strings.Contains(page, want) {
			t.Errorf("the UI has no %q state; findings would render incompletely", want)
		}
	}

	// The undetermined branch must come first, so a check that did not run
	// cannot fall through into the passed/failed comparison.
	unknownAt := strings.Index(page, "!c.determined")
	passedAt := strings.Index(page, "c.passed")
	if unknownAt == -1 || passedAt == -1 || unknownAt > passedAt {
		t.Error("the UI tests c.passed before c.determined; an undetermined check " +
			"would be rendered as a failure")
	}

	// It must also say that undetermined is not a failure, since that is the
	// least intuitive part of the model.
	if !strings.Contains(page, "Undetermined is not a failure") {
		t.Error("the UI does not explain that undetermined is not a failure")
	}
}

// TestUIRendersMetrics guards the metric surface the way the finding states
// are guarded. Metrics are a different shape from checks — a value with a
// unit, not a verdict — and an undetermined metric must not look like a
// failed check.
func TestUIRendersMetrics(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	// The renderer must read f.metrics at all, and every metric must reach
	// the page as a value with its unit.
	for _, want := range []string{
		"function metrics", "f.metrics",
		"m-value", "m-unit", "m-state", "m-unknown", "UNDETERMINED",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the UI has no %q; metrics would render incompletely", want)
		}
	}

	// An undetermined metric shows its reason in place of the value, so the
	// undetermined branch must come first — a metric that did not produce a
	// value must never fall through into the value renderer.
	undeterminedAt := strings.Index(page, "!m.determined")
	valueAt := strings.Index(page, "m.value")
	if undeterminedAt == -1 || valueAt == -1 || undeterminedAt > valueAt {
		t.Error("the UI renders a metric value before testing m.determined; " +
			"an undetermined metric would render as though it had a value")
	}

	// The panel is absent, not empty, when no metrics were run.
	if !strings.Contains(page, "f.metrics.length") {
		t.Error("the metrics panel is not gated on metrics existing; it would render empty")
	}

	// Evidence source and timestamp must be reachable for each metric.
	if !strings.Contains(page, "m.observed_at") || !strings.Contains(page, "e.observed_at") {
		t.Error("the UI does not surface a metric's evidence source and timestamp")
	}

	// No metric is graded. No threshold exists for any of them, and the panel
	// must say so rather than let a colour imply a verdict.
	if !strings.Contains(page, "No threshold") {
		t.Error("the UI does not say that metrics carry no threshold")
	}
}
