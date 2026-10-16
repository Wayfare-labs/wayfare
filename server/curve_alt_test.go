package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// The loss curve is a picture of numbers that a reader who cannot see it never
// gets. Before #305 the SVG carried a single aria-label for the whole drawing,
// and the per-point titles inside it were unreachable: role="img" makes an
// element's subtree presentational, so the only description on offer was "Loss
// against mid rising with trade size" — incomplete, and a claim about the
// shape that the measurements do not carry. The numbers themselves sat one
// element away in the Measurements table with nothing connecting the two.
//
// These tests pin the replacement: a text alternative the SVG names, built
// only from fields the engine published, and a route from the chart to the
// table that holds those numbers.

func uiPage(t *testing.T) string {
	t.Helper()
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// lossCurveSource returns the loss curve's own code — curve, its text
// alternative and the structural-state lookup — and nothing else. Assertions
// that must not be satisfied by the trend chart, which renders a different
// figure and is out of scope for this issue, are scoped through it.
func lossCurveSource(t *testing.T, page string) string {
	t.Helper()
	from := strings.Index(page, "function curve(rungs) {")
	to := strings.Index(page, "function table(rungs, scored, depends)")
	if from < 0 || to < 0 || to <= from {
		t.Fatal("the loss curve's code is no longer between curve() and table()")
	}
	return page[from:to]
}

// TestCurveNamesATextAlternative is the association itself. An
// aria-labelledby only associates something if the id it names is emitted
// alongside it, so both halves are checked: the SVG points at an id, and that
// id is a figcaption carrying the alternative.
func TestCurveNamesATextAlternative(t *testing.T) {
	page := uiPage(t)

	if !strings.Contains(page, `aria-labelledby="loss-curve-alt"`) {
		t.Error("the loss curve names no text alternative; a screen reader would " +
			"announce nothing for it")
	}
	if !strings.Contains(page, `<figcaption id="loss-curve-alt">${curveAlt(rungs)}</figcaption>`) {
		t.Error("the figcaption the SVG names is not built by curveAlt(); the " +
			"label and the alternative could drift apart")
	}

	// The drawing has to sit inside a figure for its caption to be its caption.
	// A figcaption loose in a div is associated with nothing.
	for _, want := range []string{`<figure class="chart">`, `</figure>`} {
		if !strings.Contains(page, want) {
			t.Errorf("the curve is missing %q; the caption would label nothing", want)
		}
	}
}

// TestCurveAlternativeReachesTheTable is the other half of the issue: the
// drawing and the numbers it draws should be one step apart, for a screen
// reader and for a keyboard.
func TestCurveAlternativeReachesTheTable(t *testing.T) {
	page := uiPage(t)

	if !strings.Contains(page, `href="#measurements"`) {
		t.Error("the curve offers no route to the table holding its numbers")
	}
	// The target has to exist in the same render, or the link goes nowhere.
	if !strings.Contains(page, `<h2 id="measurements">Measurements</h2>`) {
		t.Error(`no element carries id="measurements"; the link from the curve ` +
			"would land nowhere")
	}

	// The table names itself, so a reader arriving at it unprompted is told what
	// it is. Both shapes carry a caption: the scored columns, and the
	// structural-only columns the unscored path falls back to.
	if got := strings.Count(page, "<caption>"); got != 2 {
		t.Errorf("the measurements table carries %d captions, want 2: one for the "+
			"scored columns and one for the structural-only columns", got)
	}
}

// TestCurveAlternativeIsKeyboardReachableAndNotColourAlone covers what makes
// an in-page link usable rather than merely present: a focus ring a sighted
// keyboard user can see, and an underline so the link is identified by
// something other than its colour.
func TestCurveAlternativeIsKeyboardReachableAndNotColourAlone(t *testing.T) {
	page := uiPage(t)

	if !strings.Contains(page, ":focus-visible") {
		t.Error("the page sets no visible focus ring; a keyboard user following " +
			"the link cannot see where they are")
	}
	if !strings.Contains(page, "text-decoration: underline") {
		t.Error("the link below the chart is identified by colour alone")
	}
}

// TestCurveAlternativeUsesOnlyPublishedFields is the measurement-discipline
// half. The caption is prose, and prose is where a fabricated figure is
// easiest to add by accident, so it is pinned to the wire fields and nothing
// else.
func TestCurveAlternativeUsesOnlyPublishedFields(t *testing.T) {
	page := uiPage(t)

	for _, want := range []string{
		"function curveAlt(rungs)",
		"r.quote.loss_pct", // the loss, as published
		"r.quote.verdict",  // the grade, as published
		"r.send_amount",    // the size, as published
		"r.integrity",      // the structural state, as published
		"formatPct(r.quote.loss_pct)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the text alternative does not read %q; it would have to "+
				"invent the figure", want)
		}
	}

	// A second threshold comparison in the UI would be a second source of
	// truth for a maintainer-owned number. The grade is the engine's.
	if strings.Contains(page, "ThresholdGood") ||
		strings.Contains(page, "ThresholdFair") ||
		strings.Contains(page, "ThresholdPoor") {
		t.Error("the UI re-derives a verdict threshold; the grade is the engine's")
	}
}

