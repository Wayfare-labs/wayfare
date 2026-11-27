package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
)

// UnverifiedIntermediates reports intermediate assets in a priced path whose
// issuing account is not in the verified registry.
//
// # Why this is a counterparty fact and not an integrity state
//
// The DERIVATIVE integrity state answers whether a corridor has an independent
// market, and it counts only fiat-pegged dependencies. It says nothing about a
// path that routes through a token issued by an account Wayfare has never
// verified: that token may be non-fiat, freely tradeable, and still carry an
// issuer able to freeze or claw it back. The risk travels with the issuer, not
// with the peg.
//
// So this is reported alongside the route rather than folded into its
// classification. An unverified intermediate never turns a DIRECT corridor
// into a DERIVATIVE one, and it never changes a verdict — the check observes a
// party the route depends on, which is exactly the class of fact the checks
// package exists for.
//
// # What it can determine
//
// Whether each issued intermediate asset's issuer appears in the verified
// registry (asset/known.go). That is a statement about Wayfare's verification
// coverage, not about the issuer's conduct.
//
// # What it cannot determine
//
// Whether an unverified issuer is unsafe, insolvent, or will ever exercise
// freeze or clawback powers. Registry absence means only that the issuer has
// not been verified against its own stellar.toml; a registered issuer retains
// the same powers. Nor does a pass guarantee anything about a verified issuer
// beyond its presence in the registry.
type UnverifiedIntermediates struct{}

// Describe implements Check.
func (UnverifiedIntermediates) Describe() Descriptor {
	return Descriptor{
		ID:       "asset.unverified-intermediate",
		Scope:    ScopeAsset,
		Cost:     CostFree,
		Severity: SeverityWarning,
		Title:    "Path intermediates are issued by verified accounts",
		CanDetermine: "Whether every issued intermediate asset in a priced path has an " +
			"issuing account present in the verified registry (asset/known.go).",
		CannotDetermine: "Whether an unverified issuer is trustworthy, solvent, or will ever " +
			"freeze or claw back a balance. Registry absence means only that the issuer has " +
			"not been verified against its own stellar.toml; a registered issuer retains the " +
			"same powers.",
	}
}

// Run implements Check.
func (c UnverifiedIntermediates) Run(_ context.Context, s Subject) CheckResult {
	d := c.Describe()

	if len(s.Intermediates) == 0 {
		return Undetermined(d, s,
			"the path recorded no intermediate assets, so there is no hop issuer to check")
	}

	at := time.Now().UTC()
	var unverified []asset.Asset
	var evidence []Evidence
	examined := 0

	for _, hop := range s.Intermediates {
		// Native XLM has no issuer, and an off-chain fiat code cannot be a
		// path hop. Neither is a counterparty this check can assess.
		if hop.IsNative() || hop.Kind != asset.KindStellar {
			continue
		}
		examined++

		ev := Evidence{Source: "asset/known.go registry", ObservedAt: at}
		switch {
		case hop.Issuer == "":
			ev.Observed = hop.Code + " names no issuer, so no issuing account could be checked"
		case asset.IsVerifiedIssuer(hop.Issuer):
			ev.Observed = hop.Code + " issued by " + hop.Issuer + " is in the verified registry"
		default:
			ev.Observed = hop.Code + " issued by " + hop.Issuer + " is not in the verified registry"
			unverified = append(unverified, hop)
		}
		evidence = append(evidence, ev)
	}

	if examined == 0 {
		return Pass(d, s,
			"the path has no issued intermediate assets",
			Evidence{
				Source:     "asset/known.go registry",
				Observed:   "every intermediate hop is native XLM",
				ObservedAt: at,
			})
	}

	if len(unverified) == 0 {
		return Pass(d, s,
			"every issued intermediate asset's issuer is in the verified registry",
			evidence...)
	}

	labels := make([]string, 0, len(unverified))
	for _, a := range unverified {
		labels = append(labels, hopLabel(a))
	}
	return Fail(d, s,
		"path hops through issuer(s) not in the verified registry: "+
			strings.Join(labels, ", ")+
			" — that issuer's freeze and clawback powers travel with the route",
		evidence...)
}

// hopLabel renders a hop for a one-line summary. The full issuing account is
// kept because a code alone identifies nothing: anyone can issue a token called
// "BTC". An issued asset whose issuer was never established says so rather than
// printing an empty field.
func hopLabel(a asset.Asset) string {
	if a.Issuer == "" {
		return fmt.Sprintf("%s (issuer unknown)", a.Code)
	}
	return fmt.Sprintf("%s (%s)", a.Code, a.Issuer)
}
