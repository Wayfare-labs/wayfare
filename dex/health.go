package dex

import (
	"context"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// BookHealth summarises whether a market is functioning.
//
// This is a diagnostic, not a pricing input — pricing goes through
// StrictSendPaths. The distinction matters because the meaning of the order
// book's amount field is genuinely ambiguous between the base and counter
// asset, and that ambiguity is unresolved here. Every field below is chosen
// so the ambiguity cannot change the conclusion: level counts and the
// bid/ask spread are computed from prices alone.
type BookHealth struct {
	Selling   asset.Asset
	Buying    asset.Asset
	BidLevels int
	AskLevels int

	BestBid decimal.Decimal
	BestAsk decimal.Decimal
	// Mid is the average of BestBid and BestAsk when both sides are present.
	// It remains zero when the book cannot provide a two-sided mid.
	Mid     decimal.Decimal

	// SpreadPct is (ask - bid) / mid, as a percentage. On a healthy market
	// this is a fraction of one percent. The live USDC/NGNC book measured
	// 128.8% on 2026-08-04.
	SpreadPct decimal.Decimal

	// DustLevels counts levels priced at or near zero. These are abandoned
	// or malformed offers. Their presence is a strong signal that a market
	// is unmaintained, and a naive router that treated a zero-priced bid as
	// real would compute a catastrophic fill.
	DustLevels int
}

// Functional reports whether the market is healthy enough to route through.
//
// The 5% spread threshold is deliberately generous. A normal FX spread is
// well under 1%; anything past 5% signals a market with no real two-sided
// participation, and this corridor measured twenty-five times that.
func (h BookHealth) Functional() bool {
	if h.BidLevels == 0 || h.AskLevels == 0 {
		return false
	}
	return h.SpreadPct.LessThan(decimal.NewFromInt(5))
}

// Summary is a one-line human description of the market's condition.
func (h BookHealth) Summary() string {
	switch {
	case h.BidLevels == 0 && h.AskLevels == 0:
		return "no market: order book is empty"
	case h.BidLevels == 0:
		return "one-sided market: no bids, nobody is buying"
	case h.AskLevels == 0:
		return "one-sided market: no asks, nobody is selling"
	case !h.Functional():
		return "dysfunctional market: spread " + h.SpreadPct.StringFixed(1) + "% of mid"
	default:
		return "functional market: spread " + h.SpreadPct.StringFixed(2) + "% of mid"
	}
}

type wireOffer struct {
	Price  string `json:"price"`
	Amount string `json:"amount"`
}

type wireOrderBook struct {
	Bids []wireOffer `json:"bids"`
	Asks []wireOffer `json:"asks"`
}

// dustThreshold is the price below which an offer is treated as abandoned
// rather than real.
var dustThreshold = decimal.NewFromFloat(0.0000001)

// OrderBook fetches the market and summarises its health.
func (c *Client) OrderBook(ctx context.Context, selling, buying asset.Asset) (*BookHealth, error) {
	q := url.Values{}
	for k, v := range selling.HorizonParams("selling") {
		q.Set(k, v)
	}
	for k, v := range buying.HorizonParams("buying") {
		q.Set(k, v)
	}
	q.Set("limit", "200")

	var body wireOrderBook
	if err := c.get(ctx, "/order_book", q, &body); err != nil {
		return nil, err
	}

	h := &BookHealth{Selling: selling, Buying: buying}

	// Dust is excluded before picking the best price on each side. Including
	// a zero-priced bid would drag the computed spread to 100% and mask
	// whatever the real spread is.
	realBids := make([]decimal.Decimal, 0, len(body.Bids))
	for _, b := range body.Bids {
		p, err := decimal.NewFromString(b.Price)
		if err != nil {
			continue
		}
		if p.LessThanOrEqual(dustThreshold) {
			h.DustLevels++
			continue
		}
		realBids = append(realBids, p)
	}
	realAsks := make([]decimal.Decimal, 0, len(body.Asks))
	for _, a := range body.Asks {
		p, err := decimal.NewFromString(a.Price)
		if err != nil {
			continue
		}
		if p.LessThanOrEqual(dustThreshold) {
			h.DustLevels++
			continue
		}
		realAsks = append(realAsks, p)
	}

	h.BidLevels = len(realBids)
	h.AskLevels = len(realAsks)

	if h.BidLevels > 0 {
		h.BestBid = realBids[0]
		for _, p := range realBids {
			if p.GreaterThan(h.BestBid) {
				h.BestBid = p
			}
		}
	}
	if h.AskLevels > 0 {
		h.BestAsk = realAsks[0]
		for _, p := range realAsks {
			if p.LessThan(h.BestAsk) {
				h.BestAsk = p
			}
		}
	}

	if h.BidLevels > 0 && h.AskLevels > 0 {
		h.Mid = h.BestBid.Add(h.BestAsk).Div(decimal.NewFromInt(2))
		if !h.Mid.IsZero() {
			h.SpreadPct = h.BestAsk.Sub(h.BestBid).
				Div(h.Mid).
				Mul(decimal.NewFromInt(100))
		}
	}
	return h, nil
}
