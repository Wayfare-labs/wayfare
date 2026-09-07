// CostDecomposition breaks the effective transfer cost into separately-reported
// components: FX loss, fees, slippage, and expected failure cost.
//
// Currently, the verdict reports a single loss percentage against fair value.
// That number is useful but opaque. Showing the decomposition turns a single
// verdict into actionable information.
//
// Each component is computed and reported independently. Expected failure cost
// stays explicitly unknown until failure history exists.
package route

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// CostComponent names one piece of the effective transfer cost.
type CostComponent string

const (
	CostFXLoss          CostComponent = "fx_loss"
	CostFees            CostComponent = "fees"
	CostSlippage        CostComponent = "slippage"
	CostExpectedFailure CostComponent = "expected_failure"
)

// CostPart is one component of the effective transfer cost.
type CostPart struct {
	Component  CostComponent
	Amount     decimal.Decimal
	Pct        decimal.Decimal
	Determined bool
	Reason     string
}

// CostDecomposition is the full breakdown of a route's effective transfer cost.
type CostDecomposition struct {
	Parts        []CostPart
	TotalLossPct decimal.Decimal
}

// SlippageMeasurement is one size's slippage against the corridor's
// structural floor: either the measured excess loss, or the reason it could
// not be measured.
//
// The floor is the loss measured at the smallest priced size, where price
// impact is negligible, and slippage is what the size added on top of it.
// A determined zero is a real measurement here — it is what the floor rung
// itself measures, by definition — and must never be conflated with the
// undetermined case, where no comparison across sizes was possible at all.
type SlippageMeasurement struct {
	Determined bool

	// Reason is set only when Determined is false, and names the shortfall:
	// what was missing that would have established the value.
	Reason string

	// Pct is the excess loss over the floor, in percentage points, and
	// Amount is that excess in receive-asset units. Both are set only when
	// Determined is true; a negative Pct means the size priced better than
	// the floor rung, which is a finding about the corridor, not an error.
	Pct    decimal.Decimal
	Amount decimal.Decimal
}

// UnmeasuredSlippage records why a size's slippage could not be measured.
// The reason must name the shortfall — what was missing, not merely that the
// value is unknown.
func UnmeasuredSlippage(reason string) SlippageMeasurement {
	return SlippageMeasurement{Determined: false, Reason: reason}
}

// floorSlippage is the smallest priced rung's slippage: a determined zero.
//
// The floor rung has, by definition, no measurable slippage — it IS the
// floor every other rung is measured against. That zero is a measurement,
// arrived at by definition, and is reported as determined so no reader
// mistakes the one figure the ladder established outright for a gap.
func floorSlippage() SlippageMeasurement {
	return SlippageMeasurement{Determined: true}
}

// measuredSlippage records one size's excess loss over the floor.
//
// Amount is derived against the same fair value LossAmount is: fair value is
// send times mid, the floor's share of that fair value is fair times the
// floor's loss percentage, and what this size added on top is the difference
// between its own LossAmount and that share.
func measuredSlippage(q Quote, floorLossPct, mid decimal.Decimal) SlippageMeasurement {
	pct := q.LossPct.Sub(floorLossPct)
	fair := q.SendAmount.Mul(mid)
	floorShare := fair.Mul(floorLossPct).Shift(-2) // × floorPct/100, exact
	return SlippageMeasurement{
		Determined: true,
		Pct:        pct,
		Amount:     q.LossAmount.Sub(floorShare),
	}
}

