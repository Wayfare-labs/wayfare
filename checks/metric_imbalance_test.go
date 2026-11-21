package checks

import (
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

func orderBookServer(t *testing.T, bids, asks int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/order_book" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
			return
		}
		var bidOffers, askOffers []string
		for i := 0; i < bids; i++ {
			price := "1300" // Within 5% band of 1350 mid
			bidOffers = append(bidOffers, `{"price":"`+price+`","amount":"100"}`)
		}
		for i := 0; i < asks; i++ {
			price := "1400" // Within 5% band of 1350 mid
			askOffers = append(askOffers, `{"price":"`+price+`","amount":"100"}`)
		}
		body := `{"bids":[` + strings.Join(bidOffers, ",") + `],"asks":[` + strings.Join(askOffers, ",") + `]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return srv
}

func TestImbalanceMetricTwoSidedBook(t *testing.T) {
	srv := orderBookServer(t, 10, 5) // bid depth > ask depth
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	if res.ID != "imbalance.bid-ask-depth" {
		t.Errorf("id = %q, want imbalance.bid-ask-depth", res.ID)
	}
	if res.Venue != VenueOrderBook {
		t.Errorf("venue = %q, want order-book", res.Venue)
	}
	if res.Unit != UnitRatio {
		t.Errorf("unit = %q, want ratio", res.Unit)
	}
	// 10 bids * 100 amount = 1000 bid depth, 5 asks * 100 amount = 500 ask depth -> ratio = 2.0
	want := decimal.NewFromInt(2)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 2.0", res.Value)
	}
	// Summary now uses amounts, not counts
	if !strings.Contains(res.Summary, "bid depth in band 1000") || !strings.Contains(res.Summary, "ask depth in band 500") {
		t.Errorf("summary = %q, want bid/ask depths in band", res.Summary)
	}
	if len(res.Evidence) < 2 {
		t.Errorf("expected evidence for book and reference, got %d", len(res.Evidence))
	}
}

func TestImbalanceMetricAskDeeper(t *testing.T) {
	srv := orderBookServer(t, 3, 12) // ask depth > bid depth
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	// bid_depth=3, ask_depth=12 -> ratio = 0.25
	want := decimal.NewFromInt(1).Div(decimal.NewFromInt(4))
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 0.25", res.Value)
	}
}

func TestImbalanceMetricNoBidsOneSided(t *testing.T) {
	srv := orderBookServer(t, 0, 8) // no bids
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("one-sided (no bids) must be determined, got undetermined: %s", res.Reason)
	}
	// No bids, asks exist -> ratio = 0
	if !res.Value.IsZero() {
		t.Errorf("value = %s, want 0 for no bids", res.Value)
	}
	if !strings.Contains(res.Summary, "no bids") {
		t.Errorf("summary = %q, want it to name the missing bids side", res.Summary)
	}
}

func TestImbalanceMetricNoAsksOneSided(t *testing.T) {
	srv := orderBookServer(t, 8, 0) // no asks
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("one-sided (no asks) must be determined, got undetermined: %s", res.Reason)
	}
	// No asks, bids exist -> ratio = 1,000,000 (sentinel for infinite)
	want := decimal.NewFromInt(1000000)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 1000000 for no asks", res.Value)
	}
	if !strings.Contains(res.Summary, "no asks") {
		t.Errorf("summary = %q, want it to name the missing asks side", res.Summary)
	}
}

func TestImbalanceMetricEmptyBook(t *testing.T) {
	srv := orderBookServer(t, 0, 0) // empty
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if res.Determined {
		t.Error("empty book must produce an undetermined result")
	}
	if !strings.Contains(res.Reason, "no depth in band") {
		t.Errorf("reason = %q, want it to name the empty band", res.Reason)
	}
}

func TestImbalanceMetricNilDEX(t *testing.T) {
	m := ImbalanceMetric{
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if res.Determined {
		t.Error("nil DEX client must produce an undetermined result")
	}
}

func TestImbalanceMetricEmptySubject(t *testing.T) {
	m := ImbalanceMetric{
		DEX:       &dex.Client{HorizonURL: "http://example.invalid"},
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{})

	if res.Determined {
		t.Error("empty subject must produce an undetermined result")
	}
}

func TestImbalanceMetricUnscorableReference(t *testing.T) {
	srv := orderBookServer(t, 10, 5)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX: c,
		Reference: refrate.Rate{
			Base:      "USD",
			Quote:     "NGN",
			Mid:       decimal.NewFromInt(1350),
			Source:    "test",
			Agreement: refrate.AgreementMalfunction,
		},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if res.Determined {
		t.Error("unscorable reference must be undetermined")
	}
	if res.Reason == "" {
		t.Error("undetermined result must carry a reason")
	}
}

func TestImbalanceMetricUnavailableReference(t *testing.T) {
	m := ImbalanceMetric{
		DEX:                  &dex.Client{HorizonURL: "https://horizon.stellar.org"},
		ReferenceUnavailable: "provider failed",
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if res.Determined {
		t.Error("unavailable reference must be undetermined")
	}
	if !strings.Contains(res.Reason, "provider failed") {
		t.Errorf("reason = %q, want it to name the provider failure", res.Reason)
	}
}

func TestImbalanceMetricNoMarketIntegrity(t *testing.T) {
	srv := orderBookServer(t, 10, 5)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	// NO-MARKET integrity means no direct pair by construction
	res := RunMetric(ctx(), m, Subject{
		Send:      asset.USDC(),
		Receive:   asset.NGNC(),
		Integrity: "NO-MARKET",
	})

	if res.Determined {
		t.Error("NO-MARKET integrity must be undetermined")
	}
	if !strings.Contains(res.Reason, "NO-MARKET") {
		t.Errorf("reason = %q, want it to name NO-MARKET", res.Reason)
	}
}

func TestImbalanceMetricDerivativeNoUnderlying(t *testing.T) {
	srv := orderBookServer(t, 10, 5)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	// DERIVATIVE without underlying means no direct book to measure
	res := RunMetric(ctx(), m, Subject{
		Send:      asset.USDC(),
		Receive:   asset.GHSC(),
		Integrity: "DERIVATIVE",
	})

	if res.Determined {
		t.Error("DERIVATIVE without underlying must be undetermined")
	}
	if !strings.Contains(res.Reason, "DERIVATIVE") {
		t.Errorf("reason = %q, want it to name DERIVATIVE", res.Reason)
	}
}

func TestImbalanceMetricDescriptorIsValid(t *testing.T) {
	d := ImbalanceMetric{}.Describe()
	if err := d.ValidateAsMetric(); err != nil {
		t.Errorf("descriptor invalid: %v", err)
	}
	if d.Venue != VenueOrderBook || d.Scope != ScopeCorridor {
		t.Errorf("venue/scope = %q/%q, want order-book/corridor", d.Venue, d.Scope)
	}
	if d.ID != "imbalance.bid-ask-depth" {
		t.Errorf("id = %q, want imbalance.bid-ask-depth", d.ID)
	}
}

func TestImbalanceMetricCustomBandPct(t *testing.T) {
	// BandPct of 10% means band is [1215, 1485] around mid=1350
	// Our test server uses prices 1300 (bids) and 1400 (asks) - both within 10% band
	srv := orderBookServer(t, 20, 10)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
		BandPct:   decimal.NewFromInt(10),
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	// bid_depth=20, ask_depth=10 -> ratio = 2.0
	want := decimal.NewFromInt(2)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 2.0", res.Value)
	}
	// Reference evidence is at index 1 (book evidence at 0, ref at 1)
	found := false
	for _, e := range res.Evidence {
		if strings.Contains(e.Observed, "band_pct=10.00%") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("evidence should mention custom band_pct: %+v", res.Evidence)
	}
}

func TestImbalanceMetricDerivativeWithUnderlying(t *testing.T) {
	srv := orderBookServer(t, 10, 5)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	// DERIVATIVE with underlying NGNC - should measure the NGNC book
	res := RunMetric(ctx(), m, Subject{
		Send:       asset.USDC(),
		Receive:    asset.GHSC(),
		Integrity:  "DERIVATIVE",
		Underlying: asset.NGNC(),
	})

	if !res.Determined {
		t.Fatalf("DERIVATIVE with underlying must be determined, got undetermined: %s", res.Reason)
	}
	if !strings.Contains(res.Summary, "underlying") {
		t.Errorf("summary = %q, want it to mention the underlying substitution", res.Summary)
	}
}

// orderBookServerWithPrices creates a test server with custom bid/ask prices and amounts.
func orderBookServerWithPrices(t *testing.T, bidPrice, bidAmount string, bidCount int, askPrice, askAmount string, askCount int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/order_book" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
			return
		}
		var bidOffers, askOffers []string
		for i := 0; i < bidCount; i++ {
			bidOffers = append(bidOffers, `{"price":"`+bidPrice+`","amount":"`+bidAmount+`"}`)
		}
		for i := 0; i < askCount; i++ {
			askOffers = append(askOffers, `{"price":"`+askPrice+`","amount":"`+askAmount+`"}`)
		}
		body := `{"bids":[` + strings.Join(bidOffers, ",") + `],"asks":[` + strings.Join(askOffers, ",") + `]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return srv
}

// TestImbalanceMetricExcludesOffersOutsideBand verifies that offers outside the
// price band are excluded from the depth calculation.
func TestImbalanceMetricExcludesOffersOutsideBand(t *testing.T) {
	// Band is 5% around mid=1350 -> [1282.5, 1417.5]
	// Bid at 1200 (outside, below band), ask at 1500 (outside, above band)
	// These should be excluded, resulting in no depth in band -> undetermined
	srv := orderBookServerWithPrices(t, "1200", "100", 10, "1500", "100", 10)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if res.Determined {
		t.Fatalf("expected undetermined (offers outside band), got determined: %s", res.Summary)
	}
	if !strings.Contains(res.Reason, "no depth in band") {
		t.Errorf("reason = %q, want it to name the empty band", res.Reason)
	}
}

// TestImbalanceMetricComparesAmountsNotCounts verifies that depth is computed
// from offer amounts, not level counts.
func TestImbalanceMetricComparesAmountsNotCounts(t *testing.T) {
	// Band is 5% around mid=1350 -> [1282.5, 1417.5]
	// 2 bids at price 1300 with amount 500 each = 1000 total bid depth
	// 10 asks at price 1400 with amount 10 each = 100 total ask depth
	// If using counts: bid=2, ask=10 -> ratio=0.2
	// If using amounts: bid=1000, ask=100 -> ratio=10
	srv := orderBookServerWithPrices(t, "1300", "500", 2, "1400", "10", 10)
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	// Ratio should be 1000/100 = 10, not 2/10 = 0.2
	want := decimal.NewFromInt(10)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 10 (amounts), not 0.2 (counts)", res.Value)
	}
}

