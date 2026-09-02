package route

import (
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/shopspring/decimal"
)

// fixedDescriptor returns a minimal valid Descriptor for test metric results.
func fixedDescriptor(id string) checks.Descriptor {
	return checks.Descriptor{
		ID:    id,
		Scope: checks.ScopeCorridor,
		Title: "test: " + id,
	}
}

// fixedMetric builds a determined MetricResult for testing.
func fixedMetric(id string, value decimal.Decimal, unit checks.Unit) checks.MetricResult {
	return checks.MetricValue(
		fixedDescriptor(id),
		checks.Subject{},
		value,
		unit,
		"test value",
		checks.Evidence{
			Source:     "test",
			Observed:   "value",
			ObservedAt: time.Now().UTC(),
		},
	)
}

// undeterminedMetric builds an undetermined MetricResult for testing.
func undeterminedMetric(id string, reason string) checks.MetricResult {
	return checks.MetricUndetermined(
		fixedDescriptor(id),
		checks.Subject{},
		reason,
	)
}

// --- Normalisation unit tests ---

func TestNormaliseSpreadZero(t *testing.T) {
	got := normaliseSpread(decimal.Zero)
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normaliseSpread(0) = %s, want %s", got, want)
	}
}

func TestNormaliseSpreadAtCeiling(t *testing.T) {
	got := normaliseSpread(maxSpread)
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normaliseSpread(0.5) = %s, want %s", got, want)
	}
}

func TestNormaliseSpreadClamps(t *testing.T) {
	got := normaliseSpread(decimal.NewFromFloat(0.8))
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normaliseSpread(0.8) = %s, want %s (should clamp at ceiling)", got, want)
	}
}

func TestNormaliseSpreadMidpoint(t *testing.T) {
	got := normaliseSpread(decimal.NewFromFloat(0.25))
	want := decimal.NewFromInt(50)
	if !got.Equal(want) {
		t.Errorf("normaliseSpread(0.25) = %s, want %s", got, want)
	}
}

func TestNormaliseDepthZero(t *testing.T) {
	got := normaliseDepth(decimal.Zero)
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normaliseDepth(0) = %s, want %s", got, want)
	}
}

func TestNormaliseDepthAtCeiling(t *testing.T) {
	got := normaliseDepth(maxDepth)
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normaliseDepth(50) = %s, want %s", got, want)
	}
}

func TestNormaliseDepthClamps(t *testing.T) {
	got := normaliseDepth(decimal.NewFromInt(100))
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normaliseDepth(100) = %s, want %s (should clamp at ceiling)", got, want)
	}
}

func TestNormalisePriceImpactZero(t *testing.T) {
	got := normalisePriceImpact(decimal.Zero)
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normalisePriceImpact(0) = %s, want %s", got, want)
	}
}

func TestNormalisePriceImpactAtCeiling(t *testing.T) {
	got := normalisePriceImpact(maxPriceImpact)
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normalisePriceImpact(50) = %s, want %s", got, want)
	}
}

func TestNormaliseConcentrationZero(t *testing.T) {
	got := normaliseConcentration(decimal.Zero)
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normaliseConcentration(0) = %s, want %s", got, want)
	}
}

func TestNormaliseConcentrationAtCeiling(t *testing.T) {
	got := normaliseConcentration(maxConcentration)
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normaliseConcentration(1) = %s, want %s", got, want)
	}
}

func TestNormaliseCostZero(t *testing.T) {
	got := normaliseCost(decimal.Zero)
	want := decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Errorf("normaliseCost(0) = %s, want %s", got, want)
	}
}

func TestNormaliseCostAtCeiling(t *testing.T) {
	got := normaliseCost(maxCostLoss)
	want := decimal.Zero
	if !got.Equal(want) {
		t.Errorf("normaliseCost(50) = %s, want %s", got, want)
	}
}

// --- Clamp tests ---

func TestClampBelow(t *testing.T) {
	got := clamp(decimal.NewFromInt(-1), decimal.Zero, decimal.NewFromInt(10))
	if !got.Equal(decimal.Zero) {
		t.Errorf("clamp(-1, 0, 10) = %s, want 0", got)
	}
}

func TestClampAbove(t *testing.T) {
	got := clamp(decimal.NewFromInt(20), decimal.Zero, decimal.NewFromInt(10))
	if !got.Equal(decimal.NewFromInt(10)) {
		t.Errorf("clamp(20, 0, 10) = %s, want 10", got)
	}
}

func TestClampWithin(t *testing.T) {
	got := clamp(decimal.NewFromInt(5), decimal.Zero, decimal.NewFromInt(10))
	if !got.Equal(decimal.NewFromInt(5)) {
		t.Errorf("clamp(5, 0, 10) = %s, want 5", got)
	}
}

// --- HealthScore composition tests ---

