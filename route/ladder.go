package route

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// DefaultSizes is the ladder used when a caller does not specify one.
// It is defined in the dex package and shared with the depth metric so
// both measure the same corridor at the same sizes.
var DefaultSizes = dex.DefaultSizes

// ladderConcurrency bounds parallel pricing. Horizon is a shared public
// service and a ladder is a burst of identical queries, so this stays low
// enough to be a well-behaved client.
const ladderConcurrency = 4

// LadderRequest asks for one corridor priced across a range of sizes.
type LadderRequest struct {
	SendAsset    asset.Asset
	ReceiveAsset asset.Asset

	// Sizes are the send amounts to price. Empty means DefaultSizes.
	Sizes []decimal.Decimal

	ReferenceBase  string
	ReferenceQuote string
}

// Rung is one size's result within a ladder.
type Rung struct {
	SendAmount decimal.Decimal
	Result     *Result

	// MarginalCost is the change in effective receive-asset cost from the
	// previous valid priced rung to this rung. It is absent for the first
	// valid rung or when no previous valid rung exists.
	MarginalCost *MarginalCost

	// Decomposition breaks the rung's effective transfer cost into its
	// components. It is populated when the rung priced; an unpriced rung
	// carries an empty decomposition, which rendering omits entirely.
	Decomposition CostDecomposition

	// Err is set when this size alone failed to price, so one transient
	// failure does not discard the whole ladder.
	Err error
}

// Priced reports whether this rung produced a quote.
func (r Rung) Priced() bool {
	return r.Err == nil && r.Result != nil && len(r.Result.Quotes) > 0
}

// Unmeasured reports whether this rung learned anything at all about the
// corridor.
//
// Two shapes of rung learn nothing: one whose request failed before reaching
// an upstream (Err set), and one whose request never landed, recorded by
// Engine.Quote as a note rather than an error and arriving with no quotes and
// IntegrityUnknown — identical in shape to NO-MARKET, but with nothing
// learned either way. A rung Horizon answered, even "there is no path", is
// measured: NO-MARKET is a finding about the corridor, not an outage.
func (r Rung) Unmeasured() bool {
	return r.Err != nil || r.Result == nil || r.Result.Integrity == IntegrityUnknown
}

// LadderResult is a corridor measured across sizes.
type LadderResult struct {
	Request LadderRequest
	Rungs   []Rung

	// MarginalClassification describes whether adjacent marginal costs are
	// improving, flat, or worsening. It is undetermined when fewer than two
	// valid priced points exist.
	MarginalClassification MarginalClassification

	// Integrity is the corridor's structural state across the whole ladder,
	// which can be stronger than any single rung's. A corridor with no path
	// at one size might simply be too large for the pool; a corridor with no
	// path at any size has no market at all.
	Integrity Integrity
	DependsOn []asset.Asset

	ReferenceMid    decimal.Decimal
	ReferenceSource string

	// Reference carries the full benchmark, including the second
	// provider's mid and how far the two diverged.
	Reference refrate.Rate

	// Parallel carries the parallel/street-market reference reported
	// alongside the official one. Nil when no parallel source is configured.
	Parallel *refrate.Parallel

	// Floor is the loss percentage at the smallest size priced. It
	// approximates the corridor's cost with price impact removed.
	Floor decimal.Decimal

	// FloorSize is the size Floor was measured at.
	FloorSize decimal.Decimal

	// WorstLoss and WorstSize record the other end of the curve.
	WorstLoss decimal.Decimal
	WorstSize decimal.Decimal

	// Recommended is the best acceptable quote across every size, or nil
	// when no size produced one. Nil is the common case on a broken
	// corridor and must be rendered as "none", never as a best-effort pick.
	Recommended *Quote

	// RecommendedSize is the send amount Recommended was quoted at.
	RecommendedSize decimal.Decimal

	// Finding is a plain-language statement of what the ladder shows.
	Finding string
}

// Viable reports whether any size produced a recommendable route.
func (l *LadderResult) Viable() bool { return l.Recommended != nil }

