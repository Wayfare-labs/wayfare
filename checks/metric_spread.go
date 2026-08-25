// SpreadMetric measures the bid/ask spread on the direct order book for a
// corridor, where one exists.
//
// The spread is the cheapest possible signal of whether a market is real.
// A spread of 128.8% of mid says "no two-sided participation" far more
// directly than a loss percentage does.
//
// This is a metric, not a check. It produces a quantity, not a pass/fail.
// Deciding what spread is unacceptable is a maintainer-owned judgement, and
// no such threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/Wayfare-labs/wayfare/dex"
)

// SpreadMetric measures the bid/ask spread on the direct order book.
type SpreadMetric struct {
	// DEX is the Horizon client used to fetch the order book.
	DEX *dex.Client
}

// Describe implements Metric.
func (SpreadMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "spread.bid-ask",
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Title: "Bid/ask spread on the direct order book",
		CanDetermine: "The bid/ask spread as a percentage of mid, read from " +
			"Horizon's /order_book endpoint for the corridor's direct pair.",
		CannotDetermine: "Whether the spread reflects executable depth or " +
			"only the top of book. Horizon's order_book endpoint does not " +
			"expose AMM liquidity, so the spread measures the book alone.",
	}
}

// Run implements Metric.
//
// It reads the order book for the corridor's direct pair and computes the
// spread as (ask - bid) / mid, as a percentage. When one or both sides are
// empty, the result is undetermined.
func (m SpreadMetric) Run(ctx context.Context, s Subject) MetricResult {
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
		evidence.Observed = fmt.Sprintf("bids: %d, asks: %d, dust: %d",
			h.BidLevels, h.AskLevels, h.DustLevels)
		return MetricUndetermined(d, s, reason, evidence)
	}

	evidence.Observed = fmt.Sprintf("spread=%s%%, bid=%s, ask=%s, bids=%d, asks=%d, dust=%d",
		h.SpreadPct.StringFixed(4), h.BestBid, h.BestAsk,
		h.BidLevels, h.AskLevels, h.DustLevels)

	summary := fmt.Sprintf(
		"spread %s%% of mid: best bid %s, best ask %s, %d bid levels, %d ask levels",
		h.SpreadPct.StringFixed(2), h.BestBid, h.BestAsk,
		h.BidLevels, h.AskLevels)

	return MetricValue(d, s, h.SpreadPct, UnitPercent, summary, evidence)
}
