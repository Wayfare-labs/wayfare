package route_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// fullPathSetResponse holds three valid paths for the same strict-send
// request. They are deliberately different in kind, because that is what the
// contract is about:
//
//   - a direct path (no intermediate hop), which is the shape an AMM-pool-only
//     route takes in Horizon's response;
//   - a path through native XLM, the ordinary bridge route;
//   - a path through NGNT, a registered fiat token, which makes a corridor
//     derivative if it is the only kind available.
//
// The amounts are ordered so the best path is not first. Horizon generally
// returns best-first, but that is not a documented guarantee and the engine
// selects the maximum itself.
const fullPathSetResponse = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "150000.0000000",
        "path": []
      },
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "170000.0000000",
        "path": [
          {
            "asset_type": "credit_alphanum4",
            "asset_code": "NGNT",
            "asset_issuer": "GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD"
          }
        ]
      },
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "160000.0000000",
        "path": [ { "asset_type": "native" } ]
      }
    ]
  }
}`

// assertPathfinderContract runs the semantic part of the Pathfinder contract
// that Go cannot express: every path Horizon reports must come back, including
// a direct/AMM-pool-shaped path and a native-hop path, and the slippage probe
// must be answered. A "best path only" implementation would compile and fail
// here, which is the point.
func assertPathfinderContract(t *testing.T, pf route.Pathfinder) {
	t.Helper()
	ctx := context.Background()

	paths, err := pf.StrictSendPaths(ctx, asset.USDC(), decimal.NewFromInt(100), asset.NGNC())
	if err != nil {
		t.Fatalf("StrictSendPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("StrictSendPaths returned %d path(s), want all 3: a partial set "+
			"silently changes corridor classification", len(paths))
	}

	got := make(map[string]bool, len(paths))
	for _, p := range paths {
		got[p.Describe()] = true
	}
	for _, want := range []string{
		"USDC -> NGNC",         // direct, the shape an AMM-pool route takes
		"USDC -> XLM -> NGNC",  // native bridge hop
		"USDC -> NGNT -> NGNC", // fiat hop
	} {
		if !got[want] {
			t.Errorf("StrictSendPaths omitted %q; got %v", want, got)
		}
	}

	slip, err := pf.MeasureSlippage(ctx, asset.USDC(), decimal.NewFromInt(100), asset.NGNC(), decimal.NewFromInt(10))
	if err != nil {
		t.Fatalf("MeasureSlippage: %v", err)
	}
	if slip == nil {
		t.Fatal("MeasureSlippage returned nil slippage without an error")
	}
	if !slip.FullAmount.Equal(decimal.NewFromInt(100)) || !slip.ProbeAmount.Equal(decimal.NewFromInt(10)) {
		t.Errorf("MeasureSlippage sized the comparison as %s vs %s, want 100 vs 10",
			slip.FullAmount, slip.ProbeAmount)
	}
}

// TestHorizonClientSatisfiesPathfinderContract runs the contract against the
// only implementation that exists today. The interface names the requirement;
// this test keeps the current answer honest as well.
func TestHorizonClientSatisfiesPathfinderContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fullPathSetResponse))
	}))
	defer srv.Close()

	assertPathfinderContract(t, &dex.Client{HorizonURL: srv.URL})
}

// fixedPathfinder is a Pathfinder whose answer is fixed, so the engine can be
// tested without an HTTP fixture.
type fixedPathfinder struct {
	paths []dex.Path
}

func (f fixedPathfinder) StrictSendPaths(context.Context, asset.Asset, decimal.Decimal, asset.Asset) ([]dex.Path, error) {
	return f.paths, nil
}

func (f fixedPathfinder) MeasureSlippage(context.Context, asset.Asset, decimal.Decimal, asset.Asset, decimal.Decimal) (*dex.Slippage, error) {
	return &dex.Slippage{PctWorse: decimal.Zero}, nil
}

// TestEngineClassifiesFromTheCompletePathSet pins why the interface must
// return every path rather than the best one.
//
// The best route is the NGNT path (170,000 NGNC), but that path alone would
// classify the corridor DERIVATIVE. The direct and XLM paths are worse prices
// for this size and are still the reason the corridor is DIRECT. An engine
// built on "best path only" would compile, price the same number, and publish
// the wrong integrity verdict.
func TestEngineClassifiesFromTheCompletePathSet(t *testing.T) {
	pf := fixedPathfinder{paths: []dex.Path{
		{
			SourceAsset:  asset.USDC(),
			SourceAmount: decimal.NewFromInt(100),
			DestAsset:    asset.NGNC(),
			DestAmount:   decimal.NewFromInt(150000),
		},
		{
			SourceAsset:  asset.USDC(),
			SourceAmount: decimal.NewFromInt(100),
			DestAsset:    asset.NGNC(),
			DestAmount:   decimal.NewFromInt(170000),
			Hops:         []asset.Asset{asset.NGNT()},
		},
		{
			SourceAsset:  asset.USDC(),
			SourceAmount: decimal.NewFromInt(100),
			DestAsset:    asset.NGNC(),
			DestAmount:   decimal.NewFromInt(160000),
			Hops:         []asset.Asset{asset.Native()},
		},
	}}

	e := &route.Engine{
		DEX:     pf,
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1500)}),
	}
	res, err := e.Quote(context.Background(), route.Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.NewFromInt(100),
		ReceiveAsset:   asset.NGNC(),
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != route.IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT: the direct path exists even though "+
			"the best-priced path is derivative", res.Integrity)
	}
	if len(res.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty for a direct corridor", res.DependsOn)
	}
	if len(res.Quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(res.Quotes))
	}
	if res.Quotes[0].PathCount != 3 {
		t.Errorf("PathCount = %d, want all 3 paths counted", res.Quotes[0].PathCount)
	}
	if !res.Quotes[0].ReceiveAmount.Equal(decimal.NewFromInt(170000)) {
		t.Errorf("ReceiveAmount = %s, want the maximum 170000 (not the first path)",
			res.Quotes[0].ReceiveAmount)
	}
}
