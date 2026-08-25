// Package dex prices the on-chain leg of a corridor using the Stellar DEX.
//
// # Why pathfinding rather than an order book walk
//
// The obvious implementation is to fetch the order book and walk the bids,
// accumulating fills until the send amount is consumed. That implementation
// is wrong on Stellar, and measurably so.
//
// Querying the live USDC/NGNC market on 2026-08-04, the order book showed a
// best bid of 333.33 NGNC per USDC with 2184.54 units of depth. Horizon's own
// strict-send pathfinder, asked to price 100 USDC over the same market at the
// same moment, returned 21,785.78 NGNC — a figure that cannot be reconstructed
// from those order book levels under either reading of the amount field.
//
// The discrepancy is automated market makers. Stellar settles path payments
// against order book offers and AMM liquidity pools together, and the order
// book endpoint reports only the former. A router walking bids would therefore
// both misprice the route and fail to see liquidity that genuinely exists.
//
// So pricing here delegates to /paths/strict-send, which is the same engine
// that will execute the payment. The order book is still fetched, but only as
// a market health diagnostic, where its limitations do not affect the verdict.
package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/transport"
)

// DefaultHorizonURL is SDF's public mainnet Horizon instance.
const DefaultHorizonURL = "https://horizon.stellar.org"

// Client queries Horizon for DEX pricing.
type Client struct {
	HorizonURL string
	HTTPClient *http.Client

	// Logger is the structured logger for upstream call logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

func (c *Client) horizonURL() string {
	if c.HorizonURL != "" {
		return strings.TrimRight(c.HorizonURL, "/")
	}
	return DefaultHorizonURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Path is one route Horizon found through the DEX.
type Path struct {
	SourceAsset  asset.Asset
	SourceAmount decimal.Decimal
	DestAsset    asset.Asset
	DestAmount   decimal.Decimal

	// Hops are the intermediate assets between source and destination. An
	// empty slice means a direct market. On this corridor the XLM-bridged
	// path routinely beats the direct one by a wide margin, which is why
	// enumerating hops matters rather than assuming direct is best.
	Hops []asset.Asset
}

// Rate is destination units per source unit for this path.
func (p Path) Rate() decimal.Decimal {
	if p.SourceAmount.IsZero() {
		return decimal.Zero
	}
	return p.DestAmount.Div(p.SourceAmount)
}

// Describe renders the hop chain for display, e.g. "USDC -> XLM -> NGNC".
func (p Path) Describe() string {
	parts := make([]string, 0, len(p.Hops)+2)
	parts = append(parts, p.SourceAsset.Code)
	for _, h := range p.Hops {
		parts = append(parts, h.Code)
	}
	parts = append(parts, p.DestAsset.Code)
	return strings.Join(parts, " -> ")
}

// horizonAsset is Horizon's asset encoding for the destination_assets query
// parameter: "native", or "CODE:ISSUER".
func horizonAsset(a asset.Asset) string {
	if a.IsNative() {
		return "native"
	}
	return a.Code + ":" + a.Issuer
}

type wirePathRecord struct {
	SourceAssetType     string `json:"source_asset_type"`
	SourceAssetCode     string `json:"source_asset_code"`
	SourceAssetIssuer   string `json:"source_asset_issuer"`
	SourceAmount        string `json:"source_amount"`
	DestinationAssetTyp string `json:"destination_asset_type"`
	DestAssetCode       string `json:"destination_asset_code"`
	DestAssetIssuer     string `json:"destination_asset_issuer"`
	DestinationAmount   string `json:"destination_amount"`
	Path                []struct {
		AssetType   string `json:"asset_type"`
		AssetCode   string `json:"asset_code"`
		AssetIssuer string `json:"asset_issuer"`
	} `json:"path"`
}

type wirePathsResponse struct {
	Embedded struct {
		Records []wirePathRecord `json:"records"`
	} `json:"_embedded"`
}

// StrictSendPaths asks Horizon what a fixed send amount can buy.
//
// Strict-send matches the remittance question exactly: the sender has a fixed
// amount of USDC and wants to know what arrives at the other end. The
// alternative, strict-receive, answers "what must I send to deliver exactly
// X", which is the wrong shape for a comparison tool.
//
// Results are returned in Horizon's order, best first.
func (c *Client) StrictSendPaths(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset) ([]Path, error) {
	q := url.Values{}
	for k, v := range source.HorizonParams("source") {
		q.Set(k, v)
	}
	q.Set("source_amount", amount.String())
	q.Set("destination_assets", horizonAsset(dest))

	var body wirePathsResponse
	if err := c.get(ctx, "/paths/strict-send", q, &body); err != nil {
		return nil, err
	}

	paths := make([]Path, 0, len(body.Embedded.Records))
	for _, r := range body.Embedded.Records {
		srcAmt, err := decimal.NewFromString(r.SourceAmount)
		if err != nil {
			return nil, fmt.Errorf("dex: parsing source_amount %q: %w", r.SourceAmount, err)
		}
		dstAmt, err := decimal.NewFromString(r.DestinationAmount)
		if err != nil {
			return nil, fmt.Errorf("dex: parsing destination_amount %q: %w", r.DestinationAmount, err)
		}
		p := Path{
			SourceAsset:  source,
			SourceAmount: srcAmt,
			DestAsset:    dest,
			DestAmount:   dstAmt,
		}
		for _, h := range r.Path {
			if h.AssetType == "native" {
				p.Hops = append(p.Hops, asset.Native())
			} else {
				p.Hops = append(p.Hops, asset.Stellar(h.AssetCode, h.AssetIssuer))
			}
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// BestPath returns the path delivering the most destination asset.
//
// Horizon generally returns its best path first, but that is not a documented
// guarantee, so the maximum is selected explicitly rather than trusting order.
func (c *Client) BestPath(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset) (*Path, error) {
	paths, err := c.StrictSendPaths(ctx, source, amount, dest)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("dex: no path found from %s to %s for %s",
			source.Code, dest.Code, amount)
	}
	best := paths[0]
	for _, p := range paths[1:] {
		if p.DestAmount.GreaterThan(best.DestAmount) {
			best = p
		}
	}
	return &best, nil
}

// Slippage measures how much the effective rate degrades with size.
//
// A route can look excellent at 10 USDC and be unusable at 1000. Comparing
// the rate for the requested amount against the rate for a small probe
// exposes that, and on thin corridors the difference is the single most
// important number in the quote.
type Slippage struct {
	ProbeAmount decimal.Decimal
	ProbeRate   decimal.Decimal
	FullAmount  decimal.Decimal
	FullRate    decimal.Decimal

	// PctWorse is how much worse the full-size rate is than the probe rate,
	// as a percentage. Positive means the larger trade gets less per unit.
	PctWorse decimal.Decimal
}

// MeasureSlippage prices the requested amount and a small probe, and reports
// the degradation between them.
func (c *Client) MeasureSlippage(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset, probe decimal.Decimal) (*Slippage, error) {
	full, err := c.BestPath(ctx, source, amount, dest)
	if err != nil {
		return nil, err
	}
	small, err := c.BestPath(ctx, source, probe, dest)
	if err != nil {
		return nil, err
	}

	s := &Slippage{
		ProbeAmount: probe,
		ProbeRate:   small.Rate(),
		FullAmount:  amount,
		FullRate:    full.Rate(),
	}
	if !s.ProbeRate.IsZero() {
		s.PctWorse = s.ProbeRate.Sub(s.FullRate).
			Div(s.ProbeRate).
			Mul(decimal.NewFromInt(100))
	}
	return s, nil
}

func (c *Client) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	started := time.Now()

	u := c.horizonURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("dex: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		c.log().Error("horizon request failed",
			"service", "horizon",
			"endpoint", path,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", transport.SanitizeTransportError(err))
		return fmt.Errorf("dex: querying horizon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.log().Error("horizon returned error",
			"service", "horizon",
			"endpoint", path,
			"status", resp.StatusCode,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return fmt.Errorf("dex: horizon returned HTTP %d for %s", resp.StatusCode, path)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		c.log().Error("horizon response decode failed",
			"service", "horizon",
			"endpoint", path,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return fmt.Errorf("dex: decoding horizon response: %w", err)
	}

	c.log().Debug("horizon request succeeded",
		"service", "horizon",
		"endpoint", path,
		"status", resp.StatusCode,
		"duration", time.Since(started).Round(time.Millisecond).String())
	return nil
}
