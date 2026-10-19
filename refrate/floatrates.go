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

// DefaultFloatRates is FloatRates' free, keyless daily JSON feed.
//
// It is the third provider, and it is used only to diagnose a DISAGREE
// between the two scored providers: its mid is never averaged into the
// benchmark and never moves the scored mid. The feed publishes one document
// per base currency, keyed by lowercase quote code, and refreshes twice a day
// (12 AM/PM GMT) — frequent enough for a diagnostic consulted only when the
// first two providers disagree.
const DefaultFloatRates = "https://www.floatrates.com/daily/"

// FloatRates is a Provider backed by FloatRates' daily JSON feeds.
//
// # Why this provider is the diagnostic, not a third primary
//
// It covers NGN, GHS and KES in the same USD-based document, needs no API key,
// and is operationally independent of the two scored feeds: FloatRates gathers
// rates from central banks and international financial institutions and runs
// its own aggregation. That makes "which primary is farther from this
// independent observation" a meaningful diagnostic rather than a restatement
// of one of the inputs.
//
// It is deliberately not a third input to the scored mid. A third provider
// inside the conservative-selection rule would change which mid is published,
// and ADR 001 forbids blending provider values. Consulting a third source to
// attribute a disagreement is not averaging, and leaving the scored mid
// untouched keeps every existing measurement reproducible.
type FloatRates struct {
	BaseURL string       // defaults to DefaultFloatRates
	Client  *http.Client // defaults to a client with a 10s timeout

	// Logger is the structured logger for upstream call logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

// Name identifies the provider.
func (f *FloatRates) Name() string { return "floatrates" }

func (f *FloatRates) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (f *FloatRates) baseURL() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/") + "/"
	}
	return DefaultFloatRates
}

// floatRatesEntry is one quote currency's record in a daily feed.
//
// Rate is kept as raw JSON so the published digits reach decimal.Decimal
// exactly: the rate is the denominator of every loss percentage, and routing
// it through float64 first would lose digits before this package could see
// them.
type floatRatesEntry struct {
	Rate json.RawMessage `json:"rate"`
	Date string          `json:"date"`
}

// Rate implements Provider.
func (f *FloatRates) Rate(ctx context.Context, base, quote string) (Rate, error) {
	started := time.Now()

	base, quote = strings.ToUpper(base), strings.ToUpper(quote)
	lowQuote := strings.ToLower(quote)
	url := f.baseURL() + strings.ToLower(base) + ".json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Rate{}, &ErrUnavailable{Source: f.Name(), Err: fmt.Errorf("building request: %w", err)}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client().Do(req)
	if err != nil {
		log().Error("floatrates request failed",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", transport.SanitizeTransportError(err))
		return Rate{}, &ErrUnavailable{Source: f.Name(), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		log().Warn("floatrates rate limited",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrRateLimited{Source: f.Name(), RetryAfter: transport.RetryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		log().Error("floatrates returned error",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"status", resp.StatusCode,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrUnavailable{Source: f.Name(), Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	var body map[string]floatRatesEntry
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		log().Error("floatrates decode failed",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, &ErrUnparseable{Source: f.Name(), Err: fmt.Errorf("decoding response: %w", err)}
	}

	entry, ok := body[lowQuote]
	if !ok {
		log().Error("floatrates missing quote rate",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: f.Name()}
	}
	// FloatRates publishes the rate as a JSON string. Unmarshal it as a
	// string — not a float — so the published digits survive exactly.
	var rawRate string
	if err := json.Unmarshal(entry.Rate, &rawRate); err != nil {
		log().Error("floatrates rate decode failed",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, &ErrUnparseable{Source: f.Name(), Err: fmt.Errorf("decoding %s/%s rate %q: %w", base, quote, entry.Rate, err)}
	}
	mid, err := decimal.NewFromString(rawRate)
	if err != nil {
		log().Error("floatrates rate parse failed",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, &ErrUnparseable{Source: f.Name(), Err: fmt.Errorf("parsing %s/%s rate %q: %w", base, quote, rawRate, err)}
	}
	// A zero rate is absence, not a quote: taken at face value it would divide
	// by zero in the divergence calculation, or report a route as infinitely
	// good.
	if mid.IsZero() {
		log().Error("floatrates zero rate",
			"service", f.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: f.Name()}
	}

	// The feed stamps every entry with the same GMT publication time. A
	// malformed stamp leaves AsOf zero, which is this package's absent
	// sentinel; it does not invalidate the rate itself.
	var asOf time.Time
	if parsed, err := time.Parse(http.TimeFormat, entry.Date); err == nil {
		asOf = parsed.UTC()
	}

	log().Debug("floatrates rate fetched",
		"service", f.Name(),
		"pair", base+"/"+quote,
		"duration", time.Since(started).Round(time.Millisecond).String())

	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    mid,
		AsOf:   asOf,
		Source: f.Name(),
	}, nil
}
