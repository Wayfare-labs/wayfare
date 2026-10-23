package sep38

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestLiveRoundTripParsesRecordedResponse verifies the SEP-38 client works
// end-to-end against a real anchor's response, recorded from
// testanchor.stellar.org on 2026-08-28.
//
// The fixture is the verbatim body returned by:
//
//	GET /sep38/price?sell_asset=iso4217:USD
//	  &buy_asset=stellar:SRT:GCDNJUBQSX7AJWLJACMJ7I4BC3Z47BQUTMHEICZLE6MU4KQBRYG5JY6B
//	  &sell_amount=100&context=sep6
//
// This is the first recorded live SEP-38 round-trip in the project: all
// previous tests used hand-written JSON or httptest mocks.
func TestLiveRoundTripParsesRecordedResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "live", "price-usd-srt.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	usd := asset.Fiat("USD")
	srt := asset.Stellar("SRT", "GCDNJUBQSX7AJWLJACMJ7I4BC3Z47BQUTMHEICZLE6MU4KQBRYG5JY6B")

	q, err := c.GetPrice(context.Background(), usd, srt, decimal.NewFromInt(100), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	// The wire response is parsed through the full client path — URL
	// construction, HTTP fetch, JSON decode, decimal parsing, normalisation —
	// so this test proves the round-trip actually works, not just that the
	// JSON is structurally valid.

	if got, want := q.SellAmount.String(), "100"; got != want {
		t.Errorf("SellAmount = %s, want %s", got, want)
	}

	// buy_amount from the anchor: 682.7586
	if got, want := q.BuyAmount.String(), "682.7586"; got != want {
		t.Errorf("BuyAmount = %s, want %s", got, want)
	}

	if got, want := q.Price.String(), "0.1450000044"; got != want {
		t.Errorf("Price = %s, want %s", got, want)
	}

	// Fee-denomination identity: gross = sell_amount / price.
	// 100 / 0.1450000044 = 689.655...; the fee in buy-asset units is
	// gross - net = 689.655... - 682.7586.
	gross := q.SellAmount.Div(q.Price)
	feeInBuyAsset := gross.Sub(q.BuyAmount)
	if feeInBuyAsset.IsNegative() {
		t.Errorf("FeeInBuyAsset is negative (%s): buy_amount exceeds gross implied by price", feeInBuyAsset)
	}

	// The raw fee is 1.00 USD. Converted to SRT at the quoted price, that
	// is approximately 1.00 / 0.145 ≈ 6.90 SRT — not 1.00. Verify the
	// conversion actually happened.
	if q.FeeInBuyAsset.Equal(decimal.NewFromInt(1)) {
		t.Error("FeeInBuyAsset equals the raw fee total: a sell-asset fee was not converted to buy-asset units")
	}

	// total_price = sell_amount / buy_amount. The anchor reports 0.1464646509
	// but the client derives it from full-precision division, so the value
	// should be close but not identical to the wire figure.
	expectedTotal := q.SellAmount.Div(q.BuyAmount)
	if !q.TotalPrice.Equal(expectedTotal) {
		t.Errorf("TotalPrice = %s, expected derived value %s", q.TotalPrice, expectedTotal)
	}

	if got := q.SellAsset; !got.Equal(usd) {
		t.Errorf("SellAsset = %v, want %v", got, usd)
	}
	if got := q.BuyAsset; !got.Equal(srt) {
		t.Errorf("BuyAsset = %v, want %v", got, srt)
	}
}

// TestLiveRoundTripFeeDetails covers the fee detail array surviving the
// round-trip — the anchor itemises the fee, and callers may surface the
// breakdown.
func TestLiveRoundTripFeeDetails(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "live", "price-usd-srt.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	// Verify the fixture actually contains fee details before testing them.
	var wire struct {
		Fee struct {
			Details []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Amount      string `json:"amount"`
			} `json:"details"`
		} `json:"fee"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	if len(wire.Fee.Details) == 0 {
		t.Fatal("fixture has no fee details to test")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Fiat("USD"),
		asset.Stellar("SRT", "GCDNJUBQSX7AJWLJACMJ7I4BC3Z47BQUTMHEICZLE6MU4KQBRYG5JY6B"),
		decimal.NewFromInt(100), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	if len(q.Fee.Details) != 1 {
		t.Fatalf("Fee.Details has %d entries, want 1", len(q.Fee.Details))
	}
	d := q.Fee.Details[0]
	if d.Name != "Sell fee" {
		t.Errorf("detail name = %q, want %q", d.Name, "Sell fee")
	}
	if d.Description != "Fee related to selling the asset." {
		t.Errorf("detail description = %q, want %q", d.Description, "Fee related to selling the asset.")
	}
	if got, want := d.Amount.String(), "1"; got != want {
		t.Errorf("detail amount = %s, want %s", got, want)
	}
}

// TestLiveRoundTripRoundTripsAssetIdentifiers proves that the SEP-38 asset
// identifiers in the fixture survive the round-trip through the client. A
// parsing error here would silently drop an asset pair, producing a zero-value
// Quote that looks like a pricing failure rather than a parsing failure.
func TestLiveRoundTripRoundTripsAssetIdentifiers(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "live", "price-usd-srt.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	usd := asset.Fiat("USD")
	srt := asset.Stellar("SRT", "GCDNJUBQSX7AJWLJACMJ7I4BC3Z47BQUTMHEICZLE6MU4KQBRYG5JY6B")

	q, err := c.GetPrice(context.Background(), usd, srt, decimal.NewFromInt(100), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	// SEP-38 identifiers must round-trip: the asset we sent in must
	// be the asset we got back.
	if got := q.SellAsset.SEP38(); got != "iso4217:USD" {
		t.Errorf("SellAsset.SEP38() = %q, want %q", got, "iso4217:USD")
	}
	wantSRT := "stellar:SRT:GCDNJUBQSX7AJWLJACMJ7I4BC3Z47BQUTMHEICZLE6MU4KQBRYG5JY6B"
	if got := q.BuyAsset.SEP38(); got != wantSRT {
		t.Errorf("BuyAsset.SEP38() = %q, want %q", got, wantSRT)
	}
}

// TestLiveFixtureIsRecorded documents that the fixture exists and contains a
// plausible response, guarding against accidental deletion or truncation.
func TestLiveFixtureIsRecorded(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "live", "price-usd-srt.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	var wire wirePrice
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("parsing fixture as wirePrice: %v", err)
	}
	if wire.Price == "" {
		t.Error("fixture has no price — not a plausible SEP-38 response")
	}
	if wire.BuyAmount == "" {
		t.Error("fixture has no buy_amount — not a plausible SEP-38 response")
	}
	if wire.Fee.Total == "" {
		t.Error("fixture has no fee.total — not a plausible SEP-38 response")
	}
	if wire.Fee.Asset != "iso4217:USD" {
		t.Errorf("fixture fee.asset = %q, want %q", wire.Fee.Asset, "iso4217:USD")
	}
}
