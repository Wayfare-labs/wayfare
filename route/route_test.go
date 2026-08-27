package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// liveStrictSendResponse is the actual body Horizon returned for
//
//	/paths/strict-send?source_asset=USDC&source_amount=100&destination_assets=NGNC
//
// on mainnet, 2026-08-04. Both records are real: the XLM-bridged path paying
// 65,100 NGNC and the direct market paying 21,786.
//
// It is used unmodified as the fixture because the central claim of this
// project — that the corridor's best available route is not worth taking — is
// only as credible as the data behind it.
const liveStrictSendResponse = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "65100.1379550",
        "path": [ { "asset_type": "native" } ]
      },
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "21785.7821141",
        "path": []
      }
    ]
  }
}`

func horizonStub(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func usdToNGN(rate string) refrate.Provider {
	return refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString(rate),
	})
}

func ngnRequest(amount string) Request {
	return Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.RequireFromString(amount),
		ReceiveAsset:   asset.NGNC(),
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	}
}

// TestDualMid covers the parallel-rate dimension: a second reference reported
// alongside the official one, never blended into it and never able to move the
// official verdict.
func TestDualMid(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	// Baseline: no parallel source configured. The official rate scores as
	// usual and the parallel dimension is absent entirely — not
	// UNABLE-TO-DETERMINE, which would imply a source was asked and failed.
	base := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	baseRes, err := base.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if baseRes.Parallel != nil {
		t.Fatalf("expected no parallel dimension without a source, got %+v", baseRes.Parallel)
	}
	if len(baseRes.Quotes) != 1 {
		t.Fatalf("expected 1 official quote, got %d", len(baseRes.Quotes))
	}
	officialVerdict := baseRes.Quotes[0].Verdict
	officialLoss := baseRes.Quotes[0].LossPct

	// With a parallel source, the parallel mid is reported separately, the gap
	// to the official mid is derived, and the official verdict and loss are
	// byte-for-byte what they were without it.
	dual := &Engine{
		DEX:      &dex.Client{HorizonURL: srv.URL},
		RefRate:  usdToNGN("1500"),
		Parallel: usdToNGN("1650"),
	}
	dualRes, err := dual.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if dualRes.Parallel == nil || !dualRes.Parallel.Reported() {
		t.Fatalf("expected a reported parallel dimension, got %+v", dualRes.Parallel)
	}
	if !dualRes.Parallel.Mid.Equal(decimal.RequireFromString("1650")) {
		t.Fatalf("parallel mid = %s, want 1650", dualRes.Parallel.Mid)
	}
	// (1650 - 1500) / 1500 * 100 = 10%.
	if dualRes.Parallel.GapPct.Round(4).String() != "10" {
		t.Fatalf("parallel gap = %s, want 10", dualRes.Parallel.GapPct.Round(4))
	}
	if dualRes.ReferenceMid.String() != "1500" {
		t.Fatalf("official mid changed to %s", dualRes.ReferenceMid)
	}
	if got := dualRes.Quotes[0].Verdict; got != officialVerdict {
		t.Fatalf("official verdict moved from %s to %s", officialVerdict, got)
	}
	if got := dualRes.Quotes[0].LossPct; !got.Equal(officialLoss) {
		t.Fatalf("official loss moved from %s to %s", officialLoss, got)
	}
}

// TestLiveCorridorIsRefused is the project's headline test.
//
// Given the real mainnet path data and a realistic USD/NGN mid of 1,500, the
// engine must decline to recommend anything. The best route pays 651 NGN per
// USD against a mid of 1,500 — a 56.6% loss. Ranking alone would have
// happily crowned it the winner.
func TestLiveCorridorIsRefused(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{
		DEX:     &dex.Client{HorizonURL: srv.URL},
		RefRate: usdToNGN("1500"),
	}

	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if len(res.Quotes) != 1 {
		t.Fatalf("expected 1 quote (the best DEX path), got %d", len(res.Quotes))
	}
	q := res.Quotes[0]

	// Best path must be the XLM bridge, not the direct market.
	if got, want := q.ReceiveAmount.String(), "65100.137955"; got != want {
		t.Errorf("ReceiveAmount = %s, want %s (the XLM-bridged path)", got, want)
	}
	if !strings.Contains(q.Description, "XLM") {
		t.Errorf("Description = %q, want the XLM hop named", q.Description)
	}

	// 65100.137955 / 100 = 651.00137955 NGN per USD
	if got := q.EffectiveRate.StringFixed(2); got != "651.00" {
		t.Errorf("EffectiveRate = %s, want 651.00", got)
	}

	// (1500 - 651.00) / 1500 = 56.6%
	if got := q.LossPct.StringFixed(1); got != "56.6" {
		t.Errorf("LossPct = %s, want 56.6", got)
	}

	if q.Verdict != VerdictUnusable {
		t.Errorf("Verdict = %s, want UNUSABLE", q.Verdict)
	}

	if res.Viable() {
		t.Fatal("engine recommended a route that loses 56.6% of the sender's money")
	}
	if res.Recommended != nil {
		t.Fatal("Recommended must be nil when every route is unusable")
	}

	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "No viable route") {
		t.Errorf("expected an explicit no-viable-route note, got: %v", res.Notes)
	}
}

// TestLossAmountIsInRecipientCurrency checks the figure a user actually
// reacts to: not a percentage, but how much naira went missing.
func TestLossAmountIsInRecipientCurrency(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	// At mid, 100 USD is 150,000 NGN. The route delivers 65,100.14, so
	// 84,899.86 NGN is lost.
	if got, want := res.Quotes[0].LossAmount.StringFixed(2), "84899.86"; got != want {
		t.Errorf("LossAmount = %s, want %s", got, want)
	}
}

// TestGoodRouteIsRecommended is the control: with a mid the route can
// actually meet, the same machinery must recommend it. Without this, a test
// suite proving the engine refuses things would be satisfied by an engine
// that refuses everything.
func TestGoodRouteIsRecommended(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	// A mid of 660 puts the 651 route about 1.4% below mid.
	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("660")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if !res.Viable() {
		t.Fatal("expected a viable route when the rate is close to mid")
	}
	if res.Recommended.Verdict != VerdictGood {
		t.Errorf("Verdict = %s, want GOOD", res.Recommended.Verdict)
	}
}

// TestTokenDeliveryIsDisclosed ensures the user is told the on-chain leg ends
// in NGNC rather than naira in a bank account.
//
// The payout currency is named from the token's registered peg rather than
// hardcoded, so the same disclosure is correct on a GHS or KES corridor.
func TestTokenDeliveryIsDisclosed(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("660")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	warnings := strings.Join(res.Quotes[0].Warnings, " ")
	if !strings.Contains(warnings, "delivers NGNC tokens, not NGN in a bank account") {
		t.Errorf("expected a token-delivery warning, got: %v", res.Quotes[0].Warnings)
	}
}

// TestReferenceRateIsRequired pins the engine's central dependency. Degrading
// to "rank without scoring" when the reference rate is missing is exactly the
// behaviour that would let a 56% loss be presented as a winner.
func TestReferenceRateIsRequired(t *testing.T) {
	e := &Engine{DEX: &dex.Client{HorizonURL: "http://example.invalid"}}
	if _, err := e.Quote(context.Background(), ngnRequest("100")); err == nil {
		t.Fatal("expected Quote to fail without a reference rate provider")
	}
}

// TestUnknownReferencePairFails checks that a missing rate is an error rather
// than a silent zero, which would make every route look infinitely good.
func TestUnknownReferencePairFails(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{
		DEX:     &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{"EUR/GBP": decimal.NewFromInt(1)}),
	}
	if _, err := e.Quote(context.Background(), ngnRequest("100")); err == nil {
		t.Fatal("expected an error when the reference pair is unavailable")
	}
}

func TestRejectsNonPositiveAmount(t *testing.T) {
	e := &Engine{RefRate: usdToNGN("1500")}
	for _, amt := range []string{"0", "-5"} {
		if _, err := e.Quote(context.Background(), ngnRequest(amt)); err == nil {
			t.Errorf("amount %s: expected an error", amt)
		}
	}
}

func TestVerdictThresholds(t *testing.T) {
	cases := []struct {
		loss string
		want Verdict
	}{
		{"0", VerdictGood},
		{"3", VerdictGood},
		{"3.01", VerdictFair},
		{"8", VerdictFair},
		{"8.01", VerdictPoor},
		{"20", VerdictPoor},
		{"20.01", VerdictUnusable},
		{"56.6", VerdictUnusable},
	}
	for _, c := range cases {
		if got := verdictFor(decimal.RequireFromString(c.loss)); got != c.want {
			t.Errorf("verdictFor(%s) = %s, want %s", c.loss, got, c.want)
		}
	}
}

// TestNoPathsProducesNoQuotes covers a corridor Horizon cannot route at all.
func TestNoPathsProducesNoQuotes(t *testing.T) {
	srv := horizonStub(t, `{"_embedded":{"records":[]}}`)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if res.Viable() || len(res.Quotes) != 0 {
		t.Fatal("expected no quotes and no recommendation")
	}
	if len(res.Notes) == 0 {
		t.Error("expected a note explaining that nothing could be priced")
	}
}

// integrity taxonomy -----------------------------------------------------------

// kescEmptyResponse is what Horizon returned for USDC -> KESC on 2026-08-08,
// at every size from 0.1 to 5000: no records at all.
const kescEmptyResponse = `{"_embedded":{"records":[]}}`

// ghscViaNGNCResponse reproduces the shape measured for USDC -> GHSC on
// 2026-08-08. Every path routes through NGNC; none reaches GHSC independently.
const ghscViaNGNCResponse = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "GHSC",
        "destination_amount": "155.5600000",
        "path": [
          { "asset_type": "native" },
          { "asset_type": "credit_alphanum4", "asset_code": "NGNC",
            "asset_issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6" }
        ]
      },
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "GHSC",
        "destination_amount": "150.0000000",
        "path": [
          { "asset_type": "credit_alphanum4", "asset_code": "NGNC",
            "asset_issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6" }
        ]
      }
    ]
  }
}`

