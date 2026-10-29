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

	// Curve is the measured effective-rate relationship across priced and
	// unpriced rungs. Unpriced rungs remain explicit holes; no interpolation
	// or monotonicity assumption is applied.
	Curve *ExecutionRateCurve
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

	// Chain is the full dependency tree when the corridor is derivative.
	// It is the union across all rungs: if any rung discovered additional
	// dependencies, they appear here, and when the same dependency was
	// measured on some rungs but unmeasured on others the measured node
	// wins — one rung's failed request never erases another's finding.
	// Nil when the corridor is not derivative.
	Chain []DependencyNode

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

	// FloorEffectiveRate is the effective rate at the smallest priced rung.
	FloorEffectiveRate decimal.Decimal

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

// ExecutionRatePoint is one measured ladder rung. Rate is present only when
// the rung priced; an unpriced point is represented by a hole and its reason.
type ExecutionRatePoint struct {
	Size   decimal.Decimal
	Rate   decimal.Decimal
	Priced bool
	Reason string
}

// ExecutionRateCurve publishes the effective rate by measured size.
type ExecutionRateCurve struct {
	Points           []ExecutionRatePoint
	PricedCount      int
	ObservationCount int
	NonMonotonic     bool
}

// buildCurve preserves the measured ladder faithfully. Rates are compared in
// ascending size order; a missing rung neither contributes a zero nor breaks
// the non-monotonicity check between adjacent priced observations.
func (l *LadderResult) buildCurve() *ExecutionRateCurve {
	curve := &ExecutionRateCurve{Points: make([]ExecutionRatePoint, 0, len(l.Rungs))}
	var previous decimal.Decimal
	var havePrevious bool
	for _, r := range l.Rungs {
		point := ExecutionRatePoint{Size: r.SendAmount, Reason: rungReason(r)}
		if r.Priced() {
			point.Priced = true
			point.Rate = r.Result.Quotes[0].EffectiveRate
			curve.PricedCount++
			if havePrevious && point.Rate.GreaterThan(previous) {
				curve.NonMonotonic = true
			}
			previous = point.Rate
			havePrevious = true
		}
		curve.Points = append(curve.Points, point)
	}
	curve.ObservationCount = curve.PricedCount
	if curve.PricedCount < 2 {
		return nil
	}
	return curve
}

func rungReason(r Rung) string {
	if r.Err != nil {
		return r.Err.Error()
	}
	if r.Result == nil {
		return "no result"
	}
	if len(r.Result.Notes) > 0 {
		return strings.Join(r.Result.Notes, "; ")
	}
	return "no quote priced"
}

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

// CompletedSizes names the send amounts that were priced, in ascending order.
func (l *LadderResult) CompletedSizes() []decimal.Decimal {
	var out []decimal.Decimal
	for _, r := range l.Rungs {
		if r.Priced() {
			out = append(out, r.SendAmount)
		}
	}
	return out
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
		// The partial result is returned alongside the error so a caller can
		// report what was measured before cancellation.
		return out, err
	}
	}
}
