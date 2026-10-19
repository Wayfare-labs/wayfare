package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The fixtures below are the shapes docs/api.md documents, not shapes derived
// from this program's output. A fixture copied out of the implementation
// proves only that the implementation is self-consistent.

// recommendedLive is the documented live, scorable, recommended response.
const recommendedLive = `{
  "send_asset": {"code": "USDC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
  "receive_asset": {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", "peg": "NGN"},
  "integrity": "DIRECT",
  "depends_on": [],
  "reference_mid": "1350.753432",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "scored": true,
  "floor_loss_pct": "4.31",
  "floor_size": "0.1",
  "worst_loss_pct": "97.23",
  "worst_size": "5000",
  "recommended": {
    "description": "USDC -> XRP -> XLM -> NGNC",
    "source": "stellar-dex",
    "receive_amount": "129.2574648",
    "effective_rate": "1292.574648",
    "loss_pct": "4.31",
    "loss_amount": "5.82",
    "verdict": "FAIR",
    "warnings": ["delivers NGNC tokens, not NGN in a bank account; redeeming to fiat is a separate step with its own cost"]
  },
  "recommended_size": "0.1",
  "live": true,
  "measured_at": "2026-08-26T14:47:17Z",
  "finding": "Best available: 4.31% below the exchangerate-api mid at 0.1 USDC, graded FAIR.",
  "rungs": [
    {"send_amount": "0.1", "priced": true, "integrity": "DIRECT", "quote": {"description": "USDC -> XLM -> NGNC", "receive_amount": "129.2574648", "effective_rate": "1292.574648", "loss_pct": "4.31", "verdict": "FAIR"}},
    {"send_amount": "5000", "priced": true, "integrity": "DIRECT", "quote": {"description": "USDC -> XLM -> NGNC", "receive_amount": "186947.8515264", "effective_rate": "37.38957030528", "loss_pct": "97.23", "loss_amount": "6566819.31", "verdict": "UNUSABLE"}}
  ]
}`

// storedReading is the same corridor served from history: live false, an age,
// and no recommendation.
const storedReading = `{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "NGNC", "peg": "NGN"},
  "integrity": "DIRECT",
  "reference_mid": "1349.669672",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "scored": true,
  "floor_loss_pct": "27.15",
  "floor_size": "0.1",
  "worst_loss_pct": "97.52",
  "worst_size": "5000",
  "recommended": null,
  "live": false,
  "stale": {"recorded_at": "2026-08-22T12:09:59Z", "age_seconds": 162000, "age_human": "45h0m0s"},
  "measured_at": "2026-08-22T12:09:59Z",
  "finding": "No usable size.",
  "rungs": [
    {"send_amount": "0.1", "priced": true, "integrity": "DIRECT", "quote": {"description": "USDC -> XLM -> NGNC", "receive_amount": "1343.07", "effective_rate": "13430.7", "loss_pct": "27.15", "verdict": "UNUSABLE"}}
  ]
}`

// unscoredMalfunction keeps the loss figures in the body and turns `scored`
// off, which is the case a careless consumer renders as a measurement.
const unscoredMalfunction = `{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "NGNC", "peg": "NGN"},
  "integrity": "DIRECT",
  "reference_mid": "1350.753432",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "MALFUNCTION",
  "reference_note": "the two providers disagreed by 41.2%, which is not a rate disagreement",
  "scored": false,
  "recommended": null,
  "live": true,
  "measured_at": "2026-09-25T09:00:00Z",
  "rungs": [
    {"send_amount": "0.1", "priced": true, "integrity": "DIRECT", "quote": {"description": "USDC -> XLM -> NGNC", "receive_amount": "1.0", "effective_rate": "10.0", "loss_pct": "97.23", "verdict": "UNUSABLE"}}
  ]
}`

// noRouteIsRecommendable is the contract's central case: every rung priced,
// none of them POOR or better, so `recommended` is present and null.
const noRouteIsRecommendable = `{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "NGNC", "peg": "NGN"},
  "integrity": "DIRECT",
  "reference_mid": "1350.753432",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "scored": true,
  "recommended": null,
  "live": true,
  "measured_at": "2026-09-25T09:00:00Z",
  "rungs": [
    {"send_amount": "0.1", "priced": true, "integrity": "DIRECT", "quote": {"description": "USDC -> XLM -> NGNC", "receive_amount": "992.0", "effective_rate": "9920.0", "loss_pct": "27.15", "verdict": "UNUSABLE"}},
    {"send_amount": "2500", "priced": false, "integrity": "UNKNOWN", "error": "horizon: no path found"}
  ]
}`

// unusableRecommendation is the combination the API does not produce and this
// program refuses anyway.
const unusableRecommendation = `{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "NGNC", "peg": "NGN"},
  "integrity": "DIRECT",
  "reference_mid": "1350.753432",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "scored": true,
  "recommended": {"description": "USDC -> XLM -> NGNC", "receive_amount": "992.0", "effective_rate": "9920.0", "loss_pct": "27.15", "verdict": "UNUSABLE", "warnings": []},
  "recommended_size": "0.1",
  "live": true,
  "measured_at": "2026-09-25T09:00:00Z",
  "rungs": []
}`

