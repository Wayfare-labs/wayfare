package route

import (
	"sort"

	"github.com/shopspring/decimal"
)

// AdaptiveRule allows a ladder to price extra midpoint rungs where the
// measured curve bends sharply. It is opt-in: a nil *AdaptiveRule on a
// LadderRequest leaves the ladder exactly as it was.
//
// The rule never interpolates a loss or a rate. It only chooses a size that
// was not in the requested ladder, and that size is quoted by the same
// pathfinder as every other rung. An added rung is marked Adaptive so a reader
// can tell a fixed rung from one this rule chose.
type AdaptiveRule struct {
	// MaxExtraRungs is the most rungs this rule may add. Values above
	// MaxAdaptiveExtraRungs are capped, so a caller cannot make a ladder's
	// request cost unbounded by passing a large number. Values at or below
	// zero disable the rule.
	MaxExtraRungs int

	// ThresholdPct is the smallest adjacent loss difference, in percentage
	// points, that triggers an extra rung. The rule is "more than", not
	// "at least", so a difference exactly equal to the threshold adds
	// nothing. A non-positive threshold disables the rule.
	ThresholdPct decimal.Decimal
}

const (
	// DefaultAdaptiveMaxExtraRungs bounds the default rule at four extra
	// rungs: a third more than the twelve-rung default ladder, which keeps
	// the request cost bounded while still resolving the sharpest bends.
	DefaultAdaptiveMaxExtraRungs = 4

	// MaxAdaptiveExtraRungs is the hard ceiling on added rungs, whatever a
	// caller asks for. It is the request-cost bound: a ladder can never
	// cost more than len(requested sizes) + MaxAdaptiveExtraRungs rung
	// quotes because of adaptive sampling.
	MaxAdaptiveExtraRungs = 8
)

// DefaultAdaptiveThresholdPct is the default trigger: a five percentage-point
// jump between adjacent measured losses. At 5 points the curve has crossed the
// width of the FAIR verdict band, so the bend is material to a sender choosing
// an amount rather than noise between two neighbouring sizes.
var DefaultAdaptiveThresholdPct = decimal.NewFromInt(5)

// DefaultAdaptiveRule returns the rule used when a producer exposes adaptive
// sampling as a simple on/off option.
func DefaultAdaptiveRule() AdaptiveRule {
	return AdaptiveRule{
		MaxExtraRungs: DefaultAdaptiveMaxExtraRungs,
		ThresholdPct:  DefaultAdaptiveThresholdPct,
	}
}

// active reports whether the rule can add anything. An unusable rule is
// treated as absent rather than as an error: adaptive sampling is optional,
// and a zero-valued rule must not turn a valid ladder into a failed one.
func (r AdaptiveRule) active() bool {
	return r.MaxExtraRungs > 0 && r.ThresholdPct.IsPositive()
}

// extraLimit is the rule's effective maximum after the hard ceiling.
func (r AdaptiveRule) extraLimit() int {
	if !r.active() {
		return 0
	}
	if r.MaxExtraRungs > MaxAdaptiveExtraRungs {
		return MaxAdaptiveExtraRungs
	}
	return r.MaxExtraRungs
}

// adaptiveCandidate is one proposed extra rung and the loss difference that
// justifies it. gap is used only to decide which bends to spend the bounded
// budget on; it is never published and never becomes a loss figure.
type adaptiveCandidate struct {
	size decimal.Decimal
	gap  decimal.Decimal
}

// adaptiveMidpoints chooses at most rule.extraLimit() extra send amounts.
//
// Only adjacent ladder entries are compared. A rung that did not price breaks
// adjacency: the sizes on either side of a hole were not neighbours in the
// requested ladder, and filling that hole would be a different measurement
// from resolving a bend in an otherwise complete curve. Consecutive priced
// entries are compared by their measured loss percentages, and a gap strictly
// greater than the threshold proposes the arithmetic midpoint of the two send
// amounts.
//
// The largest gaps are chosen first, so the bounded budget is spent where the
// curve bends most. Ties go to the smaller size, making the selection
// deterministic. The returned sizes are a real, ascending set of rungs to
// price; no value is estimated.
func adaptiveMidpoints(rungs []Rung, rule AdaptiveRule) []decimal.Decimal {
	limit := rule.extraLimit()
	if limit == 0 || len(rungs) < 2 {
		return nil
	}

	two := decimal.NewFromInt(2)
	present := make(map[string]bool, len(rungs))
	for _, r := range rungs {
		present[r.SendAmount.String()] = true
	}

	var candidates []adaptiveCandidate
	previous := -1
	for i := range rungs {
		if !rungs[i].Priced() {
			// Not a priced neighbour; do not compare across the hole.
			previous = -1
			continue
		}
		if previous >= 0 {
			from := rungs[previous]
			to := rungs[i]
			fromLoss := from.Result.Quotes[0].LossPct
			toLoss := to.Result.Quotes[0].LossPct
			gap := toLoss.Sub(fromLoss).Abs()
			if gap.GreaterThan(rule.ThresholdPct) {
				mid := from.SendAmount.Add(to.SendAmount).Div(two)
				if mid.GreaterThan(from.SendAmount) && mid.LessThan(to.SendAmount) &&
					!present[mid.String()] {
					candidates = append(candidates, adaptiveCandidate{size: mid, gap: gap})
				}
			}
		}
		previous = i
	}

	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].gap.Equal(candidates[j].gap) {
			return candidates[i].gap.GreaterThan(candidates[j].gap)
		}
		return candidates[i].size.LessThan(candidates[j].size)
	})

	out := make([]decimal.Decimal, 0, limit)
	chosen := make(map[string]bool, limit)
	for _, c := range candidates {
		if len(out) == limit {
			break
		}
		key := c.size.String()
		if chosen[key] {
			continue
		}
		chosen[key] = true
		out = append(out, c.size)
	}
	return out
}

// mergeAdaptiveRungs merges added rungs into the requested ladder and restores
// ascending size order, which every summary and the wire contract assume.
// Stable sorting keeps a requested rung before an added one if the two ever
// share a size.
func mergeAdaptiveRungs(base, extra []Rung) []Rung {
	if len(extra) == 0 {
		return base
	}
	out := make([]Rung, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SendAmount.LessThan(out[j].SendAmount)
	})
	return out
}
