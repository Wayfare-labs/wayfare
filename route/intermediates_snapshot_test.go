package route_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
)

// A rung's best path can hop through a token whose issuing account Wayfare has
// never verified, and the DERIVATIVE state does not see it: that state counts
// fiat-pegged dependencies, while an unverified non-fiat hop carries the same
// issuer's freeze and clawback powers without creating a fiat dependency.
//
// These tests drive the fact from the recorded USDC->NGNC paths, so the rungs
// that report it are the ones mainnet actually produced.

// ladderFromSnapshot prices the recorded USDC->NGNC corridor at every default
// size against a chosen mid.
func ladderFromSnapshot(t *testing.T, mid string) *route.LadderResult {
	t.Helper()
	e := engineOver(loadSnap(t, "usdc-ngnc"), "USD/NGN", mid)
	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	return res
}

// rungAt returns the rung for one send amount, failing if it was not measured.
func rungAt(t *testing.T, res *route.LadderResult, amount string) route.Rung {
	t.Helper()
	want := decimal.RequireFromString(amount)
	for _, r := range res.Rungs {
		if r.SendAmount.Equal(want) {
			return r
		}
	}
	t.Fatalf("no rung at %s USDC", amount)
	return route.Rung{}
}

// TestRecordedUnverifiedIntermediatesAreReportedPerRung is the acceptance
// test: per rung, the best path's unverified hop issuers are reported from the
// recorded path data, and a path through verified issuers reports nothing.
//
// The recorded best paths are:
//
//	0.1 USDC  Cleanshave + AQUA   both unverified
//	1 USDC    yUSDC + AQUA        both unverified
//	5 USDC    USDZ + XLM          USDZ is registered; XLM has no issuer
//	10 USDC   BTC + XLM           BTC's issuer is unverified
//	50/100    PYUSD + XLM         PYUSD is registered
//	25+       XLM only            native has no issuer
func TestRecordedUnverifiedIntermediatesAreReportedPerRung(t *testing.T) {
	res := ladderFromSnapshot(t, "1350.2568")

	cases := []struct {
		amount string
		codes  []string // unverified hop codes expected; empty means no findings
	}{
		{"0.1", []string{"Cleanshave", "AQUA"}},
		{"1", []string{"yUSDC", "AQUA"}},
		{"5", nil},
		{"10", []string{"BTC"}},
		{"25", nil},
		{"50", nil},
		{"100", nil},
		{"250", nil},
		{"500", nil},
		{"1000", nil},
		{"2500", nil},
		{"5000", nil},
	}

	for _, tc := range cases {
		t.Run(tc.amount, func(t *testing.T) {
			r := rungAt(t, res, tc.amount)
			if !r.Priced() {
				t.Fatalf("recorded rung at %s USDC did not price", tc.amount)
			}

			if len(tc.codes) == 0 {
				if r.Findings != nil {
					t.Errorf("rung %s reported findings for a path with no unverified "+
						"intermediate: %+v", tc.amount, r.Findings.ToJSON().Checks)
				}
				return
			}

			if r.Findings == nil {
				t.Fatalf("rung %s hops through unverified issuer(s) %v but carries no findings",
					tc.amount, tc.codes)
			}
			f := r.Findings.ToJSON()
			if f.Failed != 1 || f.Passed != 0 || f.Undetermined != 0 {
				t.Fatalf("rung %s findings counts = %d passed, %d failed, %d undetermined; "+
					"want one failed result", tc.amount, f.Passed, f.Failed, f.Undetermined)
			}
			check := f.Checks[0]
			if check.ID != "asset.unverified-intermediate" {
				t.Errorf("rung %s check ID = %q, want asset.unverified-intermediate",
					tc.amount, check.ID)
			}
			if !check.Determined || check.Passed {
				t.Errorf("rung %s check = determined=%v passed=%v, want a determined failure",
					tc.amount, check.Determined, check.Passed)
			}
			for _, code := range tc.codes {
				if !strings.Contains(check.Summary, code) {
					t.Errorf("rung %s summary does not name %s: %q",
						tc.amount, code, check.Summary)
				}
			}
			if len(check.Evidence) != len(tc.codes) {
				t.Errorf("rung %s evidence count = %d, want one per issued hop (%d)",
					tc.amount, len(check.Evidence), len(tc.codes))
			}
		})
	}
}

