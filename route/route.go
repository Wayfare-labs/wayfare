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

	// Warnings are conditions the user must see before acting.
	Warnings []string

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
	Integrity Integrity

	// DependsOn names the fiat tokens every path traverses, when Integrity
	// is IntegrityDerivative. Empty otherwise.
	DependsOn []asset.Asset

	// Notes record corridor-level facts that no single quote captures.
	Notes []string
}

// Viable reports whether any route is worth recommending.
func (r *Result) Viable() bool { return r.Recommended != nil }

// Engine prices and ranks routes.
type Engine struct {
	DEX     *dex.Client
	RefRate refrate.Provider

	// Parallel is an optional parallel/street-market rate source, reported as
	// a second reference dimension alongside RefRate. It never feeds a
	// verdict: the official rate remains the only thing routes are scored
	// against. Nil leaves the parallel dimension off the result entirely.
	Parallel refrate.Provider

	// ProbeAmount sizes the slippage probe. Defaults to 10 send units.
	ProbeAmount decimal.Decimal
}

// Quote prices every available route for the request and ranks them.
func (e *Engine) Quote(ctx context.Context, req Request) (*Result, error) {
	if e.RefRate == nil {
		return nil, fmt.Errorf("route: a reference rate provider is required; " +
			"without one the engine can rank routes but cannot tell a good one from a bad one")
	}
	if req.SendAmount.IsZero() || req.SendAmount.IsNegative() {
		return nil, fmt.Errorf("route: send amount must be positive, got %s", req.SendAmount)
	}

	ref, err := e.RefRate.Rate(ctx, req.ReferenceBase, req.ReferenceQuote)
	if err != nil {
		return nil, fmt.Errorf("route: reference rate unavailable: %w", err)
	}

	// A benchmark the providers cannot agree on is not a benchmark. Scoring
	// against either mid would produce a loss figure whose value depends on
	// which feed was believed, which is closer to fabricated than measured —
	// so the corridor is reported with no verdict and the reason attached,
	// rather than with a number that looks like a measurement.
	if !ref.Scorable() {
		return e.unscored(ctx, req, ref)
	}

	res := &Result{
		Request:         req,
		ReferenceMid:    ref.Mid,
		ReferenceSource: ref.Source,
		ReferenceAsOf:   ref.AsOf,
		Reference:       ref,
	}
	if e.Parallel != nil {
		p := refrate.ParallelAgainst(ctx, e.Parallel, ref)
		res.Parallel = &p
	}

	if e.DEX != nil {
		d, err := e.quoteDEX(ctx, req, ref)
		switch {
		case err != nil:
			res.Notes = append(res.Notes, "DEX route unavailable: "+err.Error())
		default:
			res.Integrity = d.integrity
			res.DependsOn = d.dependsOn
			res.Notes = append(res.Notes, unknownHopNote(d.unknownHops)...)
			if d.quote != nil {
				res.Quotes = append(res.Quotes, *d.quote)
			}
		}
	}

	switch res.Integrity {
	case IntegrityNoMarket:
		res.Notes = append(res.Notes, fmt.Sprintf(
			"No market: Horizon returned no path from %s to %s at this size. "+
				"This is the absence of a price, not a bad one.",
			req.SendAsset.Code, req.ReceiveAsset.Code))
	case IntegrityDerivative:
		res.Notes = append(res.Notes, fmt.Sprintf(
			"Derivative corridor: every available path to %s routes through %s. "+
				"There is no independent market, so this corridor carries %s's "+
				"liquidity and failure modes in addition to its own.",
			req.ReceiveAsset.Code, describeAssets(res.DependsOn),
			describeAssets(res.DependsOn)))
	}

	sort.SliceStable(res.Quotes, func(i, j int) bool {
		return res.Quotes[i].ReceiveAmount.GreaterThan(res.Quotes[j].ReceiveAmount)
	})

	for i := range res.Quotes {
		if res.Quotes[i].Verdict.Acceptable() {
			res.Recommended = &res.Quotes[i]
			break
		}
	}

	if len(res.Quotes) > 0 && res.Recommended == nil {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"No viable route. The best of %d priced route(s) still loses %s%% against "+
				"the %s mid-market rate. Sending through this corridor at this size is "+
				"not recommended.",
			len(res.Quotes), res.Quotes[0].LossPct.StringFixed(1), ref.Source))
	}
	// The no-market note already explains an empty result more precisely, so
	// this covers only the other ways pricing can come back empty.
	if len(res.Quotes) == 0 && res.Integrity != IntegrityNoMarket {
		res.Notes = append(res.Notes, "No route could be priced for this corridor.")
	}
	return res, nil
}

