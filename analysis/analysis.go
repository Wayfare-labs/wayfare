// Package analysis provides a statistical analysis layer over runstore data.
//
// It reads existing runstore records and computes, where sufficient history
// exists:
//
//   - observation count
//   - mean
//   - standard deviation
//   - trend (direction and magnitude)
//   - regime classification using documented thresholds
//
// # Minimum sample sizes
//
// Statistical claims require evidence. Small samples produce numbers that
// look precise but are not meaningful. The minimum sample sizes are chosen
// so that the statistics we report are worth reporting:
//
//   - mean / standard deviation: n ≥ 30 (≈12.5 days at 6-hour cadence)
//   - trend direction: n ≥ 60 (≈25 days at 6-hour cadence)
//
// Below these thresholds the result is UNDETERMINED. No statistics are
// fabricated from insufficient data.
//
// # What this is NOT
//
//   - Not a new persistence mechanism
//   - Not a new data collection system
//   - Not a redesign of runstore
//   - Not ML or predictive modelling
//   - Not a composite health score
//   - Not a replacement for V2 metrics
package analysis

import (
	"github.com/shopspring/decimal"
)

// Minimum sample sizes for statistical validity.
const (
	// MinSampleSizeForMeanStdDev is the minimum number of observations
	// required to compute a meaningful mean and standard deviation.
	// At a 6-hour measurement cadence, 30 observations ≈ 12.5 days.
	MinSampleSizeForMeanStdDev = 30

	// MinSampleSizeForTrend is the minimum number of observations
	// required to compute a meaningful trend direction and magnitude.
	// At a 6-hour measurement cadence, 60 observations ≈ 25 days.
	MinSampleSizeForTrend = 60
)

// TrendDirection describes the direction of a statistical trend.
type TrendDirection string

const (
	// TrendImproving indicates the metric is getting better over time
	// (e.g., loss percentage is decreasing).
	TrendImproving TrendDirection = "improving"

	// TrendStable indicates no statistically significant change.
	TrendStable TrendDirection = "stable"

	// TrendWorsening indicates the metric is getting worse over time
	// (e.g., loss percentage is increasing).
	TrendWorsening TrendDirection = "worsening"
)

// Regime classifies the current state using documented thresholds.
type Regime string

const (
	// RegimeUndetermined means insufficient data exists to classify.
	RegimeUndetermined Regime = "undetermined"

	// RegimeNormal means the corridor is operating within expected
	// parameters.
	RegimeNormal Regime = "normal"

	// RegimeElevated means the corridor shows signs of degradation.
	RegimeElevated Regime = "elevated"

	// RegimeCritical means the corridor is in a degraded state.
	RegimeCritical Regime = "critical"
)

// RegimeThresholds documents the thresholds used for regime classification.
// These are applied to the mean loss percentage.
//
//   - Normal: mean loss < 30%
//   - Elevated: 30% ≤ mean loss < 60%
//   - Critical: mean loss ≥ 60%
type RegimeThresholds struct {
	// ElevatedBelow is the loss percentage below which the regime is Normal.
	// At or above this value, the regime becomes Elevated.
	ElevatedBelow decimal.Decimal

	// CriticalAbove is the loss percentage at or above which the regime
	// becomes Critical.
	CriticalAbove decimal.Decimal
}

// DefaultRegimeThresholds returns the standard thresholds for regime
// classification. These are documented in the analysis package README.
func DefaultRegimeThresholds() RegimeThresholds {
	return RegimeThresholds{
		ElevatedBelow: decimal.NewFromInt(30),
		CriticalAbove: decimal.NewFromInt(60),
	}
}

// TrendResult holds the output of a trend analysis.
type TrendResult struct {
	// Direction is whether the metric is improving, stable, or worsening.
	Direction TrendDirection

	// Magnitude is the absolute value of the trend slope, expressed in
	// the same units as the input (typically percentage points per
	// observation).
	Magnitude decimal.Decimal

	// Slope is the signed trend slope. Positive means increasing values,
	// negative means decreasing. A loss percentage that decreases over
	// time (improving) has a negative slope.
	Slope decimal.Decimal
}

// MetricStats holds the statistical analysis for a single metric extracted
// from runstore records.
type MetricStats struct {
	// ObservationCount is the total number of observations.
	ObservationCount int

	// Mean is the arithmetic mean of the observed values.
	// nil when undetermined (insufficient observations).
	Mean *decimal.Decimal

	// StdDev is the sample standard deviation.
	// nil when undetermined (insufficient observations).
	StdDev *decimal.Decimal

	// Trend is the trend analysis result.
	// nil when undetermined (insufficient observations).
	Trend *TrendResult

	// Regime is the current regime classification.
	Regime Regime

	// Undetermined is true when insufficient data exists to compute
	// meaningful statistics. When true, Mean, StdDev, and Trend are nil.
	Undetermined bool

	// Reason explains why the result is undetermined (empty when
	// determined).
	Reason string
}

