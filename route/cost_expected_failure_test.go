package route

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Issue #167: expected failure cost must stay undetermined, and say why.
//
// route/cost.go already reports it that way. These tests stop a future change
// from quietly defaulting it to zero — in the decomposition, on the wire, or
// for one kind of quote only — and from dropping the reason that tells a
// reader the blank means "not yet known" rather than "negligible".

// expectedFailureQuotes are the shapes of quote Decompose can be handed. The
// expected-failure component must not depend on any of them: no quote carries
// failure history, so no quote can make the component determinable.
func expectedFailureQuotes() map[string]Quote {
	return map[string]Quote{
		"dex route graded unusable": {
			Kind:          KindDEX,
			Description:   "USDC -> XLM -> NGNC",
			Source:        "stellar-dex",
			SendAsset:     asset.USDC(),
			SendAmount:    decimal.NewFromInt(100),
			ReceiveAsset:  asset.NGNC(),
			ReceiveAmount: decimal.RequireFromString("112800.51"),
			EffectiveRate: decimal.RequireFromString("1128.0051"),
			ReferenceMid:  decimal.RequireFromString("1500"),
			LossPct:       decimal.RequireFromString("24.80"),
			LossAmount:    decimal.RequireFromString("37199.49"),
			Verdict:       VerdictUnusable,
		},
		"dex route with no loss at all": {
			// A zero-loss quote is the one most likely to tempt a default:
			// "nothing was lost, so failure costs nothing". It does not
			// follow — a route that prices at the mid can still fail.
			Kind:          KindDEX,
			Description:   "USDC -> NGNC",
			Source:        "stellar-dex",
			SendAsset:     asset.USDC(),
			SendAmount:    decimal.NewFromInt(10),
			ReceiveAsset:  asset.NGNC(),
			ReceiveAmount: decimal.RequireFromString("15000"),
			EffectiveRate: decimal.RequireFromString("1500"),
			ReferenceMid:  decimal.RequireFromString("1500"),
			LossPct:       decimal.Zero,
			LossAmount:    decimal.Zero,
			Verdict:       VerdictGood,
		},
		"anchor sep-38 route without an attached quote": {
			Kind:          KindAnchorSEP38,
			Description:   "anchor SEP-38",
			Source:        "anchor",
			SendAsset:     asset.USDC(),
			SendAmount:    decimal.NewFromInt(100),
			ReceiveAsset:  asset.NGNC(),
			ReceiveAmount: decimal.RequireFromString("140000"),
			EffectiveRate: decimal.RequireFromString("1400"),
			ReferenceMid:  decimal.RequireFromString("1500"),
			LossPct:       decimal.RequireFromString("6.67"),
			LossAmount:    decimal.RequireFromString("10000"),
			Verdict:       VerdictFair,
		},
	}
}

