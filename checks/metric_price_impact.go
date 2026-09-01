// PriceImpactMetric measures price impact as a function of trade size — how
// much worse the effective rate gets as the trade grows.
//
// The shape of the curve between a small probe and a full-size trade is where
// the corridor's behaviour lives: a corridor with constant rates across sizes
// has no price impact, while one whose rate degrades steeply between 1 and
// 100 USDC is a corridor where size matters.
//
// This is a metric, not a check. No threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// defaultPriceImpactSizes is the set of send amounts the metric probes when
// no explicit Sizes are provided.  The list is deliberately coarse — each
// size is a Horizon round trip — and covers the range the project's ladder
// prices.
var defaultPriceImpactSizes = []decimal.Decimal{
	decimal.NewFromInt(1),
	decimal.NewFromInt(10),
	decimal.NewFromInt(100),
	decimal.NewFromInt(1000),
	decimal.NewFromInt(5000),
}

// PriceImpactPoint is one measurement along the price impact curve.
type PriceImpactPoint struct {
	// Size is the send amount for this measurement.
	Size decimal.Decimal

	// DestAmount is the best destination amount Horizon returned.
	DestAmount decimal.Decimal

	// Rate is the effective rate: DestAmount / Size.
	Rate decimal.Decimal

	// ImpactPct is the degradation relative to the reference rate (the rate
	// at the smallest size), expressed as a percentage.  Zero at the
	// reference size itself.
	ImpactPct decimal.Decimal
}

// PriceImpactCurve is the full shape of price impact across multiple sizes.
type PriceImpactCurve struct {
	// ReferenceRate is the rate at the smallest size — the baseline against
	// which all other points are measured.
	ReferenceRate decimal.Decimal

	// Points contains one entry per probed size, ordered smallest to
	// largest.
	Points []PriceImpactPoint
}

// PriceImpactMetric measures how much the effective rate degrades with size.
type PriceImpactMetric struct {
	DEX *dex.Client

	// Sizes is the list of send amounts to probe.  When empty the default
	// sizes are used.  Each size is a Horizon round trip; keep the list
	// short.
	Sizes []decimal.Decimal

	// ProbeSize is the smallest size to probe (backward-compatible field).
	// When Sizes is non-empty it takes precedence; otherwise ProbeSize is
	// used as the first entry and FullSize as the last, preserving the
	// original two-point comparison for callers that set them explicitly.
	ProbeSize decimal.Decimal

	// FullSize is the largest size to probe (backward-compatible field).
	// See ProbeSize above.
	FullSize decimal.Decimal
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
			"probe and a full-size trade, as a percentage, across a curve of " +
			"probed sizes.",
		CannotDetermine: "The complete price-impact shape — this reports the " +
			"observed curve across the probed sizes, which is a sample of " +
			"the full behaviour. The venue is pathfinding (order book plus " +
			"AMM), so this and a book-only spread observe different markets " +
			"— see docs/liquidity-venues.md.",
	}
}

// sizes returns the effective list of sizes to probe, falling back to the
// defaults when nothing was supplied.  The backward-compatible ProbeSize /
// FullSize fields are respected when no explicit Sizes were given.
func (m PriceImpactMetric) sizes() []decimal.Decimal {
	if len(m.Sizes) > 0 {
		out := make([]decimal.Decimal, len(m.Sizes))
		copy(out, m.Sizes)
		return out
	}
	// Backward compatibility: honour explicit ProbeSize / FullSize even
	// though the caller should migrate to Sizes.
	if !m.ProbeSize.IsZero() || !m.FullSize.IsZero() {
		probe := m.ProbeSize
		if probe.IsZero() {
			probe = decimal.NewFromInt(1)
		}
		full := m.FullSize
		if full.IsZero() {
			full = decimal.NewFromInt(5000)
		}
		return []decimal.Decimal{probe, full}
	}
	out := make([]decimal.Decimal, len(defaultPriceImpactSizes))
	copy(out, defaultPriceImpactSizes)
	return out
}

