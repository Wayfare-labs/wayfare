package runstore

import (
	"testing"

	"github.com/Wayfare-labs/wayfare/checks"
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

// TestFromCorridorJSONCarriesFindings pins that check and metric results
// ride into storage with the measurement. This is what lets the stale path
// serve stored findings (issue #93): without it a history-served corridor
// would silently lose every counterparty fact the live one showed.
func TestFromCorridorJSONCarriesFindings(t *testing.T) {
	c := route.CorridorJSON{
		SendAsset:    route.AssetJSON{Code: "USDC"},
		ReceiveAsset: route.AssetJSON{Code: "NGNC"},
		Findings: &checks.FindingsJSON{
			Checks: []checks.CheckJSON{
				{
					ID: "issuer.auth-flags", Scope: "anchor", Subject: "ngnc.online",
					Severity: "critical", Determined: true, Passed: false,
					Summary: "issuer enabled AUTH_REVOCABLE",
					Evidence: []checks.EvidenceJSON{{
						Source:     "https://horizon.stellar.org/accounts/X",
						Observed:   "flags: auth_revocable=true",
						ObservedAt: "2026-08-21T22:28:00Z",
					}},
					ObservedAt: "2026-08-21T22:28:00Z",
				},
				{
					ID: "sep24.info-lists-asset", Scope: "anchor", Subject: "ngnc.online",
					Severity: "warning", Determined: false, Passed: false,
					Reason:     "no /sep24/info endpoint declared",
					Summary:    "could not determine: no /sep24/info endpoint declared",
					ObservedAt: "2026-08-21T22:28:00Z",
				},
			},
			Metrics: []checks.MetricJSON{
				{
					ID: "spread.bid-ask", Scope: "asset", Subject: "USDC",
					Determined: true, Value: "0.0004", Unit: "ratio",
					Summary:    "bid-ask spread",
					ObservedAt: "2026-08-21T22:28:00Z",
				},
				{
					ID: "depth.observed-executable", Scope: "asset", Subject: "USDC",
					Determined: false, Reason: "no executable side",
					Summary:    "could not determine: no executable side",
					Unit:       "amount",
					ObservedAt: "2026-08-21T22:28:00Z",
				},
			},
		},
	}

	r := FromCorridorJSON(c)

	if len(r.Checks) != 2 {
		t.Fatalf("record checks = %d, want 2", len(r.Checks))
	}
	// An undetermined result must survive with its reason intact — the
	// tri-state (and its explanation) is the whole point of the block.
	und := r.Checks[1]
	if und.Determined != false || und.Reason != "no /sep24/info endpoint declared" {
		t.Errorf("undetermined check lost its reason: determined=%v reason=%q",
			und.Determined, und.Reason)
	}
	if len(r.Metrics) != 2 {
		t.Fatalf("record metrics = %d, want 2", len(r.Metrics))
	}
	if r.Metrics[0].Value != "0.0004" || r.Metrics[0].Unit != "ratio" {
		t.Errorf("determined metric = value %q unit %q, want 0.0004 ratio",
			r.Metrics[0].Value, r.Metrics[0].Unit)
	}
	if r.Metrics[1].Determined != false || r.Metrics[1].Reason != "no executable side" {
		t.Errorf("undetermined metric lost its reason: determined=%v reason=%q",
			r.Metrics[1].Determined, r.Metrics[1].Reason)
	}
}

// TestFromCorridorJSONCarryFindingsAbsentStaysAbsent keeps absence honest:
// a CorridorJSON with no findings block must produce a record with none,
// never an empty block that reads as "checked, nothing found".
func TestFromCorridorJSONFindingsAbsentStaysAbsent(t *testing.T) {
	c := route.CorridorJSON{
		SendAsset:    route.AssetJSON{Code: "USDC"},
		ReceiveAsset: route.AssetJSON{Code: "NGNC"},
	}
	r := FromCorridorJSON(c)
	if len(r.Checks) != 0 || len(r.Metrics) != 0 {
		t.Errorf("record with no findings has checks=%d metrics=%d, want none",
			len(r.Checks), len(r.Metrics))
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