// Failed reports that no size was measured at all, because every request
// failed before reaching an upstream.
//
// This is deliberately distinct from a corridor with no market. NO-MARKET is a
// finding: the request succeeded and Horizon answered that no path exists.
// Failed means the question was never put — and the two produce identical
// zero-valued figures, so a caller that did not separate them would publish
// "0.00% floor loss, nothing priced" as though it were a measurement of the
// corridor rather than a measurement of the network.
// The signal is the integrity state, not the error. Engine.Quote records an
// unreachable upstream as a note rather than an error, so a rung whose request
// never completed still arrives with Err nil and no quotes — identical in
// shape to NO-MARKET. What separates them is that Horizon answering "no path"
// yields IntegrityNoMarket, while a request that never landed leaves the state
// IntegrityUnknown, because nothing was learned about the corridor's structure.
//
// # What a half-measured ladder means
//
// Ladder returns a result even when only some rungs could be priced; it
// returns an error only when the run as a whole was impossible, such as a
// cancelled context. The contract for the in-between case:
//
//   - Every figure — Floor, WorstLoss, Recommended, the reference fields —
//     describes only the sizes that were measured. An unmeasured size
//     contributes nothing: its absence is unknown, never zero loss and never
//     no-market.
//   - Integrity still reflects every rung that learned something. One dead
//     size cannot erase a direct path found at another.
//   - PartiallyFailed reports the in-between state, UnmeasuredSizes names the
//     sizes nothing is known about, and the Finding states the qualification
//     in prose so no reader mistakes a partial curve for a complete one.
//   - Only Failed() — every rung unmeasured — means nothing was learned at
//     all, and any figure the result carries is a zero-value artefact rather
//     than a measurement.
func (l *LadderResult) Failed() bool {
	if len(l.Rungs) == 0 {
		return true
	}
	for _, r := range l.Rungs {
		if !r.Unmeasured() {
			return false
		}
	}
	return true
}

// PartiallyFailed reports whether some sizes were measured while others were
// not — the in-between case Failed does not cover.
//
// A half-measured ladder is a real measurement of the sizes that answered,
// qualified by the ones that did not; it is neither a clean run nor a failed
// one. Callers publishing figures from a ladder should consult this alongside
// Viable(): the floor of a corridor known at three of twelve sizes is a weaker
// claim than the same number on a full curve, and the Finding says so.
func (l *LadderResult) PartiallyFailed() bool {
	if len(l.Rungs) == 0 {
		return false
	}
	unmeasured := 0
	for _, r := range l.Rungs {
		if r.Unmeasured() {
			unmeasured++
		}
	}
	return unmeasured > 0 && unmeasured < len(l.Rungs)
}

// UnmeasuredSizes names the send amounts at which nothing was learned, in
// ascending order. Empty means every size produced information about the
// corridor.
func (l *LadderResult) UnmeasuredSizes() []decimal.Decimal {
	var out []decimal.Decimal
	for _, r := range l.Rungs {
		if r.Unmeasured() {
			out = append(out, r.SendAmount)
		}
	}
	return out
}

// Ladder prices a corridor at every size and summarises the curve.
//
// Individual sizes are priced concurrently but reported in ascending order,
// so the output is deterministic regardless of which request returned first.
//
// A size that cannot be priced leaves an unmeasured rung rather than failing
// the ladder: the returned result carries whatever the surviving sizes
// established, and Failed, PartiallyFailed and UnmeasuredSizes separate a
// full curve from a partial one from no measurement at all.
func (e *Engine) Ladder(ctx context.Context, req LadderRequest) (*LadderResult, error) {
	sizes := req.Sizes
	if len(sizes) == 0 {
		sizes = DefaultSizes
	}
	sorted := make([]decimal.Decimal, len(sizes))
	copy(sorted, sizes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LessThan(sorted[j]) })

	out := &LadderResult{
		Request: req,
		Rungs:   make([]Rung, len(sorted)),
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, ladderConcurrency)

	for i, size := range sorted {
		wg.Add(1)
		go func(i int, size decimal.Decimal) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := e.Quote(ctx, Request{
				SendAsset:      req.SendAsset,
				SendAmount:     size,
				ReceiveAsset:   req.ReceiveAsset,
				ReferenceBase:  req.ReferenceBase,
				ReferenceQuote: req.ReferenceQuote,
			})
			out.Rungs[i] = Rung{SendAmount: size, Result: res, Err: err}
		}(i, size)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out.summarise()
	return out, nil
}