// unscored reports a corridor whose benchmark could not be trusted.
//
// The routes are still discovered and their structure still classified — that
// part does not depend on the reference rate, and integrity is exactly the
// thing worth reporting when pricing cannot be. What is withheld is every
// verdict, the loss figures, and any recommendation.
func (e *Engine) unscored(ctx context.Context, req Request, ref refrate.Rate) (*Result, error) {
	res := &Result{
		Request:         req,
		ReferenceMid:    ref.Mid,
		ReferenceSource: ref.Source,
		ReferenceAsOf:   ref.AsOf,
		Reference:       ref,
	}
	if e.Parallel != nil {
		p := refrate.ParallelAgainst(ctx, e.Parallel, ref)
		res.Parallel = &p
	}

	if e.DEX != nil {
		if d, err := e.quoteDEX(ctx, req, ref); err == nil {
			res.Integrity = d.integrity
			res.DependsOn = d.dependsOn
			res.Notes = append(res.Notes, unknownHopNote(d.unknownHops)...)
			if d.quote != nil {
				// Carried for its route description and receive amount;
				// score() left the verdict Unknown because the mid is not
				// usable, and nothing downstream may promote it.
				q := *d.quote
				q.Verdict = VerdictUnknown
				q.LossPct = decimal.Zero
				q.LossAmount = decimal.Zero
				res.Quotes = append(res.Quotes, q)
			}
		} else {
			res.Notes = append(res.Notes, "DEX route unavailable: "+err.Error())
		}
	}

	note := ref.Note
	if note == "" {
		note = "the reference providers disagreed too far apart to score against either"
	}
	res.Notes = append(res.Notes, "No verdict: "+note)
	return res, nil
}

// dexResult carries the corridor's structure alongside its price. Both come
// from the same set of paths, and separating the calls would risk classifying
// one snapshot of the market while pricing another.
type dexResult struct {
	quote     *Quote
	integrity Integrity
	dependsOn []asset.Asset

	// unknownHops are the intermediate assets pathfinding routed through
	// that are neither native XLM nor registered in the asset registry. They
	// are carried so the caller can publish the coverage gap (see
	// unknownHopNote); they never influence the classification itself.
	unknownHops []asset.Asset
}

// describeAssets renders a list of assets for a human-readable note.
func describeAssets(as []asset.Asset) string {
	if len(as) == 0 {
		return "another fiat token"
	}
	codes := make([]string, len(as))
	for i, a := range as {
		codes[i] = a.Code
	}
	return strings.Join(codes, " and ")
}

// unknownHopNote renders the coverage finding for hops pathfinding routed
// through that the asset registry does not know. An empty list produces no
// note.
//
// The note exists because the classification treats an unregistered hop as
// evidence of an independent market — the documented false-negative in
// asset/known.go — and that decision must be visible wherever a corridor is
// reported, not buried in the registry.
func unknownHopNote(unknown []asset.Asset) []string {
	if len(unknown) == 0 {
		return nil
	}
	names := make([]string, len(unknown))
	for i, a := range unknown {
		names[i] = a.String()
	}
	return []string{fmt.Sprintf(
		"Unregistered hop asset(s) %s appeared in pathfinding and are not in the "+
			"asset registry. An unrecognised hop is currently treated as having an "+
			"independent market; see asset/known.go for the bounded false-negative.",
		strings.Join(names, ", "))}
}

