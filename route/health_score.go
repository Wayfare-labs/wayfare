// Corridor health score composition (issue #55 / backlog #148).
//
// The health score answers "how is this corridor doing overall?" by blending
// five observable metrics — spread, depth, price impact, concentration, and
// effective transfer cost — into a single decimal.Decimal on a 0–100 scale.
//
// The score is an additional signal alongside the verdict and integrity state;
// it does not replace or modify either. Missing inputs make the score
// undetermined rather than defaulting a gap to zero.
//
// Weights and normalisation constants are maintainer-owned thresholds — the
// same class of decision as the verdict bands. The current values are
// reasonable defaults; they will be adjusted once the five component metrics
// are reachable and producing real data.
package route

import (
	"strings"

	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/shopspring/decimal"
)

// Metric IDs for the five inputs to the health score.
//
// These match the Descriptor.ID of each Metric implementation in checks/.
// The constants ensure callers do not need to know the string form.
const (
	MetricSpread        = "spread.bid-ask"
	MetricDepth         = "depth.observed-executable"
	MetricPriceImpact   = "price-impact.size"
	MetricConcentration = "concentration.liquidity"
	MetricCostLoss      = "cost.total_loss_pct" // synthetic; from route.Decompose TotalLossPct
)

// Normalisation ceilings — values above these saturate the normalisation
// function at 0 (for "lower is better") or 100 (for "higher is better").
// They represent "so bad the metric is no longer discriminating".
var (
	maxSpread        = decimal.NewFromFloat(0.5)  // 50% spread saturates
	maxPriceImpact   = decimal.NewFromFloat(50.0) // 50% price impact saturates
	maxConcentration = decimal.NewFromInt(1)      // HHI of 1.0 = total monopoly
	maxCostLoss      = decimal.NewFromFloat(50.0) // 50% total loss saturates
	maxDepth         = decimal.NewFromInt(50)     // 50 levels is deep enough
)

// HealthScoreInput carries one metric result plus its normalised score.
type HealthScoreInput struct {
	// ID is the metric descriptor ID (e.g. "spread.bid-ask").
	ID string `json:"id"`

	// Determined is true when the metric produced a value.
	Determined bool `json:"determined"`

	// Value is the metric's native value (only meaningful when Determined).
	Value decimal.Decimal `json:"-"`

	// Unit is the metric's native unit (only meaningful when Determined).
	Unit checks.Unit `json:"-"`

	// Reason explains why the metric is undetermined (only meaningful when !Determined).
	Reason string `json:"reason,omitempty"`

	// Normalised is the 0–100 score derived from the metric value
	// (only meaningful when Determined).
	Normalised decimal.Decimal `json:"-"`

	// Weight is this metric's contribution to the blended score.
	Weight decimal.Decimal `json:"-"`
}

// HealthScoreResult is the corridor health score on the wire.
type HealthScoreResult struct {
	// Value is the blended health score on a 0–100 scale. It is meaningful
	// only when Determined is true.
	Value decimal.Decimal `json:"-"`

	// Determined is false when any required input is undetermined.
	Determined bool `json:"determined"`

	// Reason lists which inputs are missing (only meaningful when !Determined).
	Reason string `json:"reason,omitempty"`

	// Inputs is the per-metric breakdown, always present regardless of
	// whether the overall score is determined.
	Inputs []HealthScoreInput `json:"inputs"`
}

// HealthScoreWeights defines the relative importance of each input metric.
// Weights must sum to 1.0; the function normalises them defensively.
type HealthScoreWeights struct {
	Spread        decimal.Decimal
	Depth         decimal.Decimal
	PriceImpact   decimal.Decimal
	Concentration decimal.Decimal
	Cost          decimal.Decimal
}

// DefaultHealthScoreWeights returns the maintainer-owned default weights.
//
// The rationale: spread and price impact are the two most actionable signals
// for a user deciding whether to transact. Depth and cost capture the
// execution environment. Concentration is structural risk that matters less
// for small trades.
//
// These are initial values; they will be refined once the five metrics are
// producing real data.
func DefaultHealthScoreWeights() HealthScoreWeights {
	w := decimal.NewFromFloat(0.2)
	return HealthScoreWeights{
		Spread:        w,
		Depth:         w,
		PriceImpact:   w,
		Concentration: w,
		Cost:          w,
	}
}

// HealthScore computes a corridor health score from five metric inputs.
//
// All five inputs are required. If any is undetermined, the entire score is
// undetermined — a score that silently defaults a missing input to zero is
// worse than no score. The reason field lists which inputs could not be
// obtained.
//
// The function never panics. Metric values are clamped to normalisation
// ceilings before scoring, so extreme inputs produce 0 rather than errors.
func HealthScore(
	spread checks.MetricResult,
	depth checks.MetricResult,
	priceImpact checks.MetricResult,
	concentration checks.MetricResult,
	costLoss decimal.Decimal, // from CostDecomposition.TotalLossPct
	costDetermined bool,
	costReason string,
) HealthScoreResult {
	w := DefaultHealthScoreWeights()
	return HealthScoreWeighted(spread, depth, priceImpact, concentration, costLoss, costDetermined, costReason, w)
}