// MarginalClassification describes the direction of marginal cost as size
// increases.
type MarginalClassification string

const (
	MarginalImproving    MarginalClassification = "improving"
	MarginalFlat         MarginalClassification = "flat"
	MarginalWorsening    MarginalClassification = "worsening"
	MarginalUndetermined MarginalClassification = "undetermined"
)

// MarginalCost is the effective cost difference between adjacent valid points.
type MarginalCost struct {
	From decimal.Decimal
	To   decimal.Decimal
	Cost decimal.Decimal
}

// computeMarginalCosts computes adjacent costs without treating missing
// rungs as zero. The first valid point establishes the baseline; each later
// valid point is compared with it, even when invalid points occur between them.
func (l *LadderResult) computeMarginalCosts() {
	var previous *Rung
	var previousCost decimal.Decimal
	var marginal []decimal.Decimal
	for i := range l.Rungs {
		r := &l.Rungs[i]
		if !r.Priced() {
			continue
		}
		cost := r.SendAmount.Mul(l.ReferenceMid).Sub(r.Result.Quotes[0].ReceiveAmount)
		if previous != nil {
			r.MarginalCost = &MarginalCost{From: previous.SendAmount, To: r.SendAmount, Cost: cost.Sub(previousCost)}
			marginal = append(marginal, r.MarginalCost.Cost)
		}
		previous = r
		previousCost = cost
	}
	if len(marginal) == 0 {
		l.MarginalClassification = MarginalUndetermined
		return
	}
	const tolerance = "0.0000001"
	tol := decimal.RequireFromString(tolerance)
	improving, worsening := false, false
	for i := 1; i < len(marginal); i++ {
		delta := marginal[i].Sub(marginal[i-1])
		if delta.LessThan(tol.Neg()) {
			improving = true
		}
		if delta.GreaterThan(tol) {
			worsening = true
		}
	}
	switch {
	case improving && !worsening:
		l.MarginalClassification = MarginalImproving
	case worsening && !improving:
		l.MarginalClassification = MarginalWorsening
	default:
		l.MarginalClassification = MarginalFlat
	}
}

func (l *LadderResult) summarise() {
	var (
		anyPriced   bool
		anyDirect   bool
		allNoMarket = true
		deps        = map[string]asset.Asset{}
		firstErr    error
	)

	for i := range l.Rungs {
		r := l.Rungs[i]
		if r.Err != nil {
			if firstErr == nil {
				firstErr = r.Err
			}
			// A rung that errored says nothing about market structure.
			allNoMarket = false
			continue
		}
		if r.Result == nil {
			continue
		}

		if l.ReferenceMid.IsZero() {
			l.ReferenceMid = r.Result.ReferenceMid
			l.ReferenceSource = r.Result.ReferenceSource
			l.Reference = r.Result.Reference
			l.Parallel = r.Result.Parallel
		}

		switch r.Result.Integrity {
		case IntegrityDirect:
			anyDirect = true
			allNoMarket = false
		case IntegrityDerivative:
			allNoMarket = false
			for _, d := range r.Result.DependsOn {
				deps[d.Code+":"+d.Issuer] = d
			}
		case IntegrityNoMarket:
			// leaves allNoMarket intact
		default:
			allNoMarket = false
		}

		if !r.Priced() {
			continue
		}
		anyPriced = true
		q := r.Result.Quotes[0]

		// The decomposition answers "where did the money go" for this
		// size — the per-quote component breakdown behind the single loss
		// percentage, computed against the corridor's reference mid.
		l.Rungs[i].Decomposition = Decompose(q, l.ReferenceMid)

		if l.FloorSize.IsZero() {
			l.Floor, l.FloorSize = q.LossPct, r.SendAmount
		}
		if q.LossPct.GreaterThanOrEqual(l.WorstLoss) {
			l.WorstLoss, l.WorstSize = q.LossPct, r.SendAmount
		}

		// The first acceptable quote on an ascending ladder is also the
		// one at the smallest size, which is the honest one to surface:
		// larger sizes on a thin corridor only get worse.
		if l.Recommended == nil && r.Result.Recommended != nil {
			l.Recommended = r.Result.Recommended
			l.RecommendedSize = r.SendAmount
		}
	}

	switch {
	case allNoMarket && !anyPriced:
		l.Integrity = IntegrityNoMarket
	case anyDirect:
		l.Integrity = IntegrityDirect
	case len(deps) > 0:
		l.Integrity = IntegrityDerivative
		l.DependsOn = make([]asset.Asset, 0, len(deps))
		for _, a := range deps {
			l.DependsOn = append(l.DependsOn, a)
		}
		sort.Slice(l.DependsOn, func(i, j int) bool {
			return l.DependsOn[i].Code < l.DependsOn[j].Code
		})
	default:
		l.Integrity = IntegrityUnknown
	}

	l.computeMarginalCosts()
	l.Finding = l.finding(anyPriced, firstErr)
}

