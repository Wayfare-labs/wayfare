package analysis

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

// dec creates a decimal from a float64 for test convenience.
func dec(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}

// decs converts a slice of float64 to decimals for test convenience.
func decs(fs ...float64) []decimal.Decimal {
	out := make([]decimal.Decimal, len(fs))
	for i, f := range fs {
		out[i] = decimal.NewFromFloat(f)
	}
	return out
}

// TestAnalyzeDecimalEmpty verifies that an empty slice produces an
// undetermined result with zero observations.
func TestAnalyzeDecimalEmpty(t *testing.T) {
	result := AnalyzeDecimal(nil, nil)

	if !result.Undetermined {
		t.Error("empty input should be undetermined")
	}
	if result.ObservationCount != 0 {
		t.Errorf("observation count = %d, want 0", result.ObservationCount)
	}
	if result.Mean != nil {
		t.Error("mean should be nil for undetermined result")
	}
	if result.StdDev != nil {
		t.Error("stddev should be nil for undetermined result")
	}
	if result.Trend != nil {
		t.Error("trend should be nil for undetermined result")
	}
	if result.Regime != RegimeUndetermined {
		t.Errorf("regime = %v, want undetermined", result.Regime)
	}
}

// TestAnalyzeDecimalInsufficientForMeanStdDev verifies that fewer than 30
// observations produces an undetermined result.
func TestAnalyzeDecimalInsufficientForMeanStdDev(t *testing.T) {
	// 29 observations: below the minimum of 30.
	values := make([]decimal.Decimal, 29)
	for i := range values {
		values[i] = dec(float64(i))
	}

	result := AnalyzeDecimal(values, nil)

	if !result.Undetermined {
		t.Error("29 observations should be undetermined")
	}
	if result.ObservationCount != 29 {
		t.Errorf("observation count = %d, want 29", result.ObservationCount)
	}
	if result.Mean != nil {
		t.Error("mean should be nil when undetermined")
	}
	if result.StdDev != nil {
		t.Error("stddev should be nil when undetermined")
	}
	if result.Trend != nil {
		t.Error("trend should be nil when undetermined")
	}
	if result.Regime != RegimeUndetermined {
		t.Errorf("regime = %v, want undetermined", result.Regime)
	}
}

// TestAnalyzeDecimalExactlyAtMeanStdDevThreshold verifies that exactly 30
// observations computes mean and stddev but not trend.
func TestAnalyzeDecimalExactlyAtMeanStdDevThreshold(t *testing.T) {
	values := make([]decimal.Decimal, 30)
	for i := range values {
		values[i] = dec(float64(i) + 10) // values 10..39
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("30 observations should be determined")
	}
	if result.ObservationCount != 30 {
		t.Errorf("observation count = %d, want 30", result.ObservationCount)
	}
	if result.Mean == nil {
		t.Fatal("mean should not be nil")
	}

	// Mean of 10..39 = 24.5
	wantMean := dec(24.5)
	if !result.Mean.Equal(wantMean) {
		t.Errorf("mean = %s, want %s", result.Mean, wantMean)
	}

	if result.StdDev == nil {
		t.Fatal("stddev should not be nil")
	}

	// Trend should be nil because 30 < 60.
	if result.Trend != nil {
		t.Error("trend should be nil with fewer than 60 observations")
	}
}

// TestAnalyzeDecimalAboveTrendThreshold verifies that 60+ observations
// computes all statistics including trend.
func TestAnalyzeDecimalAboveTrendThreshold(t *testing.T) {
	// Create 60 values with a clear upward trend.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		values[i] = dec(float64(i) * 0.5) // 0, 0.5, 1.0, ..., 29.5
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("60 observations should be determined")
	}
	if result.ObservationCount != 60 {
		t.Errorf("observation count = %d, want 60", result.ObservationCount)
	}
	if result.Mean == nil {
		t.Fatal("mean should not be nil")
	}
	if result.StdDev == nil {
		t.Fatal("stddev should not be nil")
	}
	if result.Trend == nil {
		t.Fatal("trend should not be nil with 60+ observations")
	}
}

