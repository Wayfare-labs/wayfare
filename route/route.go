// Package route ranks the ways money can travel a corridor, and says plainly
// when none of them are worth taking.
//
// # Why a verdict, and not just a ranking
//
// A pure ranking has a hidden assumption: that the best option is a good
// option. On this corridor that assumption fails. Measured on mainnet on
// 2026-08-04, sending 100 USDC to NGNC returned 65,100 NGNC via the XLM
// bridge — the best of the available routes — while USD/NGN sat near 1,500.
// The winner of the ranking delivered roughly 43% of fair value.
//
// A tool that displayed "best route: 65,100 NGNC" would be accurate, useful
// looking, and would have cost the user more than half of what they sent. So
// every quote here is scored against an independent reference rate and
// carries a verdict, and a result whose best option is Unusable says so
// instead of presenting a winner.
//
// This is the reason the reference rate is a required dependency rather than
// an optional enrichment. Without it the engine can rank, but it cannot tell
// the difference between a good deal and a disaster, and shipping the former
// interface while only having the latter capability is the specific failure
// this package is built to prevent.
package route

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/sep38"
)

// Verdict grades a route against the reference mid-market rate.
type Verdict int

const (
	// VerdictUnknown means the route could not be scored, normally because
	// no reference rate was available. It is never presented as a
	// recommendation.
	VerdictUnknown Verdict = iota

	// VerdictGood is within 3% of mid — comparable to a competitive
	// remittance service.
	VerdictGood

	// VerdictFair is within 8% — worse than the best providers but not
	// unreasonable.
	VerdictFair

	// VerdictPoor is within 20% — expensive, and the user should be told
	// so clearly.
	VerdictPoor

	// VerdictUnusable is more than 20% below mid. This is not a fee, it is
	// value destruction, and the tool recommends against it.
	VerdictUnusable
)

// Thresholds separating the verdicts, as percentage loss against mid.
//
// These are judgement calls, not measurements. They are set where they are
// because established remittance corridors run a total cost in the 3-8% band,
// so "good" is anchored to what the incumbent market actually achieves rather
// than to what the DEX happens to offer.
var (
	ThresholdGood = decimal.NewFromInt(3)
	ThresholdFair = decimal.NewFromInt(8)
	ThresholdPoor = decimal.NewFromInt(20)
)

func (v Verdict) String() string {
	switch v {
	case VerdictGood:
		return "GOOD"
	case VerdictFair:
		return "FAIR"
	case VerdictPoor:
		return "POOR"
	case VerdictUnusable:
		return "UNUSABLE"
	default:
		return "UNKNOWN"
	}
}

// Acceptable reports whether a route is fit to recommend.
func (v Verdict) Acceptable() bool {
	return v == VerdictGood || v == VerdictFair || v == VerdictPoor
}

// verdictFor grades a percentage loss against mid.
func verdictFor(lossPct decimal.Decimal) Verdict {
	switch {
	case lossPct.LessThanOrEqual(ThresholdGood):
		return VerdictGood
	case lossPct.LessThanOrEqual(ThresholdFair):
		return VerdictFair
	case lossPct.LessThanOrEqual(ThresholdPoor):
		return VerdictPoor
	default:
		return VerdictUnusable
	}
}

// Integrity describes the structural state of a corridor, which is a
// different question from how good its pricing is.
//
// # Why this is not another verdict
//
// The verdict scale grades a loss percentage. That works only when there is a
// price to grade, and the sister-corridor sweep of 2026-08-08 found two
// corridors where there is not — or where the price means something other
// than it appears to.
//
// KESC returned zero paths at every size from 0.1 to 5000 USDC. There is no
// loss percentage to compute, because there is no execution to measure.
// Reporting that as "Unusable" would put it in the same bucket as a corridor
// that prices continuously and prices badly, which is a materially different
// situation for anyone deciding whether to build on it.
//
// GHSC priced at every size, but every path at every size ran through NGNC:
// USDC -> XLM -> NGNC -> GHSC. It has no independent market. Its integrity is
// therefore bounded above by NGNC's, and any statement about GHSC that does
// not mention NGNC is incomplete. A loss figure alone hides that dependency
// entirely.
//
// So integrity is carried alongside the verdict rather than folded into it.
// Collapsing the two discards the reason a corridor failed, and the reason is
// the useful part.
type Integrity int

