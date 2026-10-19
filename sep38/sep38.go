// Package sep38 is a client for the SEP-38 Anchor RFQ API.
//
// SEP-38 is how an anchor answers "what will you give me for this?" when the
// two assets are not equivalent — USDC in, naira out. An anchor advertises
// support by publishing ANCHOR_QUOTE_SERVER in its stellar.toml; absence of
// that field means the anchor cannot be quoted programmatically at all.
//
// # The fee denomination trap
//
// SEP-38 returns a fee that may be denominated in EITHER the sell asset or
// the buy asset, and the response tells you which via fee.asset. This is easy
// to miss, and getting it wrong produces a unit error rather than a crash —
// the arithmetic succeeds and the number is simply wrong.
//
// The spec's own worked example is exactly the shape a remittance uses: sell
// 100 USDC, buy 500 BRL, price 0.18, and a fee of 10.00 denominated in USDC.
// Naively computing a pre-fee gross as buy_amount + fee yields 510 — ten
// units of USDC added to five hundred units of BRL, a meaningless quantity
// that looks plausible enough to ship.
//
// The correct handling falls out of the two definitions the spec gives for
// price, depending on where the fee sits:
//
//	fee in sell asset:  price = (sell_amount - fee) / buy_amount
//	fee in buy asset:   price = sell_amount / (buy_amount + fee)
//
// Solving each for the pre-fee gross, expressed in the buy asset, gives the
// same expression in both cases:
//
//	gross_in_buy_asset = sell_amount / price
//
// which is what this package uses. It needs no branch on fee.asset, and it is
// correct whichever denomination the anchor chose. Checking the example: with
// a sell-asset fee, 100 / 0.18 = 555.56 BRL gross, so the fee is 55.56 BRL —
// not 10.
package sep38

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Context values defined by SEP-38. The anchor may quote different prices
// depending on which flow the quote is for.
const (
	ContextSEP6  = "sep6"
	ContextSEP31 = "sep31"
)

// Client talks to one anchor's SEP-38 quote server.
type Client struct {
	// BaseURL is the ANCHOR_QUOTE_SERVER value from the anchor's stellar.toml.
	BaseURL string

	// HTTPClient defaults to a client with a 15s timeout.
	HTTPClient *http.Client

	// AuthToken is the SEP-10 JWT. Indicative prices are usually public;
	// firm quotes always require authentication.
	AuthToken string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Fee is the anchor's charge for a conversion.
type Fee struct {
	Total   decimal.Decimal
	Asset   asset.Asset
	Details []FeeDetail
}

// FeeDetail is one named component of a fee, when the anchor itemises.
type FeeDetail struct {
	Name        string
	Description string
	Amount      decimal.Decimal
}

// Quote is a normalised SEP-38 price or quote response.
//
// SellAmount, BuyAmount and Price are as returned by the anchor. The derived
// fields exist so callers never have to reason about fee denomination.
type Quote struct {
	ID         string // set only for firm quotes from POST /quote
	ExpiresAt  time.Time
	SellAsset  asset.Asset
	BuyAsset   asset.Asset
	SellAmount decimal.Decimal
	BuyAmount  decimal.Decimal // net — what the recipient actually gets
	Price      decimal.Decimal // sell units per buy unit, excluding fee
	Fee        Fee

	// TotalPrice is the all-in rate including fee: sell_amount / buy_amount.
	// This is the number a user experiences, as opposed to Price, which is
	// the rate the anchor advertises.
	TotalPrice decimal.Decimal

	// GrossBuyAmount is what the recipient would receive at Price with no
	// fee deducted, in buy-asset units. Derived as sell_amount / price.
	GrossBuyAmount decimal.Decimal

	// FeeInBuyAsset is the fee converted into buy-asset units, whatever
	// denomination the anchor used. This is the figure to show a user: the
	// fee expressed in the currency the recipient is counting.
	FeeInBuyAsset decimal.Decimal
}

// normalize computes the derived fields and validates internal consistency.
func (q *Quote) normalize() error {
	if q.Price.IsZero() || q.Price.IsNegative() {
		return fmt.Errorf("sep38: anchor returned non-positive price %s", q.Price)
	}
	if q.BuyAmount.IsNegative() || q.SellAmount.IsNegative() {
		return fmt.Errorf("sep38: anchor returned negative amount")
	}

	// The identity that makes fee denomination irrelevant. See package doc.
	q.GrossBuyAmount = q.SellAmount.Div(q.Price)
	q.FeeInBuyAsset = q.GrossBuyAmount.Sub(q.BuyAmount)

	if !q.BuyAmount.IsZero() {
		q.TotalPrice = q.SellAmount.Div(q.BuyAmount)
	}

	// A negative converted fee means the anchor's own numbers disagree —
	// buy_amount exceeds the gross implied by its price. Rather than
	// silently presenting a negative fee as a bonus, refuse the quote.
	if q.FeeInBuyAsset.IsNegative() {
		return fmt.Errorf(
			"sep38: inconsistent quote: buy_amount %s exceeds gross %s implied by price %s",
			q.BuyAmount, q.GrossBuyAmount, q.Price)
	}
	return nil
}

// wire types -----------------------------------------------------------------

type wireFeeDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
}

type wireFee struct {
	Total   string          `json:"total"`
	Asset   string          `json:"asset"`
	Details []wireFeeDetail `json:"details"`
}

type wirePrice struct {
	TotalPrice string  `json:"total_price"`
	Price      string  `json:"price"`
	SellAmount string  `json:"sell_amount"`
	BuyAmount  string  `json:"buy_amount"`
	Fee        wireFee `json:"fee"`
}

