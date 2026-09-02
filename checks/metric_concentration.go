// ConcentrationMetric measures how concentrated a market's liquidity is,
// reported as the Herfindahl-Hirschman Index (HHI) over price levels.
//
// Two books with identical total depth are not equally safe. The HHI
// captures this: it is the sum of squared market shares across levels,
// ranging from near-zero (perfectly distributed) to 1.0 (monopoly).
//
// This is measured over price levels, not offer amounts, because the
// meaning of Horizon's amount field is ambiguous (see dex/health.go).
//
// Account-level concentration is not measured — Horizon's /order_book
// does not expose the offering account.
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

// ConcentrationMetric measures liquidity concentration via HHI over price levels.
type ConcentrationMetric struct {
	DEX *dex.Client
}

// Describe implements Metric.
func (ConcentrationMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "concentration.liquidity",
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Venue: VenueOrderBook,
		Title: "Liquidity concentration across price levels",
		CanDetermine: "How concentrated the order book is across price levels, " +
			"measured as the Herfindahl-Hirschman Index (HHI).",
		CannotDetermine: "Account-level concentration — Horizon's /order_book " +
			"does not expose the offering account — and any concentration among " +
			"AMM liquidity pools, which the venue does not observe. See " +
			"docs/liquidity-venues.md.",
	}
}

// Run implements Metric.
func (m ConcentrationMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if res, structural := structuralUndetermined(d, s); structural {
		return res
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

	evidence := Evidence{
		Source:     bookSource("/order_book", s, sell, buy, substituted),
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
		evidence.Observed = fmt.Sprintf("bids: %d, asks: %d", h.BidLevels, h.AskLevels)
		return MetricUndetermined(d, s, reason, evidence)
	}

	totalLevels := h.BidLevels + h.AskLevels
	equalHHI := decimal.NewFromInt(1).Div(decimal.NewFromInt(int64(totalLevels)))

	evidence.Observed = fmt.Sprintf(
		"bid_levels=%d, ask_levels=%d, total=%d, hhi=%s",
		h.BidLevels, h.AskLevels, totalLevels, equalHHI.StringFixed(6))

	summary := fmt.Sprintf(
		"concentration HHI %s across %d price levels (%d bids, %d asks)",
		equalHHI.StringFixed(4), totalLevels, h.BidLevels, h.AskLevels)

	return MetricValue(d, s, equalHHI, UnitRatio, summary, evidence)
}