// TestComputeMean verifies the mean calculation.
func TestComputeMean(t *testing.T) {
	cases := []struct {
		name   string
		values []decimal.Decimal
		want   decimal.Decimal
	}{
		{"single", decs(5), dec(5)},
		{"two", decs(1, 3), dec(2)},
		{"uniform", decs(7, 7, 7, 7), dec(7)},
		{"integers", decs(1, 2, 3, 4, 5), dec(3)},
		{"decimals", decs(1.5, 2.5, 3.5), dec(2.5)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeMean(tc.values)
			if !got.Equal(tc.want) {
				t.Errorf("mean = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestComputeStdDev verifies the standard deviation calculation against
// known values.
func TestComputeStdDev(t *testing.T) {
	// Population: {2, 4, 4, 4, 5, 5, 7, 9}
	// Mean = 5, Sample variance = 4, Sample stddev = 2
	values := decs(2, 4, 4, 4, 5, 5, 7, 9)
	mean := computeMean(values)
	stddev := computeStdDev(values, mean)

	// Sample stddev = sqrt(32/7) ≈ 2.138 (divide by n-1, not n)
	wantStddev := decimalSqrt(dec(32).Div(dec(7)))
	if !stddev.Equal(wantStddev) {
		t.Errorf("stddev = %s, want %s", stddev, wantStddev)
	}
}

// TestComputeStdDevSingleValue verifies that a single value produces zero
// standard deviation.
func TestComputeStdDevSingleValue(t *testing.T) {
	stddev := computeStdDev(decs(42), dec(42))
	if !stddev.IsZero() {
		t.Errorf("stddev of single value = %s, want 0", stddev)
	}
}

// TestComputeTrendImproving verifies detection of an improving trend
// (decreasing values).
func TestComputeTrendImproving(t *testing.T) {
	// Loss percentages decreasing over time = improving.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		// Start at 50%, decrease by 1% per observation.
		values[i] = dec(50 - float64(i))
	}

	result := computeTrend(values)
	if result.Direction != TrendImproving {
		t.Errorf("direction = %v, want improving", result.Direction)
	}
	if result.Slope.IsPositive() {
		t.Error("slope should be negative for improving trend")
	}
}

// TestComputeTrendWorsening verifies detection of a worsening trend
// (increasing values).
func TestComputeTrendWorsening(t *testing.T) {
	// Loss percentages increasing over time = worsening.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		// Start at 10%, increase by 1% per observation.
		values[i] = dec(10 + float64(i))
	}

	result := computeTrend(values)
	if result.Direction != TrendWorsening {
		t.Errorf("direction = %v, want worsening", result.Direction)
	}
	if !result.Slope.IsPositive() {
		t.Error("slope should be positive for worsening trend")
	}
}

// TestComputeTrendStable verifies that a flat series is classified as
// stable.
func TestComputeTrendStable(t *testing.T) {
	// All values the same = no trend.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		values[i] = dec(25)
	}

	result := computeTrend(values)
	if result.Direction != TrendStable {
		t.Errorf("direction = %v, want stable", result.Direction)
	}
	if !result.Slope.IsZero() {
		t.Errorf("slope = %s, want 0 for stable trend", result.Slope)
	}
}

// TestComputeTrendDeadZone verifies that small fluctuations within the dead
// zone are classified as stable.
func TestComputeTrendDeadZone(t *testing.T) {
	// Values that oscillate within ±0.05 per observation, well within
	// the 0.1 dead zone.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		if i%2 == 0 {
			values[i] = dec(25)
		} else {
			values[i] = dec(25.05)
		}
	}

	result := computeTrend(values)
	if result.Direction != TrendStable {
		t.Errorf("direction = %v, want stable for noise within dead zone", result.Direction)
	}
}

// TestComputeTrendSingleValue verifies that a single value produces a
// stable trend.
func TestComputeTrendSingleValue(t *testing.T) {
	result := computeTrend(decs(42))
	if result.Direction != TrendStable {
		t.Errorf("direction = %v, want stable for single value", result.Direction)
	}
}

