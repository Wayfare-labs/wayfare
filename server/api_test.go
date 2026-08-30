package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// liveNGNCPaths is the real Horizon strict-send body for USDC -> NGNC,
// mainnet, 2026-08-04. The XLM-bridged path pays 65,100 NGNC.
const liveNGNCPaths = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4", "destination_asset_code": "NGNC",
        "destination_amount": "65100.1379550",
        "path": [ { "asset_type": "native" } ]
      }
    ]
  }
}`

const noPaths = `{"_embedded":{"records":[]}}`

func testServer(t *testing.T, horizonBody, mid string) *httptest.Server {
	t.Helper()
	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(horizonBody))
	}))
	t.Cleanup(horizon.Close)

	s := &Server{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: horizon.URL},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString(mid),
				"USD/GHS": decimal.RequireFromString("11.7625"),
				"USD/KES": decimal.RequireFromString("129.4263"),
			}),
		},
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding %s: %v", url, err)
	}
	return resp.StatusCode, body
}

// TestBrokenCorridorReturnsNullRecommendation is the API's headline invariant.
//
// The engine refuses to recommend an unusable route; this proves the refusal
// survives serialisation. A client reading `recommended` must get JSON null,
// not the best-scoring quote demoted to a field it might render anyway.
func TestBrokenCorridorReturnsNullRecommendation(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}

	rec, present := body["recommended"]
	if !present {
		t.Fatal("the recommended field must always be present, so a client cannot miss it")
	}
	if rec != nil {
		t.Errorf("recommended = %v, want null on a corridor losing 56%% of the money", rec)
	}
	if _, ok := body["recommended_size"]; ok {
		t.Error("recommended_size must be omitted when there is no recommendation")
	}

	finding, _ := body["finding"].(string)
	if !strings.Contains(finding, "No usable size") {
		t.Errorf("finding = %q, want it to state that no size is usable", finding)
	}
}

// TestGoodCorridorIsRecommended is the control: the same pipeline must produce
// a non-null recommendation when the rate is actually acceptable. Without it,
// a server that always returned null would pass the test above.
func TestGoodCorridorIsRecommended(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	_, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	rec, _ := body["recommended"].(map[string]any)
	if rec == nil {
		t.Fatal("expected a recommendation when the route is within 3% of mid")
	}
	if got := rec["verdict"]; got != "GOOD" {
		t.Errorf("verdict = %v, want GOOD", got)
	}
	if body["recommended_size"] != "100" {
		t.Errorf("recommended_size = %v, want 100", body["recommended_size"])
	}
}

// TestNoMarketIsReportedAsItsOwnState covers the KESC shape: the response must
// say NO-MARKET rather than grading an absent price as merely unusable.
func TestNoMarketIsReportedAsItsOwnState(t *testing.T) {
	srv := testServer(t, noPaths, "1500")

	_, body := getJSON(t, srv.URL+"/api/corridor?to=KESC&sizes=1,100")
	if got := body["integrity"]; got != "NO-MARKET" {
		t.Errorf("integrity = %v, want NO-MARKET", got)
	}
	if body["recommended"] != nil {
		t.Error("recommended must be null when no market exists")
	}

	finding, _ := body["finding"].(string)
	if !strings.Contains(finding, "No market") {
		t.Errorf("finding = %q, want it to name the no-market state", finding)
	}
	if !strings.Contains(finding, "absence of a price") {
		t.Errorf("finding = %q, want absence distinguished from bad pricing", finding)
	}

	rungs, _ := body["rungs"].([]any)
	if len(rungs) != 2 {
		t.Fatalf("expected 2 rungs, got %d", len(rungs))
	}
	for _, r := range rungs {
		rung, _ := r.(map[string]any)
		if rung["priced"] != false {
			t.Error("a no-market rung must report priced=false")
		}
		if rung["integrity"] != "NO-MARKET" {
			t.Errorf("rung integrity = %v, want NO-MARKET", rung["integrity"])
		}
	}
}

// TestMoneyCrossesTheWireAsStrings guards the float64 invariant at the
// boundary. A JSON number invites the client to parse a rate into a float,
// reintroducing exactly the rounding error the engine avoids internally.
func TestMoneyCrossesTheWireAsStrings(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	resp, err := http.Get(srv.URL + "/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, field := range []string{"reference_mid", "floor_loss_pct", "worst_loss_pct"} {
		raw, ok := body[field]
		if !ok {
			t.Errorf("%s missing from the response", field)
			continue
		}
		if !strings.HasPrefix(string(raw), `"`) {
			t.Errorf("%s = %s, want a quoted string so clients cannot parse it as a float",
				field, raw)
		}
	}
}

