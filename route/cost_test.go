package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
)

func testUSDC() asset.Asset { return asset.USDC() }
func testNGNC() asset.Asset { return asset.NGNC() }

// TestDecomposeExpectedFailureCostUndetermined pins the one cost component
// that must never acquire a number.
//
// Expected failure cost is a layer 3 quantity: it needs observed failures to
// estimate, and none have been collected. Reporting it as zero would state
// that a corridor never fails, which is a much stronger claim than "we do not
// know" and the opposite of what the data supports.
//
// So it stays undetermined, and it carries a reason saying why — an unexplained
// blank invites a reader to assume the cost is negligible rather than unmeasured.
func TestDecomposeExpectedFailureCostUndetermined(t *testing.T) {
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

	var found bool
	for _, p := range d.Parts {
		if p.Component != CostExpectedFailure {
			continue
		}
		found = true

		if p.Determined {
			t.Error("expected failure cost reported as determined; it needs observed " +
				"failures to estimate, and none have been collected")
		}
		if !p.Amount.IsZero() {
			t.Errorf("undetermined expected failure cost carries amount %s; an "+
				"undetermined component must hold no figure at all", p.Amount)
		}
		if strings.TrimSpace(p.Reason) == "" {
			t.Error("undetermined expected failure cost carries no reason; an " +
				"unexplained blank invites a reader to assume the cost is negligible")
		}
	}
	if !found {
		t.Fatal("decomposition omits the expected-failure component entirely")
	}
}

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
	if len(d.Parts) != 5 {
		t.Fatalf("expected 5 cost parts, got %d", len(d.Parts))
	}

	seen := map[CostComponent]bool{}
	for _, p := range d.Parts {
		seen[p.Component] = true
	}
	for _, comp := range []CostComponent{CostFXLoss, CostNetworkFees, CostAnchorFee, CostSlippage, CostExpectedFailure} {
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

	netFees := d.Parts[1]
	if netFees.Component != CostNetworkFees {
		t.Errorf("second component = %s, want network_fees", netFees.Component)
	}
	if netFees.Determined {
		t.Error("network_fees must be undetermined when the network fee and operation count are not known")
	}
	if netFees.Reason == "" {
		t.Error("undetermined network_fees must carry a reason")
	}

	aFee := d.Parts[2]
	if aFee.Component != CostAnchorFee {
		t.Errorf("third component = %s, want anchor_fee", aFee.Component)
	}
	if aFee.Determined {
		t.Error("anchor_fee must be undetermined when the anchor does not publish a quote server")
	}
	if aFee.Reason == "" {
		t.Error("undetermined anchor_fee must carry a reason")
	}

	slippage := d.Parts[3]
	if slippage.Determined {
		t.Error("slippage should be undetermined without a size comparison")
	}
	if slippage.Reason == "" {
		t.Error("undetermined slippage must carry a reason")
	}

	failCost := d.Parts[4]
	if failCost.Determined {
		t.Error("expected failure cost must be undetermined")
	}
	if failCost.Reason == "" {
		t.Error("undetermined expected failure cost must carry a reason")
	}
}