// TestCurveAlternativeDoesNotClaimATrend is the negative case for the old
// label, and the reason this one is worth pinning.
//
// "Loss against mid rising with trade size" claims a shape. A ladder can
// fall, and a route can improve with size; the engine measures the points and
// never the trend between them. Reading the endpoints does not establish a
// trend, so the caption states the range it spans and stops there.
func TestCurveAlternativeDoesNotClaimATrend(t *testing.T) {
	page := uiPage(t)

	if strings.Contains(page, "rising with trade size") {
		t.Error("the curve still claims a direction; the engine measures points, " +
			"never the trend between them")
	}
	// Scoped to the loss curve's own code, and matched on the assignment:
	// "aria-label" is a prefix of "aria-labelledby", so the bare form is
	// "aria-label=" — anything else matches the association this test requires.
	// The trend chart carries an aria-label of its own and is a separate
	// concern from this issue.
	if strings.Contains(lossCurveSource(t, page), "aria-label=") {
		t.Error("the loss curve still carries a bare aria-label; the text " +
			"alternative must name a real element so the two cannot disagree")
	}

	// The replacement is a range, not a direction — and a range only if the
	// rungs are ordered by loss, or it would report whichever end the ladder
	// happened to be priced from.
	if !strings.Contains(page, "Loss spans ${at(byLoss[0])} to") {
		t.Error("the caption does not state the range the curve spans")
	}
	if !strings.Contains(page, "sort((a, b) =>") {
		t.Error("the caption does not order the rungs by loss; the range it " +
			"reports would depend on the order the engine priced them in")
	}
}

// TestCurveAlternativeHandlesEveryPublishedVerdict checks the alternative
// against the closed set the engine can emit, not against the set that happens
// to appear in today's fixtures.
func TestCurveAlternativeHandlesEveryPublishedVerdict(t *testing.T) {
	page := uiPage(t)

	published := []string{}
	for v := route.VerdictUnknown; ; v++ {
		published = append(published, v.String())
		if v == route.VerdictUnusable {
			break
		}
	}
	sort.Strings(published)

	// A rung can be priced and still ungraded: Quote.score leaves the verdict
	// at VerdictUnknown when the mid or the send amount is zero, and the wire
	// then carries a zero loss that is a default, not a measurement. The chart
	// still plots that rung, so the caption has to account for it separately —
	// and as not a failure.
	for _, want := range []string{
		"r.quote.verdict !== 'UNKNOWN'",
		"r.quote.verdict === 'UNUSABLE'",
		"ungraded is not a failure",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the text alternative does not handle %q; the engine "+
				"publishes the verdicts %v", want, published)
		}
	}
}