func TestHealthScoreAllDetermined(t *testing.T) {
	spread := fixedMetric(MetricSpread, decimal.NewFromFloat(0.04), checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(25), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.NewFromFloat(10.0), checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.NewFromFloat(0.12), checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.NewFromFloat(5.0), true, "")

	if !result.Determined {
		t.Fatalf("health score should be determined when all inputs are present; reason: %s", result.Reason)
	}

	if len(result.Inputs) != 5 {
		t.Fatalf("expected 5 inputs, got %d", len(result.Inputs))
	}

	// Score should be between 0 and 100.
	if result.Value.LessThan(decimal.Zero) || result.Value.GreaterThan(decimal.NewFromInt(100)) {
		t.Errorf("health score %s out of range [0, 100]", result.Value)
	}

	// Each input should be determined.
	for _, in := range result.Inputs {
		if !in.Determined {
			t.Errorf("input %s should be determined", in.ID)
		}
	}
}

func TestHealthScoreSpreadUndetermined(t *testing.T) {
	spread := undeterminedMetric(MetricSpread, "no direct book")
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(20), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.NewFromFloat(5.0), checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.NewFromFloat(0.3), checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.NewFromFloat(10.0), true, "")

	if result.Determined {
		t.Error("health score should be undetermined when spread is missing")
	}

	if !strings.Contains(result.Reason, "spread") {
		t.Errorf("reason should mention spread, got: %s", result.Reason)
	}
}

func TestHealthScoreCostUndetermined(t *testing.T) {
	spread := fixedMetric(MetricSpread, decimal.NewFromFloat(0.02), checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(30), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.NewFromFloat(3.0), checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.NewFromFloat(0.2), checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.Zero, false, "cost decomposition not wired in")

	if result.Determined {
		t.Error("health score should be undetermined when cost is missing")
	}

	if !strings.Contains(result.Reason, "cost") {
		t.Errorf("reason should mention cost, got: %s", result.Reason)
	}
}

func TestHealthScoreAllUndetermined(t *testing.T) {
	spread := undeterminedMetric(MetricSpread, "no book")
	depth := undeterminedMetric(MetricDepth, "structural")
	impact := undeterminedMetric(MetricPriceImpact, "no path")
	conc := undeterminedMetric(MetricConcentration, "empty book")

	result := HealthScore(spread, depth, impact, conc, decimal.Zero, false, "not decomposed")

	if result.Determined {
		t.Error("health score should be undetermined when all inputs are missing")
	}

	if len(result.Inputs) != 5 {
		t.Fatalf("expected 5 inputs even when all undetermined, got %d", len(result.Inputs))
	}

	for _, in := range result.Inputs {
		if in.Determined {
			t.Errorf("input %s should be undetermined", in.ID)
		}
	}
}

func TestHealthScorePerfectCorridor(t *testing.T) {
	// Perfect corridor: zero spread, deep book, no price impact, no concentration, zero cost.
	spread := fixedMetric(MetricSpread, decimal.Zero, checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(50), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.Zero, checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.Zero, checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.Zero, true, "")

	if !result.Determined {
		t.Fatalf("health score should be determined; reason: %s", result.Reason)
	}

	// Perfect inputs should produce score of 100.
	want := decimal.NewFromInt(100)
	if !result.Value.Equal(want) {
		t.Errorf("perfect corridor health score = %s, want %s", result.Value, want)
	}
}

func TestHealthScoreWorstCorridor(t *testing.T) {
	// Worst corridor: maximum spread, no depth, maximum price impact,
	// total concentration, maximum cost loss.
	spread := fixedMetric(MetricSpread, maxSpread, checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.Zero, checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, maxPriceImpact, checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, maxConcentration, checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, maxCostLoss, true, "")

	if !result.Determined {
		t.Fatalf("health score should be determined; reason: %s", result.Reason)
	}

	want := decimal.Zero
	if !result.Value.Equal(want) {
		t.Errorf("worst corridor health score = %s, want %s", result.Value, want)
	}
}

func TestHealthScoreWeightsSumToOne(t *testing.T) {
	w := DefaultHealthScoreWeights()
	sum := w.Spread.Add(w.Depth).Add(w.PriceImpact).Add(w.Concentration).Add(w.Cost)
	one := decimal.NewFromInt(1)
	if !sum.Equal(one) {
		t.Errorf("default weights sum to %s, want 1.0", sum)
	}
}

