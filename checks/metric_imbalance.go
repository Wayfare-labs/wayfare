package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// ImbalanceMetric measures the ratio of bid-side to ask-side depth on the
// direct order book within a band around the reference mid.
//
// The band is defined as a percentage of the reference mid. Both the bid
// depth and ask depth within the band are reported as evidence, and the
// headline value is the ratio bid_depth / ask_depth. A ratio > 1 means the
// bid side has more depth than the ask side; a ratio < 1 means the ask side
// is deeper. When one side is empty and the other is not, the result is
// a measured extreme (zero or infinite) rather than undetermined. A missing
// or unscorable reference mid, or a corridor with no direct pair, is
// undetermined.
type ImbalanceMetric struct {
	// DEX is the Horizon client used to fetch the order book.
	DEX *dex.Client

	// Reference is the independent reference rate for the corridor's fiat
	// pair, already resolved and cross-checked by the caller. The band is
	// defined as a percentage of this mid.
	Reference refrate.Rate

	// ReferenceUnavailable is set by the caller when no reference rate
	// could be obtained at all. Non-empty means Reference is meaningless
	// and must not be read.
	ReferenceUnavailable string

	// BandPct is the half-width of the band around the reference mid, as a
	// percentage. For example, BandPct = 5 means the band extends from
	// mid * 0.95 to mid * 1.05. Defaults to 5% when zero.
	BandPct decimal.Decimal
}

var defaultBandPct = decimal.NewFromInt(5)

// Describe implements Metric.
func (ImbalanceMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "imbalance.bid-ask-depth",
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Venue: VenueOrderBook,
		Title: "Bid/ask depth imbalance within a band around the reference mid",
		CanDetermine: "The ratio of bid-side depth to ask-side depth on the " +
			"direct order book, measured within a percentage band around the " +
			"independent reference mid. Both depths are reported as evidence.",
		CannotDetermine: "Whether the imbalance reflects executable liquidity " +
			"or stale offers. The band is defined against the reference mid; " +
			"when the reference is unscorable or unavailable, or the corridor " +
			"has no direct pair by construction, the result is undetermined. " +
			"AMM liquidity is not observed by the order-book venue.",
	}
}

// Run implements Metric.
func (m ImbalanceMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if res, structural := structuralUndetermined(d, s); structural {
		return res
	}

	if m.ReferenceUnavailable != "" {
		return MetricUndetermined(d, s, "reference rate unavailable: "+m.ReferenceUnavailable)
	}
	if !m.Reference.Scorable() {
		reason := "reference cross-check could not be scored: the two providers " +
			"disagreed too far apart to trust either"
		if m.Reference.Note != "" {
			reason += " (" + m.Reference.Note + ")"
		}
		return MetricUndetermined(d, s, reason)
	}
	if m.Reference.Mid.IsZero() || m.Reference.Mid.IsNegative() {
		return MetricUndetermined(d, s, "no positive reference mid available")
	}

	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to fetch the order book")
	}

	bandPct := m.BandPct
	if bandPct.IsZero() {
		bandPct = defaultBandPct
	}

	refMid := m.Reference.Mid
	bandHalf := refMid.Mul(bandPct).Div(decimal.NewFromInt(100))
	bandLow := refMid.Sub(bandHalf)
	bandHigh := refMid.Add(bandHalf)

	sell, buy, substituted := bookPair(s)
	bids, asks, err := m.DEX.OrderBookRaw(ctx, sell, buy)
	if err != nil {
		return MetricUndetermined(d, s,
			fmt.Sprintf("order book fetch failed: %v", err))
	}

	refEvidence := Evidence{
		Source:     fmt.Sprintf("refrate %s via %s", m.Reference.Pair(), m.Reference.Source),
		Observed:   fmt.Sprintf("mid=%s, band_pct=%s%%, band=[%s, %s]", refMid, bandPct.StringFixed(2), bandLow.StringFixed(6), bandHigh.StringFixed(6)),
		ObservedAt: at,
	}

	// Sum offer amounts on each side within the band.
	bidDepth := sumInBand(bids, bandLow, bandHigh)
	askDepth := sumInBand(asks, bandLow, bandHigh)

	// An empty side is a measured extreme, not undetermined.
	// If bids exist but asks don't, ratio is 0. If asks exist but bids don't, ratio is 1,000,000.
	var ratio decimal.Decimal
	var summary string

	switch {
	case bidDepth.IsZero() && !askDepth.IsZero():
		// No bid depth in band, ask depth exists -> ratio = 0
		ratio = decimal.Zero
		summary = fmt.Sprintf("bid depth in band %s, ask depth in band %s (one-sided: no bids in band)", bidDepth, askDepth)
	case askDepth.IsZero() && !bidDepth.IsZero():
		// No ask depth in band, bid depth exists -> ratio = 1,000,000 (sentinel for infinite)
		ratio = decimal.NewFromInt(1000000)
		summary = fmt.Sprintf("bid depth in band %s, ask depth in band %s (one-sided: no asks in band)", bidDepth, askDepth)
	case bidDepth.IsZero() && askDepth.IsZero():
		// Both sides empty in band
		evidence := Evidence{
			Source:     bookSource("/order_book", s, sell, buy, substituted),
			Observed:   "no bids in band, no asks in band",
			ObservedAt: at,
		}
		return MetricUndetermined(d, s, "order book has no depth in band: no bids and no asks", evidence, refEvidence)
	default:
		// Both sides have depth in band
		ratio = bidDepth.Div(askDepth)
		summary = fmt.Sprintf("bid depth in band %s, ask depth in band %s, ratio %s", bidDepth.StringFixed(4), askDepth.StringFixed(4), ratio.StringFixed(4))
	}

	bookEvidence := Evidence{
		Source:     bookSource("/order_book", s, sell, buy, substituted),
		Observed:   fmt.Sprintf("bid_depth_in_band=%s, ask_depth_in_band=%s, ratio=%s", bidDepth.StringFixed(4), askDepth.StringFixed(4), ratio.StringFixed(4)),
		ObservedAt: at,
	}

	if substituted {
		summary = fmt.Sprintf("on the underlying %s book: %s", buy.Code, summary)
	}

	return MetricValue(d, s, ratio, UnitRatio, summary, bookEvidence, refEvidence)
}

// sumInBand sums the amounts of offers whose price lies within [low, high].
func sumInBand(offers []dex.Offer, low, high decimal.Decimal) decimal.Decimal {
	sum := decimal.Zero
	for _, o := range offers {
		if o.Price.GreaterThanOrEqual(low) && o.Price.LessThanOrEqual(high) {
			sum = sum.Add(o.Amount)
		}
	}
	return sum
}