// moneyAsNumber is a response that violates ADR 006: a rate as a JSON number.
const moneyAsNumber = `{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "NGNC", "peg": "NGN"},
  "integrity": "DIRECT",
  "reference_mid": 1350.753432,
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "scored": true,
  "live": true,
  "measured_at": "2026-09-25T09:00:00Z",
  "rungs": []
}`

// consumeFixture serves one body and runs the consumer against it.
func consumeFixture(t *testing.T, status int, body string) (int, string, error) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/corridor" {
			t.Errorf("requested %s, want /api/corridor", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	var out strings.Builder
	code, err := consume(context.Background(), srv.Client(), srv.URL, "USDC", "NGNC", false, 5*time.Second, &out)
	return code, out.String(), err
}

func TestConsumeRendersARecommendation(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, recommendedLive)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if code != exitRendered {
		t.Errorf("exit code = %d, want %d", code, exitRendered)
	}
	for _, want := range []string{
		"USDC", "NGNC",
		"measured now", // live is true, and said so
		"129.2574648",  // the receive amount, as it crossed the wire
		"FAIR",         // the verdict
		"97.23",        // the worst rung is part of the picture
		"USDC -> XRP -> XLM -> NGNC",
		"delivers NGNC tokens", // the warning travels with the figure
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "STORED READING") {
		t.Errorf("a live response was labelled as stored:\n%s", out)
	}
}

// TestConsumeLabelsAStoredReading is the freshness rule: live is not a verdict
// and a stored reading must carry its age rather than be presented as current.
func TestConsumeLabelsAStoredReading(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, storedReading)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if code != exitNothing {
		t.Errorf("exit code = %d, want %d (no recommendation in a stored reading)", code, exitNothing)
	}
	if !strings.Contains(out, "STORED READING") {
		t.Errorf("a stored reading was not labelled as one:\n%s", out)
	}
	if !strings.Contains(out, "45h0m0s") {
		t.Errorf("the reading's age is not in the output:\n%s", out)
	}
	if strings.Contains(out, "measured now") {
		t.Errorf("a stored reading was presented as a measurement taken now:\n%s", out)
	}
	// "recommended: null" must read as a decision, not as missing data.
	if !strings.Contains(out, "no size produced a verdict") {
		t.Errorf("a null recommendation was not explained:\n%s", out)
	}
}

// TestConsumeRefusesToScoreAnUnscorableReference is the rule the whole envelope
// exists for: with no scorable reference there is nothing to score against, so
// the loss figures in the body are not rendered even though they are present.
func TestConsumeRefusesToScoreAnUnscorableReference(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, unscoredMalfunction)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if code != exitNothing {
		t.Errorf("exit code = %d, want %d", code, exitNothing)
	}
	if !strings.Contains(out, "MALFUNCTION") {
		t.Errorf("the reference agreement is not in the output:\n%s", out)
	}
	if !strings.Contains(out, "No loss figures") {
		t.Errorf("the refusal to score is not stated:\n%s", out)
	}
	for _, unwanted := range []string{"97.23", "UNUSABLE", "27.15"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an unscored response rendered %q:\n%s", unwanted, out)
		}
	}
}

// TestConsumeNeverInventsARoute covers the recommendation rule: a null
// recommendation is a decision, not an absence of data, and the best of a bad
// set must not be substituted for it.
func TestConsumeNeverInventsARoute(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, noRouteIsRecommendable)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if code != exitNothing {
		t.Errorf("exit code = %d, want %d", code, exitNothing)
	}
	if !strings.Contains(out, "recommended none") {
		t.Errorf("output does not say nothing is recommended:\n%s", out)
	}
	if strings.Contains(out, "recommended 0.1") {
		t.Errorf("a route was recommended from a set with no recommendable size:\n%s", out)
	}
	// An unpriced rung is a different fact from a rung that measured nothing.
	if !strings.Contains(out, "not measured: horizon: no path found") {
		t.Errorf("an unpriced rung was not reported with its error:\n%s", out)
	}
}

// TestConsumeWithholdsAVerdictItShouldNotRender is the defensive refusal: a
// recommendation whose own verdict is UNUSABLE is not rendered, because a
// recommendation implies its winner is worth taking.
func TestConsumeWithholdsAVerdictItShouldNotRender(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, unusableRecommendation)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if code != exitNothing {
		t.Errorf("exit code = %d, want %d", code, exitNothing)
	}
	if !strings.Contains(out, "withheld") {
		t.Errorf("an UNUSABLE recommendation was not withheld:\n%s", out)
	}
	if strings.Contains(out, "recommended 0.1") {
		t.Errorf("an UNUSABLE route was rendered as a recommendation:\n%s", out)
	}
}