// TestCurveAlternativeNamesEveryStructuralState is the same closed-set check
// for integrity. DIRECT, DERIVATIVE and NO-MARKET are structural states, not
// severities, and the chart draws all three with the same red line — so the
// caption is the only place a reader learns which one they are looking at.
func TestCurveAlternativeNamesEveryStructuralState(t *testing.T) {
	page := uiPage(t)

	published := []string{}
	for i := route.IntegrityUnknown; i <= route.IntegrityNoMarket; i++ {
		published = append(published, i.String())
	}
	sort.Strings(published)

	for _, state := range []string{
		route.IntegrityDirect.String(),
		route.IntegrityDerivative.String(),
		route.IntegrityNoMarket.String(),
	} {
		if !strings.Contains(page, "'"+state+"':") {
			t.Errorf("the caption has no sentence for the %s state; the engine "+
				"publishes %v and the chart draws them identically", state, published)
		}
	}

	// UNKNOWN is the fourth published state, and must not be dressed up as one
	// of the other three.
	if !strings.Contains(page, "was not established") {
		t.Error("the caption has no sentence for an unestablished structural " +
			"state; it would either stay silent or name one it cannot support")
	}

	// DERIVATIVE is a structural fact; its sentence must say so, not grade it.
	if !strings.Contains(page, "intermediate asset, so the loss is measured through a dependency.") {
		t.Error("the caption does not explain what DERIVATIVE means")
	}

	// A lookup that trusted its key would fall through to Object.prototype for
	// anything outside the three, and render a function as no text at all.
	if !strings.Contains(page, "typeof known === 'string'") {
		t.Error("the structural-state lookup is unguarded; an unmapped state " +
			"would inherit from Object.prototype")
	}
}

// TestCurveAlternativeReadsRealResponses is the "checked against a real API
// response" criterion. The caption reads send_amount, loss_pct, verdict and
// integrity off each rung; this drives the states the engine really produces
// and proves those fields are on the wire in both the live and the recorded
// path.
//
// It is a field-presence check rather than a golden string on purpose: the
// values are measurements, and a test that pinned them would break every time
// the market moved — which is how tests get deleted instead of fixed.
func TestCurveAlternativeReadsRealResponses(t *testing.T) {
	cases := []struct {
		name string
		// wantCurve is whether render() reaches curve() for this response,
		// which is `d.scored && priced.length` — the gate at the call site.
		wantCurve bool
		wantState string
		body      map[string]any
	}{
		{
			name:      "live and scored, full ladder",
			wantCurve: true,
			wantState: route.IntegrityDirect.String(),
			body:      liveCorridor(t, liveNGNCPaths),
		},
		{
			// A broken corridor: nothing priced, so nothing is drawn and the
			// caption is unreachable.
			name:      "no market",
			wantCurve: false,
			wantState: route.IntegrityNoMarket.String(),
			body:      liveCorridor(t, noPaths),
		},
		{
			// The stale path: a stored record served because the upstream is
			// down. The caption is built from the same rungs and must be just
			// as complete.
			name:      "recorded and stale",
			wantCurve: true,
			wantState: route.IntegrityDirect.String(),
			body: staleCorridor(t, storedRun(t, t.TempDir(),
				time.Now().UTC().Add(-6*time.Hour))),
		},
		{
			// DERIVATIVE: every path routes through an intermediate asset.
			// The live fixtures do not reach this state, but a stored record
			// carries it verbatim and the engine publishes it as a first-class
			// structural fact, so the caption has to have a sentence for it.
			name:      "recorded and derivative",
			wantCurve: true,
			wantState: route.IntegrityDerivative.String(),
			body:      staleCorridor(t, derivativeRun(t)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.body["integrity"]; got != tc.wantState {
				t.Errorf("corridor integrity = %v, want %q", got, tc.wantState)
			}

			rungs := rungsOf(t, tc.body)
			priced := 0
			for i, r := range rungs {
				if r.sendAmount == "" {
					t.Errorf("rung %d publishes no send_amount; the caption could "+
						"not name the size it plotted", i)
				}
				if r.integrity == "" {
					t.Errorf("rung %d publishes no integrity; the caption could not "+
						"name the structural state", i)
				}
				if !r.priced {
					continue
				}
				priced++
				if r.lossPct == "" {
					t.Errorf("rung %d is priced but publishes no loss_pct; the "+
						"caption would have to invent the figure", i)
				}
				if r.verdict == "" {
					t.Errorf("rung %d is priced but publishes no verdict; the "+
						"caption could not say whether it was graded", i)
				}
			}

			scored, _ := tc.body["scored"].(bool)
			if got := scored && priced > 0; got != tc.wantCurve {
				t.Errorf("the curve would render = %v, want %v (%d of %d rungs "+
					"priced, scored=%v)", got, tc.wantCurve, priced, len(rungs), scored)
			}
		})
	}
}

