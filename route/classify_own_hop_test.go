package route

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
)

// TestDestinationAsItsOwnHopIsNotADependency kills mutation M08 of the
// pricing-path catalogue (docs/qa/artifacts/667-mutation-catalogue.md, #667).
//
// Horizon can return a path that passes through the destination asset before
// arriving at it — USDC -> NGNC -> XLM -> NGNC is a legal path. NGNC is a
// registered fiat token, so without the skip in classify the corridor would be
// reported as depending on itself: DERIVATIVE, with NGNC in DependsOn. The
// skip existed and nothing exercised it, so deleting it survived the suite.
func TestDestinationAsItsOwnHopIsNotADependency(t *testing.T) {
	ngnc := asset.NGNC()
	paths := []dex.Path{{
		SourceAsset:  asset.USDC(),
		SourceAmount: decimal.NewFromInt(100),
		DestAsset:    ngnc,
		DestAmount:   decimal.NewFromInt(60000),
		Hops:         []asset.Asset{ngnc, asset.Native()},
	}}

	integrity, deps, unknown := classify(paths, ngnc)
	if integrity != IntegrityDirect {
		t.Errorf("integrity = %s, want DIRECT: the only fiat hop is the destination itself", integrity)
	}
	if len(deps) != 0 {
		t.Errorf("DependsOn = %v, want none; a corridor cannot depend on its own destination", deps)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown hops = %v, want none; NGNC and XLM are both registered", unknown)
	}

	// The skip must match on issuer as well as code: a same-code token from
	// another issuer is a different asset and stays a hop to classify.
	impostor := asset.Stellar("NGNC", "GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD")
	paths[0].Hops = []asset.Asset{impostor}
	if integrity, _, unknown := classify(paths, ngnc); integrity != IntegrityDirect || len(unknown) != 1 {
		t.Errorf("a same-code hop from another issuer classified as %s with unknown=%v; "+
			"want it treated as an unregistered hop, not as the destination", integrity, unknown)
	}
}