type wireQuote struct {
	ID         string  `json:"id"`
	ExpiresAt  string  `json:"expires_at"`
	TotalPrice string  `json:"total_price"`
	Price      string  `json:"price"`
	SellAsset  string  `json:"sell_asset"`
	SellAmount string  `json:"sell_amount"`
	BuyAsset   string  `json:"buy_asset"`
	BuyAmount  string  `json:"buy_amount"`
	Fee        wireFee `json:"fee"`
}

// dec parses a decimal string from an anchor, tolerating an empty field.
func dec(s string) (decimal.Decimal, error) {
	if strings.TrimSpace(s) == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}

func parseFee(w wireFee) (Fee, error) {
	total, err := dec(w.Total)
	if err != nil {
		return Fee{}, fmt.Errorf("sep38: parsing fee.total %q: %w", w.Total, err)
	}
	f := Fee{Total: total}
	if w.Asset != "" {
		a, err := asset.ParseSEP38(w.Asset)
		if err != nil {
			return Fee{}, fmt.Errorf("sep38: parsing fee.asset: %w", err)
		}
		f.Asset = a
	}
	for _, d := range w.Details {
		amt, err := dec(d.Amount)
		if err != nil {
			return Fee{}, fmt.Errorf("sep38: parsing fee detail %q: %w", d.Name, err)
		}
		f.Details = append(f.Details, FeeDetail{
			Name: d.Name, Description: d.Description, Amount: amt,
		})
	}
	return f, nil
}

// GetPrice fetches an indicative price for a fixed sell amount.
//
// Indicative means non-binding: it costs nothing and needs no authentication
// at most anchors, which makes it the right call for comparison shopping. Use
// PostQuote only once the user has chosen a route.
func (c *Client) GetPrice(ctx context.Context, sell, buy asset.Asset, sellAmount decimal.Decimal, quoteContext string) (*Quote, error) {
	q := url.Values{}
	q.Set("sell_asset", sell.SEP38())
	q.Set("buy_asset", buy.SEP38())
	q.Set("sell_amount", sellAmount.String())
	q.Set("context", quoteContext)

	var w wirePrice
	if err := c.get(ctx, "/price", q, &w); err != nil {
		return nil, err
	}

	quote := &Quote{SellAsset: sell, BuyAsset: buy}
	var err error
	if quote.SellAmount, err = dec(w.SellAmount); err != nil {
		return nil, fmt.Errorf("sep38: parsing sell_amount: %w", err)
	}
	if quote.BuyAmount, err = dec(w.BuyAmount); err != nil {
		return nil, fmt.Errorf("sep38: parsing buy_amount: %w", err)
	}
	if quote.Price, err = dec(w.Price); err != nil {
		return nil, fmt.Errorf("sep38: parsing price: %w", err)
	}
	if quote.Fee, err = parseFee(w.Fee); err != nil {
		return nil, err
	}

	// Some anchors omit sell_amount when it was supplied as input.
	if quote.SellAmount.IsZero() {
		quote.SellAmount = sellAmount
	}
	if err := quote.normalize(); err != nil {
		return nil, err
	}
	return quote, nil
}

// PostQuote requests a firm, time-limited quote. Requires SEP-10 auth.
func (c *Client) PostQuote(ctx context.Context, sell, buy asset.Asset, sellAmount decimal.Decimal, quoteContext string) (*Quote, error) {
	if c.AuthToken == "" {
		return nil, fmt.Errorf("sep38: POST /quote requires a SEP-10 auth token")
	}
	body := map[string]string{
		"sell_asset":  sell.SEP38(),
		"buy_asset":   buy.SEP38(),
		"sell_amount": sellAmount.String(),
		"context":     quoteContext,
	}
	var w wireQuote
	if err := c.post(ctx, "/quote", body, &w); err != nil {
		return nil, err
	}

	quote := &Quote{ID: w.ID, SellAsset: sell, BuyAsset: buy}
	var err error
	if quote.SellAmount, err = dec(w.SellAmount); err != nil {
		return nil, fmt.Errorf("sep38: parsing sell_amount: %w", err)
	}
	if quote.BuyAmount, err = dec(w.BuyAmount); err != nil {
		return nil, fmt.Errorf("sep38: parsing buy_amount: %w", err)
	}
	if quote.Price, err = dec(w.Price); err != nil {
		return nil, fmt.Errorf("sep38: parsing price: %w", err)
	}
	if quote.Fee, err = parseFee(w.Fee); err != nil {
		return nil, err
	}
	if w.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, w.ExpiresAt); err == nil {
			quote.ExpiresAt = t
		}
	}
	if quote.SellAmount.IsZero() {
		quote.SellAmount = sellAmount
	}
	if err := quote.normalize(); err != nil {
		return nil, err
	}
	return quote, nil
}

// transport ------------------------------------------------------------------

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("sep38: building request: %w", err)
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("sep38: encoding body: %w", err)
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("sep38: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("sep38: %s %s: %w", req.Method, req.URL.Host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Anchors return a JSON {"error": "..."} body on failure. Surfacing
		// it is far more useful than the bare status code, because it
		// usually names the unsupported asset or amount bound.
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error != "" {
			return fmt.Errorf("sep38: %s returned HTTP %d: %s", req.URL.Host, resp.StatusCode, e.Error)
		}
		return fmt.Errorf("sep38: %s returned HTTP %d", req.URL.Host, resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("sep38: decoding response: %w", err)
	}
	// A well-formed quote followed by anything else is not the document the
	// anchor claims to have sent. Decode stops at the end of the first JSON
	// value and silently ignores the rest, so a truncated write, a
	// concatenated body or a proxy's error page stub would read as a price.
	// Anything other than a clean end of input is a malformed response.
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("sep38: decoding response: unexpected trailing data after the JSON document")
	}
	return nil
}