// TestUnknownAssetIsRejected checks that an unverified asset code is an error
// rather than a guess at an issuer.
func TestUnknownAssetIsRejected(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	status, body := getJSON(t, srv.URL+"/api/corridor?to=SCAMC")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "unknown receive asset") {
		t.Errorf("error = %q, want it to name the unknown asset", msg)
	}
}

// TestBadSizesAreRejected covers the request bounds: a non-numeric size, a
// non-positive one, and more sizes than the endpoint will accept.
func TestBadSizesAreRejected(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	cases := map[string]string{
		"not a number": "sizes=abc",
		"zero":         "sizes=0",
		"negative":     "sizes=-5",
		"too many":     "sizes=" + strings.TrimSuffix(strings.Repeat("1,", maxSizes+1), ","),
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			status, _ := getJSON(t, srv.URL+"/api/corridor?to=NGNC&"+q)
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", status)
			}
		})
	}
}

// TestRungsAreOrderedBySize pins deterministic output. The ladder prices
// concurrently, so without an explicit sort the rows would arrive in whatever
// order Horizon happened to answer.
func TestRungsAreOrderedBySize(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	_, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=1000,1,100,10")
	rungs, _ := body["rungs"].([]any)
	if len(rungs) != 4 {
		t.Fatalf("expected 4 rungs, got %d", len(rungs))
	}

	want := []string{"1", "10", "100", "1000"}
	for i, r := range rungs {
		rung, _ := r.(map[string]any)
		if rung["send_amount"] != want[i] {
			t.Errorf("rung %d send_amount = %v, want %s", i, rung["send_amount"], want[i])
		}
	}
}

// TestUIIsServed checks the embedded single-file UI reaches the browser and
// carries no external dependencies, since a build step is what it exists to
// avoid.
func TestUIIsServed(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	page := string(raw)

	if !strings.Contains(page, "corridor integrity monitor") {
		t.Error("UI does not identify itself")
	}
	for _, external := range []string{"src=\"http", "href=\"http", "cdn."} {
		if strings.Contains(page, external) {
			t.Errorf("UI references an external asset (%q); it must be self-contained", external)
		}
	}
}

func TestHealthz(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	status, body := getJSON(t, srv.URL+"/healthz")
	if status != http.StatusOK || body["status"] != "ok" {
		t.Errorf("healthz = %d %v", status, body)
	}
}

// TestUIScoredTrueRendersVerdicts checks that when scored is true (the normal
// path through NewStatic), the UI source contains the code to render verdicts,
// loss curve, and recommendation block.
func TestUIScoredTrueRendersVerdicts(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		"recommendationBlock(d)",
		"curve(priced)",
		"verdictClass(q.verdict)",
		"loss_pct",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; scored=true rendering would be incomplete", want)
		}
	}
}

// TestUIScoredFalseSuppressesVerdicts checks that the UI has all the code
// paths needed to suppress verdicts, loss curve, and recommendation when
// scored is false. It also checks that the unscoredBlock function exists and
// shows both provider mids and the divergence percentage.
func TestUIScoredFalseSuppressesVerdicts(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	// The unscored block function must exist and show both mids.
	for _, want := range []string{
		"unscoredBlock(d)",
		"reference_secondary_mid",
		"reference_divergence_pct",
		"No verdict can be issued",
		"The two reference providers",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; scored=false path would be incomplete", want)
		}
	}

	// The render function must branch on d.scored for the recommendation.
	if !strings.Contains(page, "d.scored ? recommendationBlock(d) : unscoredBlock(d)") {
		t.Error("render() does not branch on d.scored; verdicts would render when unscored")
	}

	// The curve must be gated on d.scored.
	if !strings.Contains(page, "d.scored && priced.length") {
		t.Error("loss curve is not gated on d.scored; it would render when unscored")
	}

	// The table function must accept a scored parameter.
	if !strings.Contains(page, "function table(rungs, scored)") {
		t.Error("table() does not accept a scored parameter")
	}

	// The table must suppress loss and verdict columns when unscored.
	if !strings.Contains(page, "Loss and verdict are omitted") {
		t.Error("table() does not document the unscored column suppression")
	}

	// The verdict threshold legend must be gated on d.scored.
	if !strings.Contains(page, "if (d.scored) parts.push(legend())") {
		t.Error("legend is not gated on d.scored; it would render when unscored")
	}
}