func TestHealthScorePreservesNativeValues(t *testing.T) {
	spreadVal := decimal.NewFromFloat(0.03)
	depthVal := decimal.NewFromInt(15)
	impactVal := decimal.NewFromFloat(7.5)
	concVal := decimal.NewFromFloat(0.25)
	costVal := decimal.NewFromFloat(12.0)

	spread := fixedMetric(MetricSpread, spreadVal, checks.UnitRatio)
	depth := fixedMetric(MetricDepth, depthVal, checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, impactVal, checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, concVal, checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, costVal, true, "")

	if !result.Determined {
		t.Fatalf("health score should be determined; reason: %s", result.Reason)
	}

	for _, in := range result.Inputs {
		switch in.ID {
		case MetricSpread:
			if !in.Value.Equal(spreadVal) {
				t.Errorf("spread value = %s, want %s", in.Value, spreadVal)
			}
			if in.Unit != checks.UnitRatio {
				t.Errorf("spread unit = %s, want ratio", in.Unit)
			}
		case MetricDepth:
			if !in.Value.Equal(depthVal) {
				t.Errorf("depth value = %s, want %s", in.Value, depthVal)
			}
		case MetricPriceImpact:
			if !in.Value.Equal(impactVal) {
				t.Errorf("price impact value = %s, want %s", in.Value, impactVal)
			}
		case MetricConcentration:
			if !in.Value.Equal(concVal) {
				t.Errorf("concentration value = %s, want %s", in.Value, concVal)
			}
		case MetricCostLoss:
			if !in.Value.Equal(costVal) {
				t.Errorf("cost value = %s, want %s", in.Value, costVal)
			}
		}
	}
}

func TestHealthScoreCustomWeights(t *testing.T) {
	// Custom weights: spread counts double, others negligible.
	w := HealthScoreWeights{
		Spread:        decimal.NewFromFloat(0.6),
		Depth:         decimal.NewFromFloat(0.1),
		PriceImpact:   decimal.NewFromFloat(0.1),
		Concentration: decimal.NewFromFloat(0.1),
		Cost:          decimal.NewFromFloat(0.1),
	}

	spread := fixedMetric(MetricSpread, decimal.NewFromFloat(0.05), checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(25), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.NewFromFloat(10.0), checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.NewFromFloat(0.15), checks.UnitRatio)

	result := HealthScoreWeighted(spread, depth, impact, conc, decimal.NewFromFloat(8.0), true, "", w)

	if !result.Determined {
		t.Fatalf("health score should be determined; reason: %s", result.Reason)
	}

	// Score should be dominated by spread (which is 0.05 → normalised 90).
	if result.Value.LessThan(decimal.NewFromInt(70)) {
		t.Errorf("with 60%% weight on a good spread, score should be high, got %s", result.Value)
	}
}

func TestHealthScoreClampsExtremeValues(t *testing.T) {
	// Metrics at absurd values should clamp, not error.
	spread := fixedMetric(MetricSpread, decimal.NewFromFloat(999.0), checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(99999), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.NewFromFloat(9999.0), checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.NewFromFloat(999.0), checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.NewFromFloat(9999.0), true, "")

	if !result.Determined {
		t.Fatalf("health score should handle extreme values without failing; reason: %s", result.Reason)
	}

	// Spread, impact, cost, concentration all saturate at worst (0).
	// Depth saturates at best (100). With equal weights: 100 * 0.2 = 20.
	want := decimal.NewFromInt(20)
	if !result.Value.Equal(want) {
		t.Errorf("extreme values score = %s, want %s", result.Value, want)
	}
}

func TestHealthScoreInputsCount(t *testing.T) {
	spread := fixedMetric(MetricSpread, decimal.Zero, checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(10), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.Zero, checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.Zero, checks.UnitRatio)

	result := HealthScore(spread, depth, impact, conc, decimal.Zero, true, "")

	if len(result.Inputs) != 5 {
		t.Errorf("expected exactly 5 inputs, got %d", len(result.Inputs))
	}

	// Check IDs are present.
	ids := make(map[string]bool)
	for _, in := range result.Inputs {
		ids[in.ID] = true
	}
	for _, expected := range []string{MetricSpread, MetricDepth, MetricPriceImpact, MetricConcentration, MetricCostLoss} {
		if !ids[expected] {
			t.Errorf("missing input ID: %s", expected)
		}
	}
}

func TestHealthScoreCostDefaultReason(t *testing.T) {
	spread := fixedMetric(MetricSpread, decimal.Zero, checks.UnitRatio)
	depth := fixedMetric(MetricDepth, decimal.NewFromInt(10), checks.UnitCount)
	impact := fixedMetric(MetricPriceImpact, decimal.Zero, checks.UnitPercent)
	conc := fixedMetric(MetricConcentration, decimal.Zero, checks.UnitRatio)

	// Empty cost reason should get a default.
	result := HealthScore(spread, depth, impact, conc, decimal.Zero, false, "")

	if result.Determined {
		t.Error("should be undetermined with undetermined cost")
	}

	// Check the cost input has a reason.
	for _, in := range result.Inputs {
		if in.ID == MetricCostLoss {
			if in.Reason == "" {
				t.Error("undetermined cost input should have a reason")
			}
			if in.Determined {
				t.Error("cost input should be undetermined")
			}
		}
	}
}
