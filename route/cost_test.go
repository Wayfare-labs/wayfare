package route

import (
	"encoding/json"
	"errors"
	"fmt"
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

// TestLadderAttachesDecompositionToPricedRungs pins that the ladder itself
// computes and carries each priced rung's decomposition — the change that
// takes Decompose from a test-only function to the value behind every priced
// rung on the wire. An unpriced rung carries none.
func TestLadderAttachesDecompositionToPricedRungs(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("129000"),
		EffectiveRate: decimal.RequireFromString("1290"),
		ReferenceMid:  decimal.RequireFromString("1350.2568"),
		LossPct:       decimal.RequireFromString("4.46"),
		LossAmount:    decimal.RequireFromString("6025.68"),
		Verdict:       VerdictFair,
	}

	res := &LadderResult{
		ReferenceMid: decimal.RequireFromString("1350.2568"),
		Rungs: []Rung{
			{
				SendAmount: decimal.NewFromInt(100),
				Result: &Result{
					Quotes:    []Quote{q},
					Integrity: IntegrityDirect,
				},
			},
			{
				SendAmount: decimal.NewFromInt(5000),
				Err:        errors.New("transport error"),
			},
		},
	}
	res.summarise()

	priced := res.Rungs[0]
	if len(priced.Decomposition.Parts) == 0 {
		t.Fatal("a priced rung must carry a decomposition after summarise")
	}
	if priced.Decomposition.TotalLossPct.StringFixed(2) != "4.46" {
		t.Errorf("priced rung TotalLossPct = %s, want 4.46",
			priced.Decomposition.TotalLossPct)
	}
	seen := map[CostComponent]bool{}
	for _, p := range priced.Decomposition.Parts {
		seen[p.Component] = true
	}
	for _, comp := range []CostComponent{CostFXLoss, CostFees, CostSlippage, CostExpectedFailure} {
		if !seen[comp] {
			t.Errorf("priced rung decomposition missing component %s", comp)
		}
	}

	if len(res.Rungs[1].Decomposition.Parts) != 0 {
		t.Errorf("an errored rung must not carry a decomposition, got %d parts",
			len(res.Rungs[1].Decomposition.Parts))
	}
}

// TestCostBlockJSONShape is the schema test for the cost block: it pins the
// wire shape and the unknown discipline. A determined component carries its
// amount and pct as decimal strings; an undetermined component carries its
// reason and no number at all — a JSON 0 for an unmeasured component is the
// default-to-zero failure in a new place.
func TestCostBlockJSONShape(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("129000"),
		EffectiveRate: decimal.RequireFromString("1290"),
		ReferenceMid:  decimal.RequireFromString("1350.2568"),
		LossPct:       decimal.RequireFromString("4.46"),
		LossAmount:    decimal.RequireFromString("6025.68"),
		Verdict:       VerdictFair,
	}

	blk := ToCostBlockJSON(Decompose(q, decimal.RequireFromString("1350.2568")))
	if blk == nil {
		t.Fatal("ToCostBlockJSON returned nil for a real decomposition")
	}

	doc := struct {
		Cost *CostBlockJSON `json:"cost,omitempty"`
	}{Cost: blk}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling the cost block: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshaling the cost block: %v", err)
	}
	cost, ok := m["cost"]
	if !ok {
		t.Fatal("the wire carries no top-level cost key")
	}

	parts, total, seenTotal, err := decodeCostBlock(cost)
	if err != nil {
		t.Fatal(err)
	}
	if !seenTotal {
		t.Error("cost block is missing total_loss_pct")
	}
	if total != "4.46" {
		t.Errorf("total_loss_pct = %q, want the decimal string \"4.46\"", total)
	}
	if len(parts) != 4 {
		t.Fatalf("cost block carries %d parts, want 4", len(parts))
	}

	// Every part must carry component and determined.
	for _, p := range parts {
		if componentOf(t, p) == "" {
			t.Error("a cost part is missing its component")
		}
		if _, ok := p["determined"]; !ok {
			t.Errorf("component %q is missing the determined flag", componentOf(t, p))
		}
	}

	// Only fx_loss is determined — it is computed from the observed effective
	// rate against mid. The other three components have no observation or
	// computation behind them, so each must carry a reason and no number:
	// fees, slippage and expected failure are unknown, never zero. Fees in
	// particular used to be reported as a determined zero; #96 was filed
	// against exactly that, and Decompose now reports it undetermined.
	// Only fx_loss is determined and carries amount and pct as strings. The
	// other three carry none, only a reason: fees is unmeasured (#96),
	// slippage needs a comparison across sizes, and expected failure cost
	// needs failure history that does not exist yet.
	if got := componentOf(t, parts[0]); got != string(CostFXLoss) {
		t.Fatalf("parts[0].component = %q, want %q", got, CostFXLoss)
	}
	assertDeterminedDecimalStrings(t, parts[0], "fx_loss")

	if got := componentOf(t, parts[1]); got != string(CostFees) {
		t.Fatalf("parts[1].component = %q, want %q", got, CostFees)
	}
	for _, idx := range []int{1, 2, 3} {
		p := parts[idx]
		if got := componentOf(t, p); got == string(CostFXLoss) {
			t.Fatalf("parts[%d].component = %q, want a non-fx component", idx, got)
		}
		assertUndetermined(t, p)
	}
}