// finding states what the ladder shows, in the terms a reader needs.
//
// Every branch here describes a measurement. None of them recommends, and the
// no-market and derivative cases are never folded into a loss percentage,
// because the whole point of the taxonomy is that those are different results.
//
// A partially-measured ladder keeps its headline and carries the gap as a
// qualification appended to it — the figures are real, but a reader must be
// able to tell a full curve from a partial one.
func (l *LadderResult) finding(anyPriced bool, firstErr error) string {
	send, recv := l.Request.SendAsset.Code, l.Request.ReceiveAsset.Code

	if l.Integrity == IntegrityNoMarket {
		return fmt.Sprintf(
			"No market. Horizon returned no path from %s to %s at any of the %d "+
				"sizes tested. This is the absence of a price, not a bad price: "+
				"the corridor cannot be executed at all.",
			send, recv, len(l.Rungs))
	}

	if !anyPriced {
		if firstErr != nil {
			return fmt.Sprintf("Could not price %s to %s: %v", send, recv, firstErr)
		}
		return fmt.Sprintf("Could not price %s to %s at any size tested.", send, recv)
	}

	var prefix string
	if l.Integrity == IntegrityDerivative {
		prefix = fmt.Sprintf(
			"Derivative corridor: every path from %s to %s routes through %s, so "+
				"%s has no independent market and these figures compound %s's cost "+
				"with its own. ",
			send, recv, describeAssets(l.DependsOn), recv, describeAssets(l.DependsOn))
	}

	var body string
	if l.Viable() {
		body = fmt.Sprintf(
			"Best available: %s%% below the %s mid at %s %s, graded %s. "+
				"Loss reaches %s%% at %s %s.",
			l.Recommended.LossPct.StringFixed(2), l.ReferenceSource,
			l.RecommendedSize, send, l.Recommended.Verdict,
			l.WorstLoss.StringFixed(2), l.WorstSize, send)
	} else {
		body = fmt.Sprintf(
			"No usable size. Loss against the %s mid is %s%% at %s %s — where price "+
				"impact is negligible, so that is the corridor's structural floor, not "+
				"a depth effect — rising to %s%% at %s %s. Every size tested is graded "+
				"Unusable, so nothing is recommended.",
			l.ReferenceSource, l.Floor.StringFixed(2), l.FloorSize, send,
			l.WorstLoss.StringFixed(2), l.WorstSize, send)
	}

	return prefix + body + l.partialQualification()
}

// partialQualification names the sizes a partially-measured ladder says
// nothing about. It qualifies the headline; it never rewrites one.
func (l *LadderResult) partialQualification() string {
	sizes := l.UnmeasuredSizes()
	if len(sizes) == 0 {
		return ""
	}
	shown := make([]string, 0, len(sizes))
	for _, s := range sizes {
		shown = append(shown, s.String())
	}
	return fmt.Sprintf(
		" %d of %d sizes could not be measured (%s), so every figure here "+
			"describes only the sizes that were.",
		len(sizes), len(l.Rungs), strings.Join(shown, ", "))
}