// AnalyzeDecimal computes statistics over a slice of decimal values.
// It applies the documented minimum sample size thresholds and never
// fabricates statistics from insufficient data.
//
// The thresholds parameter controls regime classification. Pass nil to
// use DefaultRegimeThresholds().
func AnalyzeDecimal(values []decimal.Decimal, thresholds *RegimeThresholds) *MetricStats {
	n := len(values)
	if thresholds == nil {
		t := DefaultRegimeThresholds()
		thresholds = &t
	}

	result := &MetricStats{
		ObservationCount: n,
	}

	if n == 0 {
		result.Undetermined = true
		result.Reason = "no observations"
		result.Regime = RegimeUndetermined
		return result
	}

	// Observation count is always reported.
	if n < MinSampleSizeForMeanStdDev {
		result.Undetermined = true
		result.Reason = "insufficient observations: " +
			"need at least " + itoa(MinSampleSizeForMeanStdDev) +
			" for mean/stddev, have " + itoa(n)
		result.Regime = RegimeUndetermined
		return result
	}

	// Compute mean.
	mean := computeMean(values)
	result.Mean = &mean

	// Compute standard deviation.
	stddev := computeStdDev(values, mean)
	result.StdDev = &stddev

	// Classify regime based on the mean.
	result.Regime = classifyRegime(mean, *thresholds)

	// Compute trend only with sufficient data.
	if n >= MinSampleSizeForTrend {
		trend := computeTrend(values)
		result.Trend = &trend
	}

	return result
}

// computeMean calculates the arithmetic mean of a slice of decimals.
func computeMean(values []decimal.Decimal) decimal.Decimal {
	if len(values) == 0 {
		return decimal.Zero
	}

	sum := decimal.Zero
	for _, v := range values {
		sum = sum.Add(v)
	}
	return sum.Div(decimal.NewFromInt(int64(len(values))))
}

// computeStdDev calculates the sample standard deviation.
func computeStdDev(values []decimal.Decimal, mean decimal.Decimal) decimal.Decimal {
	n := len(values)
	if n <= 1 {
		return decimal.Zero
	}

	sumSqDiff := decimal.Zero
	for _, v := range values {
		diff := v.Sub(mean)
		sumSqDiff = sumSqDiff.Add(diff.Mul(diff))
	}

	// Sample standard deviation: divide by n-1.
	variance := sumSqDiff.Div(decimal.NewFromInt(int64(n - 1)))
	return decimalSqrt(variance)
}

// computeTrend calculates the trend using linear regression (least squares).
func computeTrend(values []decimal.Decimal) TrendResult {
	n := len(values)
	if n < 2 {
		return TrendResult{Direction: TrendStable}
	}

	// Simple linear regression: y = a + b*x
	// where x is the observation index (0, 1, 2, ...) and y is the value.
	sumX := decimal.Zero
	sumY := decimal.Zero
	sumXY := decimal.Zero
	sumX2 := decimal.Zero

	for i, v := range values {
		x := decimal.NewFromInt(int64(i))
		sumX = sumX.Add(x)
		sumY = sumY.Add(v)
		sumXY = sumXY.Add(x.Mul(v))
		sumX2 = sumX2.Add(x.Mul(x))
	}

	nD := decimal.NewFromInt(int64(n))
	denom := nD.Mul(sumX2).Sub(sumX.Mul(sumX))

	if denom.IsZero() {
		return TrendResult{Direction: TrendStable}
	}

	// Slope b = (n*Σ(xy) - Σx*Σy) / (n*Σ(x²) - (Σx)²)
	slope := nD.Mul(sumXY).Sub(sumX.Mul(sumY)).Div(denom)

	// Classify direction with a dead zone to avoid calling noise a trend.
	// The dead zone is 0.1 percentage points per observation, which
	// represents a change of 0.1% per 6-hour cycle (≈1% per 2.5 days).
	deadZone := decimal.NewFromFloat(0.1)

	absSlope := slope.Abs()
	direction := TrendStable
	if absSlope.GreaterThan(deadZone) {
		// Positive slope means values are increasing.
		// For loss percentage, increasing values = worsening.
		if slope.IsPositive() {
			direction = TrendWorsening
		} else {
			direction = TrendImproving
		}
	}

	return TrendResult{
		Direction: direction,
		Magnitude: absSlope,
		Slope:     slope,
	}
}

// classifyRegime determines the regime based on the mean loss percentage.
func classifyRegime(mean decimal.Decimal, thresholds RegimeThresholds) Regime {
	if mean.GreaterThanOrEqual(thresholds.CriticalAbove) {
		return RegimeCritical
	}
	if mean.GreaterThanOrEqual(thresholds.ElevatedBelow) {
		return RegimeElevated
	}
	return RegimeNormal
}

// decimalSqrt computes the square root of a decimal using the
// Newton-Raphson method with sufficient precision for standard deviation.
func decimalSqrt(d decimal.Decimal) decimal.Decimal {
	if d.IsNegative() || d.IsZero() {
		return decimal.Zero
	}

	// Newton-Raphson: x_{n+1} = 0.5 * (x_n + d / x_n)
	// Start with an initial guess.
	x := d
	if x.GreaterThan(decimal.NewFromInt(100)) {
		x = d.Div(decimal.NewFromInt(2))
	}

	for i := 0; i < 50; i++ {
		next := x.Add(d.Div(x)).Div(decimal.NewFromInt(2))
		if next.Sub(x).Abs().LessThan(decimal.NewFromFloat(1e-12)) {
			return next
		}
		x = next
	}
	return x
}

// itoa is a minimal int-to-string conversion to avoid importing strconv
// in this pure-math package.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