// Decompose splits a priced route's effective transfer cost into components.
//
// slip is the size's slippage against the corridor's structural floor,
// measured by DecomposeLadder, where the neighbouring sizes are visible; a
// lone quote structurally cannot see one. Pass UnmeasuredSlippage when no
// comparison was available, and floorSlippage for the smallest priced rung
// itself, whose zero is a determined measurement, not a gap.
func Decompose(q Quote, mid decimal.Decimal, slip SlippageMeasurement) CostDecomposition {
	parts := make([]CostPart, 0, 4)

	// FX loss: the structural component of the loss against mid. When
	// slippage is measured, the fx component carries the loss minus what
	// depth added — the corridor's structural floor — so the determined
	// components partition the total instead of overlapping it: floor plus
	// slippage is the whole loss. When slippage is undetermined, nothing
	// has yet attributed any share of the loss, so fx carries all of it
	// rather than leaving the unattributed remainder nowhere.
	fxLossPct := q.LossPct
	fxLossAmount := q.LossAmount
	if slip.Determined {
		fxLossPct = q.LossPct.Sub(slip.Pct)
		fxLossAmount = q.LossAmount.Sub(slip.Amount)
	}
	parts = append(parts, CostPart{
		Component:  CostFXLoss,
		Amount:     fxLossAmount,
		Pct:        fxLossPct,
		Determined: true,
	})

	// Fees: undetermined. A Stellar path payment charges a base fee per
	// operation, and a multi-hop path is more operations than a direct one,
	// but Decompose sees only a Quote and has neither the path's operation
	// count nor a currently-effective base fee. Naming the gap keeps the
	// units honest: unavailable is unknown, not a default, and small is not
	// zero.
	parts = append(parts, CostPart{
		Component:  CostFees,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "network fee not measured; determining it requires the path's operation count and the current Stellar base fee",
	})

	// Slippage: determined only where a comparison across sizes was
	// possible. The floor rung's determined zero and an unmeasured size's
	// undetermined reason are different facts and must never blur: one says
	// the ladder measured no depth effect at this size, the other says the
	// ladder could not measure depth here at all.
	if slip.Determined {
		parts = append(parts, CostPart{
			Component:  CostSlippage,
			Amount:     slip.Amount,
			Pct:        slip.Pct,
			Determined: true,
		})
	} else {
		parts = append(parts, CostPart{
			Component:  CostSlippage,
			Amount:     decimal.Zero,
			Pct:        decimal.Zero,
			Determined: false,
			Reason:     slip.Reason,
		})
	}

	// Expected failure cost: explicitly unknown.
	parts = append(parts, CostPart{
		Component:  CostExpectedFailure,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "no failure history exists yet; runstore is collecting but has not accumulated enough observations",
	})

	return CostDecomposition{
		Parts:        parts,
		TotalLossPct: q.LossPct,
	}
}

// DecomposeLadder decomposes every rung of a ladder in one pass, so slippage
// can be measured where the comparison lives.
//
// Slippage at a size is the excess loss over the corridor's structural
// floor. The floor is the loss at the smallest priced size, where price
// impact is negligible: whatever loss remains there is the corridor's spread
// and fixed cost, not its depth, and slippage at every other priced size is
// what the size added on top of that floor:
//
//	slippage(q) = loss(q) − loss(smallest priced size)
//
// That definition is the one the project's founding measurement argues for —
// 25% loss at 0.1 USDC is the structural floor, 97.68% at 5000 is the floor
// plus slippage — and it can only be computed here, at the ladder level,
// where both sizes are in hand. Decomposing a lone quote structurally cannot
// see a neighbour, which is why the per-quote Decompose used to report
// slippage as permanently undetermined while the ladder held the comparison
// all along.
//
//   - With at least two priced rungs, slippage is determined for every
//     priced rung. The floor rung's slippage is a determined zero, not an
//     undetermined one: zero is what it measures, by definition.
//   - With fewer than two priced rungs, slippage stays undetermined with a
//     reason naming the shortfall.
//   - Only measured sizes are used. No curve is interpolated between rungs,
//     and a size that priced better than the floor rung reports negative
//     slippage as measured rather than clamped — a larger size opening a
//     better path is a finding about the corridor, not an error to smooth
//     away.
//
// The returned slice is aligned with rungs. Unpriced rungs carry the zero
// decomposition, which rendering omits entirely.
func DecomposeLadder(rungs []Rung, mid decimal.Decimal) []CostDecomposition {
	out := make([]CostDecomposition, len(rungs))

	floorIdx := -1
	priced := 0
	for i, r := range rungs {
		if !r.Priced() {
			continue
		}
		priced++
		if floorIdx < 0 {
			floorIdx = i
		}
	}
	if floorIdx < 0 {
		// Nothing priced anywhere: there is no quote to decompose, and an
		// empty decomposition for every rung is what rendering omits.
		return out
	}

	floorLossPct := rungs[floorIdx].Result.Quotes[0].LossPct
	unmeasuredReason := UnmeasuredSlippage(fmt.Sprintf(
		"only %d of %d sizes priced; measuring slippage requires at least two priced sizes, so the structural floor cannot be separated from the depth effect",
		priced, len(rungs)))

	for i, r := range rungs {
		if !r.Priced() {
			continue
		}
		q := r.Result.Quotes[0]

		var slip SlippageMeasurement
		switch {
		case priced < 2:
			// The shortfall case is checked first: with a single priced rung
			// that rung is trivially the smallest, but a floor one size wide
			// is indistinguishable from the total loss, so there is no
			// measured slippage to report — only the shortfall.
			slip = unmeasuredReason
		case i == floorIdx:
			slip = floorSlippage()
		default:
			slip = measuredSlippage(q, floorLossPct, mid)
		}
		out[i] = Decompose(q, mid, slip)
	}
	return out
}