// TestCostDecomposeZeroLoss covers a route that achieves mid exactly.
//
// Zero loss is a real measurement, not a missing one: the route was priced and
// found to cost nothing against the benchmark. It must therefore report a
// determined zero rather than an undetermined component, which is the
// distinction the rest of this file exists to protect.
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
func TestReconcileComponents(t *testing.T) {
	total := decimal.NewFromInt(100)
	comps := map[string]decimal.Decimal{
		"fx_loss":  decimal.NewFromInt(60),
		"slippage": decimal.NewFromInt(30),
	}
	undetermined := decimal.NewFromInt(10)
	tolerance := decimal.NewFromFloat(0.1)

	err := ReconcileComponents(total, comps, undetermined, tolerance)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Mismatch case
	badComps := map[string]decimal.Decimal{
		"fx_loss": decimal.NewFromInt(50),
	}
	err = ReconcileComponents(total, badComps, undetermined, tolerance)
	if !errors.Is(err, ErrComponentSumMismatch) {
		t.Errorf("expected ErrComponentSumMismatch, got %v", err)
	}
}

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
	for _, comp := range []CostComponent{CostFXLoss, CostNetworkFees, CostAnchorFee, CostSlippage, CostExpectedFailure} {
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
	if len(parts) != 5 {
		t.Fatalf("cost block carries %d parts, want 5", len(parts))
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
	// rate against mid. The other four components have no observation or
	// computation behind them, so each must carry a reason and no number:
	// network_fees, anchor_fee, slippage and expected failure are unknown,
	// never zero. Fees in particular used to be reported as a determined
	// zero; #96 was filed against exactly that, and Decompose now reports
	// it undetermined as network_fees. Anchor fees are a separate component
	// from network fees (#169): an anchor's own charge is obtainable via
	// SEP-38 when one publishes ANCHOR_QUOTE_SERVER; the absence is a fact
	// about the anchor.
	// The determined component carries amount and pct as strings; the
	// undetermined ones carry none, only a reason.
	if got := componentOf(t, parts[0]); got != string(CostFXLoss) {
		t.Fatalf("parts[0].component = %q, want %q", got, CostFXLoss)
	}
	assertDeterminedDecimalStrings(t, parts[0], "fx_loss")

	for _, idx := range []int{1, 2, 3, 4} {
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

// TestAnchorFeeSeparateFromNetworkFees pins issue #169: anchor fees and
// network fees are separate components in the cost decomposition. For a DEX
// route (which has no anchor involved), both are undetermined, but for
// different reasons: network fees are unmeasured because the operation count
// and base fee are not available; anchor fees are absent because the route
// does not go through an anchor at all. When a route does go through an
// anchor that publishes ANCHOR_QUOTE_SERVER, the anchor fee can be priced
// via SEP-38; when it does not, the absence is a fact about the anchor.
func TestAnchorFeeSeparateFromNetworkFees(t *testing.T) {
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

	var netFees, anchorFee *CostPart
	for i := range d.Parts {
		switch d.Parts[i].Component {
		case CostNetworkFees:
			netFees = &d.Parts[i]
		case CostAnchorFee:
			anchorFee = &d.Parts[i]
		}
	}

	if netFees == nil {
		t.Fatal("decomposition is missing network_fees component")
	}
	if anchorFee == nil {
		t.Fatal("decomposition is missing anchor_fee component")
	}

	// Network fees are undetermined: the operation count and base fee are not
	// available to Decompose.
	if netFees.Determined {
		t.Error("network_fees must be undetermined: the path operation count and Stellar base fee are not available")
	}
	if netFees.Reason == "" {
		t.Error("undetermined network_fees must carry a reason")
	} else if !strings.Contains(strings.ToLower(netFees.Reason), "network") {
		t.Errorf("network_fees reason should mention network fee, got: %s", netFees.Reason)
	}

	// Anchor fee is undetermined for a DEX route: there is no anchor involved,
	// and the anchor's own fee is not part of on-chain DEX pricing.
	if anchorFee.Determined {
		t.Error("anchor_fee must be undetermined for a DEX route: no anchor is involved")
	}
	if anchorFee.Reason == "" {
		t.Error("undetermined anchor_fee must carry a reason")
	}
	// The reason should mention ANCHOR_QUOTE_SERVER to make the absence
	// actionable: a reader can tell whether the anchor could be priced.
	if !strings.Contains(anchorFee.Reason, "ANCHOR_QUOTE_SERVER") {
		t.Errorf("anchor_fee reason should mention ANCHOR_QUOTE_SERVER to make the absence actionable, got: %s", anchorFee.Reason)
	}

	// The two components are distinct — neither is a duplicate or alias.
	if netFees.Component == anchorFee.Component {
		t.Error("network_fees and anchor_fee must be distinct components")
	}
}

// ---------------------------------------------------------------------------
// Ladder-level slippage (issue #97 / backlog #79)
// ---------------------------------------------------------------------------
//
// Slippage is the excess loss a size added over the corridor's structural
// floor, and a floor only exists across sizes — so it is determined at the
// ladder level, where the neighbours are measured, not by Decompose on a
// lone quote. These tests pin the ladder-level contract: determined for
// every priced rung when at least two priced, a determined zero at the floor
// rung, undetermined with the real reason below that, and no interpolation
// between rungs.

// slippageRung builds a priced rung whose quote figures are internally
// consistent with a mid of 1000: LossAmount is what send*mid minus the
// receive amount leaves behind, and LossPct is that amount against send*mid.
// decomposeSlippage reads only LossPct and LossAmount, but a fixture that
// could not have been produced by the engine is a fixture that lies.
func slippageRung(size, lossPct, lossAmount string) Rung {
	send := decimal.RequireFromString(size)
	pct := decimal.RequireFromString(lossPct)
	amt := decimal.RequireFromString(lossAmount)
	mid := decimal.NewFromInt(1000)
	receive := send.Mul(mid).Sub(amt)
	return Rung{
		SendAmount: send,
		Result: &Result{
			Quotes: []Quote{
				{
					Kind:          KindDEX,
					Description:   "USDC -> NGNC",
					Source:        "stellar-dex",
					SendAsset:     testUSDC(),
					SendAmount:    send,
					ReceiveAsset:  testNGNC(),
					ReceiveAmount: receive,
					EffectiveRate: receive.Div(send),
					ReferenceMid:  mid,
					LossPct:       pct,
					LossAmount:    amt,
					Verdict:       verdictFor(pct),
				},
			},
			Integrity: IntegrityDirect,
		},
	}
}

// foundryLadder is the README's founding measurement as a ladder: 25.02%
// loss at 0.1 USDC — the structural floor, price impact negligible — and
// 97.68% at 5000, which is that floor plus slippage. A 100 rung at 30.30%
// sits between them, and a 25 rung failed to reach an upstream, so the
// curve has a hole that no figure may be invented for.
func foundryLadder() *LadderResult {
	return &LadderResult{
		ReferenceMid: decimal.NewFromInt(1000),
		Rungs: []Rung{
			slippageRung("0.1", "25.02", "25.02"),
			{SendAmount: decimal.NewFromInt(25), Err: errors.New("transport error")},
			slippageRung("100", "30.30", "30300"),
			slippageRung("5000", "97.68", "4884000"),
		},
	}
}

// TestLadderSlippageDeterminedAcrossPricedRungs pins the headline
// requirement: when at least two rungs priced, every priced rung's
// slippage is determined, computed as the excess over the floor at the
// smallest priced size.
func TestLadderSlippageDeterminedAcrossPricedRungs(t *testing.T) {
	res := foundryLadder()
	res.summarise()

	for _, i := range []int{0, 2, 3} {
		p := findPart(&res.Rungs[i].Decomposition, CostSlippage)
		if p == nil {
			t.Fatalf("rung %s carries no slippage part", res.Rungs[i].SendAmount)
		}
		if !p.Determined {
			t.Errorf("rung %s: slippage must be determined when at least two rungs priced", res.Rungs[i].SendAmount)
		}
	}

	// The floor is the loss at the smallest priced size.
	if res.Floor.StringFixed(2) != "25.02" || res.FloorSize.String() != "0.1" {
		t.Errorf("floor = %s%% at %s, want 25.02%% at 0.1", res.Floor, res.FloorSize)
	}

	// fx_loss is re-scoped to the floor on every priced rung, so the
	// determined components partition the total instead of overlapping it.
	for _, i := range []int{0, 2, 3} {
		fx := findPart(&res.Rungs[i].Decomposition, CostFXLoss)
		if fx == nil {
			t.Fatalf("rung %s carries no fx_loss part", res.Rungs[i].SendAmount)
		}
		if fx.Pct.StringFixed(2) != "25.02" {
			t.Errorf("rung %s: fx_loss = %s%%, want the structural floor 25.02%%",
				res.Rungs[i].SendAmount, fx.Pct)
		}
	}

	// Slippage is the excess over the floor, per measured size.
	want := map[int][2]string{
		2: {"5.28", "30274.98"},
		3: {"72.66", "4883974.98"},
	}
	for i, w := range want {
		p := findPart(&res.Rungs[i].Decomposition, CostSlippage)
		if p.Pct.String() != w[0] {
			t.Errorf("rung %s: slippage pct = %s, want %s",
				res.Rungs[i].SendAmount, p.Pct, w[0])
		}
		if p.Amount.String() != w[1] {
			t.Errorf("rung %s: slippage amount = %s, want %s",
				res.Rungs[i].SendAmount, p.Amount, w[1])
		}
	}

	// The errored rung learned nothing and carries nothing.
	if len(res.Rungs[1].Decomposition.Parts) != 0 {
		t.Errorf("an errored rung must not carry a decomposition, got %d parts",
			len(res.Rungs[1].Decomposition.Parts))
	}
}

// TestFloorRungSlippageIsDeterminedZero pins the distinction the issue
// exists for: the floor rung's slippage is a measured zero — the size was
// priced and at the floor there is nothing beneath it to compare against —
// never an undetermined blank, and never confused with one.
func TestFloorRungSlippageIsDeterminedZero(t *testing.T) {
	res := foundryLadder()
	res.summarise()

	p := findPart(&res.Rungs[0].Decomposition, CostSlippage)
	if p == nil {
		t.Fatal("the floor rung carries no slippage part")
	}
	if !p.Determined {
		t.Error("the floor rung's slippage is a determined zero: the size was " +
			"measured, and at the floor there is by definition no excess loss")
	}
	if !p.Amount.IsZero() || !p.Pct.IsZero() {
		t.Errorf("the floor rung's slippage must be zero, got amount %s pct %s",
			p.Amount, p.Pct)
	}
	if strings.TrimSpace(p.Reason) == "" {
		t.Error("the determined zero must say it is the floor, so a reader can " +
			"tell it apart from an unmeasured component at a glance")
	}
}

// TestSlippageUndeterminedWithFewerThanTwoPricedRungs pins the other side
// of the distinction: one priced rung leaves nothing to compare, and the
// undetermined figure must carry a reason naming the real shortfall — how
// many sizes priced and how many were needed — not a generic placeholder.
func TestSlippageUndeterminedWithFewerThanTwoPricedRungs(t *testing.T) {
	res := &LadderResult{
		ReferenceMid: decimal.NewFromInt(1000),
		Rungs: []Rung{
			slippageRung("0.1", "25.02", "25.02"),
			{SendAmount: decimal.NewFromInt(5000), Err: errors.New("transport error")},
		},
	}
	res.summarise()

	p := findPart(&res.Rungs[0].Decomposition, CostSlippage)
	if p == nil {
		t.Fatal("the single priced rung carries no slippage part")
	}
	if p.Determined {
		t.Error("slippage must be undetermined when only one rung priced: " +
			"there is no second size to compare against")
	}
	if !strings.Contains(p.Reason, "1 of 2") {
		t.Errorf("reason must name the shortfall (1 of 2 priced), got: %s", p.Reason)
	}
	if !strings.Contains(p.Reason, "at least two") {
		t.Errorf("reason must say what would determine it, got: %s", p.Reason)
	}

	// With nothing priced at all there is no decomposition to patch.
	empty := &LadderResult{
		Rungs: []Rung{
			{SendAmount: decimal.NewFromInt(1), Err: errors.New("boom")},
			{SendAmount: decimal.NewFromInt(5000), Err: errors.New("boom")},
		},
	}
	empty.summarise()
	for i := range empty.Rungs {
		if len(empty.Rungs[i].Decomposition.Parts) != 0 {
			t.Errorf("an unmeasured rung must carry no decomposition, got %d parts",
				len(empty.Rungs[i].Decomposition.Parts))
		}
	}
}

// TestSlippageUsesOnlyMeasuredSizes pins the no-interpolation rule:
// slippage at a size is its loss minus the floor's loss — both measured —
// and never a difference against the previous priced rung, a curve fitted
// between rungs, or a figure for a rung that did not price. The ladder has
// holes at 25 (transport error); nothing may be invented to fill them.
func TestSlippageUsesOnlyMeasuredSizes(t *testing.T) {
	res := &LadderResult{
		ReferenceMid: decimal.NewFromInt(1000),
		Rungs: []Rung{
			slippageRung("0.1", "25.02", "25.02"),
			slippageRung("100", "30.30", "30300"),
			{SendAmount: decimal.NewFromInt(2500), Result: &Result{Integrity: IntegrityNoMarket}},
			slippageRung("5000", "97.68", "4884000"),
		},
	}
	res.summarise()

	p := findPart(&res.Rungs[3].Decomposition, CostSlippage)
	if p == nil {
		t.Fatal("the 5000 rung carries no slippage part")
	}
	// Measured against the floor (97.68 - 25.02 = 72.66), not against the
	// previous priced rung (97.68 - 30.30 = 67.38), and not interpolated
	// across the NO-MARKET hole at 2500.
	if !p.Pct.Equal(decimal.RequireFromString("97.68").Sub(decimal.RequireFromString("25.02"))) {
		t.Errorf("slippage pct = %s, want the exact excess over the floor 72.66", p.Pct)
	}
	if p.Pct.String() == "67.38" {
		t.Error("slippage must be measured against the floor, not the previous priced rung")
	}

	// The NO-MARKET rung is measured but unpriced: it contributes no
	// figure and carries no decomposition.
	if len(res.Rungs[2].Decomposition.Parts) != 0 {
		t.Errorf("an unpriced rung must carry no decomposition, got %d parts",
			len(res.Rungs[2].Decomposition.Parts))
	}
}

// TestSlippageReconcilesWithPublishedFloor pins the acceptance criterion
// the wire must satisfy: for every priced rung, the published floor plus
// the published slippage equals the rung's published total loss within the
// stated tolerance. The tolerance is zero in exact arithmetic — both
// components are differences of the same two measured decimals — so it is
// stated as 1e-7 only to survive a future consumer's string rounding.
func TestSlippageReconcilesWithPublishedFloor(t *testing.T) {
	const tolerance = "0.0000001"
	tol := decimal.RequireFromString(tolerance)

	res := foundryLadder()
	res.summarise()
	doc := ToCorridorJSON(res, "USD/NGN")

	floorPct := decimal.RequireFromString(doc.Floor)
	for _, rj := range doc.Rungs {
		if !rj.Priced || rj.Cost == nil {
			continue
		}
		var slip, total decimal.Decimal
		for _, p := range rj.Cost.Parts {
			switch p.Component {
			case string(CostSlippage):
				if !p.Determined {
					t.Errorf("rung %s: slippage must be determined on the wire when "+
						"at least two rungs priced", rj.SendAmount)
					continue
				}
				var err error
				slip, err = decimal.NewFromString(p.Pct)
				if err != nil {
					t.Fatalf("rung %s: slippage pct %q is not a decimal: %v",
						rj.SendAmount, p.Pct, err)
				}
			}
		}
		var err error
		total, err = decimal.NewFromString(rj.Cost.TotalLossPct)
		if err != nil {
			t.Fatalf("rung %s: total_loss_pct %q is not a decimal: %v",
				rj.SendAmount, rj.Cost.TotalLossPct, err)
		}
		if diff := floorPct.Add(slip).Sub(total).Abs(); diff.GreaterThan(tol) {
			t.Errorf("rung %s: floor %s + slippage %s = %s, total loss %s "+
				"(differs by %s, tolerance %s)",
				rj.SendAmount, floorPct, slip, floorPct.Add(slip), total, diff, tolerance)
		}
	}
}

// TestCostNoDeterminedComponentDefaultsToZero pins the project's rule that an
// unavailable quantity is unknown, not a default: every component that is
// genuinely unmeasured must report Determined: false, so that no consumer is
// told a number was established when nothing was observed.
func TestSweepCostClassBounding(t *testing.T) {
	if got := SweepCostClass(1); got != checks.CostOneRequest {
		Errorf(t, "SweepCostClass(1) = %v, want CostOneRequest", got)
	}
	if got := SweepCostClass(5); got != checks.CostExpensive {
		Errorf(t, "SweepCostClass(5) = %v, want CostExpensive", got)
	}
}

func Errorf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
}

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
		case CostNetworkFees, CostAnchorFee, CostSlippage, CostExpectedFailure:
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

func TestReconcileComponents_ExactMatch(t *testing.T) {
	total := decimal.NewFromFloat(10.50)
	comps := map[string]decimal.Decimal{
		"fee":     decimal.NewFromFloat(0.50),
		"fx_loss": decimal.NewFromFloat(10.00),
	}
	tolerance := decimal.NewFromFloat(0.001)
	undetermined := decimal.NewFromFloat(0)

	err := ReconcileComponents(total, comps, undetermined, tolerance)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestReconcileComponents_WithinTolerance(t *testing.T) {
	total := decimal.NewFromFloat(10.50)
	comps := map[string]decimal.Decimal{
		"fee":     decimal.NewFromFloat(0.501),
		"fx_loss": decimal.NewFromFloat(10.00),
	}
	tolerance := decimal.NewFromFloat(0.01)
	undetermined := decimal.NewFromFloat(0)

	err := ReconcileComponents(total, comps, undetermined, tolerance)
	if err != nil {
		t.Fatalf(
			"expected nil error within tolerance, got %v",
			err,
		)
	}
}

func TestReconcileComponents_CoveredByUndetermined(t *testing.T) {
	total := decimal.NewFromFloat(15.00)
	comps := map[string]decimal.Decimal{
		"fee": decimal.NewFromFloat(5.00),
	}
	undetermined := decimal.NewFromFloat(10.00)
	tolerance := decimal.NewFromFloat(0.001)

	err := ReconcileComponents(total, comps, undetermined, tolerance)
	if err != nil {
		t.Fatalf(
			"expected undetermined shortfall to reconcile, got %v",
			err,
		)
	}
}

func TestReconcileComponents_MismatchError(t *testing.T) {
	total := decimal.NewFromFloat(20.00)
	comps := map[string]decimal.Decimal{
		"fee": decimal.NewFromFloat(5.00),
	}
	undetermined := decimal.NewFromFloat(2.00)
	tolerance := decimal.NewFromFloat(0.001)

	err := ReconcileComponents(total, comps, undetermined, tolerance)
	if err != ErrComponentSumMismatch {
		t.Fatalf(
			"expected ErrComponentSumMismatch, got %v",
			err,
		)
	}
}