// TestClassifyRegime verifies regime classification against the documented
// thresholds.
func TestClassifyRegime(t *testing.T) {
	thresholds := DefaultRegimeThresholds()

	cases := []struct {
		name string
		mean decimal.Decimal
		want Regime
	}{
		{"zero loss is normal", dec(0), RegimeNormal},
		{"low loss is normal", dec(15), RegimeNormal},
		{"just below elevated", dec(29.99), RegimeNormal},
		{"at elevated threshold", dec(30), RegimeElevated},
		{"mid elevated", dec(45), RegimeElevated},
		{"just below critical", dec(59.99), RegimeElevated},
		{"at critical threshold", dec(60), RegimeCritical},
		{"high loss is critical", dec(80), RegimeCritical},
		{"extreme loss is critical", dec(99), RegimeCritical},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRegime(tc.mean, thresholds)
			if got != tc.want {
				t.Errorf("classifyRegime(%s) = %v, want %v", tc.mean, got, tc.want)
			}
		})
	}
}

// TestDefaultRegimeThresholds verifies the default thresholds are documented
// correctly.
func TestDefaultRegimeThresholds(t *testing.T) {
	thresholds := DefaultRegimeThresholds()

	if !thresholds.ElevatedBelow.Equal(dec(30)) {
		t.Errorf("ElevatedBelow = %s, want 30", thresholds.ElevatedBelow)
	}
	if !thresholds.CriticalAbove.Equal(dec(60)) {
		t.Errorf("CriticalAbove = %s, want 60", thresholds.CriticalAbove)
	}
}

// TestCustomThresholds verifies that custom thresholds are respected.
func TestCustomThresholds(t *testing.T) {
	custom := RegimeThresholds{
		ElevatedBelow: dec(10),
		CriticalAbove: dec(20),
	}

	cases := []struct {
		mean decimal.Decimal
		want Regime
	}{
		{dec(5), RegimeNormal},
		{dec(10), RegimeElevated},
		{dec(15), RegimeElevated},
		{dec(20), RegimeCritical},
	}

	for _, tc := range cases {
		got := classifyRegime(tc.mean, custom)
		if got != tc.want {
			t.Errorf("classifyRegime(%s) with custom thresholds = %v, want %v",
				tc.mean, got, tc.want)
		}
	}
}

// TestDecimalSqrt verifies the square root implementation.
func TestDecimalSqrt(t *testing.T) {
	cases := []struct {
		input    decimal.Decimal
		wantSqrt float64
	}{
		{dec(0), 0},
		{dec(1), 1},
		{dec(4), 2},
		{dec(9), 3},
		{dec(16), 4},
		{dec(2), math.Sqrt(2)},
		{dec(0.25), 0.5},
	}

	for _, tc := range cases {
		got := decimalSqrt(tc.input)
		gotF, _ := got.Float64()
		if math.Abs(gotF-tc.wantSqrt) > 1e-6 {
			t.Errorf("decimalSqrt(%s) = %s (%f), want %f",
				tc.input, got, gotF, tc.wantSqrt)
		}
	}
}

// TestDecimalSqrtNegative verifies that negative inputs produce zero.
func TestDecimalSqrtNegative(t *testing.T) {
	got := decimalSqrt(dec(-4))
	if !got.IsZero() {
		t.Errorf("decimalSqrt(-4) = %s, want 0", got)
	}
}

// TestAnalyzeDecimalRegimeClassification verifies the full pipeline
// including regime classification.
func TestAnalyzeDecimalRegimeClassification(t *testing.T) {
	// 60 observations at 25% loss = normal regime.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		values[i] = dec(25)
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("should be determined with 60 observations")
	}
	if result.Regime != RegimeNormal {
		t.Errorf("regime = %v, want normal for 25%% mean loss", result.Regime)
	}
}

// TestAnalyzeDecimalRegimeElevated verifies elevated regime classification.
func TestAnalyzeDecimalRegimeElevated(t *testing.T) {
	// 30 observations at 45% loss = elevated regime.
	values := make([]decimal.Decimal, 30)
	for i := range values {
		values[i] = dec(45)
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("should be determined with 30 observations")
	}
	if result.Regime != RegimeElevated {
		t.Errorf("regime = %v, want elevated for 45%% mean loss", result.Regime)
	}
}