// TestCurveAlternativeStaysOffAnUnscoredResponse is the negative case proper:
// a response the caption must not describe.
//
// When the reference providers disagree past the malfunction threshold the
// engine derives no loss and no verdict, and the response is unscored. The
// curve is gated on d.scored, so a caption rendered here would put loss figures
// on a page that has none — the failure the unscored block exists to prevent.
//
// The fixture matters: the stored rung really does carry a loss and a verdict,
// so the fields the caption reads are all present. Only the gate keeps them off
// the page, which is exactly the claim under test.
func TestCurveAlternativeStaysOffAnUnscoredResponse(t *testing.T) {
	page := uiPage(t)

	asOf := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	rec := &runstore.Record{
		RecordedAt:   asOf.Add(time.Hour),
		Corridor:     "USDC-NGNC",
		Integrity:    route.IntegrityDirect.String(),
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Finding: "No usable size.",
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api",
			AsOf:         asOf.Format(time.RFC3339),
			SecondaryMid: "1620.3081", SecondarySource: "exchangerate-api",
			DivergencePct: "20.00",
		},
		Rungs: []runstore.Rung{{
			SendAmount: "100", Priced: true, Integrity: route.IntegrityDirect.String(),
			ReceiveAmount: "45000", EffectiveRate: "450",
			LossPct: "0.00", Verdict: route.VerdictUnusable.String(),
			Path: "USDC -> NGNC",
		}},
	}

	body := corridorBody(t, staleJSON(rec, "USD/NGN", time.Now().UTC()))

	if scored, _ := body["scored"].(bool); scored {
		t.Fatal("a 20% provider divergence scored; the fixture is wrong, not the UI")
	}
	// live is asserted on the value, not on whether the field happens to be a
	// bool: binding the ok flag to a variable named after the field reads as
	// a check and silently passes on every value.
	if live, ok := body["live"].(bool); !ok || live {
		t.Error("the stored reading is not reported as a recorded, non-live one")
	}

	rungs := rungsOf(t, body)
	if len(rungs) != 1 || !rungs[0].priced {
		t.Fatalf("fixture rungs = %+v, want one priced rung", rungs)
	}
	// The gate is load-bearing only if the fields the caption wants are there
	// to be wrongly shown.
	if rungs[0].lossPct == "" || rungs[0].verdict == "" {
		t.Fatal("the unscored rung publishes no loss and verdict; the fixture no " +
			"longer proves the d.scored gate keeps figures off an unscored page")
	}

	if !strings.Contains(page, "d.scored && priced.length") {
		t.Error("the curve is no longer gated on d.scored; unscored responses " +
			"would be drawn and captioned")
	}
	if !strings.Contains(page, "d.scored ? recommendationBlock(d) : unscoredBlock(d)") {
		t.Error("render() no longer branches on d.scored for the recommendation")
	}
}

// TestCurveRendersBeforeTheTableItPointsAt keeps the link pointing forwards.
// The anchor is emitted with the chart and the target in the panel below, so
// the reader is sent to something they have not already passed.
func TestCurveRendersBeforeTheTableItPointsAt(t *testing.T) {
	page := uiPage(t)

	curveAt := strings.Index(page, "curve(priced)")
	tableAt := strings.Index(page, "table(d.rungs, d.scored)")
	if curveAt == -1 || tableAt == -1 {
		t.Fatal("the curve and the table are no longer both rendered by render()")
	}
	if curveAt > tableAt {
		t.Error("the curve now renders after the table; its link would point " +
			"backwards to something the reader has already passed")
	}
}

