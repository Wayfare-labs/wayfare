// DeviationMetric measures the gap between the mid implied by a corridor's
// direct order book and the independent reference mid, as a metric separate
// from route loss.
//
// # Why this is a different number from loss
//
// Route loss conflates two different things: how the market prices the pair,
// and what the route costs to traverse. A corridor can have a book priced
// close to the reference and still execute terribly because the path is
// long, or a book priced far from the reference that a short path traverses
// cheaply. This metric isolates the first half. It says nothing about the
// second — see price-impact.size and depth.executable for that.
//
// For a corridor whose structural floor sits at some loss percentage at
// dust size, knowing whether that floor sits in the book (this metric) or in
// the routing (price-impact.size, depth.executable) is the difference
// between "this market is mispriced" and "this market is fine but
// unreachable".
//
// # What this does not claim
//
// This reports a percentage, not a verdict — see checks.go's package
// comment: metrics qualify the headline, they never move it. It concludes
// nothing about manipulation or intent, and it is not a parallel/street-rate
// benchmark (see refrate.Parallel for that dimension). Thresholding the
// deviation is a maintainer-owned judgement that does not exist yet.
package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// DeviationMetric measures the direct book mid against an independent
// reference mid.
type DeviationMetric struct {
	// DEX is the Horizon client used to fetch the order book.
	DEX *dex.Client

	// Reference is the independent reference rate to compare the book
	// against, already resolved and cross-checked by the caller.
	//
	// This metric does not fetch or cache reference rates itself — that
	// policy (provider choice, caching, cross-provider agreement) lives in
	// refrate and is resolved once per sweep, not once per metric. Passing
	// the already-resolved rate in keeps this metric from either
	// duplicating that policy or silently diverging from it.
	Reference refrate.Rate

	// ReferenceUnavailable is set by the caller when no reference rate
	// could be obtained at all — a provider failure, typically. Non-empty
	// means Reference is meaningless and must not be read.
	ReferenceUnavailable string
}

// Describe implements Metric.
func (DeviationMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "deviation.book-vs-reference",
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Venue: VenueOrderBook,
		Title: "Deviation of the direct book mid from the reference mid",
		CanDetermine: "The signed percentage gap between the mid implied by the " +
			"corridor's direct order book and an independent reference mid, with " +
			"both mids and their sources as evidence.",
		CannotDetermine: "Whether a deviation reflects manipulation, a stale " +
			"reference, or a genuinely different local price. This is a " +
			"measurement of disagreement, not an explanation for it — and it says " +
			"nothing about what a route through this corridor actually costs; see " +
			"price-impact.size and depth.executable for that half.",
	}
}

// Run implements Metric.
//
// Undetermined covers: either side of the book is empty; the corridor has no
// direct pair by construction (DERIVATIVE without an Underlying substitute,
// or NO-MARKET); no reference rate was supplied; or the reference
// cross-check came back unscorable (see refrate.Rate.Scorable). The last two
// are the common case for two of the three corridors this project supports
// today, and are reported with the specific reason rather than skipped.
func (m DeviationMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if res, structural := structuralUndetermined(d, s); structural {
		return res
	}

	if m.ReferenceUnavailable != "" {
		return MetricUndetermined(d, s,
			"reference rate unavailable: "+m.ReferenceUnavailable)
	}
	if !m.Reference.Scorable() {
		reason := "reference cross-check could not be scored: the two providers " +
			"disagreed too far apart to trust either"
		if m.Reference.Note != "" {
			reason += " (" + m.Reference.Note + ")"
		}
		return MetricUndetermined(d, s, reason)
	}
	if m.Reference.Mid.IsZero() {
		return MetricUndetermined(d, s, "no reference mid available")
	}

	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to fetch the order book")
	}

	sell, buy, substituted := bookPair(s)
	h, err := m.DEX.OrderBook(ctx, sell, buy)
	if err != nil {
		return MetricUndetermined(d, s,
			fmt.Sprintf("order book fetch failed: %v", err))
	}

	refEvidence := Evidence{
		Source:     "refrate " + m.Reference.Pair() + " via " + m.Reference.Source,
		Observed:   fmt.Sprintf("mid=%s, as_of=%s", m.Reference.Mid, m.Reference.AsOf.UTC().Format(time.RFC3339)),
		ObservedAt: at,
	}

	if h.BidLevels == 0 || h.AskLevels == 0 {
		reason := "order book is empty"
		switch {
		case h.BidLevels == 0 && h.AskLevels == 0:
			reason = "order book is empty: no bids and no asks"
		case h.BidLevels == 0:
			reason = "one-sided market: no bids, nobody is buying"
		case h.AskLevels == 0:
			reason = "one-sided market: no asks, nobody is selling"
		}
		bookEvidence := Evidence{
			Source:     bookSource("/order_book", s, sell, buy, substituted),
			Observed:   fmt.Sprintf("bids: %d, asks: %d", h.BidLevels, h.AskLevels),
			ObservedAt: at,
		}
		return MetricUndetermined(d, s, reason, bookEvidence, refEvidence)
	}

	bookMid := h.BestBid.Add(h.BestAsk).Div(decimal.NewFromInt(2))
	if bookMid.IsZero() {
		bookEvidence := Evidence{
			Source:     bookSource("/order_book", s, sell, buy, substituted),
			Observed:   fmt.Sprintf("bid=%s, ask=%s", h.BestBid, h.BestAsk),
			ObservedAt: at,
		}
		return MetricUndetermined(d, s, "book mid is zero, cannot compute a deviation", bookEvidence, refEvidence)
	}

	// Signed: positive means the book prices Buy richer against Sell than
	// the reference does, negative means cheaper. Never blended with the
	// reference — both mids are reported so a reader can recompute this.
	deviation := bookMid.Sub(m.Reference.Mid).Div(m.Reference.Mid).Mul(decimal.NewFromInt(100))

	bookEvidence := Evidence{
		Source: bookSource("/order_book", s, sell, buy, substituted),
		Observed: fmt.Sprintf("book_mid=%s, bid=%s, ask=%s, bids=%d, asks=%d",
			bookMid, h.BestBid, h.BestAsk, h.BidLevels, h.AskLevels),
		ObservedAt: at,
	}

	summary := fmt.Sprintf(
		"book mid %s deviates %s%% from reference mid %s (%s)",
		bookMid.StringFixed(6), deviation.StringFixed(4), m.Reference.Mid.StringFixed(6), m.Reference.Source)

	return MetricValue(d, s, deviation, UnitPercent, summary, bookEvidence, refEvidence)
}
