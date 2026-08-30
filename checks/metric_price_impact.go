// PriceImpactMetric measures price impact as a function of trade size — how
// much worse the effective rate gets as the trade grows.
//
// This is a metric, not a check. No threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// PriceImpactMetric measures how much the effective rate degrades with size.
type PriceImpactMetric struct {
	DEX       *dex.Client
	ProbeSize decimal.Decimal
	FullSize  decimal.Decimal
}

// Describe implements Metric.
func (PriceImpactMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "price-impact.size",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Venue: VenuePathfinding,
		Title: "Price impact as a function of trade size",
		CanDetermine: "How much the effective rate degrades between a small " +
			"probe and a full-size trade, as a percentage.",
		CannotDetermine: "The full curve shape — this reports the single " +
			"degradation figure between probe and full size. The venue is " +
			"pathfinding (order book plus AMM), so this and a book-only spread " +
			"observe different markets — see docs/liquidity-venues.md.",
	}
}

// Run implements Metric.
func (m PriceImpactMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	// Price impact is measured via pathfinding, the same as depth.executable:
	// a DERIVATIVE corridor prices end to end through its intermediate asset
	// without substitution, so only NO-MARKET is a structural dead end here.
	if s.Integrity == integrityNoMarket {
		return MetricUndetermined(d, s, fmt.Sprintf(
			"%s has no path to %s by construction (NO-MARKET): there is no rate at "+
				"any size for a probe-to-full comparison to measure degradation between",
			s.Send.Code, s.Receive.Code))
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to price paths")
	}

	probe := m.ProbeSize
	if probe.IsZero() {
		probe = decimal.NewFromInt(1)
	}
	full := m.FullSize
	if full.IsZero() {
		full = decimal.NewFromInt(5000)
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	probePath, probeErr := m.DEX.BestPath(ctx, s.Send, probe, s.Receive)
	fullPath, fullErr := m.DEX.BestPath(ctx, s.Send, full, s.Receive)

	switch {
	case probeErr != nil && fullPath != nil:
		evidence.Observed = fmt.Sprintf("probe=%s: error: %v, full=%s: %s",
			probe, probeErr, full, fullPath.DestAmount)
		return MetricUndetermined(d, s,
			fmt.Sprintf("probe size %s failed: %v", probe, probeErr), evidence)
	case probeErr != nil && fullPath == nil:
		evidence.Observed = fmt.Sprintf("probe=%s: error: %v, full=%s: error: %v",
			probe, probeErr, full, fullErr)
		return MetricUndetermined(d, s,
			fmt.Sprintf("both sizes failed: probe=%v, full=%v", probeErr, fullErr), evidence)
	case probePath == nil:
		return MetricUndetermined(d, s, "no path found at probe size", evidence)
	case fullPath == nil:
		evidence.Observed = fmt.Sprintf("probe=%s: %s, full=%s: no path",
			probe, probePath.DestAmount, full)
		return MetricUndetermined(d, s, "no path found at full size", evidence)
	}

	probeRate := probePath.Rate()
	fullRate := fullPath.Rate()

	if probeRate.IsZero() {
		return MetricUndetermined(d, s, "probe rate is zero, cannot compute impact", evidence)
	}

	impact := probeRate.Sub(fullRate).Div(probeRate).Mul(decimal.NewFromInt(100))
	if impact.IsNegative() {
		impact = decimal.Zero
	}

	evidence.Observed = fmt.Sprintf(
		"probe=%s: dest=%s, rate=%s; full=%s: dest=%s, rate=%s; impact=%s%%",
		probe, probePath.DestAmount, probeRate.StringFixed(4),
		full, fullPath.DestAmount, fullRate.StringFixed(4),
		impact.StringFixed(4))

	summary := fmt.Sprintf(
		"price impact %s%% from %s to %s %s",
		impact.StringFixed(2), probe, full, s.Send.Code)

	return MetricValue(d, s, impact, UnitPercent, summary, evidence)
}
