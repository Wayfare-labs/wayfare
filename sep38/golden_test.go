package sep38

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

var update = flag.Bool("update", false, "rewrite the golden files from current behaviour")

// Golden files pin the fee-denomination arithmetic.
//
// # Why this is pinned rather than merely tested
//
// SEP-38 returns a fee that may be denominated in EITHER the sell asset or the
// buy asset. The naive handling adds fee.total to buy_amount, which adds units
// of one currency to units of another: the arithmetic succeeds, no error is
// raised, and the number is simply wrong. That bug was in this package once.
//
// A regression would not crash. It would quietly change a published figure. So
// the expected output is frozen on disk rather than left in an assertion that
// somebody could adjust while "fixing" a failing test.
//
// # The identity being pinned
//
// The spec gives two definitions of price depending on where the fee sits:
//
//	fee in sell asset:  price = (sell_amount - fee) / buy_amount
//	fee in buy asset:   price = sell_amount / (buy_amount + fee)
//
// Solving each for the pre-fee gross in buy-asset units gives the same
// expression, which is why the implementation needs no branch on fee.asset:
//
//	gross_in_buy_asset = sell_amount / price
//
// # Coverage note
//
// Both denominations are covered, and the cases were chosen carefully because
// the obvious pair does not do it. The spec's own worked example (542 BRL ->
// 100 USDC, fee 42 BRL) and the package doc's example (100 USDC -> 500 BRL,
// fee 10 USDC) look like opposite directions — the assets swap places — but in
// BOTH the fee is denominated in the sell asset. Pinning only those two would
// exercise one branch twice and leave the buy-asset branch unpinned.
//
// buyAssetFee is therefore included as a case: it is one where fee.asset
// names the buy asset, and FeeInBuyAsset should come back equal to the raw
// fee.total. A reversed variant exercises the same branch with the assets
// swapped.
//
// feeAbsent covers the case where the anchor returns no fee at all. The
// identity still holds: gross == buy_amount, so FeeInBuyAsset must be zero.

type goldenCase struct {
	name     string
	file     string
	response string
	sell     asset.Asset
	buy      asset.Asset
	sellAmt  string
	// note records, in the golden file, which branch the case exercises.
	note string
}

func brl() string { return "iso4217:BRL" }

var goldenCases = []goldenCase{
	{
		name: "fee in sell asset, spec worked example",
		file: "fee-in-sell-spec.json",
		note: "SEP-0038's own example. Fee 42 is BRL, the SELL asset. " +
			"gross = 542/5 = 108.4 USDC, so the fee is 8.4 USDC — not 42.",
		response: `{
          "total_price": "5.42",
          "price": "5.00",
          "sell_amount": "542",
          "buy_amount": "100",
          "fee": {"total": "42.00", "asset": "` + brl() + `"}
        }`,
		sell:    asset.Fiat("BRL"),
		buy:     asset.Stellar("USDC", asset.USDCIssuer),
		sellAmt: "542",
	},
	{
		name: "fee in sell asset, assets reversed",
		file: "fee-in-sell-reversed.json",
		note: "Same branch as the spec example with the assets swapped: fee 10 " +
			"is USDC, the SELL asset. gross = 100/0.18 = 555.56 BRL, fee 55.56 BRL. " +
			"Included because it is the example in the package doc comment.",
		response: `{
          "total_price": "0.2",
          "price": "0.18",
          "sell_amount": "100",
          "buy_amount": "500",
          "fee": {"total": "10.00", "asset": "stellar:USDC:` + asset.USDCIssuer + `"}
        }`,
		sell:    asset.Stellar("USDC", asset.USDCIssuer),
		buy:     asset.Fiat("BRL"),
		sellAmt: "100",
	},
	{
		name: "fee in buy asset",
		file: "fee-in-buy.json",
		note: "The other branch, and the only case where fee.asset names the BUY " +
			"asset. price = 100/(500+10), so gross = 510 BRL and FeeInBuyAsset " +
			"should equal fee.total exactly — no conversion needed.",
		response: `{
          "total_price": "0.2",
          "price": "0.19607843137254901960",
          "sell_amount": "100",
          "buy_amount": "500",
          "fee": {"total": "10", "asset": "` + brl() + `"}
        }`,
		sell:    asset.Stellar("USDC", asset.USDCIssuer),
		buy:     asset.Fiat("BRL"),
		sellAmt: "100",
	},
	{
		name: "fee in buy asset, reversed",
		file: "fee-in-buy-reversed.json",
		note: "Same branch as fee-in-buy with the assets swapped: fee 8.4 is USDC, " +
			"the BUY asset. price = 542/(100+8.4) = 5.0, gross = 542/5 = 108.4 USDC, " +
			"fee = 8.4 USDC — the fee survives round-trip unchanged.",
		response: `{
          "total_price": "5.42",
          "price": "5.00",
          "sell_amount": "542",
          "buy_amount": "100",
          "fee": {"total": "8.4", "asset": "stellar:USDC:` + asset.USDCIssuer + `"}
        }`,
		sell:    asset.Fiat("BRL"),
		buy:     asset.Stellar("USDC", asset.USDCIssuer),
		sellAmt: "542",
	},
	{
		name: "fee absent",
		file: "fee-absent.json",
		note: "No fee at all. gross = sell/price = 100/0.2 = 500 BRL, which equals " +
			"buy_amount exactly, so FeeInBuyAsset must be zero — not absent, not " +
			"NaN, zero.",
		response: `{
          "total_price": "0.2",
          "price": "0.2",
          "sell_amount": "100",
          "buy_amount": "500",
          "fee": {"total": "0", "asset": ""}
        }`,
		sell:    asset.Stellar("USDC", asset.USDCIssuer),
		buy:     asset.Fiat("BRL"),
		sellAmt: "100",
	},
}