// dexClientAt builds a dex client pointed at a test Horizon.
func dexClientAt(url string) *dex.Client {
	return &dex.Client{HorizonURL: url}
}

// TestFindingsMetricsSchema pins the findings.metrics wire shape — the field
// checks.FindingsJSON already declares and ToJSON already serialises, but that
// has never been exercised on the corridor response because nothing yet
// produces metrics. Until a measurement is on the wire it does not exist for a
// consumer, and a client that starts rendering metrics against a shape that
// quietly changed would misread every number; this test makes the shape a
// contract that fails on drift.
//
// It exercises the same composition the live handler uses: a rendered corridor
// passed through route.WithFindings, exactly as handleCorridor calls it with
// the checks runner's output. The runner producing metrics is the separate
// keystone (issue #49); the point here is that when it lands, the wire is
// already pinned.
func TestFindingsMetricsSchema(t *testing.T) {
	t.Run("corridor with metrics returns them under findings.metrics", func(t *testing.T) {
		corridor := route.WithFindings(
			route.ToCorridorJSON(wellPopulatedLadderResult(), "USD/NGN"),
			metricsSchemaFixture())

		metrics := corridorMetrics(t, corridor)
		if len(metrics) != 2 {
			t.Fatalf("findings.metrics carries %d entries, want 2", len(metrics))
		}

		// The determined metric carries every field MetricJSON declares,
		// value as a decimal string, and no reason — a measured quantity
		// has nothing to explain.
		det := metrics[0]
		assertMetricKeys(t, det, map[string]bool{
			"id": true, "scope": true, "subject": true, "determined": true,
			"value": true, "unit": true, "summary": true,
			"evidence": true, "observed_at": true,
		})
		if string(det["determined"]) != "true" {
			t.Errorf("determined metric: determined = %s, want true", det["determined"])
		}
		if got := metricString(t, det, "value"); got != "0.0004" {
			t.Errorf("determined metric: value = %q, want the decimal string \"0.0004\"", got)
		}
		if got := metricString(t, det, "unit"); got != "ratio" {
			t.Errorf("determined metric: unit = %q, want ratio", got)
		}
		if _, ok := det["reason"]; ok {
			t.Error("determined metric must not carry a reason key")
		}
		// Evidence must be present and an array — a measurement without
		// evidence is an assertion, and the array must survive even when
		// empty.
		var evidence []json.RawMessage
		if err := json.Unmarshal(det["evidence"], &evidence); err != nil {
			t.Errorf("determined metric: evidence = %s, want a JSON array", det["evidence"])
		}

		// The undetermined metric is the same field set minus value, plus
		// reason. Absence of the entry would mean the metric was not run;
		// presence with no value means it ran and could not determine.
		// Those are different facts, and the response must keep them
		// different — the same tri-state the checks carry.
		und := metrics[1]
		assertMetricKeys(t, und, map[string]bool{
			"id": true, "scope": true, "subject": true, "determined": true,
			"unit": true, "summary": true, "reason": true,
			"evidence": true, "observed_at": true,
		})
		if string(und["determined"]) != "false" {
			t.Errorf("undetermined metric: determined = %s, want false", und["determined"])
		}
		if _, ok := und["value"]; ok {
			t.Error("undetermined metric must not carry a value key — a missing measurement must not read as zero")
		}
		if got := metricString(t, und, "reason"); got == "" {
			t.Error("undetermined metric: reason is empty")
		}
	})

	t.Run("a metric that was not run does not appear at all", func(t *testing.T) {
		f := &checks.Findings{}
		f.Add(schemaCheck())

		corridor := route.WithFindings(
			route.ToCorridorJSON(wellPopulatedLadderResult(), "USD/NGN"),
			f)

		raw, err := json.Marshal(corridor)
		if err != nil {
			t.Fatalf("marshaling the corridor: %v", err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshaling the corridor: %v", err)
		}
		findings, ok := doc["findings"]
		if !ok {
			t.Fatal("the findings block must be present when checks ran")
		}
		var fblock map[string]json.RawMessage
		if err := json.Unmarshal(findings, &fblock); err != nil {
			t.Fatalf("unmarshaling findings: %v", err)
		}
		if _, ok := fblock["metrics"]; ok {
			t.Error("findings.metrics must be absent when no metric ran — presence would read as \"measured, nothing found\"")
		}
	})
}

// metricsSchemaFixture returns findings carrying one determined metric and one
// undetermined metric — the two shapes findings.metrics can contain — plus one
// passing check so the block is anchored the way a real sweep's is.
func metricsSchemaFixture() *checks.Findings {
	at := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	f := &checks.Findings{}
	f.Add(schemaCheck())

	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "spread.bid-ask", Scope: checks.ScopeAsset, Subject: "NGNC",
			At: at, Determined: true,
			Evidence: []checks.Evidence{{
				Source:     "https://horizon.stellar.org/order_book",
				Observed:   "0.0004",
				ObservedAt: at,
			}},
		},
		Value: decimal.RequireFromString("0.0004"), Unit: checks.UnitRatio,
		Summary: "bid-ask spread on the USDC/NGNC book",
	})
	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "depth.observed-executable", Scope: checks.ScopeAsset, Subject: "NGNC",
			At: at, Determined: false,
			Reason: "no executable side at 5000 USDC",
		},
		Unit:    checks.UnitAmount,
		Summary: "could not determine: no executable side at 5000 USDC",
	})
	return f
}

