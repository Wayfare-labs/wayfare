package dex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// roundTripFunc is an http.RoundTripper built from a function, so the fuzz
// loop can serve arbitrary bytes without standing up a server per iteration.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// FuzzOrderBookDecoding fuzzes the /order_book decoding path behind
// Client.OrderBook (#589).
//
// Spread, depth and concentration metrics read this book, so a malformed
// level must become undetermined — a skipped or dust-counted level — and
// never a priced one. Example-based tests cover the cases anyone thought of;
// this target mutates a real recorded market and asserts, on every accepted
// body, the invariants the decoder promises:
//
//   - level counts and the dust count match a restatement of the documented
//     contract (price parses, amount parses and is positive, price above the
//     dust threshold);
//   - the best prices on accepted books are strictly positive and are the
//     true extrema of the priced levels — never a dust or malformed level;
//   - when the book arrives in Horizon's documented order (bids descending,
//     asks ascending), the best price on each side is that side's first
//     priced level;
//   - the mid exists exactly when both sides do, and the spread is computed
//     from the two best prices.
func FuzzOrderBookDecoding(f *testing.F) {
	// The seed corpus is a real mainnet XLM/NGNC book captured on
	// 2026-08-21, plus the shapes the decoder must survive. A corpus built
	// from wireOrderBook alone would carry whatever this package already
	// expects; the recorded bytes carry what Horizon actually sends.
	raw, err := os.ReadFile("testdata/orderbook-xlm-ngnc.json")
	if err != nil {
		f.Fatalf("reading recorded order book: %v", err)
	}
	f.Add(string(raw))

	f.Add(`{"bids":[{"price":"100.0000000","amount":"50"}],"asks":[{"price":"100.5000000","amount":"50"}]}`)
	f.Add(`{"bids":[],"asks":[]}`)
	f.Add(`{"bids":[],"asks":[{"price":"100","amount":"1"}]}`)
	f.Add(`{"bids":[{"price":"100","amount":"1"}],"asks":[]}`)
	// Dust and near-dust on both sides.
	f.Add(`{"bids":[{"price":"0.0000001","amount":"1000"}],"asks":[{"price":"100","amount":"1"}]}`)
	f.Add(`{"bids":[{"price":"1e-9","amount":"1000"}],"asks":[{"price":"1E+2","amount":"1"}]}`)
	// Malformed prices and amounts must never become priced levels.
	f.Add(`{"bids":[{"price":"oops","amount":"1"},{"price":"100","amount":"1"}],"asks":[{"price":"100.5","amount":"1"}]}`)
	f.Add(`{"bids":[{"price":"100","amount":"oops"},{"price":"101","amount":"-5"},{"price":"102","amount":"0"}],"asks":[{"price":"100.5","amount":"1"}]}`)
	// A crossed book is legal input; it just is not a healthy market.
	f.Add(`{"bids":[{"price":"101","amount":"1"}],"asks":[{"price":"100","amount":"1"}]}`)
	// Extreme magnitudes and scales.
	f.Add(`{"bids":[{"price":"1000000000000000000000000000000","amount":"0.0000001"}],"asks":[{"price":"1000000000000000000000000000001","amount":"1"}]}`)
	// Exponents beyond what any Stellar asset can carry: lazy decimal
	// parsing makes these cheap to hold and ruinous to compare (#589).
	f.Add(`{"bids":[{"price":"1e999999999","amount":"1"}],"asks":[{"price":"100","amount":"1e-999999999"}]}`)
	// Not a book at all.
	f.Add(`null`)
	f.Add(`{}`)
	f.Add(`{"bids":null,"asks":null}`)

	f.Fuzz(func(t *testing.T, body string) {
		c := &Client{
			HorizonURL: "http://orderbook.fuzz",
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/hal+json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			})},
		}

		h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())

		// A body that does not decode is an error, never an empty market.
		var wire wireOrderBook
		if jsonErr := json.Unmarshal([]byte(body), &wire); jsonErr != nil {
			if err == nil {
				t.Fatalf("OrderBook accepted a body that does not decode: %s", body)
			}
			return
		}
		if err != nil {
			t.Fatalf("OrderBook rejected a decodable body: %v", err)
		}

		// Restates the documented contract of OrderBook — the same rules
		// pricedLevels applies — so a decoder change that stops honouring
		// the contract fails here rather than silently redefining it.
		bids, bidDust := expectedPriced(wire.Bids)
		asks, askDust := expectedPriced(wire.Asks)

		if h.BidLevels != len(bids) || h.AskLevels != len(asks) {
			t.Fatalf("levels = %d bids / %d asks, want %d / %d (body %s)",
				h.BidLevels, h.AskLevels, len(bids), len(asks), body)
		}
		if h.DustLevels != bidDust+askDust {
			t.Fatalf("DustLevels = %d, want %d (body %s)",
				h.DustLevels, bidDust+askDust, body)
		}

		if len(bids) > 0 {
			want := maxDecimal(bids)
			if !h.BestBid.Equal(want) || !h.BestBid.GreaterThan(dustThreshold) {
				t.Fatalf("BestBid = %s, want the positive maximum %s (body %s)",
					h.BestBid, want, body)
			}
			// Horizon documents bids in descending price order; when the
			// book obeys that, the first priced level is the best bid.
			if descending(bids) && !h.BestBid.Equal(bids[0]) {
				t.Fatalf("BestBid = %s, want the first bid %s of a descending book (body %s)",
					h.BestBid, bids[0], body)
			}
		}
		if len(asks) > 0 {
			want := minDecimal(asks)
			if !h.BestAsk.Equal(want) || !h.BestAsk.GreaterThan(dustThreshold) {
				t.Fatalf("BestAsk = %s, want the positive minimum %s (body %s)",
					h.BestAsk, want, body)
			}
			if ascending(asks) && !h.BestAsk.Equal(asks[0]) {
				t.Fatalf("BestAsk = %s, want the first ask %s of an ascending book (body %s)",
					h.BestAsk, asks[0], body)
			}
		}

		switch {
		case len(bids) > 0 && len(asks) > 0:
			wantMid := h.BestBid.Add(h.BestAsk).Div(decimal.NewFromInt(2))
			if !h.Mid.Equal(wantMid) {
				t.Fatalf("Mid = %s, want %s (body %s)", h.Mid, wantMid, body)
			}
			wantSpread := h.BestAsk.Sub(h.BestBid).Div(h.Mid).Mul(decimal.NewFromInt(100))
			if !h.SpreadPct.Equal(wantSpread) {
				t.Fatalf("SpreadPct = %s, want %s (body %s)", h.SpreadPct, wantSpread, body)
			}
		default:
			// A one-sided or empty book has no two-sided mid, and must
			// say so rather than inventing one from one side.
			if !h.Mid.IsZero() || !h.SpreadPct.IsZero() {
				t.Fatalf("Mid/SpreadPct = %s/%s on a one-sided book, want 0/0 (body %s)",
					h.Mid, h.SpreadPct, body)
			}
		}
	})
}

