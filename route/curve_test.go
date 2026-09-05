package route_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/shopspring/decimal"
)

func TestCurvePublishesMeasuredRatesAndHoles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.URL.Query().Get("source_amount")
		d, _ := decimal.NewFromString(a)
		if d.Equal(decimal.NewFromInt(10)) {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		rate := decimal.NewFromInt(1000)
		if d.Equal(decimal.NewFromInt(100)) {
			rate = decimal.NewFromInt(1200)
		}
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{"source_asset_type":"credit_alphanum4","source_asset_code":"USDC","source_amount":"` + a + `","destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC","destination_amount":"` + d.Mul(rate).String() + `","path":[]}]}}`))
	}))
	defer srv.Close()

	e := &route.Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1300)})}
	res, err := e.Ladder(context.Background(), route.LadderRequest{SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(), Sizes: []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(10), decimal.NewFromInt(100)}, ReferenceBase: "USD", ReferenceQuote: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Curve == nil || res.Curve.PricedCount != 2 || len(res.Curve.Points) != 3 {
		t.Fatalf("curve = %#v", res.Curve)
	}
	if res.Curve.Points[1].Priced || res.Curve.Points[1].Reason == "" {
		t.Errorf("hole = %#v", res.Curve.Points[1])
	}
	if !res.Curve.NonMonotonic {
		t.Error("curve did not flag rate increase at larger size")
	}

	wire := route.ToCorridorJSON(res, "USD/NGN")
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	curve := raw["curve"].(map[string]any)
	points := curve["points"].([]any)
	if _, ok := points[1].(map[string]any)["rate"]; ok {
		t.Error("unpriced hole has a rate")
	}
}

func TestCurveRequiresTwoPricedObservations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.URL.Query().Get("source_amount")
		if a != "1" {
			http.Error(w, "fail", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{"source_asset_type":"credit_alphanum4","source_asset_code":"USDC","source_amount":"1","destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC","destination_amount":"1000","path":[]}]}}`))
	}))
	defer srv.Close()
	e := &route.Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1300)})}
	res, err := e.Ladder(context.Background(), route.LadderRequest{SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(), Sizes: []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2)}, ReferenceBase: "USD", ReferenceQuote: "NGN"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Curve != nil {
		t.Fatalf("curve = %#v, want nil", res.Curve)
	}
}
