package route_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// TestLadderReportsEachRungAsItResolves pins OnRung (#639): the progress
// callback fires once per rung, with the rung's index in the ladder and the
// rung itself, so the streaming endpoint can emit a rung event the moment its
// price resolves instead of waiting for the whole ladder. The callback runs
// from the pricing goroutines, so the test guards its own map.
func TestLadderReportsEachRungAsItResolves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		dest := decimal.RequireFromString(amount).Mul(decimal.RequireFromString("1300"))
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":"` + amount + `",
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":"` + dest.String() + `","path":[]}]}}`))
	}))
	defer srv.Close()

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1350"),
		}),
	}

	var mu sync.Mutex
	seen := map[int]string{}
	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("10"), decimal.RequireFromString("100"), decimal.RequireFromString("500")},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
		OnRung: func(index int, rung route.Rung) {
			mu.Lock()
			defer mu.Unlock()
			seen[index] = rung.SendAmount.String()
		},
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}

	if len(seen) != len(res.Rungs) {
		t.Fatalf("OnRung fired for %d rungs, want %d", len(seen), len(res.Rungs))
	}
	for i, r := range res.Rungs {
		if seen[i] != r.SendAmount.String() {
			t.Errorf("rung %d: OnRung saw send_amount %q, ladder has %q", i, seen[i], r.SendAmount.String())
		}
	}
}

// TestLadderWithoutOnRungStillMeasures guards that OnRung is optional: a
// caller that never wants progress (every existing caller) must be unaffected
// by its existence.
func TestLadderWithoutOnRungStillMeasures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		dest := decimal.RequireFromString(amount).Mul(decimal.RequireFromString("1300"))
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":"` + amount + `",
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":"` + dest.String() + `","path":[]}]}}`))
	}))
	defer srv.Close()

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1350"),
		}),
	}

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("100")},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if res.Failed() {
		t.Fatal("a one-rung ladder against a working upstream must not fail")
	}
}
