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

	d := Decompose(q, decimal.RequireFromString("1500"), UnmeasuredSlippage(
		"only 1 of 12 sizes priced; measuring slippage requires at least two priced sizes, "+
			"so the structural floor cannot be separated from the depth effect"))

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
	if !strings.Contains(slippage.Reason, "two priced sizes") {
		t.Errorf("undetermined slippage must name the shortfall, got %q", slippage.Reason)
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

	// The zero-loss quote here is the floor rung of a measured ladder, so
	// its slippage is a determined zero: the measurement is zero, not a gap.
	d := Decompose(q, decimal.NewFromInt(1500), floorSlippage())
	if !d.TotalLossPct.IsZero() {
		t.Errorf("TotalLossPct = %s, want zero", d.TotalLossPct)
	}
	if !d.Parts[0].Pct.IsZero() {
		t.Errorf("FX loss pct = %s, want zero at mid", d.Parts[0].Pct)
	}
	slippage := d.Parts[2]
	if !slippage.Determined {
		t.Error("the floor rung's slippage is a determined zero, not an undetermined one")
	}
	if !slippage.Pct.IsZero() || !slippage.Amount.IsZero() {
		t.Errorf("floor rung slippage = %s%% / %s, want zero", slippage.Pct, slippage.Amount)
	}
	if slippage.Reason != "" {
		t.Errorf("a determined component must not carry an undetermined reason, got %q", slippage.Reason)
	}
}

