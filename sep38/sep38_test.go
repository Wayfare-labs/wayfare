package sep38

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// specExamplePrice is the worked example from SEP-0038 itself, reproduced
// verbatim. It is used as the fixture deliberately: the fee handling in this
// package is derived from the spec's price definitions, so the spec's own
// numbers are the only fixture that can actually falsify the derivation.
//
// Selling 542 BRL to buy 100 USDC, with the 42 unit fee denominated in BRL —
// the SELL asset, not the buy asset.
const specExamplePrice = `{
  "total_price": "5.42",
  "price": "5.00",
  "sell_amount": "542",
  "buy_amount": "100",
  "fee": {
    "total": "42.00",
    "asset": "iso4217:BRL"
  }
}`

func newTestServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

func mustDec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("bad decimal %q: %v", s, err)
	}
	return d
}

func TestGetPriceParsesSpecExample(t *testing.T) {
	srv := newTestServer(t, specExamplePrice)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	brl := asset.Fiat("BRL")
	usdc := asset.Stellar("USDC", asset.USDCIssuer)

	q, err := c.GetPrice(context.Background(), brl, usdc, mustDec(t, "542"), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	if got, want := q.SellAmount.String(), "542"; got != want {
		t.Errorf("SellAmount = %s, want %s", got, want)
	}
	if got, want := q.BuyAmount.String(), "100"; got != want {
		t.Errorf("BuyAmount = %s, want %s", got, want)
	}
	if got, want := q.Price.String(), "5"; got != want {
		t.Errorf("Price = %s, want %s", got, want)
	}
}

// TestFeeConvertedToBuyAsset is the regression test for the unit error this
// package's doc comment describes.
//
// The fee is 42.00 BRL. The recipient is counting USDC. At a price of 5.00
// BRL per USDC that fee is worth 8.4 USDC — and 8.4 is the only number it is
// ever correct to show the user. A previous implementation added the raw fee
// total to the buy amount, producing 142: forty-two units of Brazilian reais
// added to one hundred US dollars. The arithmetic succeeds, no error is
// raised, and the result is meaningless.
func TestFeeConvertedToBuyAsset(t *testing.T) {
	srv := newTestServer(t, specExamplePrice)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.Stellar("USDC", asset.USDCIssuer),
		mustDec(t, "542"), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	// gross = sell_amount / price = 542 / 5.00 = 108.4 USDC
	if got, want := q.GrossBuyAmount.String(), "108.4"; got != want {
		t.Errorf("GrossBuyAmount = %s, want %s", got, want)
	}

	// fee in buy asset = gross - net = 108.4 - 100 = 8.4 USDC
	if got, want := q.FeeInBuyAsset.String(), "8.4"; got != want {
		t.Errorf("FeeInBuyAsset = %s, want %s", got, want)
	}

	// Explicitly reject the raw sell-asset fee leaking through unconverted.
	if q.FeeInBuyAsset.Equal(q.Fee.Total) {
		t.Errorf("FeeInBuyAsset (%s) equals the raw fee total (%s): "+
			"a sell-asset fee was reported without converting to the buy asset",
			q.FeeInBuyAsset, q.Fee.Total)
	}
}

// TestTotalPriceMatchesSpec checks our derived all-in rate against the value
// the anchor independently reported, which is a genuine cross-check: we
// compute sell/buy while the anchor computed total_price on its own side.
func TestTotalPriceMatchesSpec(t *testing.T) {
	srv := newTestServer(t, specExamplePrice)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.Stellar("USDC", asset.USDCIssuer),
		mustDec(t, "542"), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if got, want := q.TotalPrice.String(), "5.42"; got != want {
		t.Errorf("TotalPrice = %s, want %s (the anchor's own total_price)", got, want)
	}
}

// TestFeeInBuyAssetDenomination covers the other branch: an anchor that
// denominates the fee in the BUY asset. Here the fee needs no conversion, and
// the identity gross = sell/price must still hold.
//
// Selling 100 USDC for 500 BRL with a 10 BRL fee: price excludes the fee, so
// price = 100 / (500 + 10) = 0.196078... and gross = 100 / price = 510.
func TestFeeInBuyAssetDenomination(t *testing.T) {
	payload := `{
      "total_price": "0.2",
      "price": "0.19607843137254901960",
      "sell_amount": "100",
      "buy_amount": "500",
      "fee": { "total": "10", "asset": "iso4217:BRL" }
    }`
	srv := newTestServer(t, payload)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Stellar("USDC", asset.USDCIssuer), asset.Fiat("BRL"),
		mustDec(t, "100"), ContextSEP31)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	// The fee is already in buy-asset units, so it should survive round-trip
	// essentially unchanged (allowing for the anchor's truncated price).
	diff := q.FeeInBuyAsset.Sub(mustDec(t, "10")).Abs()
	if diff.GreaterThan(mustDec(t, "0.001")) {
		t.Errorf("FeeInBuyAsset = %s, want ~10 (fee already in buy asset)", q.FeeInBuyAsset)
	}
}

// TestRejectsNonPositivePrice guards the division in normalize. An anchor
// returning a zero price would otherwise produce a division panic or an
// infinite gross amount presented as a spectacular deal.
func TestRejectsNonPositivePrice(t *testing.T) {
	for _, price := range []string{"0", "-1.5"} {
		payload := `{"price":"` + price + `","sell_amount":"100","buy_amount":"500",
                     "fee":{"total":"0","asset":"iso4217:BRL"}}`
		srv := newTestServer(t, payload)
		c := &Client{BaseURL: srv.URL}
		_, err := c.GetPrice(context.Background(),
			asset.Stellar("USDC", asset.USDCIssuer), asset.Fiat("BRL"),
			mustDec(t, "100"), ContextSEP31)
		srv.Close()
		if err == nil {
			t.Errorf("price %q: expected an error, got none", price)
		}
	}
}

// TestRejectsInconsistentQuote covers an anchor whose buy_amount exceeds the
// gross its own price implies. Rather than showing the user a negative fee,
// which would read as free money, the quote is refused.
func TestRejectsInconsistentQuote(t *testing.T) {
	payload := `{"price":"5.00","sell_amount":"542","buy_amount":"200",
                 "fee":{"total":"42.00","asset":"iso4217:BRL"}}`
	srv := newTestServer(t, payload)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.Stellar("USDC", asset.USDCIssuer),
		mustDec(t, "542"), ContextSEP6)
	if err == nil {
		t.Fatal("expected an error for a quote implying a negative fee")
	}
}

// TestPostQuoteRequiresAuth documents that firm quotes are authenticated;
// callers should not silently fall back to unauthenticated requests.
func TestPostQuoteRequiresAuth(t *testing.T) {
	c := &Client{BaseURL: "https://example.invalid"}
	_, err := c.PostQuote(context.Background(),
		asset.USDC(), asset.NGN(), mustDec(t, "100"), ContextSEP31)
	if err == nil {
		t.Fatal("expected PostQuote to refuse without a SEP-10 token")
	}
}

// TestSurfacesAnchorError checks that the anchor's JSON error message reaches
// the caller. "unsupported asset" is far more actionable than "HTTP 400".
func TestSurfacesAnchorError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported asset pair"})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.GetPrice(context.Background(),
		asset.USDC(), asset.NGN(), mustDec(t, "100"), ContextSEP31)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !contains(got, "Unsupported asset pair") {
		t.Errorf("error %q does not surface the anchor's message", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