// componentOf reads a cost part's component name off the wire.
func componentOf(t *testing.T, p map[string]json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(p["component"], &s); err != nil {
		t.Fatalf("component is %q, not a string: %v", string(p["component"]), err)
	}
	return s
}

// decodeCostBlock unmarshals a cost block's raw JSON into its shape: a list
// of component objects. It returns whether total_loss_pct was present and its
// value, so the caller can assert on presence independently of value.
func decodeCostBlock(raw json.RawMessage) (parts []map[string]json.RawMessage, total string, hasTotal bool, err error) {
	var wrap struct {
		Parts        []map[string]json.RawMessage `json:"parts"`
		TotalLossPct json.RawMessage              `json:"total_loss_pct"`
	}
	if err = json.Unmarshal(raw, &wrap); err != nil {
		return nil, "", false, fmt.Errorf("unmarshaling the cost block body: %w", err)
	}
	hasTotal = len(wrap.TotalLossPct) > 0 && string(wrap.TotalLossPct) != "null"
	if hasTotal {
		if err = json.Unmarshal(wrap.TotalLossPct, &total); err != nil {
			return nil, "", false, fmt.Errorf("total_loss_pct is not a string: %w", err)
		}
	}
	return wrap.Parts, total, hasTotal, nil
}

func assertDeterminedDecimalStrings(t *testing.T, p map[string]json.RawMessage, name string) {
	t.Helper()
	for _, k := range []string{"amount", "pct"} {
		rv, ok := p[k]
		if !ok {
			t.Errorf("%s: %q carries no %q, but it is determined and must", name, k, k)
			continue
		}
		var s string
		if err := json.Unmarshal(rv, &s); err != nil {
			t.Errorf("%s: %q is %q, not a decimal string", name, k, string(rv))
			continue
		}
		if s == "" {
			t.Errorf("%s: %q is an empty string", name, k)
		}
		if _, err := decimal.NewFromString(s); err != nil {
			t.Errorf("%s: %q is %q, which is not a parseable decimal", name, k, s)
		}
	}
}

func assertUndetermined(t *testing.T, p map[string]json.RawMessage) {
	t.Helper()
	name := componentOf(t, p)
	if _, ok := p["amount"]; ok {
		t.Errorf("%s: undetermined component must not carry an amount", name)
	}
	if _, ok := p["pct"]; ok {
		t.Errorf("%s: undetermined component must not carry a pct", name)
	}
	reason, ok := p["reason"]
	if !ok {
		t.Errorf("%s: undetermined component must carry a reason", name)
	} else {
		var s string
		if err := json.Unmarshal(reason, &s); err != nil {
			t.Errorf("%s: reason is %q, not a string", name, string(reason))
		} else if strings.TrimSpace(s) == "" {
			t.Errorf("%s: reason must be non-empty", name)
		}
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