// goldenQuote is the pinned view of a normalised Quote.
//
// Every figure is a decimal string. Serialising these as JSON numbers would
// round them through float64 in the golden file itself, which would defeat the
// purpose of pinning the arithmetic.
type goldenQuote struct {
	Note           string `json:"_note"`
	SellAmount     string `json:"sell_amount"`
	BuyAmount      string `json:"buy_amount"`
	Price          string `json:"price"`
	TotalPrice     string `json:"total_price"`
	GrossBuyAmount string `json:"gross_buy_amount"`
	FeeTotal       string `json:"fee_total"`
	FeeAsset       string `json:"fee_asset"`
	FeeInBuyAsset  string `json:"fee_in_buy_asset"`
}

func TestGoldenFeeDenomination(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL}
			q, err := c.GetPrice(context.Background(), tc.sell, tc.buy,
				mustDec(t, tc.sellAmt), ContextSEP31)
			if err != nil {
				t.Fatalf("GetPrice: %v", err)
			}

			got := goldenQuote{
				Note:           tc.note,
				SellAmount:     q.SellAmount.String(),
				BuyAmount:      q.BuyAmount.String(),
				Price:          q.Price.String(),
				TotalPrice:     q.TotalPrice.String(),
				GrossBuyAmount: q.GrossBuyAmount.String(),
				FeeTotal:       q.Fee.Total.String(),
				FeeAsset:       q.Fee.Asset.SEP38(),
				FeeInBuyAsset:  q.FeeInBuyAsset.String(),
			}

			path := filepath.Join("testdata", "golden", tc.file)
			if *update {
				writeGolden(t, path, got)
				return
			}

			var want goldenQuote
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file: %v\nRun: go test ./sep38/... -update", err)
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("parsing golden file %s: %v", path, err)
			}

			if got != want {
				t.Errorf("normalised quote does not match %s.\n got: %+v\nwant: %+v\n\n"+
					"This is the fee-denomination arithmetic. A change here changes "+
					"published figures, so confirm the new values against the spec "+
					"before regenerating with -update.", path, got, want)
			}
		})
	}
}

// TestGoldenCoversBothDenominations guards the coverage claim itself.
//
// The two obvious examples both put the fee in the sell asset, so a future
// edit that dropped the buy-asset case would leave one branch of the identity
// unpinned while still looking like it covered both.
func TestGoldenCoversBothDenominations(t *testing.T) {
	var sawSellFee, sawBuyFee, sawNoFee bool

	for _, tc := range goldenCases {
		var wire struct {
			SellAmount string `json:"sell_amount"`
			Fee        struct {
				Asset string `json:"asset"`
			} `json:"fee"`
		}
		if err := json.Unmarshal([]byte(tc.response), &wire); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		switch wire.Fee.Asset {
		case tc.sell.SEP38():
			sawSellFee = true
		case tc.buy.SEP38():
			sawBuyFee = true
		case "":
			sawNoFee = true
		default:
			t.Errorf("%s: fee.asset %q is neither the sell nor the buy asset",
				tc.name, wire.Fee.Asset)
		}
	}

	if !sawSellFee {
		t.Error("no golden case denominates the fee in the sell asset")
	}
	if !sawBuyFee {
		t.Error("no golden case denominates the fee in the buy asset; " +
			"the branch where FeeInBuyAsset equals fee.total is unpinned")
	}
	if !sawNoFee {
		t.Error("no golden case has an absent fee; the zero-fee path is unpinned")
	}
}

func writeGolden(t *testing.T, path string, q goldenQuote) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buf, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(buf, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
