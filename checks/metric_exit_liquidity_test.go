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

// reversePathServer prices NGNC -> USDC at a fixed effective rate
// (USDC received per NGNC sent), one strict-send record per request.
func reversePathServer(t *testing.T, rate decimal.Decimal) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/paths/strict-send" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
			return
		}
		sourceAmt, err := decimal.NewFromString(r.URL.Query().Get("source_amount"))
		if err != nil {
			http.Error(w, "bad source_amount", http.StatusBadRequest)
			return
		}
		dest := sourceAmt.Mul(rate)
		record := fmt.Sprintf(`{
			"source_asset_type":"credit_alphanum4",
			"source_asset_code":"NGNC",
			"source_asset_issuer":"%s",
			"source_amount":"%s",
			"destination_asset_type":"credit_alphanum4",
			"destination_asset_code":"USDC",
			"destination_asset_issuer":"%s",
			"destination_amount":"%s",
			"path":[]
		}`, asset.LinkIOIssuer, sourceAmt.String(), asset.USDCIssuer, dest.String())
		body := `{"_embedded":{"records":[` + record + `]}}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return srv
}

func TestExitLiquidityMetricTwoReverseRungs(t *testing.T) {
	// Reference USD/NGN = 2, so the inverse mid is 0.5 USDC per NGNC. At an
	// effective rate of 0.4 the loss is 20% at both rungs; the headline is
	// the medium-size loss.
	srv := reversePathServer(t, decimal.NewFromInt(4).Div(decimal.NewFromInt(10)))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ExitLiquidityMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	if res.ID != "exit-liquidity.reverse-send-loss" {
		t.Errorf("id = %q, want exit-liquidity.reverse-send-loss", res.ID)
	}
	if res.Venue != VenuePathfinding {
		t.Errorf("venue = %q, want pathfinding", res.Venue)
	}
	if res.Unit != UnitPercent {
		t.Errorf("unit = %q, want percent", res.Unit)
	}
	want := decimal.NewFromInt(20)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 20%%", res.Value)
	}
	if !strings.Contains(res.Summary, "20.0000") {
		t.Errorf("summary = %q, want the medium-size loss", res.Summary)
	}
	if len(res.Evidence) < 3 {
		t.Errorf("expected evidence for the reference plus both rungs, got %d", len(res.Evidence))
	}
}

func TestExitLiquidityMetricMediumUnpricedFallsBackToSmall(t *testing.T) {
	// Medium (10) returns no path; small (0.1) prices at 0.4 effective.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceAmt := r.URL.Query().Get("source_amount")
		dest := "0"
		if sourceAmt == "0.1" {
			dest = "0.04" // effective 0.4
		}
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4",
			"source_asset_code":"NGNC",
			"source_asset_issuer":"GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
			"source_amount":"` + sourceAmt + `",
			"destination_asset_type":"credit_alphanum4",
			"destination_asset_code":"USDC",
			"destination_asset_issuer":"GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
			"destination_amount":"` + dest + `",
			"path":[]
		}]}}`))
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ExitLiquidityMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("expected determined, got undetermined: %s", res.Reason)
	}
	want := decimal.NewFromInt(20)
	if res.Value.Cmp(want) != 0 {
		t.Errorf("value = %s, want 20%% (small-size fallback)", res.Value)
	}
}

func TestExitLiquidityMetricNoMarketIsDetermined(t *testing.T) {
	// No reverse path at either size: NO-MARKET is a measured result, not
	// undetermined.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"_embedded":{"records":[]}}`))
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL, HTTPClient: srv.Client()}
	m := ExitLiquidityMetric{
		DEX:       c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !res.Determined {
		t.Fatalf("NO-MARKET must be determined, got undetermined: %s", res.Reason)
	}
	if !res.Value.IsZero() {
		t.Errorf("value = %s, want zero for NO-MARKET", res.Value)
	}
	haystack := res.Summary + " " + res.Reason
	if !strings.Contains(haystack, "no reverse path") && !strings.Contains(haystack, "NO-MARKET") {
		t.Errorf("summary/reason = %q/%q, want it to name NO-MARKET", res.Summary, res.Reason)
	}
	for _, e := range res.Evidence {
		if e.Source == "" || e.Observed == "" || e.ObservedAt.IsZero() {
			t.Errorf("evidence %+v is incomplete", e)
		}
	}
}

func TestExitLiquidityMetricUnscorableReference(t *testing.T) {
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org"}
	m := ExitLiquidityMetric{
		DEX: c,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test",
			Agreement: refrate.AgreementMalfunction},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})
	if res.Determined {
		t.Error("unscorable reference must be undetermined")
	}
	if res.Reason == "" {
		t.Error("undetermined result must carry a reason")
	}
}

func TestExitLiquidityMetricNilDEX(t *testing.T) {
	m := ExitLiquidityMetric{
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test"},
	}
	res := RunMetric(ctx(), m, Subject{Send: asset.USDC(), Receive: asset.NGNC()})
	if res.Determined {
		t.Error("nil DEX client must be undetermined")
	}
}

func TestExitLiquidityMetricEmptySubject(t *testing.T) {
	m := ExitLiquidityMetric{DEX: nil,
		Reference: refrate.Rate{Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(2), Source: "test"}}
	res := RunMetric(ctx(), m, Subject{})
	if res.Determined {
		t.Error("empty subject must be undetermined")
	}
}

func TestExitLiquidityMetricDescriptorIsValid(t *testing.T) {
	d := ExitLiquidityMetric{}.Describe()
	if err := d.ValidateAsMetric(); err != nil {
		t.Errorf("descriptor invalid: %v", err)
	}
	if d.Venue != VenuePathfinding || d.Scope != ScopeCorridor {
		t.Errorf("venue/scope = %q/%q, want pathfinding/corridor", d.Venue, d.Scope)
	}
}