// TestCostComponentsDoNotOverlap pins the undetermined case: with slippage
// unmeasured, nothing has attributed a share of the loss, so fx_loss carries
// all of it and the determined parts still sum to the total. The measured
// case — where slippage takes its share out of fx_loss — is pinned by
// TestCostComponentsReconcileWithMeasuredSlippage.
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

	d := Decompose(q, decimal.NewFromInt(1000), UnmeasuredSlippage(
		"only 1 of 12 sizes priced; measuring slippage requires at least two priced sizes, "+
			"so the structural floor cannot be separated from the depth effect"))

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
//
// With only one priced rung there is no second size to compare against, so
// that rung's slippage must arrive undetermined with the shortfall named —
// the distinction the ladder-level decomposition exists to draw.
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

	slippage := priced.Decomposition.Parts[2]
	if slippage.Component != CostSlippage || slippage.Determined {
		t.Fatalf("single priced rung's slippage = determined %v, want undetermined: "+
			"one size cannot be compared against another", slippage.Determined)
	}
	if !strings.Contains(slippage.Reason, "only 1 of 2 sizes priced") {
		t.Errorf("undetermined slippage must name the shortfall, got %q", slippage.Reason)
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

	blk := ToCostBlockJSON(Decompose(q, decimal.RequireFromString("1350.2568"), UnmeasuredSlippage(
		"only 1 of 12 sizes priced; measuring slippage requires at least two priced sizes, "+
			"so the structural floor cannot be separated from the depth effect")))
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
	// computation behind them here, so each must carry a reason and no
	// number: fees, slippage and expected failure are unknown, never zero.
	// Fees in particular used to be reported as a determined zero; #96 was
	// filed against exactly that, and Decompose now reports it undetermined.
	// Only fx_loss is determined and carries amount and pct as strings. The
	// other three carry none, only a reason: fees is unmeasured (#96),
	// slippage needs a comparison across at least two priced sizes and this
	// decomposition saw one, and expected failure cost needs failure history
	// that does not exist yet. The determined-slippage shape is pinned
	// separately by TestCostBlockJSONDeterminedSlippage.
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

	d := Decompose(q, decimal.NewFromInt(1500), UnmeasuredSlippage(
		"only 1 of 12 sizes priced; measuring slippage requires at least two priced sizes, "+
			"so the structural floor cannot be separated from the depth effect"))
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
	for _, p := range Decompose(q, decimal.RequireFromString("1500"), UnmeasuredSlippage(
		"only 1 of 12 sizes priced; measuring slippage requires at least two priced sizes, "+
			"so the structural floor cannot be separated from the depth effect")).Parts {
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

// rungAt builds a priced rung at one size with the given loss figures, in the
// shape summarise and DecomposeLadder consume. lossAmount must be consistent
// with lossPct against the fixture's mid — send×mid×pct/100 — so the
// slippage arithmetic under test is exercised on coherent inputs.
func rungAt(size, lossPct, lossAmount string) Rung {
	return Rung{
		SendAmount: decimal.RequireFromString(size),
		Result: &Result{
			Quotes: []Quote{{
				Kind:         KindDEX,
				SendAsset:    testUSDC(),
				SendAmount:   decimal.RequireFromString(size),
				ReceiveAsset: testNGNC(),
				LossPct:      decimal.RequireFromString(lossPct),
				LossAmount:   decimal.RequireFromString(lossAmount),
			}},
			Integrity: IntegrityDirect,
		},
	}
}

// noMarketRung builds a rung Horizon answered with "there is no path": a
// finding about the corridor, and not a priced size.
func noMarketRung(size string) Rung {
	return Rung{
		SendAmount: decimal.RequireFromString(size),
		Result:     &Result{Integrity: IntegrityNoMarket},
	}
}

// slippagePartOf fishes the slippage component out of a decomposition.
func slippagePartOf(t *testing.T, d CostDecomposition) CostPart {
	t.Helper()
	for _, p := range d.Parts {
		if p.Component == CostSlippage {
			return p
		}
	}
	t.Fatal("decomposition carries no slippage component")
	return CostPart{}
}

// reconcile asserts the published figures hold together: floor plus slippage
// matches the rung's total loss. The arithmetic is exact decimal throughout,
// so the tolerance is tight — but the comparison is written as a tolerance
// rather than an equality because the contract being pinned is about
// published figures agreeing, not about bit-identical decimals.
func reconcile(t *testing.T, floor, slippagePct, totalLossPct decimal.Decimal) {
	t.Helper()
	const tol = "0.000000001"
	gap := floor.Add(slippagePct).Sub(totalLossPct).Abs()
	if gap.GreaterThanOrEqual(decimal.RequireFromString(tol)) {
		t.Errorf("floor %s + slippage %s = %s, want the rung's total loss %s "+
			"within %s", floor, slippagePct, floor.Add(slippagePct), totalLossPct, tol)
	}
}

// TestLadderSlippageMeasuredAcrossPricedRungs is the acceptance test for the
// split the project was founded on. The ladder prices two sizes; the floor is
// the loss at the smaller one, where price impact is negligible, and the
// larger rung's slippage is what its size added on top. This is the README's
// founding measurement — 25% at 0.1 USDC is the structural floor, 97.68% at
// 5000 is the floor plus slippage — computed as a number instead of argued in
// prose.
func TestLadderSlippageMeasuredAcrossPricedRungs(t *testing.T) {
	const mid = "1000"
	res := &LadderResult{
		ReferenceMid: decimal.RequireFromString(mid),
		Rungs: []Rung{
			// Fair value 100×1000 = 100,000; delivered 95,400; loss 4.6%.
			rungAt("100", "4.6", "4600"),
			// Fair value 5000×1000 = 5,000,000; delivered 116,000; loss 97.68%.
			rungAt("5000", "97.68", "4884000"),
		},
	}
	res.summarise()

	if res.Floor.StringFixed(2) != "4.60" || res.FloorSize.String() != "100" {
		t.Fatalf("floor = %s%% at %s, want 4.60%% at 100: the smallest priced size is the floor",
			res.Floor, res.FloorSize)
	}

	floorSlip := slippagePartOf(t, res.Rungs[0].Decomposition)
	if !floorSlip.Determined {
		t.Fatal("the floor rung's slippage must be determined: zero is what it measures, by definition")
	}
	if !floorSlip.Pct.IsZero() || !floorSlip.Amount.IsZero() {
		t.Errorf("floor rung slippage = %s%% / %s, want a determined zero",
			floorSlip.Pct, floorSlip.Amount)
	}
	if floorSlip.Reason != "" {
		t.Errorf("a determined component must not carry an undetermined reason, got %q", floorSlip.Reason)
	}

	deepSlip := slippagePartOf(t, res.Rungs[1].Decomposition)
	if !deepSlip.Determined {
		t.Fatal("slippage must be determined for every priced rung once two rungs priced")
	}
	// Excess loss over the floor: 97.68 − 4.6 = 93.08 points; in receive
	// units, 4,884,000 − (5,000,000 × 4.6%) = 4,654,000.
	if deepSlip.Pct.StringFixed(2) != "93.08" {
		t.Errorf("deep rung slippage pct = %s, want 93.08", deepSlip.Pct)
	}
	if deepSlip.Amount.StringFixed(2) != "4654000.00" {
		t.Errorf("deep rung slippage amount = %s, want 4654000.00", deepSlip.Amount)
	}

	// The published figures reconcile: floor + slippage is the rung's total
	// loss, on the deep rung and on the floor rung alike.
	reconcile(t, res.Floor, deepSlip.Pct, res.Rungs[1].Decomposition.TotalLossPct)
	reconcile(t, res.Floor, floorSlip.Pct, res.Rungs[0].Decomposition.TotalLossPct)

	// And the parts themselves partition the total: fx carries the floor,
	// slippage the excess, and nothing overlaps.
	fx := res.Rungs[1].Decomposition.Parts[0]
	if fx.Pct.StringFixed(2) != "4.60" || fx.Amount.StringFixed(2) != "230000.00" {
		t.Errorf("deep rung fx = %s%% / %s, want the floor 4.60%% / 230000.00", fx.Pct, fx.Amount)
	}
	sum := fx.Pct.Add(deepSlip.Pct)
	if sum.StringFixed(2) != res.Rungs[1].Decomposition.TotalLossPct.StringFixed(2) {
		t.Errorf("fx %s + slippage %s = %s, want total %s",
			fx.Pct, deepSlip.Pct, sum, res.Rungs[1].Decomposition.TotalLossPct)
	}
}

// TestLadderSlippageUndeterminedNamesShortfall pins the sub-two-rungs case:
// with a single priced size there is no neighbour to compare against, and
// slippage must stay undetermined with a reason that names the shortfall —
// how many sizes priced, and what was missing. NO-MARKET and errored rungs
// are not priced sizes and must not be counted as comparison points.
func TestLadderSlippageUndeterminedNamesShortfall(t *testing.T) {
	res := &LadderResult{
		ReferenceMid: decimal.NewFromInt(1000),
		Rungs: []Rung{
			noMarketRung("0.1"),
			{SendAmount: decimal.NewFromInt(1), Err: errors.New("transport error")},
			rungAt("10", "25.5", "2550"),
		},
	}
	res.summarise()

	slip := slippagePartOf(t, res.Rungs[2].Decomposition)
	if slip.Determined {
		t.Fatal("slippage must be undetermined with one priced size: a floor needs a second size to be a floor")
	}
	for _, want := range []string{"only 1 of 3 sizes priced", "two priced sizes"} {
		if !strings.Contains(slip.Reason, want) {
			t.Errorf("slippage reason %q does not name the shortfall (missing %q)", slip.Reason, want)
		}
	}
	if !slip.Pct.IsZero() || !slip.Amount.IsZero() {
		t.Errorf("undetermined slippage carries figures %s%% / %s; it must carry only its reason",
			slip.Pct, slip.Amount)
	}
}

// TestLadderSlippageNegativeWhenRungBeatsFloor pins that a measured excess
// is reported as measured, even when it is negative. A larger size can open
// a better path than the floor rung; the negative slippage is the finding,
// and clamping it to zero would erase the one curve the ladder measured.
func TestLadderSlippageNegativeWhenRungBeatsFloor(t *testing.T) {
	const mid = "1000"
	res := &LadderResult{
		ReferenceMid: decimal.RequireFromString(mid),
		Rungs: []Rung{
			// Fair value 1×1000 = 1,000; delivered 500; loss 50%.
			rungAt("1", "50", "500"),
			// Fair value 100×1000 = 100,000; delivered 98,000; loss 2% —
			// the larger size beats the floor rung.
			rungAt("100", "2", "2000"),
		},
	}
	res.summarise()

	if res.Floor.StringFixed(2) != "50.00" {
		t.Fatalf("floor = %s, want 50.00 at the smallest priced size", res.Floor)
	}
	slip := slippagePartOf(t, res.Rungs[1].Decomposition)
	if !slip.Determined {
		t.Fatal("slippage must be determined for every priced rung once two rungs priced")
	}
	if !slip.Pct.IsNegative() {
		t.Errorf("slippage = %s, want −48 as measured: the size beat the floor and must not be clamped", slip.Pct)
	}
	if slip.Pct.StringFixed(2) != "-48.00" {
		t.Errorf("slippage pct = %s, want −48.00", slip.Pct)
	}

	// Reconciliation survives the sign: 50 + (−48) = 2, the rung's loss.
	reconcile(t, res.Floor, slip.Pct, res.Rungs[1].Decomposition.TotalLossPct)
	fx := res.Rungs[1].Decomposition.Parts[0]
	if fx.Pct.StringFixed(2) != "50.00" {
		t.Errorf("fx = %s%%, want the floor 50.00%%", fx.Pct)
	}
}

// TestCostBlockJSONDeterminedSlippage pins the wire shape for a measured
// slippage component: it carries amount and pct as decimal strings and no
// reason, while fees and expected failure stay undetermined with theirs. A
// determined zero would be a lie here only if nothing was measured; the
// floor rung measured a zero, and the wire must say determined.
func TestCostBlockJSONDeterminedSlippage(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(5000),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("116000"),
		EffectiveRate: decimal.RequireFromString("23.2"),
		ReferenceMid:  decimal.NewFromInt(1000),
		LossPct:       decimal.RequireFromString("97.68"),
		LossAmount:    decimal.RequireFromString("4884000"),
		Verdict:       VerdictUnusable,
	}
	slip := measuredSlippage(q, decimal.RequireFromString("4.6"), decimal.NewFromInt(1000))
	blk := ToCostBlockJSON(Decompose(q, decimal.NewFromInt(1000), slip))
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
	if len(parts) != 4 {
		t.Fatalf("cost block carries %d parts, want 4", len(parts))
	}

	if got := componentOf(t, parts[2]); got != string(CostSlippage) {
		t.Fatalf("parts[2].component = %q, want %q", got, CostSlippage)
	}
	var slippage struct {
		Amount     string `json:"amount"`
		Pct        string `json:"pct"`
		Determined bool   `json:"determined"`
		Reason     string `json:"reason"`
	}
	part, err := json.Marshal(parts[2])
	if err != nil {
		t.Fatalf("re-marshaling the slippage part: %v", err)
	}
	if err := json.Unmarshal(part, &slippage); err != nil {
		t.Fatalf("unmarshaling the slippage part: %v", err)
	}
	if !slippage.Determined {
		t.Error("measured slippage must be reported as determined")
	}
	if slippage.Pct != "93.08" {
		t.Errorf("slippage pct = %q, want the decimal string \"93.08\"", slippage.Pct)
	}
	if slippage.Amount != "4654000" {
		t.Errorf("slippage amount = %q, want the decimal string \"4654000\"", slippage.Amount)
	}
	if slippage.Reason != "" {
		t.Errorf("determined slippage must carry no reason, got %q", slippage.Reason)
	}

	// Fees and expected failure remain undetermined with their reasons.
	for _, idx := range []int{1, 3} {
		if got := componentOf(t, parts[idx]); got == string(CostSlippage) || got == string(CostFXLoss) {
			t.Fatalf("parts[%d].component = %q, want fees or expected_failure", idx, got)
		}
		assertUndetermined(t, parts[idx])
	}
}
