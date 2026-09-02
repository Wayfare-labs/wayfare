package dex_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
)

// bookServer serves one canned order-book body.
func bookServer(t *testing.T, body string) *dex.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/hal+json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &dex.Client{HorizonURL: srv.URL}
}

// TestOrderBookParsesRecordedMarket runs against a real XLM/NGNC order book
// captured from mainnet on 2026-08-21 (dex/testdata/orderbook-xlm-ngnc.json).
//
// Recorded rather than hand-written for the same reason the pathfinding
// fixtures are: a body built from wireOrderBook would carry whatever shape
// this package already expects, and the price_r rational alongside the decimal
// price string is exactly the kind of real-world detail that gets left out.
func TestOrderBookParsesRecordedMarket(t *testing.T) {
	raw, err := os.ReadFile("testdata/orderbook-xlm-ngnc.json")
	if err != nil {
		t.Fatalf("reading recorded order book: %v", err)
	}
	c := bookServer(t, string(raw))

	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}

	if h.BidLevels == 0 || h.AskLevels == 0 {
		t.Fatalf("recorded book has %d bids and %d asks; expected both sides populated",
			h.BidLevels, h.AskLevels)
	}
	if !h.BestBid.IsPositive() || !h.BestAsk.IsPositive() {
		t.Errorf("best bid %s / best ask %s, want both positive", h.BestBid, h.BestAsk)
	}
	// Best bid must be the highest price someone will pay and best ask the
	// lowest price someone will accept; a crossed book here would mean the
	// sides were mixed up.
	if h.BestBid.GreaterThan(h.BestAsk) {
		t.Errorf("best bid %s exceeds best ask %s: the book is crossed, sides likely swapped",
			h.BestBid, h.BestAsk)
	}
	if h.SpreadPct.IsNegative() {
		t.Errorf("SpreadPct = %s, want non-negative", h.SpreadPct)
	}
	if !h.Mid.Equal(decFromString(t, "185.75851395")) {
		t.Errorf("Mid = %s, want 185.75851395 from the recorded best prices", h.Mid)
	}
	if h.Selling.Code != "XLM" || h.Buying.Code != "NGNC" {
		t.Errorf("book endpoints = %s/%s, want XLM/NGNC", h.Selling.Code, h.Buying.Code)
	}
}

// TestDustLevelsAreExcludedFromBestPrice pins the reason dust is filtered at
// all: a zero-priced bid left in the book drags the computed spread to ~100%
// and hides whatever the real spread is.
func TestDustLevelsAreExcludedFromBestPrice(t *testing.T) {
	withDust := `{
      "bids":[{"price":"0.0000000","amount":"1000"},
              {"price":"100.0000000","amount":"50"}],
      "asks":[{"price":"101.0000000","amount":"50"}]}`

	c := bookServer(t, withDust)
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}

	if h.DustLevels != 1 {
		t.Errorf("DustLevels = %d, want 1", h.DustLevels)
	}
	if !h.BestBid.Equal(decFromString(t, "100")) {
		t.Errorf("BestBid = %s, want 100; the dust bid was not excluded", h.BestBid)
	}
	// (101 - 100) / 100.5 ≈ 0.995%
	if got := h.SpreadPct.StringFixed(2); got != "1.00" {
		t.Errorf("SpreadPct = %s, want ~1.00 computed from the real levels", got)
	}
	if !h.Functional() {
		t.Error("a 1% spread with both sides populated should be functional")
	}
}

// TestFunctionalAndSummary covers the market-condition verdicts, including
// the one-sided cases that a spread alone cannot express.
func TestFunctionalAndSummary(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		functional bool
		summary    string
	}{
		{
			name:       "empty book",
			body:       `{"bids":[],"asks":[]}`,
			functional: false,
			summary:    "no market",
		},
		{
			name:       "no bids",
			body:       `{"bids":[],"asks":[{"price":"100","amount":"1"}]}`,
			functional: false,
			summary:    "nobody is buying",
		},
		{
			name:       "no asks",
			body:       `{"bids":[{"price":"100","amount":"1"}],"asks":[]}`,
			functional: false,
			summary:    "nobody is selling",
		},
		{
			name: "wide spread",
			body: `{"bids":[{"price":"50","amount":"1"}],
			        "asks":[{"price":"150","amount":"1"}]}`,
			functional: false,
			summary:    "dysfunctional market",
		},
		{
			name: "healthy",
			body: `{"bids":[{"price":"100","amount":"1"}],
			        "asks":[{"price":"100.5","amount":"1"}]}`,
			functional: true,
			summary:    "functional market",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := bookServer(t, tc.body)
			h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
			if err != nil {
				t.Fatalf("OrderBook: %v", err)
			}
			if h.Functional() != tc.functional {
				t.Errorf("Functional() = %v, want %v (spread %s)",
					h.Functional(), tc.functional, h.SpreadPct)
			}
			if !strings.Contains(h.Summary(), tc.summary) {
				t.Errorf("Summary() = %q, want it to contain %q", h.Summary(), tc.summary)
			}
		})
	}
}