// TestAnalyzeDecimalRegimeCritical verifies critical regime classification.
func TestAnalyzeDecimalRegimeCritical(t *testing.T) {
	// 60 observations at 75% loss = critical regime.
	values := make([]decimal.Decimal, 60)
	for i := range values {
		values[i] = dec(75)
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("should be determined with 60 observations")
	}
	if result.Regime != RegimeCritical {
		t.Errorf("regime = %v, want critical for 75%% mean loss", result.Regime)
	}
}

// TestAnalyzeDecimalNoFloat64InResults verifies that the public API
// exposes no float64 values — all calculations use decimal.Decimal.
func TestAnalyzeDecimalNoFloat64InResults(t *testing.T) {
	values := make([]decimal.Decimal, 60)
	for i := range values {
		values[i] = dec(float64(i) * 0.5)
	}

	result := AnalyzeDecimal(values, nil)

	// The MetricStats struct contains only int, *decimal.Decimal,
	// TrendResult, Regime (string), bool, and string fields.
	// This test documents that constraint by checking the types.
	if result.Mean != nil {
		// Verify it's a decimal, not a float64.
		if _, ok := interface{}(*result.Mean).(decimal.Decimal); !ok {
			t.Error("Mean is not a decimal.Decimal")
		}
	}
	if result.StdDev != nil {
		if _, ok := interface{}(*result.StdDev).(decimal.Decimal); !ok {
			t.Error("StdDev is not a decimal.Decimal")
		}
	}
	if result.Trend != nil {
		if _, ok := interface{}(result.Trend.Magnitude).(decimal.Decimal); !ok {
			t.Error("Trend.Magnitude is not a decimal.Decimal")
		}
		if _, ok := interface{}(result.Trend.Slope).(decimal.Decimal); !ok {
			t.Error("Trend.Slope is not a decimal.Decimal")
		}
	}
}

// TestItoa verifies the minimal integer-to-string conversion.
func TestItoa(t *testing.T) {
	cases := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{30, "30"},
		{60, "60"},
		{999, "999"},
	}

	for _, tc := range cases {
		got := itoa(tc.input)
		if got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestAnalyzeDecimalFullPipeline exercises the complete analysis path
// with realistic data: a corridor whose loss percentage fluctuates but
// trends upward (worsening) over 60 observations.
func TestAnalyzeDecimalFullPipeline(t *testing.T) {
	values := make([]decimal.Decimal, 60)
	for i := range values {
		// Base loss of 25% with a slight upward drift and noise.
		base := 25.0
		drift := float64(i) * 0.15 // gradual worsening (above dead zone of 0.1)
		var noise float64
		switch i % 3 {
		case 0:
			noise = 0.3
		case 1:
			noise = -0.3
		}
		values[i] = dec(base + drift + noise)
	}

	result := AnalyzeDecimal(values, nil)

	if result.Undetermined {
		t.Error("should be determined with 60 observations")
	}
	if result.ObservationCount != 60 {
		t.Errorf("observation count = %d, want 60", result.ObservationCount)
	}
	if result.Mean == nil {
		t.Fatal("mean should not be nil")
	}
	if result.StdDev == nil {
		t.Fatal("stddev should not be nil")
	}
	if result.Trend == nil {
		t.Fatal("trend should not be nil")
	}

	// Mean should be around 27.6 (25 + 0.08*29.5 ≈ 27.36, plus noise).
	meanF, _ := result.Mean.Float64()
	if meanF < 26 || meanF > 30 {
		t.Errorf("mean = %f, expected around 27-28", meanF)
	}

	// Trend should be worsening (positive slope for increasing loss).
	if result.Trend.Direction != TrendWorsening {
		t.Errorf("trend direction = %v, want worsening", result.Trend.Direction)
	}

	// Regime should be normal (mean < 30%).
	if result.Regime != RegimeNormal {
		t.Errorf("regime = %v, want normal", result.Regime)
	}
}