func ghsRequest(amount string) Request {
	return Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.RequireFromString(amount),
		ReceiveAsset:   asset.GHSC(),
		ReferenceBase:  "USD",
		ReferenceQuote: "GHS",
	}
}

// TestNoMarketIsDistinctFromUnusable is the KESC case.
//
// Zero paths at any size is the absence of a price, not a bad price. Grading
// it Unusable would file it alongside a corridor that prices continuously and
// prices badly — a materially different situation for anyone deciding whether
// to build on it.
func TestNoMarketIsDistinctFromUnusable(t *testing.T) {
	srv := horizonStub(t, kescEmptyResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityNoMarket {
		t.Errorf("Integrity = %s, want NO-MARKET", res.Integrity)
	}
	if res.Integrity.Priceable() {
		t.Error("a corridor with no paths must not report as priceable")
	}
	if len(res.Quotes) != 0 {
		t.Errorf("expected no quotes, got %d", len(res.Quotes))
	}
	if res.Recommended != nil {
		t.Error("Recommended must be nil when there is no market")
	}

	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "No market") {
		t.Errorf("expected the note to name the no-market state, got: %v", res.Notes)
	}
	if !strings.Contains(joined, "absence of a price") {
		t.Errorf("expected the note to distinguish absence from bad pricing, got: %v", res.Notes)
	}
}

