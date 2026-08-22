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