// schemaCheck builds one passing check result, the checks-side anchor every
// findings fixture here carries.
func schemaCheck() checks.CheckResult {
	return checks.Pass(
		checks.Descriptor{
			ID: "anchor-asset-iso4217", Scope: checks.ScopeAnchor,
			Severity: checks.SeverityNotice, Title: "anchor_asset names a fiat currency",
			CanDetermine:    "the anchor names its fiat currency",
			CannotDetermine: "when the asset is not a fiat currency",
		},
		checks.Subject{Domain: "ngnc.online"},
		"anchor_asset names the shilling",
	)
}

// corridorMetrics decodes a rendered corridor and returns its findings.metrics
// entries as raw JSON, failing if the findings block or the metrics array is
// missing.
func corridorMetrics(t *testing.T, corridor route.CorridorJSON) []map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(corridor)
	if err != nil {
		t.Fatalf("marshaling the corridor: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshaling the corridor: %v", err)
	}
	findings, ok := doc["findings"]
	if !ok {
		t.Fatal("the corridor response carries no findings block")
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(findings, &f); err != nil {
		t.Fatalf("unmarshaling findings: %v", err)
	}
	metrics, ok := f["metrics"]
	if !ok {
		t.Fatal("findings carries no metrics key")
	}
	var out []map[string]json.RawMessage
	if err := json.Unmarshal(metrics, &out); err != nil {
		t.Fatalf("unmarshaling findings.metrics: %v", err)
	}
	return out
}

// assertMetricKeys fails unless the entry's keys are exactly want — no more,
// no fewer. That is what makes the test fail on drift: rename a JSON tag, add
// a field, or drop one, and the set stops matching.
func assertMetricKeys(t *testing.T, entry map[string]json.RawMessage, want map[string]bool) {
	t.Helper()
	for k := range entry {
		if !want[k] {
			t.Errorf("metric entry carries unexpected key %q — the wire shape has drifted", k)
		}
	}
	for k := range want {
		if _, ok := entry[k]; !ok {
			t.Errorf("metric entry is missing key %q — the wire shape has drifted", k)
		}
	}
}

// metricString decodes a metric entry field as a JSON string, failing if the
// field is missing or is not a string — a JSON number would slip past a
// client that expects decimal strings.
func metricString(t *testing.T, entry map[string]json.RawMessage, field string) string {
	t.Helper()
	raw, ok := entry[field]
	if !ok {
		t.Fatalf("metric entry is missing %q", field)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s = %s, want a JSON string (decimal strings cross the wire, never numbers)", field, raw)
	}
	return s
}
