package sep38

// Fuzz target over SEP-38 quote parsing and normalisation.
//
// # Why this target exists
//
// The golden files pin five fee shapes and the edge tests cover the obvious
// failure modes; neither can enumerate what an anchor might send. The fee
// denomination handling is maintainer-owned because getting it wrong produces
// a wrong number rather than an error, so the remaining input space is worth
// exploring mechanically.
//
// # The identity asserted on every accepted quote
//
// The spec gives two definitions of price, depending on where the fee sits:
//
//	fee in sell asset:  price = (sell_amount - fee) / buy_amount
//	fee in buy asset:   price = sell_amount / (buy_amount + fee)
//
// Solving each for the pre-fee gross, in buy-asset units, gives the same
// expression — which is why the client never branches on fee.asset:
//
//	gross_in_buy_asset = sell_amount / price
//	fee_in_buy_asset   = gross_in_buy_asset - buy_amount
//
// So every accepted quote must satisfy
//
//	buy_amount + fee_in_buy_asset == gross_in_buy_asset
//
// with fee_in_buy_asset non-negative, and its derived figures must not move
// when the anchor's fee is re-denominated in either asset of the pair. Those
// are the properties this target asserts, on every quote the client accepts,
// whatever the input looked like.
//
// # What a rejection means
//
// Most mutated bodies are rejected — malformed JSON, a non-numeric amount, a
// non-positive price, a net amount above the gross implied by the price. That
// is the documented contract (see sep38_edge_test.go): bad data fails closed
// instead of being repaired into a plausible quote. A rejection is therefore
// not a failure here; only an accepted quote that breaks the identity is.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// quoteBodyTransport answers every request with one fixed body, so the target
// exercises the client's real path — request construction, response decoding,
// decimal parsing and normalisation — without a network round trip per
// execution. A shared httptest handler would need a lock around the body: the
// handler goroutine and the fuzz goroutine would otherwise race on it.
type quoteBodyTransport struct{ body []byte }

func (t quoteBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(t.body)),
		Request:    req,
	}, nil
}

func quoteClient(body []byte) *Client {
	return &Client{
		BaseURL:    "https://quote.example",
		HTTPClient: &http.Client{Transport: quoteBodyTransport{body: body}},
	}
}

// fuzzPair is the pair the target quotes on the request side. The fee's
// denomination is read from the response, never from these, so the choice only
// matters for readability: USDC/BRL is the pair the golden shapes use.
func fuzzPair() (sell, buy asset.Asset) {
	return asset.Stellar("USDC", asset.USDCIssuer), asset.Fiat("BRL")
}

// fuzzSellAmount is what the target asks to sell. Anchors may omit
// sell_amount, in which case the client backfills it from this value.
const fuzzSellAmount = "100"

func fuzzSellAmountDec() decimal.Decimal {
	return decimal.RequireFromString(fuzzSellAmount)
}

// quoteSeed is one entry of the seed corpus.
type quoteSeed struct {
	name   string
	body   []byte
	golden *goldenQuote // set for seeds rebuilt from a golden file
}

// loadQuoteSeeds builds the corpus from the two fixture directories the
// project already maintains: testdata/golden pins the fee arithmetic and
// testdata/live is a real anchor response.
//
// The golden files pin the *normalised* view of a quote, not the wire body it
// came from, so two seeds are derived from each one: the file verbatim, which
// keeps the target reading the pinned field names, and the wire response
// rebuilt from the pinned figures, which is the input that actually produces
// those figures. Without the rebuilt form the golden corpus would parse with
// no fee at all and never reach the fee paths.
func loadQuoteSeeds(tb testing.TB) []quoteSeed {
	tb.Helper()

	var seeds []quoteSeed

	goldenPaths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		tb.Fatalf("globbing golden fixtures: %v", err)
	}
	if len(goldenPaths) == 0 {
		tb.Fatal("no golden fixtures under testdata/golden: the seed corpus would be empty")
	}
	for _, path := range goldenPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			tb.Fatalf("reading %s: %v", path, err)
		}
		var g goldenQuote
		if err := json.Unmarshal(raw, &g); err != nil {
			tb.Fatalf("parsing %s: %v", path, err)
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		seeds = append(seeds,
			quoteSeed{name: name + "/as-pinned", body: raw},
			quoteSeed{name: name + "/wire", body: goldenWireBody(tb, g), golden: &g},
		)
	}

	livePaths, err := filepath.Glob(filepath.Join("testdata", "live", "*.json"))
	if err != nil {
		tb.Fatalf("globbing live fixtures: %v", err)
	}
	if len(livePaths) == 0 {
		tb.Fatal("no recorded responses under testdata/live: the seed corpus would not cover a real anchor")
	}
	for _, path := range livePaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			tb.Fatalf("reading %s: %v", path, err)
		}
		seeds = append(seeds, quoteSeed{name: "live/" + filepath.Base(path), body: raw})
	}

	return seeds
}

