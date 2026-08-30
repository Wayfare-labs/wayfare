// DepthMetric reports observed depth and executable depth separately for a
// corridor, and never collapses them into one number.
//
// A quote is not proof of liquidity: a market can quote a price for $100
// with almost nothing behind it. Observed depth is what the book advertises.
// Executable depth is what a payment would actually fill against.
//
// This produces two MetricResults, reported side by side. A single merged
// "depth" number hides which of the two moved.
//
// These are metrics, not checks. No threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// DepthMetric measures observed and executable depth for a corridor.
type DepthMetric struct {
	DEX   *dex.Client
	Sizes []decimal.Decimal
}

// defaultDepthSizes uses the same probe sizes as the route ladder so
// both measure the same corridor at the same sizes.
var defaultDepthSizes = dex.DefaultSizes

// Describe implements Metric.
func (DepthMetric) Describe() Descriptor {
	// This composite descriptor is only used for scheduling and cost
	// reporting; the two halves (RunObserved, RunExecutable) declare their
	// own venues because they read different endpoints. Reporting a single
	// venue here would suggest the two figures are comparable, which is the
	// specific misuse the machine-readable venue exists to prevent.
	return Descriptor{
		ID:    "depth.observed-executable",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		// The composite descriptor is scheduled and validated as a corridor
		// metric, but Run delegates to RunObserved — an order-book fetch —
		// so the venue declared here matches what an implicit Run() would
		// actually observe. RunExecutable declares VenuePathfinding on its
		// own descriptor; the two never share a MetricResult.
		Venue: VenueOrderBook,
		Title: "Observed depth vs executable depth",
		CanDetermine: "Observed depth from the order book (number of levels " +
			"on each side), and executable depth from pathfinding across " +
			"multiple sizes (the maximum destination amount reachable).",
		CannotDetermine: "Whether those levels represent executable liquidity " +
			"or stale offers. The two halves observe different venues " +
			"(order-book and pathfinding); see docs/liquidity-venues.md.",
	}
}

