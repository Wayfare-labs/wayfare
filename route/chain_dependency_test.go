package route

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// The dependency-chain tests replay scenario fixtures through
// snapshot.Replayer, like every other recorded test in this package. The
// cases they pin — a dependency that is itself derivative, a cycle, a
// dependency with no market at the tested size — do not occur on the
// recorded mainnet set, so each fixture under testdata/chain-snapshots is
// a scenario captured once from a synthetic upstream through the standard
// snapshot.Recorder (see the note in each manifest). They live in their own
// directory rather than testdata/snapshots so corridor tools that walk that
// directory (cmd/hop-analysis, the recorded-integrity tests) keep seeing
// exactly the three mainnet corridors. Replaying them keeps the same
// guarantee as the real recordings: a request the fixture does not know
// about, such as a regression that adds an extra Horizon call, fails loudly
// with snapshot.ErrNotRecorded instead of passing silently.

// chainSnap loads the scenario fixture for one dependency-chain case.
func chainSnap(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "testdata", "chain-snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no chain fixture matching %q under testdata/chain-snapshots", prefix)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading chain fixture %s: %v", matches[0], err)
	}
	return m
}

// chainEngine builds an engine answering only from a chain scenario fixture,
// with the reference mid pinned so the assertions are about the chain rather
// than about whatever the rate provider says today.
func chainEngine(m *snapshot.Manifest, pair, mid string) *Engine {
	return &Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			pair: decimal.RequireFromString(mid),
		}),
	}
}

// TestChainMeasuredDirect verifies that when a derivative corridor's
// dependency is measured, the chain carries the measured integrity.
// USDC→GHSC depends on NGNC; USDC→NGNC has an XLM path (bridge asset),
// so NGNC is DIRECT. The chain should be depth 1 with NGNC measured as DIRECT.
func TestChainMeasuredDirect(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-direct-ngnc"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "NGNC" {
		t.Fatalf("Chain = %v, want exactly NGNC", res.Chain)
	}
	node := res.Chain[0]
	if !node.Measured {
		t.Error("NGNC should be measured")
	}
	if node.Integrity != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", node.Integrity)
	}
	if len(node.Dependencies) != 0 {
		t.Errorf("NGNC should have no sub-dependencies, got %v", node.Dependencies)
	}

	// The warning should use the measured variant.
	warnings := strings.Join(res.Quotes[0].Warnings, " ")
	if !strings.Contains(warnings, "DIRECT, independent market exists") {
		t.Errorf("expected measured warning with market status, got: %v",
			res.Quotes[0].Warnings)
	}
}

// TestChainDepthTwo verifies recursive chain measurement through two levels.
// USDC→GHSC depends on KESC, KESC depends on NGNC, NGNC is DIRECT (reached
// via XLM, a bridge asset).
func TestChainDepthTwo(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-depth-two"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	// Chain: GHSC depends on KESC (depth 2), KESC depends on NGNC (depth 1),
	// NGNC is DIRECT (depth 0).
	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "KESC" {
		t.Fatalf("Chain top level = %v, want KESC", res.Chain)
	}
	kescNode := res.Chain[0]
	if !kescNode.Measured {
		t.Error("KESC should be measured")
	}
	if kescNode.Integrity != IntegrityDerivative {
		t.Errorf("KESC integrity = %s, want DERIVATIVE", kescNode.Integrity)
	}
	if len(kescNode.Dependencies) != 1 || kescNode.Dependencies[0].Asset.Code != "NGNC" {
		t.Fatalf("KESC dependencies = %v, want NGNC", kescNode.Dependencies)
	}
	ngncNode := kescNode.Dependencies[0]
	if !ngncNode.Measured {
		t.Error("NGNC should be measured")
	}
	if ngncNode.Integrity != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", ngncNode.Integrity)
	}

	// All measured.
	if !allMeasured(res.Chain) {
		t.Error("all nodes should be measured in this chain")
	}
}

