// This file gives every book metric — spread, depth, concentration and
// price-impact — one shared way to tell apart the three reasons a book
// metric can end up unable to determine a value:
//
//   - a structural fact: the corridor has no direct pair at all, by
//     construction (DERIVATIVE or NO-MARKET);
//   - a market fact: a direct pair exists, and its order book is empty or
//     one-sided;
//   - an availability fact: the book could not be fetched at all.
//
// Before this file existed, every book metric collapsed the first two into
// the same "order book is empty" reason, because an order book response
// genuinely cannot tell them apart — Horizon answers an empty
// {"bids":[],"asks":[]} either way. Only the ladder's own pathfinding sees
// the difference, so distinguishing them here requires the caller to pass
// it in via Subject.Integrity. See GitHub issue #105 / docs/backlog.md.
package checks

import (
	"fmt"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Integrity string constants, matching route.Integrity.String() by
// convention. Duplicated rather than imported: route imports this package
// for Findings, so checks cannot import route without a cycle.
const (
	integrityDirect     = "DIRECT"
	integrityDerivative = "DERIVATIVE"
	integrityNoMarket   = "NO-MARKET"
)

// structuralUndetermined reports the structural reason a book metric cannot
// even attempt a fetch, before any network call: the corridor has no direct
// pair by construction. ok is false when the caller should proceed to fetch
// a book — either because Integrity is DIRECT, unset, or DERIVATIVE with an
// Underlying pair supplied to measure instead.
func structuralUndetermined(d Descriptor, s Subject) (res MetricResult, ok bool) {
	switch s.Integrity {
	case integrityNoMarket:
		return MetricUndetermined(d, s, fmt.Sprintf(
			"%s has no direct market to %s by construction (NO-MARKET): no path "+
				"exists between them at all, which is a structural fact about the "+
				"corridor and not something an order book fetch could change",
			s.Send.Code, s.Receive.Code)), true
	case integrityDerivative:
		if s.Underlying.Code == "" {
			return MetricUndetermined(d, s, fmt.Sprintf(
				"%s has no direct pair to %s by construction (DERIVATIVE): every "+
					"path routes through an intermediate fiat-pegged asset, so there "+
					"is no market between %s and %s to fetch a book for",
				s.Send.Code, s.Receive.Code, s.Send.Code, s.Receive.Code)), true
		}
	}
	return MetricResult{}, false
}

// bookPair resolves which pair a book metric actually measures: the
// requested Send/Receive, unless the corridor is DERIVATIVE and the caller
// supplied an Underlying pair to substitute — in which case substituted is
// true and the caller must say so in its evidence, never silently.
func bookPair(s Subject) (sell, buy asset.Asset, substituted bool) {
	if s.Integrity == integrityDerivative && s.Underlying.Code != "" {
		return s.Send, s.Underlying, true
	}
	return s.Send, s.Receive, false
}

// bookSource renders the evidence Source for a book fetch, naming the
// substitution explicitly when one happened.
func bookSource(endpoint string, s Subject, sell, buy asset.Asset, substituted bool) string {
	base := fmt.Sprintf("%s %s/%s", endpoint, sell.Code, buy.Code)
	if !substituted {
		return base
	}
	return fmt.Sprintf(
		"%s (substituted for %s/%s: %s is DERIVATIVE and has no direct book of its own)",
		base, s.Send.Code, s.Receive.Code, s.Receive.Code)
}
