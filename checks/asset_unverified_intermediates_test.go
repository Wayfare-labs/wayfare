package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

const unregisteredIssuer = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5"

// The three states, and the distinction the check exists to draw: a hop issued
// by an account Wayfare has not verified is a fact, not a verdict.

func TestUnverifiedIntermediatesReportsUnverifiedIssuer(t *testing.T) {
	btc := asset.Stellar("BTC", unregisteredIssuer)
	s := Subject{
		Send:          asset.USDC(),
		Receive:       asset.NGNC(),
		Intermediates: []asset.Asset{btc, asset.Native()},
	}

	res := Run(context.Background(), UnverifiedIntermediates{}, s)

	if !res.Determined || res.Passed {
		t.Fatalf("result = determined=%v passed=%v, want a determined failure",
			res.Determined, res.Passed)
	}
	if res.ID != "asset.unverified-intermediate" {
		t.Errorf("ID = %q, want asset.unverified-intermediate", res.ID)
	}
	if res.Severity != SeverityWarning {
		t.Errorf("Severity = %s, want warning", res.Severity)
	}
	if !strings.Contains(res.Summary, "BTC") || !strings.Contains(res.Summary, unregisteredIssuer) {
		t.Errorf("summary does not identify the unverified hop: %q", res.Summary)
	}
	if len(res.Evidence) != 1 {
		t.Fatalf("evidence count = %d, want one for the one issued hop", len(res.Evidence))
	}
	if !strings.Contains(res.Evidence[0].Observed, unregisteredIssuer) {
		t.Errorf("evidence does not record the issuer verbatim: %q", res.Evidence[0].Observed)
	}
	if res.Evidence[0].ObservedAt.IsZero() || res.Evidence[0].Source == "" {
		t.Error("evidence is missing its source or observation time")
	}
}

func TestUnverifiedIntermediatesPassesForVerifiedIssuers(t *testing.T) {
	s := Subject{
		Send:          asset.USDC(),
		Receive:       asset.NGNC(),
		Intermediates: []asset.Asset{asset.USDZ(), asset.Native(), asset.PYUSD()},
	}

	res := Run(context.Background(), UnverifiedIntermediates{}, s)

	if !res.Determined || !res.Passed {
		t.Fatalf("result = determined=%v passed=%v, want a determined pass: %s",
			res.Determined, res.Passed, res.Summary)
	}
	if len(res.Evidence) != 2 {
		t.Errorf("evidence count = %d, want one per issued hop (native has no issuer)", len(res.Evidence))
	}
}

// A registered code from an unregistered account is the impostor case the
// registry exists to catch; matching on code alone must not pass it.
func TestUnverifiedIntermediatesUsesIssuerNotCode(t *testing.T) {
	impostor := asset.Stellar("USDC", unregisteredIssuer)
	s := Subject{
		Send:          asset.USDC(),
		Receive:       asset.NGNC(),
		Intermediates: []asset.Asset{impostor},
	}

	res := Run(context.Background(), UnverifiedIntermediates{}, s)

	if !res.Failed() {
		t.Fatalf("an impostor USDC from an unregistered issuer passed: %s", res.Summary)
	}
}

func TestUnverifiedIntermediatesNoPathIsUndetermined(t *testing.T) {
	s := Subject{Send: asset.USDC(), Receive: asset.NGNC()}

	res := Run(context.Background(), UnverifiedIntermediates{}, s)

	if res.Determined {
		t.Fatal("a path with no recorded intermediates must not read as a determined fact")
	}
	if res.Reason == "" {
		t.Error("an undetermined result carries no reason")
	}
}

func TestUnverifiedIntermediatesNativeOnlyPasses(t *testing.T) {
	s := Subject{
		Send:          asset.USDC(),
		Receive:       asset.NGNC(),
		Intermediates: []asset.Asset{asset.Native()},
	}

	res := Run(context.Background(), UnverifiedIntermediates{}, s)

	if !res.Determined || !res.Passed {
		t.Fatalf("a native-only path = determined=%v passed=%v, want a determined pass",
			res.Determined, res.Passed)
	}
}

// The result is a normal check result on the wire, with the tri-state fields
// separate and the failed count set.
func TestUnverifiedIntermediatesReachesTheWire(t *testing.T) {
	s := Subject{
		Send:          asset.USDC(),
		Receive:       asset.NGNC(),
		Intermediates: []asset.Asset{asset.Stellar("AQUA", unregisteredIssuer)},
	}
	var f Findings
	f.Add(Run(context.Background(), UnverifiedIntermediates{}, s))

	out := f.ToJSON()

	if out.Failed != 1 || out.Passed != 0 || out.Undetermined != 0 {
		t.Fatalf("counts = %d/%d/%d, want 0 passed, 1 failed, 0 undetermined",
			out.Passed, out.Failed, out.Undetermined)
	}
	if out.WorstSeverity != "warning" {
		t.Errorf("WorstSeverity = %q, want warning", out.WorstSeverity)
	}
	if len(out.Checks) != 1 || out.Checks[0].ID != "asset.unverified-intermediate" {
		t.Fatalf("checks = %+v, want the unverified-intermediate result", out.Checks)
	}
	if out.Checks[0].Determined != true || out.Checks[0].Passed != false {
		t.Errorf("wire collapsed failure and unknown: determined=%v passed=%v",
			out.Checks[0].Determined, out.Checks[0].Passed)
	}
}