// liveCorridor measures USDC -> NGNC live against a Horizon stub, and returns
// the response body. The default size ladder is used, so the response carries
// every rung the engine prices, not one size.
func liveCorridor(t *testing.T, horizonBody string) map[string]any {
	t.Helper()
	srv := testServer(t, horizonBody, "1350")
	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&live=1")
	if status != http.StatusOK {
		t.Fatalf("GET /api/corridor = %d, want 200: %v", status, body)
	}
	return body
}

// staleCorridor returns the body of a response served from a stored record,
// which is the shape a history-first deployment shows.
func staleCorridor(t *testing.T, store runstore.Store) map[string]any {
	t.Helper()
	srv := deadServer(t, store)
	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC")
	if status != http.StatusOK {
		t.Fatalf("GET /api/corridor = %d, want 200 when history can be served: %v",
			status, body)
	}
	return body
}

// derivativeRun records a DERIVATIVE reading: every priced path routes through
// an intermediate fiat-pegged asset, so there is no market between the two
// assets themselves.
func derivativeRun(t *testing.T) runstore.Store {
	t.Helper()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := &runstore.Record{
		RecordedAt: time.Now().UTC().Add(-3 * time.Hour),
		Corridor:   "USDC-NGNC",
		Integrity:  route.IntegrityDerivative.String(),
		DependsOn:  []string{"USDT"},
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api",
			AsOf:         time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339),
			SecondaryMid: "1348.0585", SecondarySource: "exchangerate-api",
			DivergencePct: "0.16", ScoredAgainst: "currency-api",
		},
		FloorLossPct: "0.42", FloorSize: "10",
		WorstLossPct: "2.10", WorstSize: "1000",
		Finding: "Usable through a dependency.",
		Rungs: []runstore.Rung{
			{SendAmount: "10", Priced: true, Integrity: route.IntegrityDerivative.String(),
				ReceiveAmount: "135449.35", EffectiveRate: "13544.94",
				LossPct: "0.42", Verdict: route.VerdictGood.String(),
				Path: "USDC -> USDT -> NGNC"},
			{SendAmount: "1000", Priced: true, Integrity: route.IntegrityDerivative.String(),
				ReceiveAmount: "134470.12", EffectiveRate: "134.47",
				LossPct: "2.10", Verdict: route.VerdictFair.String(),
				Path: "USDC -> USDT -> NGNC"},
		},
	}
	if err := store.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	return store
}

// corridorBody marshals a real CorridorJSON back into the generic shape the
// other assertions read, so they test the wire form and not the Go struct.
func corridorBody(t *testing.T, c route.CorridorJSON) map[string]any {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// rungFields is the subset of RungJSON the caption reads, decoded off a real
// response rather than hand-built, so a rename on the wire breaks the test
// instead of silently feeding the caption undefined.
type rungFields struct {
	sendAmount string
	priced     bool
	integrity  string
	lossPct    string
	verdict    string
}

func rungsOf(t *testing.T, body map[string]any) []rungFields {
	t.Helper()
	raw, ok := body["rungs"]
	if !ok {
		t.Fatal("the response has no rungs")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var wire []struct {
		SendAmount string `json:"send_amount"`
		Priced     bool   `json:"priced"`
		Integrity  string `json:"integrity"`
		Quote      *struct {
			LossPct string `json:"loss_pct"`
			Verdict string `json:"verdict"`
		} `json:"quote"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	out := make([]rungFields, 0, len(wire))
	for _, r := range wire {
		f := rungFields{sendAmount: r.SendAmount, priced: r.Priced, integrity: r.Integrity}
		if r.Quote != nil {
			f.lossPct = r.Quote.LossPct
			f.verdict = r.Quote.Verdict
		}
		out = append(out, f)
	}
	return out
}