// TestDerivativeCorridorIsFlagged is the GHSC case: priced at every size, but
// every path traverses NGNC, so the corridor has no independent market and
// inherits NGNC's failure modes on top of its own.
func TestDerivativeCorridorIsFlagged(t *testing.T) {
	srv := horizonStub(t, ghscViaNGNCResponse)
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}
	if !res.Integrity.Priceable() {
		t.Error("a derivative corridor still has a price and must report as priceable")
	}

	if len(res.DependsOn) != 1 || res.DependsOn[0].Code != "NGNC" {
		t.Errorf("DependsOn = %v, want exactly NGNC", res.DependsOn)
	}

	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "Derivative corridor") {
		t.Errorf("expected a derivative note, got: %v", res.Notes)
	}
	if !strings.Contains(joined, "NGNC") {
		t.Errorf("expected the note to name the dependency, got: %v", res.Notes)
	}

	// The dependency must also ride on the quote itself. A caller rendering
	// one route must not be able to show the rate without it.
	warnings := strings.Join(res.Quotes[0].Warnings, " ")
	if !strings.Contains(warnings, "derivative corridor") {
		t.Errorf("expected the quote to carry the dependency warning, got: %v",
			res.Quotes[0].Warnings)
	}
}

// TestDirectCorridorIsNotDerivative is the control. The NGNC fixture routes
// through XLM, which is a bridge asset and not a fiat token, so the corridor
// is direct. Without this, a classifier that marked everything derivative
// would pass the test above.
func TestDirectCorridorIsNotDerivative(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT (XLM is a bridge asset, not a fiat token)",
			res.Integrity)
	}
	if len(res.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty for a direct corridor", res.DependsOn)
	}
}

