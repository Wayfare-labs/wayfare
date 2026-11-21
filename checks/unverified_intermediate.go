package checks

import (
	"context"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
)

// UnverifiedIntermediate reports whether a rung's best path routes through an
// asset whose issuer is absent from the verified registry.
//
// # Why this is separate from DERIVATIVE
//
// DERIVATIVE classifies fiat-peg dependencies: a path through another fiat
// token inherits that token's pricing and liquidity. It says nothing about a
// hop that is not fiat-pegged, and nothing about who issues a hop at all. A
// token from an unregistered issuer carries that issuer's freeze and clawback
// powers regardless of its peg, and those powers are exactly what a corridor's
// own integrity and verdict never see.
//
// # What it can determine
//
// Whether each intermediate asset's issuer account appears in the verified
// registry. That is a membership fact about a local list.
//
// # What it cannot determine
//
// Whether an unregistered issuer is honest, solvent, or will exercise freeze
// or clawback. Registration is not a safety rating; absence of registration is
// not a finding of misconduct. It is a counterparty the project has not
// verified.
type UnverifiedIntermediate struct{}

// Describe implements Check.
func (UnverifiedIntermediate) Describe() Descriptor {
	return Descriptor{
		ID:       "hop.issuer-verified",
		Scope:    ScopeCorridor,
		Cost:     CostFree,
		Severity: SeverityWarning,
		Title:    "Intermediate hop issuers are in the verified registry",
		CanDetermine: "Whether every intermediate asset on the rung's best path is " +
			"issued by an account recorded in the verified asset registry.",
		CannotDetermine: "Whether an unverified issuer is honest or will exercise " +
			"freeze or clawback powers. Only registry membership is observed; a " +
			"registered issuer may still carry those risks, and an unregistered one " +
			"has simply not been verified.",
	}
}

// Run implements Check.
func (c UnverifiedIntermediate) Run(_ context.Context, s Subject) CheckResult {
	d := c.Describe()
	at := time.Now().UTC()

	var unverified []asset.Asset
	var evidence []Evidence
	for _, hop := range s.Hops {
		// Native XLM has no issuer and cannot carry an issuer's powers.
		if hop.IsNative() {
			continue
		}
		// A path only traverses Stellar assets. Anything else is not a hop
		// this check can assess.
		if hop.Kind != asset.KindStellar {
			continue
		}
		if hop.Issuer != "" && asset.IsVerifiedIssuer(hop.Issuer) {
			continue
		}

		unverified = append(unverified, hop)
		source := hop.Issuer
		observed := hop.Code + ":" + hop.Issuer
		if hop.Issuer == "" {
			// An issued asset with no issuing account has no identity to
			// check. That is not a verified issuer, so it is reported
			// rather than skipped.
			source = "(no issuing account established)"
			observed = hop.Code + ":(issuer unknown)"
		}
		evidence = append(evidence, Evidence{
			Source:     source,
			Observed:   observed,
			ObservedAt: at,
		})
	}

	if len(unverified) == 0 {
		return Pass(d, s,
			"every intermediate hop is issued by an account in the verified registry")
	}

	names := make([]string, len(unverified))
	for i, hop := range unverified {
		names[i] = hop.String()
	}
	return Fail(d, s,
		"intermediate hop(s) "+strings.Join(names, ", ")+
			" have issuers not in the verified registry, so the route carries "+
			"unassessed freeze and clawback risk",
		evidence...)
}
