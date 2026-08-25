package route

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

func testUSDC() asset.Asset { return asset.USDC() }
func testNGNC() asset.Asset { return asset.NGNC() }

func TestCostDecomposeSplitsCorrectly(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> XLM -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("112800.51"),
		EffectiveRate: decimal.RequireFromString("1128.0051"),
		ReferenceMid:  decimal.RequireFromString("1500"),
		LossPct:       decimal.RequireFromString("24.80"),
		LossAmount:    decimal.RequireFromString("37199.49"),
		Verdict:       VerdictUnusable,
	}

	d := Decompose(q, decimal.RequireFromString("1500"))

	if d.TotalLossPct.StringFixed(2) != "24.80" {
		t.Errorf("TotalLossPct = %s, want 24.80", d.TotalLossPct)
	}
	if len(d.Parts) != 4 {
		t.Fatalf("expected 4 cost parts, got %d", len(d.Parts))
	}

	seen := map[CostComponent]bool{}
	for _, p := range d.Parts {
		seen[p.Component] = true
	}
	for _, comp := range []CostComponent{CostFXLoss, CostFees, CostSlippage, CostExpectedFailure} {
		if !seen[comp] {
			t.Errorf("missing cost component: %s", comp)
		}
	}

	fxLoss := d.Parts[0]
	if fxLoss.Component != CostFXLoss {
		t.Errorf("first component = %s, want fx_loss", fxLoss.Component)
	}
	if !fxLoss.Determined {
		t.Error("FX loss should be determined")
	}

	fees := d.Parts[1]
	if fees.Component != CostFees {
		t.Errorf("second component = %s, want fees", fees.Component)
	}
	if fees.Determined {
		t.Error("fees must be undetermined when the network fee and operation count are not known")
	}
	if fees.Reason == "" {
		t.Error("undetermined fees must carry a reason")
	}

	slippage := d.Parts[2]
	if slippage.Determined {
		t.Error("slippage should be undetermined without a size comparison")
	}
	if slippage.Reason == "" {
		t.Error("undetermined slippage must carry a reason")
	}

	failCost := d.Parts[3]
	if failCost.Determined {
		t.Error("expected failure cost must be undetermined")
	}
	if failCost.Reason == "" {
		t.Error("undetermined expected failure cost must carry a reason")
	}
}

func TestCostDecomposeZeroLoss(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.NewFromInt(150000),
		EffectiveRate: decimal.NewFromInt(1500),
		ReferenceMid:  decimal.NewFromInt(1500),
		LossPct:       decimal.Zero,
		LossAmount:    decimal.Zero,
		Verdict:       VerdictGood,
	}

	d := Decompose(q, decimal.NewFromInt(1500))
	if !d.TotalLossPct.IsZero() {
		t.Errorf("TotalLossPct = %s, want zero", d.TotalLossPct)
	}
	if !d.Parts[0].Pct.IsZero() {
		t.Errorf("FX loss pct = %s, want zero at mid", d.Parts[0].Pct)
	}
}

func TestCostComponentsDoNotOverlap(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("75100"),
		EffectiveRate: decimal.RequireFromString("751"),
		ReferenceMid:  decimal.NewFromInt(1000),
		LossPct:       decimal.RequireFromString("24.9"),
		LossAmount:    decimal.RequireFromString("24900"),
		Verdict:       VerdictPoor,
	}

	d := Decompose(q, decimal.NewFromInt(1000))

	sumPct := decimal.Zero
	for _, p := range d.Parts {
		if p.Determined {
			sumPct = sumPct.Add(p.Pct)
		}
	}
	if sumPct.StringFixed(2) != d.TotalLossPct.StringFixed(2) {
		t.Errorf("sum of determined = %s, total = %s", sumPct.StringFixed(2), d.TotalLossPct.StringFixed(2))
	}
}

func TestCostDecomposeReasonsAreNonEmpty(t *testing.T) {
	q := Quote{
		Kind:         KindDEX,
		SendAsset:    testUSDC(),
		SendAmount:   decimal.NewFromInt(100),
		ReceiveAsset: testNGNC(),
		LossPct:      decimal.RequireFromString("50"),
	}

	d := Decompose(q, decimal.NewFromInt(1500))
	for _, p := range d.Parts {
		if !p.Determined && strings.TrimSpace(p.Reason) == "" {
			t.Errorf("component %s is undetermined but has no reason", p.Component)
		}
	}
}

// TestCostNoDeterminedComponentDefaultsToZero pins the project's rule that an
// unavailable quantity is unknown, not a default: every component that is
// genuinely unmeasured must report Determined: false, so that no consumer is
// told a number was established when nothing was observed.
func TestCostNoDeterminedComponentDefaultsToZero(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> XLM -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("112800.51"),
		EffectiveRate: decimal.RequireFromString("1128.0051"),
		ReferenceMid:  decimal.RequireFromString("1500"),
		LossPct:       decimal.RequireFromString("24.80"),
		LossAmount:    decimal.RequireFromString("37199.49"),
		Verdict:       VerdictUnusable,
	}

	// The only component with data to determine it is FX loss, which is
	// computed from the observed effective rate against mid. Every other
	// component has no observation or computation behind it, so each must
	// report Determined: false — a value that was not observed or computed
	// may never be presented as established.
	for _, p := range Decompose(q, decimal.RequireFromString("1500")).Parts {
		switch p.Component {
		case CostFXLoss:
			if !p.Determined {
				t.Error("fx_loss is computed from observed rates and must be determined")
			}
		case CostFees, CostSlippage, CostExpectedFailure:
			if p.Determined {
				t.Errorf(
					"%s must be undetermined: nothing was observed or computed "+
						"that establishes its value", p.Component)
			}
			if strings.TrimSpace(p.Reason) == "" {
				t.Errorf("undetermined %s must name what would determine it", p.Component)
			}
		}
	}
}