// TestConsumeSurfacesAnErrorResponse: an error body is not a measurement, and
// nothing is rendered from one.
func TestConsumeSurfacesAnErrorResponse(t *testing.T) {
	body := `{"error": "measuring corridor: no size could be measured", "code": "measurement_failed"}`
	code, out, err := consumeFixture(t, http.StatusBadGateway, body)
	if err == nil {
		t.Fatal("an HTTP 502 was accepted as a corridor")
	}
	if code != exitUnread {
		t.Errorf("exit code = %d, want %d", code, exitUnread)
	}
	if out != "" {
		t.Errorf("output was produced from an error body:\n%s", out)
	}
	if !strings.Contains(err.Error(), "measurement_failed") {
		t.Errorf("the machine-readable code is not in the error: %v", err)
	}
	if !strings.Contains(err.Error(), "no size could be measured") {
		t.Errorf("the message is not in the error: %v", err)
	}
}

// TestConsumeRejectsMoneyAsANumber is the money boundary: a rate as a JSON
// number is a decode failure, not a figure quietly rounded into a float64.
func TestConsumeRejectsMoneyAsANumber(t *testing.T) {
	code, out, err := consumeFixture(t, http.StatusOK, moneyAsNumber)
	if err == nil {
		t.Fatal("a JSON number in a money field was decoded as a figure")
	}
	if code != exitUnread {
		t.Errorf("exit code = %d, want %d", code, exitUnread)
	}
	if out != "" {
		t.Errorf("output was produced from a body that could not be decoded:\n%s", out)
	}
}

// TestOptionalIdentityFieldsDecode pins the fields this program declares but
// does not render. A consumer asking "who exactly is this asset" reads `asset`
// and `peg` rather than a bare code, and a response that stopped carrying them
// must not look the same as one that carries them.
func TestOptionalIdentityFieldsDecode(t *testing.T) {
	const doc = `{
  "send_asset": {"code": "USDC", "issuer": "GISSUER", "asset": "stellar:USDC:GISSUER"},
  "receive_asset": {"code": "NGNC", "issuer": "GOTHER", "peg": "NGN", "asset": "stellar:NGNC:GOTHER"},
  "reference_agreement": "AGREE",
  "scored": true,
  "live": true,
  "recommended": {
    "description": "USDC -> NGNC", "source": "stellar-dex", "receive_amount": "1",
    "effective_rate": "1", "loss_pct": "0", "loss_amount": "0", "verdict": "GOOD", "warnings": []
  },
  "rungs": [
    {"send_amount": "1", "priced": true, "integrity": "DIRECT",
     "quote": {"description": "USDC -> NGNC", "receive_amount": "1", "effective_rate": "1", "loss_pct": "0", "verdict": "GOOD"}}
  ]
}`

	var got corridorDoc
	if err := json.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if got.SendAsset.Asset != "stellar:USDC:GISSUER" {
		t.Errorf("send_asset.asset = %q, want the SEP-38 identity form", got.SendAsset.Asset)
	}
	if got.ReceiveAsset.Peg != "NGN" {
		t.Errorf("receive_asset.peg = %q, want NGN", got.ReceiveAsset.Peg)
	}
	if got.Recommended.Source != "stellar-dex" {
		t.Errorf("recommended.source = %q", got.Recommended.Source)
	}
	if got.Recommended.LossAmount != "0" {
		t.Errorf("recommended.loss_amount = %q, want it decoded as a string", got.Recommended.LossAmount)
	}
	if got.Rungs[0].Integrity != "DIRECT" {
		t.Errorf("rungs[0].integrity = %q", got.Rungs[0].Integrity)
	}
}

func TestConsumeRefusesABaseURLThatIsNotHTTP(t *testing.T) {
	var out strings.Builder
	code, err := consume(context.Background(), &http.Client{}, "ftp://example.test",
		"USDC", "NGNC", false, time.Second, &out)
	if err == nil {
		t.Fatal("a non-http base URL was accepted")
	}
	if code != exitUnread {
		t.Errorf("exit code = %d, want %d", code, exitUnread)
	}
}

// TestCorridorURLCarriesOnlyKnownParameters pins the request shape: an asset
// code is a value, never a second parameter or a path segment.
func TestCorridorURLCarriesOnlyKnownParameters(t *testing.T) {
	got, err := corridorURL("https://example.test/", "USDC", "NGNC", false)
	if err != nil {
		t.Fatalf("corridorURL: %v", err)
	}
	if got != "https://example.test/api/corridor?from=USDC&to=NGNC" {
		t.Errorf("URL = %q", got)
	}

	withLive, err := corridorURL("https://example.test", "USDC", "NGNC", true)
	if err != nil {
		t.Fatalf("corridorURL: %v", err)
	}
	if !strings.Contains(withLive, "live=1") {
		t.Errorf("live=1 is missing from %q; without it a history-first instance "+
			"answers from history", withLive)
	}

	escaped, err := corridorURL("https://example.test", "US&DC", "NGNC", false)
	if err != nil {
		t.Fatalf("corridorURL: %v", err)
	}
	if strings.Contains(escaped, "US&DC") {
		t.Errorf("an asset code was interpolated unescaped: %q", escaped)
	}
}