const (
	// IntegrityUnknown means the corridor's structure was not established,
	// normally because pricing failed for an unrelated reason.
	IntegrityUnknown Integrity = iota

	// IntegrityDirect means an independent market exists: at least one path
	// reaches the destination without routing through another fiat token.
	IntegrityDirect

	// IntegrityDerivative means every available path traverses another
	// fiat-pegged token, so the corridor inherits that token's liquidity and
	// failure modes on top of its own. DependsOn names what it depends on.
	IntegrityDerivative

	// IntegrityNoMarket means no path exists at all. This is the absence of
	// a price, not a bad one.
	IntegrityNoMarket
)

func (i Integrity) String() string {
	switch i {
	case IntegrityDirect:
		return "DIRECT"
	case IntegrityDerivative:
		return "DERIVATIVE"
	case IntegrityNoMarket:
		return "NO-MARKET"
	default:
		return "UNKNOWN"
	}
}

// Priceable reports whether the corridor has a market to measure at all.
func (i Integrity) Priceable() bool {
	return i == IntegrityDirect || i == IntegrityDerivative
}

// maxDependencyDepth bounds recursive dependency measurement. The Stellar
// protocol caps path length at 5 intermediate hops (XDR Asset path<5>),
// so the real depth can never exceed that. The cap exists to prevent a
// corrupted or expanded registry from turning one corridor request into
// unbounded Horizon fan-out.
const maxDependencyDepth = 5

// DependencyNode is one link in a dependency chain. It is a tree, not a
// flat list: a dependency can itself depend on other fiat tokens.
//
// Measured reports whether the node's Integrity was determined by an actual
// Horizon query. When Measured is false, Integrity is the zero value
// (IntegrityUnknown) and Reason explains why measurement did not happen.
// A consumer must never present an unmeasured node as though its integrity
// were a finding.
type DependencyNode struct {
	Asset        asset.Asset
	Integrity    Integrity
	Measured     bool
	Reason       string
	Dependencies []DependencyNode
}

// Kind identifies how a route delivers value.
type Kind string

const (
	// KindDEX settles entirely on-chain through path payments. It ends in
	// a token, not a bank account: someone must still redeem NGNC for
	// naira, which is a separate step with its own cost.
	KindDEX Kind = "dex"

	// KindAnchorSEP38 is a quote from an anchor's RFQ endpoint, which can
	// terminate in an actual bank account.
	KindAnchorSEP38 Kind = "anchor-sep38"
)

// Quote is one priced way to move the money.
type Quote struct {
	Kind        Kind
	Description string // e.g. "USDC -> XLM -> NGNC"
	Source      string // anchor domain, or "stellar-dex"

	// Hops are the intermediate assets the priced path routes through, in
	// execution order, between SendAsset and ReceiveAsset. Empty means a
	// direct market — the same fact dex.Path.Hops records, carried onto
	// the quote because Description cannot carry it: the arrow-joined
	// string is display text, and an asset code identifies nothing
	// without its issuer. The full path in execution order is SendAsset,
	// then Hops, then ReceiveAsset; QuoteJSON.Hops publishes exactly that
	// sequence (backlog #84, issue #100).
	Hops []asset.Asset

	SendAsset     asset.Asset
	SendAmount    decimal.Decimal
	ReceiveAsset  asset.Asset
	ReceiveAmount decimal.Decimal

	// EffectiveRate is receive units per send unit — the all-in rate the
	// user actually experiences, after every fee and spread.
	EffectiveRate decimal.Decimal

	// ReferenceMid is the independent mid-market rate for the same pair.
	ReferenceMid    decimal.Decimal
	ReferenceSource string

	// LossPct is how far below mid the effective rate falls, as a
	// percentage. This is the true all-in cost, and it captures the FX
	// spread that fee-only disclosures leave out.
	LossPct decimal.Decimal

	// LossAmount is LossPct applied to the send amount, expressed in the
	// receive asset — what the user gave up, in the currency the recipient
	// counts.
	LossAmount decimal.Decimal

	Verdict Verdict

	// PathCount is the number of distinct routes Horizon returned for this
	// size. The value is published alongside the selected quote so a client
	// can tell whether a route was unique or part of a fragmented market,
	// without re-deriving it from the raw Horizon response.
	PathCount int

	// Warnings are conditions the user must see before acting.
	Warnings []string

	// SEP38Quote carries the normalised anchor quote used to measure its
	// buy-asset-denominated fee in the cost decomposition.
	SEP38Quote *sep38.Quote

	QuotedAt time.Time
}