// expectedFailurePart returns the single expected-failure component, failing
// the test when it is missing or duplicated.
func expectedFailurePart(t *testing.T, d CostDecomposition) CostPart {
	t.Helper()
	var found []CostPart
	for _, p := range d.Parts {
		if p.Component == CostExpectedFailure {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		t.Fatal("decomposition omits the expected-failure component; an absent " +
			"component reads as a cost nobody thought of, not as an unknown one")
	case 1:
	default:
		t.Fatalf("decomposition carries %d expected-failure components, want exactly one", len(found))
	}
	return found[0]
}

// TestExpectedFailureCostUndeterminedForEveryQuoteKind: whatever the quote,
// the component is undetermined, holds no figure, and gives the named reason.
func TestExpectedFailureCostUndeterminedForEveryQuoteKind(t *testing.T) {
	for name, q := range expectedFailureQuotes() {
		t.Run(name, func(t *testing.T) {
			p := expectedFailurePart(t, Decompose(q, q.ReferenceMid))

			if p.Determined {
				t.Error("expected failure cost reported as determined; estimating it " +
					"needs observed failures, and none have been accumulated")
			}
			if !p.Amount.IsZero() || !p.Pct.IsZero() {
				t.Errorf("undetermined expected failure cost carries amount %s / pct %s; "+
					"an undetermined component holds no figure", p.Amount, p.Pct)
			}
			if p.Reason != ExpectedFailureUndeterminedReason {
				t.Errorf("reason = %q, want %q", p.Reason, ExpectedFailureUndeterminedReason)
			}
		})
	}
}

// TestExpectedFailureReasonSaysWhy pins what the reason must communicate. A
// reason that merely says "unknown" invites the reader to assume the cost is
// small; this one has to name the missing input — failure history.
func TestExpectedFailureReasonSaysWhy(t *testing.T) {
	reason := strings.ToLower(ExpectedFailureUndeterminedReason)
	if strings.TrimSpace(reason) == "" {
		t.Fatal("ExpectedFailureUndeterminedReason is empty")
	}
	if !strings.Contains(reason, "failure history") {
		t.Errorf("reason %q does not name the missing input (failure history)", ExpectedFailureUndeterminedReason)
	}
	for _, claim := range []string{"zero", "negligible", "none expected", "never fails"} {
		if strings.Contains(reason, claim) {
			t.Errorf("reason %q asserts %q; an unknown must not read as a small number", ExpectedFailureUndeterminedReason, claim)
		}
	}
}

// TestExpectedFailureCostCarriesNoNumberOnTheWire: on the wire an
// undetermined component omits amount and pct entirely. A JSON "0" there is
// the default-to-zero failure in a new place, so the keys must be absent, the
// determined flag present and false, and the reason carried through.
func TestExpectedFailureCostCarriesNoNumberOnTheWire(t *testing.T) {
	for name, q := range expectedFailureQuotes() {
		t.Run(name, func(t *testing.T) {
			blk := ToCostBlockJSON(Decompose(q, q.ReferenceMid))
			if blk == nil {
				t.Fatal("ToCostBlockJSON returned nil for a real decomposition")
			}
			raw, err := json.Marshal(blk)
			if err != nil {
				t.Fatalf("marshaling the cost block: %v", err)
			}
			parts, _, _, err := decodeCostBlock(raw)
			if err != nil {
				t.Fatal(err)
			}

			var seen int
			for _, p := range parts {
				if componentOf(t, p) != string(CostExpectedFailure) {
					continue
				}
				seen++
				for _, key := range []string{"amount", "pct"} {
					if v, ok := p[key]; ok {
						t.Errorf("expected_failure carries %s = %s on the wire; an undetermined component has no number", key, v)
					}
				}
				var determined bool
				rawDetermined, ok := p["determined"]
				if !ok {
					t.Fatal("expected_failure is missing the determined flag")
				}
				if err := json.Unmarshal(rawDetermined, &determined); err != nil {
					t.Fatalf("decoding determined: %v", err)
				}
				if determined {
					t.Error("expected_failure is determined on the wire")
				}
				var reason string
				if err := json.Unmarshal(p["reason"], &reason); err != nil {
					t.Fatalf("decoding reason: %v", err)
				}
				if reason != ExpectedFailureUndeterminedReason {
					t.Errorf("wire reason = %q, want %q", reason, ExpectedFailureUndeterminedReason)
				}
			}
			if seen != 1 {
				t.Errorf("wire carries %d expected_failure parts, want exactly one", seen)
			}
		})
	}
}

// TestExpectedFailureCostIsNotFoldedIntoTheTotal: an unknown component must
// not be quietly counted as zero inside a determined figure either. The total
// stays the measured loss, and the fx_loss component (the one determined
// part of a DEX quote) is not reduced to make room for a failure cost.
func TestExpectedFailureCostIsNotFoldedIntoTheTotal(t *testing.T) {
	q := expectedFailureQuotes()["dex route graded unusable"]
	d := Decompose(q, q.ReferenceMid)

	if !d.TotalLossPct.Equal(q.LossPct) {
		t.Errorf("TotalLossPct = %s, want the measured loss %s", d.TotalLossPct, q.LossPct)
	}
	for _, p := range d.Parts {
		if p.Component != CostFXLoss {
			continue
		}
		if !p.Amount.Equal(q.LossAmount) || !p.Pct.Equal(q.LossPct) {
			t.Errorf("fx_loss = %s (%s%%), want the full measured loss %s (%s%%); "+
				"nothing may be carved out of it for an unmeasured component",
				p.Amount, p.Pct, q.LossAmount, q.LossPct)
		}
	}
}