// TestChainCycleTerminates verifies that a circular dependency does not
// cause infinite recursion. When USDC→NGNC routes through GHSC while GHSC is
// already on the path (it is the destination), the second encounter is
// detected as a cycle and reported as unmeasured.
func TestChainCycleTerminates(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-cycle"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	// The chain should have NGNC as the top-level dependency.
	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "NGNC" {
		t.Fatalf("Chain top level = %v, want NGNC", res.Chain)
	}
	ngncNode := res.Chain[0]
	if !ngncNode.Measured {
		t.Error("NGNC should be measured (first encounter)")
	}
	if ngncNode.Integrity != IntegrityDerivative {
		t.Errorf("NGNC integrity = %s, want DERIVATIVE", ngncNode.Integrity)
	}

	// NGNC depends on GHSC, but GHSC is already visited (it's the
	// destination), so it should be reported as unmeasured with cycle reason.
	if len(ngncNode.Dependencies) != 1 || ngncNode.Dependencies[0].Asset.Code != "GHSC" {
		t.Fatalf("NGNC dependencies = %v, want GHSC", ngncNode.Dependencies)
	}
	ghscNode := ngncNode.Dependencies[0]
	if ghscNode.Measured {
		t.Error("GHSC should NOT be measured (cycle detected)")
	}
	if ghscNode.Reason != "cycle detected" {
		t.Errorf("GHSC reason = %q, want 'cycle detected'", ghscNode.Reason)
	}
}

// TestChainDependencyHasNoMarket verifies that a dependency whose own
// market is NO-MARKET is reported honestly in the chain.
func TestChainDependencyHasNoMarket(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-no-market"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	if len(res.Chain) != 1 || res.Chain[0].Asset.Code != "KESC" {
		t.Fatalf("Chain = %v, want KESC", res.Chain)
	}
	kescNode := res.Chain[0]
	if !kescNode.Measured {
		t.Error("KESC should be measured")
	}
	if kescNode.Integrity != IntegrityNoMarket {
		t.Errorf("KESC integrity = %s, want NO-MARKET", kescNode.Integrity)
	}

	// NO-MARKET is still a measurement, so the whole chain is measured.
	if !allMeasured(res.Chain) {
		t.Error("all nodes should be measured (NO-MARKET is still a measurement)")
	}
}

// TestChainSharedDependencyIsNotACycle pins the visited-set rule: GHSC
// depends on KESC and NGNC, and KESC also depends on NGNC. NGNC is a
// sibling-shared sub-dependency — it appears on two branches of the tree —
// which is not a cycle, and both top-level dependencies must come back
// measured rather than the second one being mislabelled "cycle detected".
func TestChainSharedDependencyIsNotACycle(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-shared"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDerivative {
		t.Fatalf("Integrity = %s, want DERIVATIVE", res.Integrity)
	}

	// Both dependencies must be measured, in code order.
	if len(res.Chain) != 2 || res.Chain[0].Asset.Code != "KESC" || res.Chain[1].Asset.Code != "NGNC" {
		t.Fatalf("Chain top level = %v, want [KESC NGNC]", res.Chain)
	}
	for _, n := range res.Chain {
		if !n.Measured {
			t.Errorf("%s should be measured, not %q", n.Asset.Code, n.Reason)
		}
	}

	// KESC is derivative and its own dependency (NGNC) is measured.
	kescNode := res.Chain[0]
	if kescNode.Integrity != IntegrityDerivative {
		t.Errorf("KESC integrity = %s, want DERIVATIVE", kescNode.Integrity)
	}
	if len(kescNode.Dependencies) != 1 || kescNode.Dependencies[0].Asset.Code != "NGNC" {
		t.Fatalf("KESC dependencies = %v, want NGNC", kescNode.Dependencies)
	}
	if !kescNode.Dependencies[0].Measured {
		t.Error("NGNC under KESC should be measured")
	}

	// NGNC appears again as a sibling top-level dependency, measured.
	ngncNode := res.Chain[1]
	if ngncNode.Integrity != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", ngncNode.Integrity)
	}

	if !allMeasured(res.Chain) {
		t.Error("every node should be measured; none of the shared dependencies is a cycle")
	}
}