// expectedPriced restates OrderBook's documented level contract for one side
// of the book: a priced level parses in both fields with a positive amount
// and a price above the dust threshold, and neither field carries an
// exponent beyond maxDecimalExponent; dust is counted; anything else is
// skipped.
func expectedPriced(offers []wireOffer) (prices []decimal.Decimal, dust int) {
	for _, o := range offers {
		p, err := decimal.NewFromString(o.Price)
		if err != nil || absExponent(p) > maxDecimalExponent {
			continue
		}
		if p.LessThanOrEqual(dustThreshold) {
			dust++
			continue
		}
		a, err := decimal.NewFromString(o.Amount)
		if err != nil || !a.IsPositive() || absExponent(a) > maxDecimalExponent {
			continue
		}
		prices = append(prices, p)
	}
	return prices, dust
}

func maxDecimal(ds []decimal.Decimal) decimal.Decimal {
	m := ds[0]
	for _, d := range ds[1:] {
		if d.GreaterThan(m) {
			m = d
		}
	}
	return m
}

func minDecimal(ds []decimal.Decimal) decimal.Decimal {
	m := ds[0]
	for _, d := range ds[1:] {
		if d.LessThan(m) {
			m = d
		}
	}
	return m
}

func descending(ds []decimal.Decimal) bool {
	for i := 1; i < len(ds); i++ {
		if ds[i].GreaterThan(ds[i-1]) {
			return false
		}
	}
	return true
}

func ascending(ds []decimal.Decimal) bool {
	for i := 1; i < len(ds); i++ {
		if ds[i].LessThan(ds[i-1]) {
			return false
		}
	}
	return true
}
