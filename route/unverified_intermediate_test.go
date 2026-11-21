package route_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
)

// TestRungUnverifiedIntermediatesFromSnapshot drives the per-rung counterparty
// fact from the recorded 2026-08-21 USDC->NGNC paths.
//
// The sizes were chosen because their best paths differ in exactly the way
// this change must distinguish: unregistered issuers at 0.1, 1 and 10, and a
// registered issuer at 5. A corridor-level answer would lose that distinction.
func TestRungUnverifiedIntermediatesFromSnapshot(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")
	e := engineOver(m, "USD/NGN", "1350.2568")

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:    asset.USDC(),
		ReceiveAsset: asset.NGNC(),
		Sizes: []decimal.Decimal{
			decimal.RequireFromString("0.1"),
			decimal.RequireFromString("1"),
			decimal.RequireFromString("5"),
			decimal.RequireFromString("10"),
		},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if len(res.Rungs) != 4 {
		t.Fatalf("got %d rungs, want 4", len(res.Rungs))
	}

	cases := []struct {
		amount       string
		wantFindings bool
		wantCodes    []string
	}{
		// USDC -> Cleanshave -> AQUA -> NGNC
		{"0.1", true, []string{"Cleanshave", "AQUA"}},
		// USDC -> yUSDC -> AQUA -> NGNC
		{"1", true, []string{"yUSDC", "AQUA"}},
		// USDC -> USDZ -> XLM -> NGNC: USDZ is registered.
		{"5", false, nil},
		// USDC -> BTC -> XLM -> NGNC
		{"10", true, []string{"BTC"}},
	}

	wire := route.ToCorridorJSON(res, "USD/NGN")

	for i, tc := range cases {
		t.Run(tc.amount, func(t *testing.T) {
			r := res.Rungs[i]
			if r.SendAmount.String() != tc.amount {
				t.Fatalf("rung %d is %s, want %s (ascending ladder)",
					i, r.SendAmount, tc.amount)
			}
			if !r.Priced() {
				t.Fatalf("rung did not price: %v", r.Err)
			}

			if !tc.wantFindings {
				if r.Findings != nil {
					t.Errorf("rung carries findings on an all-verified path: %+v",
						r.Findings.Checks)
				}
				if wire.Rungs[i].Findings != nil {
					t.Error("wire carries a findings block on an all-verified path")
				}
				return
			}

			if r.Findings == nil {
				t.Fatal("rung has no findings; an unverified intermediate must be reported")
			}

			var found bool
			for _, c := range r.Findings.Checks {
				if c.ID != "hop.issuer-verified" {
					continue
				}
				found = true
				if !c.Determined || c.Passed {
					t.Errorf("Determined=%t Passed=%t, want a determined failure",
						c.Determined, c.Passed)
				}
				for _, code := range tc.wantCodes {
					if !strings.Contains(c.Summary, code) {
						t.Errorf("summary does not name %s: %s", code, c.Summary)
					}
				}
			}
			if !found {
				t.Errorf("rung findings have no hop.issuer-verified result: %+v",
					r.Findings.Checks)
			}

			if wire.Rungs[i].Findings == nil {
				t.Fatal("per-rung findings did not reach the wire")
			}
			if wire.Rungs[i].Findings.Failed != 1 {
				t.Errorf("wire failed count = %d, want 1",
					wire.Rungs[i].Findings.Failed)
			}
			if wire.Rungs[i].Findings.WorstSeverity != "warning" {
				t.Errorf("wire worst severity = %q, want warning",
					wire.Rungs[i].Findings.WorstSeverity)
			}
		})
	}
}

// TestRungFindingsDoNotMoveVerdict is the composition rule applied to the new
// per-rung block: a counterparty fact never changes the headline.
//
// The mid is pinned so the 0.1 USDC rung is GOOD despite routing through two
// unverified issuers. That is the only shape in which a regression — wiring
// the finding into the verdict — is detectable: a rung already graded UNUSABLE
// cannot be downgraded.
func TestRungFindingsDoNotMoveVerdict(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")
	e := engineOver(m, "USD/NGN", "660")

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("0.1")},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if len(res.Rungs) != 1 || !res.Rungs[0].Priced() {
		t.Fatalf("expected one priced rung, got %+v", res.Rungs)
	}

	r := res.Rungs[0]
	q := r.Result.Quotes[0]
	if q.Verdict != route.VerdictGood {
		t.Fatalf("test setup is wrong: verdict is %s, want GOOD for the "+
			"downgrade to be detectable", q.Verdict)
	}
	if r.Findings == nil {
		t.Fatal("test setup is wrong: the 0.1 rung must carry unverified-hop findings")
	}

	clean := route.ToCorridorJSON(res, "USD/NGN")
	if clean.Recommended == nil {
		t.Fatal("test setup is wrong: this corridor must have a recommendation")
	}

	// Replace the real warning with the worst possible block: every check
	// failed, one of them critical. If any of it reaches the verdict, this
	// test fails.
	res.Rungs[0].Findings = failingFindings()
	withFindings := route.ToCorridorJSON(res, "USD/NGN")

	if withFindings.Rungs[0].Findings == nil {
		t.Fatal("wire dropped the injected findings block")
	}
	if withFindings.Rungs[0].Findings.WorstSeverity != "critical" {
		t.Fatalf("test setup is wrong: worst severity is %q, want critical",
			withFindings.Rungs[0].Findings.WorstSeverity)
	}

	if withFindings.Integrity != clean.Integrity {
		t.Errorf("integrity moved from %s to %s once the rung findings failed",
			clean.Integrity, withFindings.Integrity)
	}
	if withFindings.Floor != clean.Floor || withFindings.WorstLoss != clean.WorstLoss {
		t.Error("loss figures changed once the rung findings failed")
	}
	if (withFindings.Recommended == nil) != (clean.Recommended == nil) {
		t.Error("the recommendation changed once the rung findings failed")
	}

	a, b := clean.Rungs[0], withFindings.Rungs[0]
	if a.Quote == nil || b.Quote == nil {
		t.Fatal("the rung lost its quote")
	}
	if a.Quote.Verdict != b.Quote.Verdict {
		t.Errorf("rung verdict moved from %s to %s", a.Quote.Verdict, b.Quote.Verdict)
	}
	if a.Quote.LossPct != b.Quote.LossPct {
		t.Errorf("rung loss moved from %s to %s", a.Quote.LossPct, b.Quote.LossPct)
	}
	if a.Priced != b.Priced {
		t.Error("rung priced state changed")
	}
}