// goldenWireBody rebuilds the response body a golden file was produced from.
//
// total_price is carried through as pinned; the client derives its own and
// never reads the anchor's. Itemised fee details are not in the golden view,
// so the rebuilt body has none — the fee total and its denomination, which are
// what the identity is about, do survive.
func goldenWireBody(tb testing.TB, g goldenQuote) []byte {
	tb.Helper()

	fee := wireFee{Total: g.FeeTotal}
	// A golden file records an absent fee.asset as the zero Asset's SEP-38
	// form, "stellar::", which no anchor would send. Omitting the field is
	// what the original response actually looked like.
	if g.FeeAsset != (asset.Asset{}).SEP38() {
		fee.Asset = g.FeeAsset
	}

	raw, err := json.Marshal(wirePrice{
		TotalPrice: g.TotalPrice,
		Price:      g.Price,
		SellAmount: g.SellAmount,
		BuyAmount:  g.BuyAmount,
		Fee:        fee,
	})
	if err != nil {
		tb.Fatalf("rebuilding wire body: %v", err)
	}
	return raw
}

// FuzzQuoteNormalize feeds arbitrary response bodies through a real client
// call and asserts the fee identity on every quote the client accepts.
func FuzzQuoteNormalize(f *testing.F) {
	for _, seed := range loadQuoteSeeds(f) {
		f.Add(seed.body)
	}

	sell, buy := fuzzPair()
	sellAmount := fuzzSellAmountDec()

	f.Fuzz(func(t *testing.T, body []byte) {
		q, err := quoteClient(body).GetPrice(context.Background(), sell, buy, sellAmount, ContextSEP31)
		if err != nil {
			// Rejected input: the client refused to turn it into a quote,
			// which is the documented contract. Nothing to assert beyond
			// that the failure was an error and not a panic.
			return
		}
		assertFeeIdentity(t, body, q, sell, buy, sellAmount)
	})
}