// RunCurve measures price impact across multiple sizes and returns the full
// curve.  It is the richer entry point; Run implements the Metric interface
// by returning the maximum impact observed.
func (m PriceImpactMetric) RunCurve(ctx context.Context, s Subject) (*PriceImpactCurve, MetricResult) {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return nil, MetricUndetermined(d, s, "no send or receive asset specified")
	}
	// Price impact is measured via pathfinding, the same as depth.executable:
	// a DERIVATIVE corridor prices end to end through its intermediate asset
	// without substitution, so only NO-MARKET is a structural dead end here.
	if s.Integrity == integrityNoMarket {
		return nil, MetricUndetermined(d, s, fmt.Sprintf(
			"%s has no path to %s by construction (NO-MARKET): there is no rate at "+
				"any size for a curve of probe sizes to measure degradation between",
			s.Send.Code, s.Receive.Code))
	}
	if m.DEX == nil {
		return nil, MetricUndetermined(d, s, "no DEX client available to price paths")
	}

	sizes := m.sizes()
	if len(sizes) == 0 {
		return nil, MetricUndetermined(d, s, "no sizes to probe")
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	type probeResult struct {
		size decimal.Decimal
		path *dex.Path
		err  error
	}

	// Probe all sizes.
	results := make([]probeResult, 0, len(sizes))
	for _, size := range sizes {
		path, err := m.DEX.BestPath(ctx, s.Send, size, s.Receive)
		results = append(results, probeResult{size: size, path: path, err: err})
	}

	// Separate successful probes from failures.
	var points []PriceImpactPoint
	var probeErrors []string
	var probeNoPath []string

	for _, r := range results {
		switch {
		case r.err != nil && strings.Contains(r.err.Error(), "no path found"):
			probeNoPath = append(probeNoPath, r.size.String())
		case r.err != nil:
			probeErrors = append(probeErrors, fmt.Sprintf("size=%s: %v", r.size, r.err))
		case r.path == nil:
			probeNoPath = append(probeNoPath, r.size.String())
		default:
			points = append(points, PriceImpactPoint{
				Size:       r.size,
				DestAmount: r.path.DestAmount,
				Rate:       r.path.Rate(),
			})
		}
	}

	// All sizes failed or returned no path.
	if len(points) == 0 {
		reasons := []string{}
		if len(probeErrors) > 0 {
			reasons = append(reasons, fmt.Sprintf("%d probe errors (%s)",
				len(probeErrors), strings.Join(probeErrors, "; ")))
		}
		if len(probeNoPath) > 0 {
			reasons = append(reasons, fmt.Sprintf("no path at sizes %s",
				strings.Join(probeNoPath, ", ")))
		}
		evidence.Observed = fmt.Sprintf("probed %d sizes, all failed: %s",
			len(sizes), strings.Join(reasons, "; "))
		return nil, MetricUndetermined(d, s,
			fmt.Sprintf("no path found at any of the %d sizes probed", len(sizes)),
			evidence)
	}

	// Some sizes succeeded, some may have failed — record this in evidence
	// but continue with the points we have.
	if len(probeErrors) > 0 || len(probeNoPath) > 0 {
		var parts []string
		if len(probeErrors) > 0 {
			parts = append(parts, fmt.Sprintf("%d errors", len(probeErrors)))
		}
		if len(probeNoPath) > 0 {
			parts = append(parts, fmt.Sprintf("no path at sizes %s",
				strings.Join(probeNoPath, ", ")))
		}
		evidence.Observed = fmt.Sprintf("probed %d sizes, %d priced, partial: %s",
			len(sizes), len(points), strings.Join(parts, "; "))
	}

	// The reference rate is the rate at the smallest probed size.
	refRate := points[0].Rate
	if refRate.IsZero() {
		evidence.Observed = fmt.Sprintf("reference rate at size %s is zero", points[0].Size)
		return nil, MetricUndetermined(d, s,
			"reference rate is zero, cannot compute impact curve", evidence)
	}

	// Compute impact for each point.
	var maxImpact decimal.Decimal
	for i := range points {
		if i == 0 {
			points[i].ImpactPct = decimal.Zero
			continue
		}
		impact := refRate.Sub(points[i].Rate).Div(refRate).Mul(decimal.NewFromInt(100))
		if impact.IsNegative() {
			impact = decimal.Zero
		}
		points[i].ImpactPct = impact
		if impact.GreaterThan(maxImpact) {
			maxImpact = impact
		}
	}

	curve := &PriceImpactCurve{
		ReferenceRate: refRate,
		Points:        points,
	}

	// Build evidence showing each point.
	var obsParts []string
	for _, p := range points {
		obsParts = append(obsParts, fmt.Sprintf("size=%s: rate=%s, impact=%s%%",
			p.Size.StringFixed(1), p.Rate.StringFixed(4), p.ImpactPct.StringFixed(4)))
	}
	evidence.Observed = strings.Join(obsParts, "; ")

	summary := fmt.Sprintf(
		"price impact curve across %d sizes: max impact %s%% (ref rate %s at size %s)",
		len(points), maxImpact.StringFixed(2), refRate.StringFixed(4),
		points[0].Size.StringFixed(1))

	return curve, MetricValue(d, s, maxImpact, UnitPercent, summary, evidence)
}

// Run implements Metric.  It returns the maximum price impact observed across
// the probed sizes, with the full curve encoded in the evidence.
func (m PriceImpactMetric) Run(ctx context.Context, s Subject) MetricResult {
	_, res := m.RunCurve(ctx, s)
	return res
}
