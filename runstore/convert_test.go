package runstore

import (
	"testing"

	"github.com/Wayfare-labs/wayfare/route"
)

// TestFromCorridorJSONCarriesReferenceCrossCheck pins that a cross-checked
// reference rate survives the trip into storage.
//
// Reference already had SecondaryMid, SecondarySource and DivergencePct
// fields before this test existed; FromCorridorJSON simply never populated
// them, so every stored record silently forgot a corridor had been
// cross-checked at all. A replay built from that record would then disagree
// with the live document it was supposed to reproduce on exactly the fields
// this package exists to keep honest.
func TestFromCorridorJSONCarriesReferenceCrossCheck(t *testing.T) {
	c := route.CorridorJSON{
		SendAsset:                route.AssetJSON{Code: "USDC"},
		ReceiveAsset:             route.AssetJSON{Code: "NGNC"},
		ReferenceMid:             "1350.2568",
		ReferenceSource:          "exchangerate-api",
		ReferenceSecondaryMid:    "1348.9000",
		ReferenceSecondarySource: "currency-api",
		ReferenceDivergencePct:   "0.0931",
		Scored:                   true,
	}

	r := FromCorridorJSON(c)

	if r.Reference.SecondaryMid != c.ReferenceSecondaryMid {
		t.Errorf("Reference.SecondaryMid = %q, want %q", r.Reference.SecondaryMid, c.ReferenceSecondaryMid)
	}
	if r.Reference.SecondarySource != c.ReferenceSecondarySource {
		t.Errorf("Reference.SecondarySource = %q, want %q", r.Reference.SecondarySource, c.ReferenceSecondarySource)
	}
	if r.Reference.DivergencePct != c.ReferenceDivergencePct {
		t.Errorf("Reference.DivergencePct = %q, want %q", r.Reference.DivergencePct, c.ReferenceDivergencePct)
	}
	if r.Reference.ScoredAgainst != c.ReferenceSource {
		t.Errorf("Reference.ScoredAgainst = %q, want %q (the corridor was scored)", r.Reference.ScoredAgainst, c.ReferenceSource)
	}
}

// TestFromCorridorJSONUnscoredHasNoScoredAgainst covers the other half: a
// corridor that was never scorable must not claim a source produced its
// (nonexistent) verdicts.
func TestFromCorridorJSONUnscoredHasNoScoredAgainst(t *testing.T) {
	c := route.CorridorJSON{
		SendAsset:       route.AssetJSON{Code: "USDC"},
		ReceiveAsset:    route.AssetJSON{Code: "NGNC"},
		ReferenceMid:    "1350.2568",
		ReferenceSource: "exchangerate-api",
		Scored:          false,
	}

	r := FromCorridorJSON(c)

	if r.Reference.ScoredAgainst != "" {
		t.Errorf("Reference.ScoredAgainst = %q, want empty for an unscorable rate", r.Reference.ScoredAgainst)
	}
}
