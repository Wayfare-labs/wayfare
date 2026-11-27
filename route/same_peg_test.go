package route

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// liveUSDZStrictSend is the body Horizon returned for
//
//	/paths/strict-send?source_asset=USDC&source_amount=5000&destination_assets=USDZ
//
// on mainnet, 2026-11-22T04:24:57Z: a single direct path delivering
// 4180.7711282 USDZ for 5000 USDC. It is used because it falsifies a wrong
// benchmark: against par the loss is 16.38%, and any other mid gives a
// different figure.
const liveUSDZStrictSend = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_asset_issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
        "source_amount": "5000.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "USDZ",
        "destination_asset_issuer": "GAKTLPC4ZV37SSCITQ5IS5AQ4WPF4CF4VZJQPPAROSGXMYOATF5U6XPR",
        "destination_amount": "4180.7711282",
        "path": []
      }
    ]
  }
}`

// refusingProvider fails the test if it is asked for anything. A same-peg
// corridor must never consult a provider: the only thing one could answer is
// an echo of its own base currency.
type refusingProvider struct{ t *testing.T }

func (p refusingProvider) Name() string { return "refusing" }

func (p refusingProvider) Rate(_ context.Context, base, quote string) (refrate.Rate, error) {
	p.t.Errorf("provider consulted for %s/%s; a same-peg reference is definitional", base, quote)
	return refrate.Rate{}, &refrate.ErrNoRate{Base: base, Quote: quote, Source: p.Name()}
}

func usdzRequest(amount string) Request {
	usdz, ok := asset.Lookup("USDZ")
	if !ok {
		panic("USDZ is not registered")
	}
	return Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.RequireFromString(amount),
		ReceiveAsset:   usdz,
		ReferenceBase:  "USD",
		ReferenceQuote: "USD",
	}
}

// TestSamePegCorridorIsScoredAgainstParWithoutAProvider is the engine half
// of docs/adr-same-peg-corridors.md: USD/USD is answered by definition, loss
// is deviation from par, and the result says the reference is definitional.
func TestSamePegCorridorIsScoredAgainstParWithoutAProvider(t *testing.T) {
	srv := horizonStub(t, liveUSDZStrictSend)
	defer srv.Close()

	e := &Engine{
		DEX:      &dex.Client{HorizonURL: srv.URL},
		RefRate:  refusingProvider{t},
		Parallel: refusingProvider{t},
	}
	res, err := e.Quote(context.Background(), usdzRequest("5000"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if !res.Reference.Definitional() {
		t.Fatalf("reference kind = %s, want definitional", res.Reference.Kind)
	}
	if !res.ReferenceMid.Equal(decimal.NewFromInt(1)) {
		t.Errorf("reference mid = %s, want 1", res.ReferenceMid)
	}
	if res.ReferenceSource != refrate.DefinitionalSource {
		t.Errorf("reference source = %q, want %q", res.ReferenceSource, refrate.DefinitionalSource)
	}
	if res.Parallel != nil {
		t.Error("a parallel dimension was attached to a currency priced against itself")
	}
	if res.Integrity != IntegrityDirect {
		t.Errorf("integrity = %s, want DIRECT: same-peg corridors keep the ordinary structural states", res.Integrity)
	}
	if len(res.Quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(res.Quotes))
	}

	// (5000 - 4180.7711282) / 5000 = 16.38457...% below par.
	q := res.Quotes[0]
	if got := q.LossPct.StringFixed(2); got != "16.38" {
		t.Errorf("loss = %s%%, want 16.38%% below par", got)
	}
	if q.Verdict != VerdictPoor {
		t.Errorf("verdict = %s, want POOR at 16.38%%", q.Verdict)
	}
}

// TestFXCorridorStillConsultsItsProvider is the other side: only a currency
// against itself is definitional. A real FX pair keeps going to the provider
// and stays an observation.
func TestFXCorridorStillConsultsItsProvider(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if res.Reference.Definitional() {
		t.Error("USD/NGN was treated as definitional")
	}
	if res.ReferenceSource != "static" {
		t.Errorf("reference source = %q, want the configured provider", res.ReferenceSource)
	}
}

// TestSamePegWireNamesTheReferenceKind pins the acceptance criterion that
// wire output makes the reference's nature explicit.
func TestSamePegWireNamesTheReferenceKind(t *testing.T) {
	srv := horizonStub(t, liveUSDZStrictSend)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: refusingProvider{t}}
	l, err := e.Ladder(context.Background(), LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   usdzRequest("1").ReceiveAsset,
		Sizes:          []decimal.Decimal{decimal.NewFromInt(5000)},
		ReferenceBase:  "USD",
		ReferenceQuote: "USD",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	out := ToCorridorJSON(l, "USD/USD")
	if out.ReferenceKind != "definitional" {
		t.Errorf("reference_kind = %q, want definitional", out.ReferenceKind)
	}
	if out.ReferenceSource != refrate.DefinitionalSource {
		t.Errorf("reference_source = %q, want %q", out.ReferenceSource, refrate.DefinitionalSource)
	}
	if out.ReferenceAsOf != "" {
		t.Errorf("reference_as_of = %q; a definition carries no timestamp", out.ReferenceAsOf)
	}
	if out.ReferenceNote == "" {
		t.Error("reference_note is empty; a client needs the reason the mid is 1")
	}

	// No reference was obtained, so its kind is unknown, not observed.
	none := ToCorridorJSON(&LadderResult{}, "USD/NGN")
	if none.ReferenceKind != "unknown" {
		t.Errorf("reference_kind with no reference = %q, want unknown", none.ReferenceKind)
	}
	fx := ToCorridorJSON(&LadderResult{Reference: refrate.Rate{Source: "exchangerate-api"}}, "USD/NGN")
	if fx.ReferenceKind != "observed" {
		t.Errorf("provider reference_kind = %q, want observed", fx.ReferenceKind)
	}
}