// HealthScoreWeighted is like HealthScore but accepts explicit weights.
func HealthScoreWeighted(
	spread checks.MetricResult,
	depth checks.MetricResult,
	priceImpact checks.MetricResult,
	concentration checks.MetricResult,
	costLoss decimal.Decimal,
	costDetermined bool,
	costReason string,
	w HealthScoreWeights,
) HealthScoreResult {
	inputs := make([]HealthScoreInput, 0, 5)

	// Collect undetermined reasons.
	var missing []string

	// Spread.
	si := normaliseMetric(spread, MetricSpread, w.Spread, normaliseSpread)
	inputs = append(inputs, si)
	if !si.Determined {
		missing = append(missing, "spread: "+si.Reason)
	}

	// Depth.
	di := normaliseMetric(depth, MetricDepth, w.Depth, normaliseDepth)
	inputs = append(inputs, di)
	if !di.Determined {
		missing = append(missing, "depth: "+di.Reason)
	}

	// Price impact.
	pi := normaliseMetric(priceImpact, MetricPriceImpact, w.PriceImpact, normalisePriceImpact)
	inputs = append(inputs, pi)
	if !pi.Determined {
		missing = append(missing, "price impact: "+pi.Reason)
	}

	// Concentration.
	ci := normaliseMetric(concentration, MetricConcentration, w.Concentration, normaliseConcentration)
	inputs = append(inputs, ci)
	if !ci.Determined {
		missing = append(missing, "concentration: "+ci.Reason)
	}

	// Cost decomposition (not a checks.MetricResult; comes from route.Decompose).
	costInput := HealthScoreInput{
		ID:         MetricCostLoss,
		Determined: costDetermined,
		Weight:     w.Cost,
	}
	if costDetermined {
		costInput.Value = costLoss
		costInput.Unit = checks.UnitPercent
		costInput.Normalised = normaliseCost(costLoss)
	} else {
		costInput.Reason = costReason
		if costInput.Reason == "" {
			costInput.Reason = "cost decomposition not available"
		}
		missing = append(missing, "cost: "+costInput.Reason)
	}
	inputs = append(inputs, costInput)

	result := HealthScoreResult{
		Determined: len(missing) == 0,
		Inputs:     inputs,
	}

	if !result.Determined {
		result.Reason = "undetermined inputs: " + strings.Join(missing, "; ")
		return result
	}

	// Blend: weighted sum of normalised scores.
	totalWeight := w.Spread.Add(w.Depth).Add(w.PriceImpact).Add(w.Concentration).Add(w.Cost)
	score := decimal.Zero
	for _, in := range inputs {
		score = score.Add(in.Normalised.Mul(in.Weight))
	}
	if totalWeight.GreaterThan(decimal.Zero) {
		score = score.Div(totalWeight)
	}
	result.Value = score

	return result
}

// --- Normalisation functions ---
//
// Each maps a metric's native value to 0–100 where 100 is "best".

// normaliseSpread maps a bid/ask spread ratio (0–0.5) to 0–100.
// Lower spread is better: 0 → 100, 0.5 → 0.
func normaliseSpread(v decimal.Decimal) decimal.Decimal {
	v = clamp(v, decimal.Zero, maxSpread)
	return decimal.NewFromInt(100).Mul(decimal.NewFromInt(1).Sub(v.Div(maxSpread)))
}

// normaliseDepth maps observed depth (count of order book levels) to 0–100.
// More depth is better: 0 → 0, 50+ → 100.
func normaliseDepth(v decimal.Decimal) decimal.Decimal {
	v = clamp(v, decimal.Zero, maxDepth)
	return decimal.NewFromInt(100).Mul(v.Div(maxDepth))
}

// normalisePriceImpact maps price impact percentage (0–50) to 0–100.
// Lower impact is better: 0 → 100, 50 → 0.
func normalisePriceImpact(v decimal.Decimal) decimal.Decimal {
	v = clamp(v, decimal.Zero, maxPriceImpact)
	return decimal.NewFromInt(100).Mul(decimal.NewFromInt(1).Sub(v.Div(maxPriceImpact)))
}

// normaliseConcentration maps HHI ratio (0–1) to 0–100.
// Lower concentration is better: 0 → 100, 1 → 0.
func normaliseConcentration(v decimal.Decimal) decimal.Decimal {
	v = clamp(v, decimal.Zero, maxConcentration)
	return decimal.NewFromInt(100).Mul(decimal.NewFromInt(1).Sub(v))
}

// normaliseCost maps total loss percentage (0–50) to 0–100.
// Lower loss is better: 0 → 100, 50 → 0.
func normaliseCost(v decimal.Decimal) decimal.Decimal {
	v = clamp(v, decimal.Zero, maxCostLoss)
	return decimal.NewFromInt(100).Mul(decimal.NewFromInt(1).Sub(v.Div(maxCostLoss)))
}

// --- Helpers ---

// normaliseMetric converts a MetricResult into a HealthScoreInput.
// If the metric is undetermined, the input is undetermined with the metric's
// reason. If determined, the value is normalised using the provided function.
func normaliseMetric(
	m checks.MetricResult,
	id string,
	weight decimal.Decimal,
	normalise func(decimal.Decimal) decimal.Decimal,
) HealthScoreInput {
	in := HealthScoreInput{
		ID:     id,
		Weight: weight,
	}

	if !m.Determined {
		in.Determined = false
		in.Reason = m.Reason
		if in.Reason == "" {
			in.Reason = "metric undetermined"
		}
		return in
	}

	in.Determined = true
	in.Value = m.Value
	in.Unit = m.Unit
	in.Normalised = normalise(m.Value)
	return in
}

// clamp restricts v to [lo, hi].
func clamp(v, lo, hi decimal.Decimal) decimal.Decimal {
	if v.LessThan(lo) {
		return lo
	}
	if v.GreaterThan(hi) {
		return hi
	}
	return v
}