// classify determines corridor integrity from the complete set of paths.
//
// The claim "reachable only through another fiat token" is about every path,
// not the best one, so this examines all of them: a single path that avoids
// fiat intermediaries is enough to prove an independent market exists.
//
// The third return value is the set of hop assets that are neither native
// XLM nor registered — the coverage gap. It never affects the integrity
// classification: an unknown hop is treated exactly like a bridge hop, which
// is the known, bounded false-negative documented in asset/known.go. It is
// returned so the caller can surface it rather than let it stay silent.
func classify(paths []dex.Path, dest asset.Asset) (Integrity, []asset.Asset, []asset.Asset) {
	if len(paths) == 0 {
		return IntegrityNoMarket, nil, nil
	}

	seen := map[string]asset.Asset{}
	unknown := map[string]asset.Asset{}
	independent := false

	for _, p := range paths {
		fiatHops := 0
		for _, h := range p.Hops {
			// The destination appearing as its own hop is not a dependency.
			if h.Code == dest.Code && h.Issuer == dest.Issuer {
				continue
			}
			switch asset.ClassifyHop(h) {
			case asset.HopFiat:
				fiatHops++
				seen[h.Code+":"+h.Issuer] = h
			case asset.HopBridge:
				// Native XLM, or a registered non-fiat token: deliberately
				// not a fiat dependency. See asset/known.go.
			default:
				// Unregistered hop: the false-negative documented in
				// asset/known.go. Not a fiat dependency, and collected so
				// the coverage gap is visible instead of silent.
				unknown[h.Code+":"+h.Issuer] = h
			}
		}
		if fiatHops == 0 {
			independent = true
		}
	}

	unknownList := make([]asset.Asset, 0, len(unknown))
	for _, a := range unknown {
		unknownList = append(unknownList, a)
	}
	sort.Slice(unknownList, func(i, j int) bool { return unknownList[i].Code < unknownList[j].Code })

	if independent {
		return IntegrityDirect, nil, unknownList
	}

	deps := make([]asset.Asset, 0, len(seen))
	for _, a := range seen {
		deps = append(deps, a)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Code < deps[j].Code })
	return IntegrityDerivative, deps, unknownList
}

// quoteDEX prices the on-chain leg via Horizon pathfinding, and classifies
// the corridor's structure from the same set of paths.
func (e *Engine) quoteDEX(ctx context.Context, req Request, ref refrate.Rate) (*dexResult, error) {
	paths, err := e.DEX.StrictSendPaths(ctx, req.SendAsset, req.SendAmount, req.ReceiveAsset)
	if err != nil {
		return nil, err
	}

	integrity, dependsOn, unknownHops := classify(paths, req.ReceiveAsset)
	if integrity == IntegrityNoMarket {
		return &dexResult{integrity: integrity}, nil
	}

	// Horizon generally returns its best path first, but that is not a
	// documented guarantee, so the maximum is selected explicitly.
	best := paths[0]
	for _, p := range paths[1:] {
		if p.DestAmount.GreaterThan(best.DestAmount) {
			best = p
		}
	}

	q := &Quote{
		Kind:          KindDEX,
		Description:   best.Describe(),
		Source:        "stellar-dex",
		SendAsset:     req.SendAsset,
		SendAmount:    req.SendAmount,
		ReceiveAsset:  req.ReceiveAsset,
		ReceiveAmount: best.DestAmount,
		QuotedAt:      time.Now(),
	}
	q.score(ref.Mid, ref.Source)

	// The on-chain leg stops at a token. Saying so matters: a user
	// comparing this against a bank-payout quote is otherwise comparing
	// two different products, and the redemption step has its own cost
	// that this figure does not include.
	if req.ReceiveAsset.Kind == asset.KindStellar {
		payout := "fiat"
		if peg, ok := asset.FiatPeg(req.ReceiveAsset); ok {
			payout = peg
		}
		q.Warnings = append(q.Warnings, fmt.Sprintf(
			"delivers %s tokens, not %s in a bank account; redeeming to fiat is a separate step with its own cost",
			req.ReceiveAsset.Code, payout))
	}

	// A derivative corridor's price is not its own. Carrying this on the
	// quote as well as the result means a caller rendering a single route
	// cannot present the number without the dependency attached to it.
	if integrity == IntegrityDerivative {
		q.Warnings = append(q.Warnings, fmt.Sprintf(
			"derivative corridor: every path routes through %s, so this rate "+
				"compounds %s's loss with this corridor's own",
			describeAssets(dependsOn), describeAssets(dependsOn)))
	}

	probe := e.ProbeAmount
	if probe.IsZero() {
		probe = decimal.NewFromInt(10)
	}
	if req.SendAmount.GreaterThan(probe) {
		if s, err := e.DEX.MeasureSlippage(ctx, req.SendAsset, req.SendAmount, req.ReceiveAsset, probe); err == nil {
			if s.PctWorse.GreaterThan(decimal.NewFromInt(2)) {
				q.Warnings = append(q.Warnings, fmt.Sprintf(
					"thin liquidity: this size gets %s%% worse pricing than a %s %s trade",
					s.PctWorse.StringFixed(1), probe, req.SendAsset.Code))
			}
		}
	}
	return &dexResult{quote: q, integrity: integrity, dependsOn: dependsOn, unknownHops: unknownHops}, nil
}