// TestChainWireShape verifies the JSON wire shape of the dependency chain.
func TestChainWireShape(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-direct-ngnc"), "USD/GHS", "11.7625")
	res, err := e.Quote(context.Background(), ghsRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	chain := ToDependencyChainJSON(res.Chain)
	if chain == nil {
		t.Fatal("chain should not be nil for a derivative corridor")
	}
	if chain.Depth != 1 {
		t.Errorf("depth = %d, want 1", chain.Depth)
	}
	if len(chain.DependsOn) != 1 {
		t.Fatalf("depends_on = %d nodes, want 1", len(chain.DependsOn))
	}
	node := chain.DependsOn[0]
	if node.Code != "NGNC" {
		t.Errorf("code = %s, want NGNC", node.Code)
	}
	if !node.Measured {
		t.Error("measured should be true")
	}
	if node.Integrity != "DIRECT" {
		t.Errorf("integrity = %s, want DIRECT", node.Integrity)
	}
	if node.Peg != "NGN" {
		t.Errorf("peg = %s, want NGN", node.Peg)
	}
	if len(node.Dependencies) != 0 {
		t.Errorf("sub-dependencies = %d, want 0", len(node.Dependencies))
	}
}

// TestChainBackwardCompatible pins the compatibility contract on the
// serialized document rather than on Go struct fields: a consumer that only
// knows the flat depends_on array must still find it in the same JSON as a
// chain-aware one finds dependency_chain.
func TestChainBackwardCompatible(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-direct-ngnc"), "USD/GHS", "11.7625")
	lr, err := e.Ladder(context.Background(), LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.GHSC(),
		Sizes:          []decimal.Decimal{decimal.NewFromInt(100)},
		ReferenceBase:  "USD",
		ReferenceQuote: "GHS",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}

	raw, err := json.Marshal(ToCorridorJSON(lr, "USD/GHS"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The flat depends_on array is what a pre-chain consumer reads.
	var flat []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(doc["depends_on"], &flat); err != nil {
		t.Fatalf("depends_on: %v", err)
	}
	if len(flat) != 1 || flat[0].Code != "NGNC" {
		t.Errorf("depends_on = %v, want exactly NGNC", flat)
	}

	// The chain is additive: dependency_chain must be present alongside it.
	chainRaw, ok := doc["dependency_chain"]
	if !ok {
		t.Fatal("serialized corridor is missing dependency_chain")
	}
	var chain struct {
		Depth     int `json:"depth"`
		DependsOn []struct {
			Code     string `json:"code"`
			Measured bool   `json:"measured"`
		} `json:"depends_on"`
	}
	if err := json.Unmarshal(chainRaw, &chain); err != nil {
		t.Fatalf("dependency_chain: %v", err)
	}
	if chain.Depth != 1 {
		t.Errorf("dependency_chain depth = %d, want 1", chain.Depth)
	}
	if len(chain.DependsOn) != 1 || chain.DependsOn[0].Code != "NGNC" ||
		!chain.DependsOn[0].Measured {
		t.Errorf("dependency_chain depends_on = %+v, want NGNC measured",
			chain.DependsOn)
	}
}

// TestLadderChainKeepsMeasuredOverUnmeasured pins the aggregation rule for
// chains across rungs: the union across the ladder may not let one rung's
// unmeasured placeholder erase another rung's measurement of the same
// dependency.
func TestLadderChainKeepsMeasuredOverUnmeasured(t *testing.T) {
	measured := []DependencyNode{
		{Asset: asset.NGNC(), Measured: true, Integrity: IntegrityDirect},
	}
	unmeasured := []DependencyNode{
		{Asset: asset.NGNC(), Measured: false, Reason: "Horizon error: timeout"},
	}

	mk := func(first, second []DependencyNode) *LadderResult {
		return &LadderResult{
			Request: LadderRequest{
				SendAsset:      asset.USDC(),
				ReceiveAsset:   asset.GHSC(),
				ReferenceBase:  "USD",
				ReferenceQuote: "GHS",
			},
			Rungs: []Rung{
				{SendAmount: decimal.NewFromInt(1), Result: &Result{
					Integrity: IntegrityDerivative,
					DependsOn: []asset.Asset{asset.NGNC()},
					Chain:     first,
				}},
				{SendAmount: decimal.NewFromInt(10), Result: &Result{
					Integrity: IntegrityDerivative,
					DependsOn: []asset.Asset{asset.NGNC()},
					Chain:     second,
				}},
			},
		}
	}

	// Unmeasured rung first, measured rung second: the measurement wins.
	l := mk(unmeasured, measured)
	l.summarise()
	if len(l.Chain) != 1 || !l.Chain[0].Measured {
		t.Fatalf("Chain = %v, want the measured NGNC to survive aggregation", l.Chain)
	}
	if got := l.Chain[0].Integrity; got != IntegrityDirect {
		t.Errorf("NGNC integrity = %s, want DIRECT", got)
	}

	// Measured rung first, unmeasured second: the measurement is kept.
	l = mk(measured, unmeasured)
	l.summarise()
	if len(l.Chain) != 1 || !l.Chain[0].Measured {
		t.Fatalf("Chain = %v, want the measured NGNC to keep its place", l.Chain)
	}
}

// TestDirectCorridorHasNoChain verifies that a direct corridor does not
// produce a dependency chain.
func TestDirectCorridorHasNoChain(t *testing.T) {
	e := chainEngine(chainSnap(t, "usdc-ngnc-chain-direct"), "USD/NGN", "1500")
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if res.Integrity != IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT", res.Integrity)
	}
	if res.Chain != nil {
		t.Errorf("Chain = %v, want nil for direct corridor", res.Chain)
	}
}