// TestOneIndependentPathDefeatsDerivative pins the rule that the claim is
// about every path, not the best one. If any path avoids fiat intermediaries,
// an independent market exists and the corridor is not derivative — even
// when the best-paying path happens to route through one.
func TestOneIndependentPathDefeatsDerivative(t *testing.T) {
	mixed := `{
      "_embedded": {
        "records": [
          {
            "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
            "source_amount": "100.0000000",
            "destination_asset_type": "credit_alphanum4", "destination_asset_code": "GHSC",
            "destination_amount": "900.0000000",
            "path": [
              { "asset_type": "credit_alphanum4", "asset_code": "NGNC",
                "asset_issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6" }
            ]
          },
          {
            "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
            "source_amount": "100.0000000",
            "destination_asset_type": "credit_alphanum4", "destination_asset_code": "GHSC",
            "destination_amount": "800.0000000",
            "path": [ { "asset_type": "native" } ]
          }
        ]
      }
    }`
	srv := horizonStub(t, mixed)
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT: the XLM path proves an independent market",
			res.Integrity)
	}
}

// TestUnknownIssuerIsNotTreatedAsFiat guards the registry lookup. A token
// whose code matches a known fiat token but whose issuer does not must not
// be credited with that peg — asset code alone never identifies an asset.
func TestUnknownIssuerIsNotTreatedAsFiat(t *testing.T) {
	impostor := asset.Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5")
	if asset.IsFiatToken(impostor) {
		t.Error("a token from an unregistered issuer must not be treated as fiat-pegged")
	}
	if asset.IsFiatToken(asset.Native()) {
		t.Error("XLM is a bridge asset, not a fiat token")
	}
	if !asset.IsFiatToken(asset.NGNC()) {
		t.Error("the registered NGNC issuer must be recognised")
	}
}

// ---------------------------------------------------------------------------
// Dependency chain tests
// ---------------------------------------------------------------------------