// TestUnverifiedIntermediatesDoNotMoveTheVerdict is the second acceptance
// criterion, attacked where a change would be visible.
//
// The mid is set to 660 NGN/USD, which the recorded paths beat comfortably, so
// this corridor is recommendable at every size. If an unverified hop issuer
// were allowed to influence the verdict, the rungs that report one would lose
// their recommendation and stop being GOOD — exactly what this checks.
func TestUnverifiedIntermediatesDoNotMoveTheVerdict(t *testing.T) {
	res := ladderFromSnapshot(t, "660")

	if res.Recommended == nil {
		t.Fatal("test setup is wrong: this corridor must be recommendable for a " +
			"verdict movement to be detectable")
	}

	for _, amount := range []string{"0.1", "1", "10"} {
		t.Run(amount, func(t *testing.T) {
			r := rungAt(t, res, amount)
			if r.Findings == nil {
				t.Fatalf("test setup is wrong: rung %s should carry an unverified-intermediate finding", amount)
			}

			q := r.Result.Quotes[0]
			if q.Verdict != route.VerdictGood {
				t.Errorf("rung %s verdict = %s, want GOOD: the unverified hop must not "+
					"downgrade a route that prices at mid", amount, q.Verdict)
			}
			if !q.LossPct.IsZero() {
				t.Errorf("rung %s loss = %s, want 0: a finding must not add a cost",
					amount, q.LossPct)
			}
			if r.Result.Recommended == nil {
				t.Errorf("rung %s lost its recommendation to a counterparty fact", amount)
			}
		})
	}
}

// TestRungFindingsReachTheWireWithoutChangingTheQuote pins the rendering:
// the rung carries the shared findings shape, a rung with nothing to report
// omits the key entirely, and the quote fields beside it are unchanged.
func TestRungFindingsReachTheWireWithoutChangingTheQuote(t *testing.T) {
	res := ladderFromSnapshot(t, "660")
	out := route.ToCorridorJSON(res, "USD/NGN")

	var withFinding, clean *route.RungJSON
	for i := range out.Rungs {
		switch out.Rungs[i].SendAmount {
		case "10":
			withFinding = &out.Rungs[i]
		case "5":
			clean = &out.Rungs[i]
		}
	}
	if withFinding == nil || clean == nil {
		t.Fatal("test setup: expected rungs at 5 and 10 USDC")
	}

	if withFinding.Findings == nil {
		t.Fatalf("rung 10 USDC lost its per-rung findings on the wire")
	}
	if withFinding.Findings.Failed != 1 {
		t.Errorf("rung 10 USDC wire findings failed = %d, want 1", withFinding.Findings.Failed)
	}
	if clean.Findings != nil {
		t.Errorf("rung 5 USDC carries a findings block for a fully verified path")
	}

	// The headline fields rendered beside the finding are the engine's own.
	if withFinding.Quote == nil {
		t.Fatal("rung 10 USDC lost its quote")
	}
	engineRung := rungAt(t, res, "10")
	if withFinding.Quote.Verdict != engineRung.Result.Quotes[0].Verdict.String() {
		t.Errorf("rendered verdict = %q, engine verdict = %q",
			withFinding.Quote.Verdict, engineRung.Result.Quotes[0].Verdict)
	}
	if withFinding.Quote.LossPct != engineRung.Result.Quotes[0].LossPct.String() {
		t.Errorf("rendered loss = %q, engine loss = %q",
			withFinding.Quote.LossPct, engineRung.Result.Quotes[0].LossPct)
	}

	// The key's absence is structural, not accidental: a client must be able
	// to tell "no unverified issuer" from "not recorded".
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rungs []map[string]json.RawMessage `json:"rungs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var sawFinding, sawAbsent bool
	for _, r := range doc.Rungs {
		_, has := r["findings"]
		switch string(r["send_amount"]) {
		case `"10"`:
			sawFinding = has
		case `"5"`:
			sawAbsent = !has
		}
	}
	if !sawFinding {
		t.Error("rung 10 USDC does not emit a findings key")
	}
	if !sawAbsent {
		t.Error("rung 5 USDC emits a findings key even though nothing was reported")
	}
}