// score fills in the reference comparison and verdict.
func (q *Quote) score(mid decimal.Decimal, source string) {
	q.ReferenceMid = mid
	q.ReferenceSource = source

	if mid.IsZero() || q.SendAmount.IsZero() {
		q.Verdict = VerdictUnknown
		return
	}
	q.EffectiveRate = q.ReceiveAmount.Div(q.SendAmount)

	q.LossPct = mid.Sub(q.EffectiveRate).Div(mid).Mul(decimal.NewFromInt(100))

	// A route better than mid is possible in principle and is reported as
	// zero loss rather than a negative cost, since a negative "cost" reads
	// as profit and invites misplaced confidence in a thin market.
	if q.LossPct.IsNegative() {
		q.LossPct = decimal.Zero
	}

	atMid := q.SendAmount.Mul(mid)
	q.LossAmount = atMid.Sub(q.ReceiveAmount)
	if q.LossAmount.IsNegative() {
		q.LossAmount = decimal.Zero
	}

	q.Verdict = verdictFor(q.LossPct)
}

// Request describes the transfer a user wants priced.
type Request struct {
	SendAsset  asset.Asset
	SendAmount decimal.Decimal

	// ReceiveAsset is what the recipient ends up holding. For the on-chain
	// leg this is NGNC; for an anchor payout it is fiat NGN.
	ReceiveAsset asset.Asset

	// ReferenceBase and ReferenceQuote name the fiat pair to benchmark
	// against, e.g. USD and NGN. These are the real-world currencies behind
	// the tokens, since nobody publishes a mid-market rate for "USDC".
	ReferenceBase  string
	ReferenceQuote string
}

// Result is the engine's full answer.
type Result struct {
	Request Request
	Quotes  []Quote

	// Recommended is the best acceptable quote, or nil when every route is
	// Unusable. A nil Recommended alongside a non-empty Quotes is the
	// engine's way of saying "these exist, and you should take none of
	// them".
	Recommended *Quote

	ReferenceMid    decimal.Decimal
	ReferenceSource string
	ReferenceAsOf   time.Time

	// Reference is the full reference rate, including the second
	// provider's mid and how far the two diverged. Carried whole so a
	// caller can report which benchmark produced a verdict without the
	// engine having to flatten that into prose.
	Reference refrate.Rate

	// Parallel is the parallel/street-market reference, reported alongside
	// the official one and never blended into it. It is nil when the engine
	// has no parallel source configured at all — absent rather than
	// UNABLE-TO-DETERMINE, since a corridor that was never asked the question
	// is a different state from one whose source failed. When non-nil it is
	// REPORTED with a gap, or UNABLE-TO-DETERMINE with a reason. The official
	// verdict and every loss figure are scored without reference to it.
	Parallel *refrate.Parallel

	// Integrity is the corridor's structural state, independent of pricing.
	// A caller that reports only Quotes and Recommended will silently
	// misrepresent a no-market or derivative corridor.
	Integrity
}