// assertFeeIdentity asserts the properties a normalised quote must have: the
// gross is sell_amount / price, the fee is what that gross loses on the way to
// buy_amount, and neither figure depends on fee.asset.
func assertFeeIdentity(t *testing.T, body []byte, q *Quote, sell, buy asset.Asset, sellAmount decimal.Decimal) {
	t.Helper()

	// normalize refuses a non-positive price, so the division below is safe.
	if !q.Price.IsPositive() {
		t.Fatalf("accepted quote has non-positive price %s", q.Price)
	}
	if q.SellAmount.IsNegative() || q.BuyAmount.IsNegative() {
		t.Fatalf("accepted quote has a negative amount: sell %s, buy %s", q.SellAmount, q.BuyAmount)
	}

	// gross_in_buy_asset = sell_amount / price. This is the expression that
	// makes the fee denomination irrelevant, so it is the one asserted.
	gross := q.SellAmount.Div(q.Price)
	if !q.GrossBuyAmount.Equal(gross) {
		t.Fatalf("GrossBuyAmount = %s, want sell_amount/price = %s: "+
			"the gross is not derived from the fee-denomination identity",
			q.GrossBuyAmount, gross)
	}

	// fee_in_buy_asset = gross_in_buy_asset - buy_amount
	if want := q.GrossBuyAmount.Sub(q.BuyAmount); !q.FeeInBuyAsset.Equal(want) {
		t.Fatalf("FeeInBuyAsset = %s, want gross - buy_amount = %s",
			q.FeeInBuyAsset, want)
	}

	// ... and therefore, rearranged, the identity itself:
	// buy_amount + fee_in_buy_asset == gross_in_buy_asset.
	if got := q.BuyAmount.Add(q.FeeInBuyAsset); !got.Equal(q.GrossBuyAmount) {
		t.Fatalf("buy_amount + FeeInBuyAsset = %s, want GrossBuyAmount = %s",
			got, q.GrossBuyAmount)
	}

	// A net amount above the gross is a negative fee in disguise; normalize
	// refuses it rather than presenting it as a bonus, so a quote that got
	// this far cannot carry one.
	if q.FeeInBuyAsset.IsNegative() {
		t.Fatalf("accepted quote has negative FeeInBuyAsset %s", q.FeeInBuyAsset)
	}
	if q.GrossBuyAmount.LessThan(q.BuyAmount) {
		t.Fatalf("accepted quote has buy_amount %s above gross %s",
			q.BuyAmount, q.GrossBuyAmount)
	}

	// TotalPrice is sell_amount / buy_amount, and zero only when there is no
	// buy amount to divide by.
	if q.BuyAmount.IsZero() {
		if !q.TotalPrice.IsZero() {
			t.Fatalf("TotalPrice = %s with buy_amount 0, want 0", q.TotalPrice)
		}
	} else if want := q.SellAmount.Div(q.BuyAmount); !q.TotalPrice.Equal(want) {
		t.Fatalf("TotalPrice = %s, want sell_amount/buy_amount = %s", q.TotalPrice, want)
	}

	// The fee's denomination must not move the arithmetic. Re-send the same
	// response with the fee declared first in the sell asset and then in the
	// buy asset and require the identical gross and fee — this is the trap
	// the package exists to avoid, so it is asserted directly rather than
	// inferred from the sums above.
	var wire wirePrice
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("accepted body does not decode as a price response: %v", err)
	}
	for _, a := range []asset.Asset{sell, buy} {
		variant := wire
		variant.Fee.Asset = a.SEP38()
		raw, err := json.Marshal(variant)
		if err != nil {
			// wirePrice is all strings; this cannot fail.
			t.Fatalf("re-encoding an accepted body: %v", err)
		}
		got, err := quoteClient(raw).GetPrice(context.Background(), sell, buy, sellAmount, ContextSEP31)
		if err != nil {
			t.Fatalf("quote accepted with fee.asset %q is rejected with fee.asset %q: %v",
				wire.Fee.Asset, variant.Fee.Asset, err)
		}
		if !got.GrossBuyAmount.Equal(q.GrossBuyAmount) || !got.FeeInBuyAsset.Equal(q.FeeInBuyAsset) {
			t.Fatalf("fee.asset changed the derived figures: %q gives gross %s / fee %s, "+
				"%q gives gross %s / fee %s",
				wire.Fee.Asset, q.GrossBuyAmount, q.FeeInBuyAsset,
				variant.Fee.Asset, got.GrossBuyAmount, got.FeeInBuyAsset)
		}
	}
}

// TestFuzzSeedsAreAccepted guards the seed corpus itself.
//
// Fuzzing only reaches the identity assertions through accepted quotes, so a
// corpus that no longer parses would leave the target exercising the reject
// path forever while still passing. Every seed must be a quote the client
// accepts, and a seed rebuilt from a golden file must reproduce the figures
// that file pins — otherwise the corpus has drifted from the shapes it claims
// to seed.
func TestFuzzSeedsAreAccepted(t *testing.T) {
	seeds := loadQuoteSeeds(t)
	if len(seeds) == 0 {
		t.Fatal("empty seed corpus")
	}

	sell, buy := fuzzPair()
	sellAmount := fuzzSellAmountDec()

	for _, seed := range seeds {
		t.Run(seed.name, func(t *testing.T) {
			q, err := quoteClient(seed.body).GetPrice(context.Background(), sell, buy, sellAmount, ContextSEP31)
			if err != nil {
				t.Fatalf("seed is rejected: %v", err)
			}
			assertFeeIdentity(t, seed.body, q, sell, buy, sellAmount)

			if seed.golden == nil {
				return
			}
			got := goldenQuote{
				SellAmount:     q.SellAmount.String(),
				BuyAmount:      q.BuyAmount.String(),
				Price:          q.Price.String(),
				TotalPrice:     q.TotalPrice.String(),
				GrossBuyAmount: q.GrossBuyAmount.String(),
				FeeTotal:       q.Fee.Total.String(),
				FeeAsset:       q.Fee.Asset.SEP38(),
				FeeInBuyAsset:  q.FeeInBuyAsset.String(),
			}
			want := *seed.golden
			want.Note = ""
			if got != want {
				t.Errorf("rebuilt seed does not reproduce its golden file.\n got: %+v\nwant: %+v",
					got, want)
			}
		})
	}
}
