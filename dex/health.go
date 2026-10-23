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
	Mid decimal.Decimal

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

// maxDecimalExponent bounds how far a wire decimal may stray from unity
// before its level is treated as corrupted rather than measured.
//
// A Stellar price or amount carries at most 7 decimal places, and no asset on
// the network approaches a magnitude of 1e30 — an offer outside this bound
// is not a market fact but garbage bytes. The bound is not pedantry:
// shopspring/decimal parses exponents lazily, so "1e999999999" is cheap to
// hold but materialises to a billion-digit number the first time it is
// compared against anything — fuzzing the order-book path found the decoder
// pegging a worker on exactly such a body (#589). A level beyond the bound is
// skipped before any arithmetic can touch it.
const maxDecimalExponent = 30

// pricedLevels reduces one side of the wire book to the prices that count.
//
// A level becomes a priced level only when it could actually fill: the price
// must parse and the amount must parse and be positive. A level with a
// garbled or non-positive amount is malformed — counting it would let a
// nonsense offer inflate the level counts and make an abandoned market look
// populated, which is the same failure mode a garbled price would cause.
//
// Dust is the one malformed shape that is counted rather than skipped: a
// parseable price at or near zero is a signal about the market in its own
// right (abandoned offers), so it is reported via the dust count instead of
// silently dropped.
func pricedLevels(offers []wireOffer) (prices []decimal.Decimal, dust int) {
	prices = make([]decimal.Decimal, 0, len(offers))
	for _, o := range offers {
		p, err := decimal.NewFromString(o.Price)
		if err != nil || absExponent(p) > maxDecimalExponent {
			continue
		}
		if p.LessThanOrEqual(dustThreshold) {
			dust++
			continue
		}
		a, err := decimal.NewFromString(o.Amount)
		if err != nil || !a.IsPositive() || absExponent(a) > maxDecimalExponent {
			continue
		}
		prices = append(prices, p)
	}
	return prices, dust
}

// absExponent reports a decimal's exponent as an absolute value, saturating
// at max int so the comparison against maxDecimalExponent is total.
func absExponent(d decimal.Decimal) int {
	e := int(d.Exponent())
	if e < 0 {
		e = -e
	}
	return e
}

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
	realBids, bidDust := pricedLevels(body.Bids)
	realAsks, askDust := pricedLevels(body.Asks)
	h.DustLevels = bidDust + askDust

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