// TestUnparseableOfferIsSkippedNotFatal covers a single malformed level: the
// book is a diagnostic, so one bad offer should not discard the market's
// condition entirely.
func TestUnparseableOfferIsSkippedNotFatal(t *testing.T) {
	body := `{"bids":[{"price":"oops","amount":"1"},{"price":"100","amount":"1"}],
	          "asks":[{"price":"100.5","amount":"1"}]}`

	c := bookServer(t, body)
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}
	if !h.BestBid.Equal(decFromString(t, "100")) {
		t.Errorf("BestBid = %s, want 100 with the malformed level skipped", h.BestBid)
	}
}

// TestMalformedOrderBookJSONReturnsError ensures a successful HTTP response
// with invalid JSON is not treated as an empty, no-market book.
func TestMalformedOrderBookJSONReturnsError(t *testing.T) {
	c := bookServer(t, `{"bids":[`)
	_, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err == nil {
		t.Fatal("OrderBook returned nil error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decoding horizon response") {
		t.Errorf("error %q should identify response decoding", err)
	}
}

// TestOrderBookRateLimitReturnsTypedError ensures HTTP 429 is an error and
// remains distinguishable from a generic upstream failure.
func TestOrderBookRateLimitReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err == nil {
		t.Fatal("OrderBook returned nil error for HTTP 429")
	}
	var limited *dex.ErrRateLimited
	if !errors.As(err, &limited) {
		t.Fatalf("error %q does not unwrap to ErrRateLimited", err)
	}
	if limited.Endpoint != "/order_book" {
		t.Errorf("Endpoint = %q, want /order_book", limited.Endpoint)
	}
	if limited.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %s, want 30s", limited.RetryAfter)
	}
}

// TestOrderBookTimeoutReturnsClearError verifies context cancellation is
// surfaced rather than producing a partial BookHealth.
func TestOrderBookTimeoutReturnsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.OrderBook(ctx, asset.Native(), asset.NGNC())
	if err == nil {
		t.Fatal("OrderBook returned nil error after context timeout")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("error %q should identify the timeout", err)
	}
}

// TestDustOnlyBookIsNotLiquidity ensures dust offers are removed from both
// sides, leaving an explicit no-market health state.
func TestDustOnlyBookIsNotLiquidity(t *testing.T) {
	body := `{"bids":[{"price":"0.0000001","amount":"1000000000"}],
	          "asks":[{"price":"0.0000001","amount":"1000000000"}]}`
	c := bookServer(t, body)
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}
	if h.DustLevels != 2 || h.BidLevels != 0 || h.AskLevels != 0 {
		t.Errorf("health = bids %d, asks %d, dust %d; want 0/0/2", h.BidLevels, h.AskLevels, h.DustLevels)
	}
	if h.Functional() {
		t.Fatal("dust-only book reported a functional market")
	}
	if h.Summary() != "no market: order book is empty" {
		t.Errorf("Summary() = %q, want explicit no-market state", h.Summary())
	}
}

// TestExtremeSpreadIsPreserved ensures a very wide spread is reported rather
// than normalized into a healthy-looking value.
func TestExtremeSpreadIsPreserved(t *testing.T) {
	c := bookServer(t, `{"bids":[{"price":"0.000001","amount":"1"}],"asks":[{"price":"1000000","amount":"1"}]}`)
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}
	if h.Functional() {
		t.Fatal("extreme-spread book reported a functional market")
	}
	if !h.SpreadPct.GreaterThan(decFromString(t, "199")) {
		t.Errorf("SpreadPct = %s, want an extreme spread near 200%%", h.SpreadPct)
	}
	if !strings.Contains(h.Summary(), "dysfunctional market") {
		t.Errorf("Summary() = %q, want dysfunctional market", h.Summary())
	}
}

// TestLargeOrderBookRetainsHealthPerformance exercises the Horizon limit of
// 200 levels while checking that the extrema are selected correctly.
func TestLargeOrderBookRetainsHealthPerformance(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"bids":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"price":"100","amount":"1"}`)
	}
	b.WriteString(`],"asks":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"price":"100.5","amount":"1"}`)
	}
	b.WriteString(`]}`)

	c := bookServer(t, b.String())
	start := time.Now()
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("200-level order book took %s; want under 1s", elapsed)
	}
	if h.BidLevels != 200 || h.AskLevels != 200 || !h.Functional() {
		t.Errorf("health = bids %d, asks %d, functional %v; want 200/200/true", h.BidLevels, h.AskLevels, h.Functional())
	}
}

// TestStaleLikeOffersAreStillReportedAsBookData documents the boundary: the
// order-book endpoint has no timestamp on levels, so health cannot invent a
// stale classification. It still must not claim a market when the entries are
// dust-only.
func TestStaleLikeOffersAreStillReportedAsBookData(t *testing.T) {
	c := bookServer(t, `{"bids":[{"price":"0.00000001","amount":"1"}],"asks":[{"price":"0.00000001","amount":"1"}]}`)
	h, err := c.OrderBook(context.Background(), asset.Native(), asset.NGNC())
	if err != nil {
		t.Fatalf("OrderBook: %v", err)
	}
	if h.Functional() || h.BidLevels != 0 || h.AskLevels != 0 {
		t.Fatalf("stale-like dust book = %+v; want no market", h)
	}
}
