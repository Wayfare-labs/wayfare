package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// adaptiveCorridorServer prices USDC -> NGNC with a large loss jump between
// sizes 1 and 3, so the default adaptive rule adds the midpoint at 2.
func adaptiveCorridorServer(t *testing.T) *httptest.Server {
	t.Helper()
	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		d, err := decimal.NewFromString(amount)
		if err != nil {
			http.Error(w, "bad amount", http.StatusBadRequest)
			return
		}
		rate := decimal.RequireFromString("1500").Sub(d.Mul(decimal.RequireFromString("100")))
		if d.Equal(decimal.NewFromInt(2)) {
			rate = decimal.RequireFromString("500")
		}
		dest := d.Mul(rate)
		_, _ = fmt.Fprintf(w, `{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":%q,
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":%q,"path":[]}]}}`, amount, dest.String())
	}))
	t.Cleanup(horizon.Close)

	s := &Server{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: horizon.URL},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1500"),
			}),
		},
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

// TestCorridorAdaptiveIsOptInAndMarked checks both halves of the API contract:
// without the parameter the ladder is exactly the requested set, and with it an
// extra midpoint rung appears carrying adaptive:true.
func TestCorridorAdaptiveIsOptInAndMarked(t *testing.T) {
	srv := adaptiveCorridorServer(t)

	plain := getCorridor(t, srv.URL+"/api/corridor?to=NGNC&sizes=1,3")
	if len(plain.Rungs) != 2 {
		t.Errorf("rungs without adaptive = %d, want the 2 requested", len(plain.Rungs))
	}
	for _, r := range plain.Rungs {
		if r.Adaptive {
			t.Errorf("rung %s is adaptive without ?adaptive: the rule must be opt-in", r.SendAmount)
		}
	}

	adapted := getCorridor(t, srv.URL+"/api/corridor?to=NGNC&sizes=1,3&adaptive=1")
	if len(adapted.Rungs) != 3 {
		t.Fatalf("rungs with adaptive = %d, want 3 (1, midpoint 2, 3): %+v",
			len(adapted.Rungs), adapted.Rungs)
	}
	var added *route.RungJSON
	for i := range adapted.Rungs {
		if adapted.Rungs[i].Adaptive {
			added = &adapted.Rungs[i]
		}
	}
	if added == nil {
		t.Fatalf("no rung is marked adaptive: %+v", adapted.Rungs)
	}
	if added.SendAmount != "2" {
		t.Errorf("adaptive rung size = %s, want the midpoint 2", added.SendAmount)
	}
	if added.Quote == nil {
		t.Fatal("the adaptive rung carries no quote; it must be measured, not interpolated")
	}
	if got := added.Quote.EffectiveRate; got != "500" {
		t.Errorf("adaptive rung effective_rate = %s, want the measured 500", got)
	}
}