// TestImbalanceMetricDeepTwoSidedSnapshot verifies a deep two-sided order book
// with many levels on both sides.
func TestImbalanceMetricDeepTwoSidedSnapshot(t *testing.T) {
	// Band is 5% around mid=1350 -> [1282.5, 1417.5]
	// 50 bids at prices 1283-1349, varying amounts
	// 50 asks at prices 1351-1417, varying amounts
	var bidOffers, askOffers []string
	bidTotal := decimal.Zero
	for i := 0; i < 50; i++ {
		price := 1283 + i
		amount := 100 + i*10
		bidOffers = append(bidOffers, fmt.Sprintf(`{"price":"%d","amount":"%d"}`, price, amount))
		bidTotal = bidTotal.Add(decimal.NewFromInt(int64(amount)))
	}
	askTotal := decimal.Zero
	for i := 0; i < 50; i++ {
		price := 1351 + i
		amount := 200 + i*5
		askOffers = append(askOffers, fmt.Sprintf(`{"price":"%d","amount":"%d"}`, price, amount))
		askTotal = askTotal.Add(decimal.NewFromInt(int64(amount)))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/order_book" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
			return
		}
		body := `{"bids":[` + strings.Join(bidOffers, ",") + `],"asks":[` + strings.Join(askOffers, ",") + `]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ImbalanceMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	// Verify ratio matches amount-based calculation
	wantRatio := bidTotal.Div(askTotal)
	if res.Value.Cmp(wantRatio) != 0 {
		t.Errorf("value = %s, want %s", res.Value, wantRatio.StringFixed(4))
	}
	// Evidence should report the summed amounts
	found := false
	for _, e := range res.Evidence {
		if strings.Contains(e.Observed, "bid_depth_in_band") && strings.Contains(e.Observed, "ask_depth_in_band") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("evidence should report bid_depth_in_band and ask_depth_in_band: %+v", res.Evidence)
	}
}