// chainHorizonStub returns a server that dispatches based on the
// destination_assets query parameter, allowing multi-asset chain tests.
// Keys in the routes map should be asset codes (e.g. "NGNC"); the
// handler matches on the code portion of "CODE:ISSUER" or plain "CODE".
func chainHorizonStub(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dest := r.URL.Query().Get("destination_assets")
		// Horizon sends "CODE:ISSUER" — extract just the code.
		code := dest
		if idx := strings.Index(dest, ":"); idx != -1 {
			code = dest[:idx]
		}
		body, ok := routes[code]
		if !ok {
			body = `{"_embedded":{"records":[]}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// ghscDirectNGNCResponse is a modified fixture where NGNC is measured as
// having an independent market (XLM path avoids fiat intermediaries).
// This is the same as liveStrictSendResponse but for the USDC→NGNC pair,
// meaning NGNC's integrity is DIRECT when measured.
const ngncDirectResponse = liveStrictSendResponse

// TestChainMeasuredDirect verifies that when a derivative corridor's
// dependency is measured, the chain carries the measured integrity.
// USDC→GHSC depends on NGNC; USDC→NGNC has an XLM path (bridge asset),
// so NGNC is DIRECT. The chain should be depth 1 with NGNC measured as DIRECT.
func TestChainMeasuredDirect(t *testing.T) {
	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaNGNCResponse,
		"NGNC": ngncDirectResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "NGNC" {
		t.Fatalf("Chain = %v, want exactly NGNC", res.Chain)
	}
	node := res.Chain[0]
	if !node.Measured {
		t.Error("NGNC should be measured")
	}
	if node.Integrity != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", node.Integrity)
	}
	if len(node.Dependencies) != 0 {
		t.Errorf("NGNC should have no sub-dependencies, got %v", node.Dependencies)
	}

	// The warning should use the measured variant.
	warnings := strings.Join(res.Quotes[0].Warnings, " ")
	if !strings.Contains(warnings, "DIRECT, independent market exists") {
		t.Errorf("expected measured warning with market status, got: %v",
			res.Quotes[0].Warnings)
	}
}

// TestChainDepthTwo verifies recursive chain measurement through two levels.
// USDC→TOKEN_C depends on TOKEN_B, TOKEN_B depends on TOKEN_A, TOKEN_A is
// DIRECT (reached via XLM, a bridge asset).
func TestChainDepthTwo(t *testing.T) {
	// USDC→GHSC depends on KESC, KESC depends on NGNC, NGNC is DIRECT.

	ghscViaKescResponse := `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4", "destination_asset_code": "GHSC",
        "destination_amount": "100.0000000",
        "path": [
          { "asset_type": "credit_alphanum4", "asset_code": "KESC",
            "asset_issuer": "` + asset.LinkIOIssuer + `" }
        ]
      }
    ]
  }
}`

	kescViaNGNCResponse := `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4", "destination_asset_code": "KESC",
        "destination_amount": "100.0000000",
        "path": [
          { "asset_type": "credit_alphanum4", "asset_code": "NGNC",
            "asset_issuer": "` + asset.LinkIOIssuer + `" }
        ]
      }
    ]
  }
}`

	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaKescResponse,
		"KESC": kescViaNGNCResponse,
		"NGNC": ngncDirectResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	// Chain: GHSC depends on KESC (depth 2), KESC depends on NGNC (depth 1),
	// NGNC is DIRECT (depth 0).
	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "KESC" {
		t.Fatalf("Chain top level = %v, want KESC", res.Chain)
	}
	kescNode := res.Chain[0]
	if !kescNode.Measured {
		t.Error("KESC should be measured")
	}
	if kescNode.Integrity != IntegrityDerivative {
		t.Errorf("KESC integrity = %s, want DERIVATIVE", kescNode.Integrity)
	}
	if len(kescNode.Dependencies) != 1 || kescNode.Dependencies[0].Asset.Code != "NGNC" {
		t.Fatalf("KESC dependencies = %v, want NGNC", kescNode.Dependencies)
	}
	ngncNode := kescNode.Dependencies[0]
	if !ngncNode.Measured {
		t.Error("NGNC should be measured")
	}
	if ngncNode.Integrity != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", ngncNode.Integrity)
	}

	// All measured.
	if !allMeasured(res.Chain) {
		t.Error("all nodes should be measured in this chain")
	}
}

// TestChainCycleTerminates verifies that a circular dependency does not
// cause infinite recursion. When USDC→A routes through B and USDC→B
// routes through A, the second encounter is detected as a cycle and
// reported as unmeasured.
func TestChainCycleTerminates(t *testing.T) {
	// We can't easily create new fiat tokens, so we simulate the cycle
	// by using the actual fiat tokens in a way that creates mutual
	// dependency. But the registry is fixed. Instead, we test the
	// measureChain logic directly with a mock that creates a cycle
	// between NGNC and GHSC by returning GHSC paths through NGNC and
	// NGNC paths through GHSC.
	//
	// Note: in reality, USDC→NGNC does NOT go through GHSC (NGNC is
	// direct). But we can force the cycle by returning a custom response
	// for NGNC that routes through GHSC.

	ngncViaGHSCResponse := `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4", "destination_asset_code": "NGNC",
        "destination_amount": "100.0000000",
        "path": [
          { "asset_type": "credit_alphanum4", "asset_code": "GHSC",
            "asset_issuer": "` + asset.LinkIOIssuer + `" }
        ]
      }
    ]
  }
}`

	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaNGNCResponse,
		"NGNC": ngncViaGHSCResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	// The chain should have NGNC as the top-level dependency.
	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "NGNC" {
		t.Fatalf("Chain top level = %v, want NGNC", res.Chain)
	}
	ngncNode := res.Chain[0]
	if !ngncNode.Measured {
		t.Error("NGNC should be measured (first encounter)")
	}
	if ngncNode.Integrity != IntegrityDerivative {
		t.Errorf("NGNC integrity = %s, want DERIVATIVE", ngncNode.Integrity)
	}

	// NGNC depends on GHSC, but GHSC is already visited (it's the
	// destination), so it should be reported as unmeasured with cycle reason.
	if len(ngncNode.Dependencies) != 1 || ngncNode.Dependencies[0].Asset.Code != "GHSC" {
		t.Fatalf("NGNC dependencies = %v, want GHSC", ngncNode.Dependencies)
	}
	ghscNode := ngncNode.Dependencies[0]
	if ghscNode.Measured {
		t.Error("GHSC should NOT be measured (cycle detected)")
	}
	if ghscNode.Reason != "cycle detected" {
		t.Errorf("GHSC reason = %q, want 'cycle detected'", ghscNode.Reason)
	}
}

// TestChainDependencyHasNoMarket verifies that a dependency whose own
// market is NO-MARKET is reported honestly in the chain.
func TestChainDependencyHasNoMarket(t *testing.T) {
	// USDC→GHSC depends on KESC, and USDC→KESC has no paths.
	ghscViaKescResponse := `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4", "destination_asset_code": "GHSC",
        "destination_amount": "100.0000000",
        "path": [
          { "asset_type": "credit_alphanum4", "asset_code": "KESC",
            "asset_issuer": "` + asset.LinkIOIssuer + `" }
        ]
      }
    ]
  }
}`

	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaKescResponse,
		"KESC": kescEmptyResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "KESC" {
		t.Fatalf("Chain = %v, want KESC", res.Chain)
	}
	kescNode := res.Chain[0]
	if !kescNode.Measured {
		t.Error("KESC should be measured")
	}
	if kescNode.Integrity != IntegrityNoMarket {
		t.Errorf("KESC integrity = %s, want NO-MARKET", kescNode.Integrity)
	}

	// Since not all nodes are measured cleanly (NO-MARKET is measured but
	// the warning text differs), check the warning uses the unmeasured path.
	// Actually NO-MARKET is measured — the node is Measured=true. The
	// allMeasured check passes. The describeChainStatus renders it as
	// "KESC (NO-MARKET)".
	if !allMeasured(res.Chain) {
		t.Error("all nodes should be measured (NO-MARKET is still a measurement)")
	}
}

// TestChainWireShape verifies the JSON wire shape of the dependency chain.
func TestChainWireShape(t *testing.T) {
	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaNGNCResponse,
		"NGNC": ngncDirectResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	chain := ToDependencyChainJSON(res.Chain)
	if chain == nil {
		t.Fatal("chain should not be nil for a derivative corridor")
	}
	if chain.Depth != 1 {
		t.Errorf("depth = %d, want 1", chain.Depth)
	}
	if len(chain.DependsOn) != 1 {
		t.Fatalf("depends_on = %d nodes, want 1", len(chain.DependsOn))
	}
	node := chain.DependsOn[0]
	if node.Code != "NGNC" {
		t.Errorf("code = %s, want NGNC", node.Code)
	}
	if !node.Measured {
		t.Error("measured should be true")
	}
	if node.Integrity != "DIRECT" {
		t.Errorf("integrity = %s, want DIRECT", node.Integrity)
	}
	if node.Peg != "NGN" {
		t.Errorf("peg = %s, want NGN", node.Peg)
	}
	if len(node.Dependencies) != 0 {
		t.Errorf("sub-dependencies = %d, want 0", len(node.Dependencies))
	}
}

// TestChainBackwardCompatible verifies that the flat depends_on array is
// still present alongside the new dependency_chain on the wire.
func TestChainBackwardCompatible(t *testing.T) {
	srv := chainHorizonStub(t, map[string]string{
		"GHSC": ghscViaNGNCResponse,
		"NGNC": ngncDirectResponse,
	})
	defer srv.Close()

	e := &Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/GHS": decimal.RequireFromString("11.7625"),
		}),
	}
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	// Simulate what a consumer sees: the JSON must have both depends_on
	// and dependency_chain.
	if len(res.DependsOn) != 1 || res.DependsOn[0].Code != "NGNC" {
		t.Errorf("DependsOn = %v, want NGNC", res.DependsOn)
	}
	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "NGNC" {
		t.Errorf("Chain = %v, want NGNC", res.Chain)
	}
}

// TestDirectCorridorHasNoChain verifies that a direct corridor does not
// produce a dependency chain.
func TestDirectCorridorHasNoChain(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT", res.Integrity)
	}
	if res.Chain != nil {
		t.Errorf("Chain = %v, want nil for direct corridor", res.Chain)
	}
}

// TestAllMeasuredAndChainDepth are unit tests for the helper functions.
func TestAllMeasuredAndChainDepth(t *testing.T) {
	t.Run("all measured", func(t *testing.T) {
		nodes := []DependencyNode{
			{Asset: asset.NGNC(), Measured: true, Integrity: IntegrityDirect},
		}
		if !allMeasured(nodes) {
			t.Error("expected all measured")
		}
		if chainDepth(nodes) != 1 {
			t.Errorf("depth = %d, want 1", chainDepth(nodes))
		}
	})

	t.Run("unmeasured node", func(t *testing.T) {
		nodes := []DependencyNode{
			{Asset: asset.NGNC(), Measured: false, Reason: "cycle detected"},
		}
		if allMeasured(nodes) {
			t.Error("should not be all measured")
		}
	})

	t.Run("nested depth", func(t *testing.T) {
		nodes := []DependencyNode{
			{
				Asset:     asset.GHSC(),
				Measured:  true,
				Integrity: IntegrityDerivative,
				Dependencies: []DependencyNode{
					{Asset: asset.NGNC(), Measured: true, Integrity: IntegrityDirect},
				},
			},
		}
		if !allMeasured(nodes) {
			t.Error("expected all measured")
		}
		if chainDepth(nodes) != 2 {
			t.Errorf("depth = %d, want 2", chainDepth(nodes))
		}
	})

	t.Run("empty", func(t *testing.T) {
		if !allMeasured(nil) {
			t.Error("nil should be all measured")
		}
		if chainDepth(nil) != 0 {
			t.Errorf("depth = %d, want 0", chainDepth(nil))
		}
	})
}

// TestDescribeChainStatus verifies the human-readable chain status rendering.
func TestDescribeChainStatus(t *testing.T) {
	nodes := []DependencyNode{
		{Asset: asset.NGNC(), Measured: true, Integrity: IntegrityDirect},
		{Asset: asset.KESC(), Measured: false, Reason: "Horizon error: timeout"},
	}
	got := describeChainStatus(nodes)
	if !strings.Contains(got, "NGNC (DIRECT, independent market exists)") {
		t.Errorf("expected NGNC DIRECT status, got: %s", got)
	}
	if !strings.Contains(got, "KESC (not measured: Horizon error: timeout)") {
		t.Errorf("expected KESC unmeasured status, got: %s", got)
	}
}
