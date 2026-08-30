package route

import (
	"context"
	"fmt"
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

// TestVerdictThresholdBoundaries pins the three grade bands at the exact
// edge documented in the Verdict constants, immediately below it, and
// immediately above it.
//
// verdictFor uses LessThanOrEqual, so "within 3%" means 3.0% is GOOD and
// 3.000001% is FAIR -- that is the intended reading of "<= 3%", and nothing
// else in this suite fails if a refactor quietly swaps LessThanOrEqual for
// LessThan. These nine values, expressed as decimals rather than floats so
// no binary rounding can nudge a value across a boundary before it is even
// compared, make that swap fail here instead.
func TestVerdictThresholdBoundaries(t *testing.T) {
	cases := []struct {
		loss           string
		wantVerdict    Verdict
		wantAcceptable bool
	}{
		// ThresholdGood = 3
		{"2.999", VerdictGood, true},
		{"3.0", VerdictGood, true},
		{"3.001", VerdictFair, true},
		// ThresholdFair = 8
		{"7.999", VerdictFair, true},
		{"8.0", VerdictFair, true},
		{"8.001", VerdictPoor, true},
		// ThresholdPoor = 20
		{"19.999", VerdictPoor, true},
		{"20.0", VerdictPoor, true},
		{"20.001", VerdictUnusable, false},
	}
	for _, c := range cases {
		loss := decimal.RequireFromString(c.loss)
		got := verdictFor(loss)
		if got != c.wantVerdict {
			t.Errorf("verdictFor(%s) = %s, want %s", c.loss, got, c.wantVerdict)
		}
		if gotAcceptable := got.Acceptable(); gotAcceptable != c.wantAcceptable {
			t.Errorf("verdictFor(%s).Acceptable() = %v, want %v", c.loss, gotAcceptable, c.wantAcceptable)
		}
	}
}

// stubDestAmount is a strict-send Horizon fixture for a single direct
// (path-less) route from 100 USDC to the given NGNC amount. Paired with a
// mid of 1000 USD/NGN and a send amount of 100, the destination amount
// determines LossPct exactly:
//
//	LossPct = (1000 - destAmount/100) / 1000 * 100
//
// which is how each case below is constructed to land precisely on or
// beside a threshold rather than merely near it.
func stubDestAmount(destAmount string) string {
	return fmt.Sprintf(`{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "%s",
        "path": []
      }
    ]
  }
}`, destAmount)
}

// TestLadderRecommendationAtPoorBoundary pins the recommendation rule at
// exactly the Poor/Unusable edge: at 20.0% loss a quote is POOR, therefore
// Acceptable, therefore recommendable; at 20.001% it is UNUSABLE and a
// ladder containing only that one rung must recommend nothing at all. One
// comparison operator separates "we recommend this route" from "we
// recommend nothing", and this test fails if verdictFor's
// LessThanOrEqual against ThresholdPoor is weakened to LessThan.
func TestLadderRecommendationAtPoorBoundary(t *testing.T) {
	cases := []struct {
		name          string
		destAmount    string // chosen to yield exactly lossPct against a mid of 1000
		lossPct       string
		wantVerdict   Verdict
		wantRecommend bool
	}{
		{"at_threshold_20.0_is_recommended", "80000", "20", VerdictPoor, true},
		{"just_over_threshold_20.001_is_not_recommended", "79999", "20.001", VerdictUnusable, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := horizonStub(t, stubDestAmount(c.destAmount))
			defer srv.Close()

			e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1000")}

			// A ladder made of exactly one rung, at the send amount this
			// fixture's loss was constructed against.
			lr, err := e.Ladder(context.Background(), LadderRequest{
				SendAsset:      asset.USDC(),
				ReceiveAsset:   asset.NGNC(),
				Sizes:          []decimal.Decimal{decimal.NewFromInt(100)},
				ReferenceBase:  "USD",
				ReferenceQuote: "NGN",
			})
			if err != nil {
				t.Fatalf("Ladder: %v", err)
			}
			if len(lr.Rungs) != 1 || !lr.Rungs[0].Priced() {
				t.Fatalf("expected exactly one priced rung, got %+v", lr.Rungs)
			}

			q := lr.Rungs[0].Result.Quotes[0]
			if got := q.LossPct.String(); got != c.lossPct {
				t.Fatalf("fixture produced loss %s, want %s -- fix the fixture, this case does not test the boundary it claims to", got, c.lossPct)
			}
			if q.Verdict != c.wantVerdict {
				t.Fatalf("Verdict = %s, want %s", q.Verdict, c.wantVerdict)
			}

			if got := lr.Viable(); got != c.wantRecommend {
				t.Errorf("Viable() = %v, want %v", got, c.wantRecommend)
			}
			switch {
			case c.wantRecommend && lr.Recommended == nil:
				t.Error("expected a recommendation at exactly the Poor threshold, got nil")
			case !c.wantRecommend && lr.Recommended != nil:
				t.Errorf("expected no recommendation just over the Poor threshold, got %+v", lr.Recommended)
			}
		})
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

// TestUnknownOnlyPathIsTheDocumentedFalseNegative pins the bounded
// false-negative written down in asset/known.go: a corridor whose only hops
// are unregistered is classified DIRECT, because an unrecognised fiat token
// is indistinguishable from XLM. The classification is the documented
// default; the point of the test is that the gap is surfaced, not hidden.
//
// The mixed unknown-plus-XLM case is covered by
// TestRecordedNewHopsAreClassifiedFromSnapshots, which runs the same
// assertions over the recorded 2026-08-21 path bytes (AQUA at 0.1 and 1
// USDC, yUSDC at 1, BTC at 10) rather than an inline fixture.
func TestUnknownOnlyPathIsTheDocumentedFalseNegative(t *testing.T) {
	onlyUnknown := `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "1.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "900.0000000",
        "path": [
          {
            "asset_type": "credit_alphanum4",
            "asset_code": "BLND",
            "asset_issuer": "GDLDCRZ3F6O7DXC5Q3SNSIBPZFDWLFBHWDTSRXHS6EQ4LQ7Y4G7K7LKA"
          }
        ]
      }
    ]
  }
}`
	srv := horizonStub(t, onlyUnknown)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("1"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	// The documented false-negative: BLND is not a fiat token, so the
	// corridor reports an independent market. This is the default the
	// registry exists to shrink, and the note keeps it visible.
	if res.Integrity != IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT (the documented false-negative)",
			res.Integrity)
	}

	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "Unregistered hop") || !strings.Contains(joined, "BLND") {
		t.Errorf("expected the note to surface BLND, got notes: %v", res.Notes)
	}
}