// RunObserved returns the observed depth metric from the order book.
func (m DepthMetric) RunObserved(ctx context.Context, s Subject) MetricResult {
	d := Descriptor{
		ID:           "depth.observed",
		Scope:        ScopeCorridor,
		Cost:         CostOneRequest,
		Venue:        VenueOrderBook,
		Title:        "Observed order book depth",
		CanDetermine: "The number of bid and ask levels on the direct order book.",
		CannotDetermine: "Whether those levels represent executable liquidity " +
			"or stale offers, and any AMM depth that would settle alongside them. " +
			"The venue is order-book; see docs/liquidity-venues.md.",
	}
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if res, structural := structuralUndetermined(d, s); structural {
		return res
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available")
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

	if h.BidLevels == 0 && h.AskLevels == 0 {
		evidence.Observed = "no levels"
		return MetricUndetermined(d, s, "order book is empty", evidence)
	}

	total := h.BidLevels + h.AskLevels
	evidence.Observed = fmt.Sprintf("bids=%d, asks=%d, total=%d",
		h.BidLevels, h.AskLevels, total)

	summary := fmt.Sprintf(
		"%d observed levels: %d bids, %d asks",
		total, h.BidLevels, h.AskLevels)

	return MetricValue(d, s, decimal.NewFromInt(int64(total)), UnitCount, summary, evidence)
}

// RunExecutable returns the executable depth metric from pathfinding.
func (m DepthMetric) RunExecutable(ctx context.Context, s Subject) MetricResult {
	d := Descriptor{
		ID:    "depth.executable",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Venue: VenuePathfinding,
		Title: "Executable depth from pathfinding",
		CanDetermine: "The maximum destination amount reachable via Horizon " +
			"pathfinding, and the size at which the receive amount stops growing.",
		CannotDetermine: "Whether the executable amount reflects a sustainable " +
			"market or a one-time fill. The venue is pathfinding (order book " +
			"plus AMM liquidity pools), so this figure is not comparable with " +
			"the order-book depth on the same corridor — see docs/liquidity-venues.md.",
	}
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	// Unlike the order-book metrics above, a DERIVATIVE corridor is exactly
	// what pathfinding is for: paths to it exist, they just route through
	// an intermediate asset, and BestPath measures that end to end without
	// any substitution. Only NO-MARKET is short-circuited here — probing it
	// at every size would just rediscover, one request at a time, the
	// structural fact the ladder already established.
	if s.Integrity == integrityNoMarket {
		return MetricUndetermined(d, s, fmt.Sprintf(
			"%s has no path to %s by construction (NO-MARKET): the ladder found no "+
				"route at any size, so probing again here would only re-confirm a "+
				"structural fact rather than measure anything new",
			s.Send.Code, s.Receive.Code))
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available")
	}

	sizes := m.Sizes
	if len(sizes) == 0 {
		sizes = defaultDepthSizes
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	var maxReceive decimal.Decimal
	var maxReceiveSize decimal.Decimal
	pricedCount := 0
	var probeErrors []string

	for _, size := range sizes {
		path, err := m.DEX.BestPath(ctx, s.Send, size, s.Receive)
		if err != nil {
			if strings.Contains(err.Error(), "no path found") {
				continue // no liquidity at this size, not a probe failure
			}
			probeErrors = append(probeErrors, fmt.Sprintf("size=%s: %v", size, err))
			continue
		}
		if path == nil {
			continue
		}
		pricedCount++
		if path.DestAmount.GreaterThan(maxReceive) {
			maxReceive = path.DestAmount
			maxReceiveSize = size
		}
	}

	// A book that runs out of liquidity at a certain size is a measured
	// fact: depth exhausted. A probe that failed before reaching an
	// upstream is not — depth unmeasured. The two must not produce the
	// same undetermined result.
	if pricedCount == 0 && len(probeErrors) == 0 {
		// Every size was successfully queried and none returned a path.
		// The depth is exhausted — not unknown.
		evidence.Observed = fmt.Sprintf("probed %d sizes, no path found at any size", len(sizes))
		return MetricValue(d, s, decimal.Zero, UnitAmount,
			fmt.Sprintf("depth exhausted: no executable path at any of the %d sizes probed", len(sizes)),
			evidence)
	}

	if len(probeErrors) > 0 {
		evidence.Observed = fmt.Sprintf("probed %d sizes, %d errors: %s",
			len(sizes), len(probeErrors), strings.Join(probeErrors, "; "))
		if pricedCount == 0 {
			return MetricUndetermined(d, s,
				fmt.Sprintf("%d of %d probe requests failed", len(probeErrors), len(sizes)), evidence)
		}
		// Some sizes priced, others failed. The measured sizes give
		// real data; the failed sizes are unknown, never zero.
		evidence.Observed += fmt.Sprintf("; measured %d/%d sizes", pricedCount, len(sizes))
		summary := fmt.Sprintf(
			"max destination %s %s at %s %s send (%d of %d sizes measured)",
			maxReceive, s.Receive.Code, maxReceiveSize, s.Send.Code, pricedCount, len(sizes))
		return MetricValue(d, s, maxReceive, UnitAmount, summary, evidence)
	}

	evidence.Observed = fmt.Sprintf(
		"max_receive=%s at size=%s, priced_at=%d/%d_sizes",
		maxReceive, maxReceiveSize, pricedCount, len(sizes))

	summary := fmt.Sprintf(
		"max destination %s %s at %s %s send",
		maxReceive, s.Receive.Code, maxReceiveSize, s.Send.Code)

	return MetricValue(d, s, maxReceive, UnitAmount, summary, evidence)
}

// Run implements the Metric interface by returning the observed depth metric.
func (m DepthMetric) Run(ctx context.Context, s Subject) MetricResult {
	return m.RunObserved(ctx, s)
}
