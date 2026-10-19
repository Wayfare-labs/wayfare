package route

import (
	"errors"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestToRungJSONMatchesTheWholeCorridorRung pins that the streaming endpoint's
// per-rung conversion is the same one the whole-corridor response uses (#639).
// A client that follows the rungs off the stream and a client that reads the
// final document must see the identical rung; two converters would drift and
// the streaming view would be the one that drifted.
func TestToRungJSONMatchesTheWholeCorridorRung(t *testing.T) {
	send, recv := asset.USDC(), asset.NGNC()
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     send,
		SendAmount:    decimal.RequireFromString("100"),
		ReceiveAsset:  recv,
		ReceiveAmount: decimal.RequireFromString("129000"),
		EffectiveRate: decimal.RequireFromString("1290"),
		LossPct:       decimal.RequireFromString("4.46"),
		Verdict:       VerdictFair,
		PathCount:     3,
	}
	priced := Rung{
		SendAmount: decimal.RequireFromString("100"),
		Result:     &Result{Quotes: []Quote{q}, Integrity: IntegrityDirect},
	}
	failed := Rung{
		SendAmount: decimal.RequireFromString("200"),
		Err:        errors.New("horizon unreachable"),
	}

	lr := &LadderResult{
		Request: LadderRequest{SendAsset: send, ReceiveAsset: recv, ReferenceBase: "USD", ReferenceQuote: "NGN"},
		Rungs:   []Rung{priced, failed},
	}

	out := ToCorridorJSON(lr, "USD/NGN")
	for i, r := range lr.Rungs {
		want := ToRungJSON(r)
		if !reflect.DeepEqual(out.Rungs[i], want) {
			t.Errorf("rung %d: ToCorridorJSON = %+v, ToRungJSON = %+v; the two views of one measurement must agree", i, out.Rungs[i], want)
		}
	}
}

// TestToRungJSONBeforeSummarisationOmitsDerivedFields documents the one
// deliberate difference between a streamed rung and a finished one: marginal
// cost and the cost decomposition are computed across the finished ladder, so
// a rung sent the moment it resolves carries neither. Everything the rung
// knows on its own — priced, integrity, quote, path count — must be present.
func TestToRungJSONBeforeSummarisationOmitsDerivedFields(t *testing.T) {
	r := Rung{
		SendAmount: decimal.RequireFromString("100"),
		Result: &Result{
			Quotes:    []Quote{{Kind: KindDEX, Verdict: VerdictGood, EffectiveRate: decimal.RequireFromString("1000"), PathCount: 2}},
			Integrity: IntegrityDirect,
		},
	}
	got := ToRungJSON(r)
	if got.MarginalCost != "" || got.Cost != nil {
		t.Errorf("a streamed rung carries derived fields: marginal_cost=%q cost=%+v", got.MarginalCost, got.Cost)
	}
	if !got.Priced {
		t.Error("a rung with a quote must report priced=true")
	}
	if got.PathCount != 2 {
		t.Errorf("path_count = %d, want 2", got.PathCount)
	}
	if got.Quote == nil {
		t.Fatal("a priced rung must carry its quote")
	}
}
