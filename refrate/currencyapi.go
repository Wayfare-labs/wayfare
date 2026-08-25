package refrate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/transport"
)

// DefaultCurrencyAPI is the keyless endpoint of the CC0-licensed
// currency-api feed.
//
// It is the second reference source because of three properties, in order of
// importance. It publishes NGN — the ECB-backed feeds cover only the ~30
// currencies the ECB tracks, which excludes every currency this project
// exists to measure. It needs no API key, so cross-checking costs the
// deployment no secret to rotate, leak, or silently let expire. And its data
// is CC0, so a recorded response can be committed as a test fixture, which
// exchangerate-api's terms do not visibly permit.
const DefaultCurrencyAPI = "https://latest.currency-api.pages.dev/v1/currencies/"

// CurrencyAPI is a Provider backed by the currency-api feed.
//
// # Why this one is a useful cross-check rather than just a spare
//
// A second provider that resold the first one's data would give redundancy
// against an outage and nothing against an error. This feed aggregates from a
// different set of upstreams, so the two disagree for different reasons than
// they fail. That is what makes the divergence between them a measurement of
// how well-defined the benchmark is, rather than noise.
//
// The same caveat as every other feed still applies, and applies hardest to
// exactly the corridors measured here: this is an official/interbank-style
// rate, and for currencies under exchange controls the rate people actually
// transact at can diverge sharply from it.
type CurrencyAPI struct {
	BaseURL string       // defaults to DefaultCurrencyAPI
	Client  *http.Client // defaults to a client with a 10s timeout

	// Logger is the structured logger for upstream call logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

// Name identifies the provider.
func (c *CurrencyAPI) Name() string { return "currency-api" }

func (c *CurrencyAPI) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *CurrencyAPI) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultCurrencyAPI
}

// Rate implements Provider.
//
// The payload is shaped {"date": "...", "<base>": {"<quote>": <number>, ...}}
// with lowercase currency codes. It is decoded into json.RawMessage rather
// than float64 so the upstream digits reach decimal.Decimal exactly as
// published: routing a rate through binary floating point is the same
// rounding bug this project refuses everywhere else, and it is worst here,
// where the number is the denominator of every loss percentage.
func (c *CurrencyAPI) Rate(ctx context.Context, base, quote string) (Rate, error) {
	started := time.Now()

	base, quote = strings.ToUpper(base), strings.ToUpper(quote)
	lowBase, lowQuote := strings.ToLower(base), strings.ToLower(quote)

	url := c.baseURL() + lowBase + ".json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Rate{}, fmt.Errorf("refrate: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		log().Error("currency-api request failed",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", transport.SanitizeTransportError(err))
		return Rate{}, fmt.Errorf("refrate: fetching %s rates: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		log().Warn("currency-api rate limited",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrRateLimited{Source: c.Name(), RetryAfter: retryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		log().Error("currency-api returned error",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"status", resp.StatusCode,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, fmt.Errorf("refrate: %s returned HTTP %d", c.Name(), resp.StatusCode)
	}

	var envelope map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		log().Error("currency-api decode failed",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, fmt.Errorf("refrate: decoding response: %w", err)
	}

	rates, ok := envelope[lowBase]
	if !ok {
		log().Error("currency-api missing base rates",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: c.Name()}
	}
	var byCode map[string]json.RawMessage
	if err := json.Unmarshal(rates, &byCode); err != nil {
		log().Error("currency-api rates decode failed",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, fmt.Errorf("refrate: decoding %s rates: %w", base, err)
	}

	raw, ok := byCode[lowQuote]
	if !ok {
		log().Error("currency-api missing quote rate",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: c.Name()}
	}
	mid, err := decimal.NewFromString(string(raw))
	if err != nil {
		log().Error("currency-api rate parse failed",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, fmt.Errorf("refrate: parsing %s/%s rate %q: %w", base, quote, raw, err)
	}
	// A rate of exactly zero is absence, not a quote. Taken at face value it
	// would divide by zero in the spread calculation, or report a route as
	// infinitely good.
	if mid.IsZero() {
		log().Error("currency-api zero rate",
			"service", c.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: c.Name()}
	}

	asOf := time.Now().UTC()
	if d, ok := envelope["date"]; ok {
		var day string
		if json.Unmarshal(d, &day) == nil {
			if parsed, err := time.Parse("2006-01-02", day); err == nil {
				asOf = parsed
			}
		}
	}

	log().Debug("currency-api rate fetched",
		"service", c.Name(),
		"pair", base+"/"+quote,
		"duration", time.Since(started).Round(time.Millisecond).String())

	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    mid,
		AsOf:   asOf,
		Source: c.Name(),
	}, nil
}
