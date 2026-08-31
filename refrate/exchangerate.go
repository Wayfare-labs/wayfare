package refrate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/transport"
)

// DefaultExchangeRateAPI is the open endpoint of exchangerate-api's free
// tier. It is used rather than the ECB-backed alternatives because those
// publish only the ~30 currencies the ECB tracks, and NGN is not among them —
// which would make them useless for the one corridor this project exists to
// serve.
const DefaultExchangeRateAPI = "https://open.er-api.com/v6/latest/"

// ExchangeRateAPI is a Provider backed by an exchangerate-api-style endpoint
// returning base-relative rates.
//
// A caveat worth understanding before trusting the output: feeds like this
// publish an official or interbank rate. For currencies with exchange
// controls — NGN historically among them — the rate people actually transact
// at can diverge sharply from the official one. So this is a defensible
// benchmark, not ground truth, and the Source field is carried through to the
// UI so the user knows what they are being compared against.
type ExchangeRateAPI struct {
	BaseURL string       // defaults to DefaultExchangeRateAPI
	Client  *http.Client // defaults to a client with a 10s timeout

	// Logger is the structured logger for upstream call logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

// Name identifies the provider.
func (e *ExchangeRateAPI) Name() string { return "exchangerate-api" }

func (e *ExchangeRateAPI) client() *http.Client {
	if e.Client != nil {
		return e.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (e *ExchangeRateAPI) baseURL() string {
	if e.BaseURL != "" {
		return e.BaseURL
	}
	return DefaultExchangeRateAPI
}

// erAPIResponse is the subset of the upstream payload we rely on.
//
// Rates are held as raw JSON rather than float64 on purpose. Decoding a rate
// into binary floating point and converting afterwards has already lost
// digits by the time decimal sees it, and this particular number is the
// denominator of every loss percentage the project publishes — a rounding
// error here moves a corridor across a verdict boundary with nothing having
// changed on-chain.
type erAPIResponse struct {
	Result             string                     `json:"result"`
	BaseCode           string                     `json:"base_code"`
	TimeLastUpdateUnix int64                      `json:"time_last_update_unix"`
	Rates              map[string]json.RawMessage `json:"rates"`
	ErrorType          string                     `json:"error-type"`
}

// Rate implements Provider.
func (e *ExchangeRateAPI) Rate(ctx context.Context, base, quote string) (Rate, error) {
	started := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL()+base, nil)
	if err != nil {
		return Rate{}, &ErrUnavailable{Source: e.Name(), Err: fmt.Errorf("building request: %w", err)}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := e.client().Do(req)
	if err != nil {
		log().Error("exchangerate-api request failed",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", transport.SanitizeTransportError(err))
		return Rate{}, &ErrUnavailable{Source: e.Name(), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		log().Warn("exchangerate-api rate limited",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrRateLimited{Source: e.Name(), RetryAfter: transport.RetryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		log().Error("exchangerate-api returned error",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"status", resp.StatusCode,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrUnavailable{Source: e.Name(), Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	var body erAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		log().Error("exchangerate-api decode failed",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, &ErrUnparseable{Source: e.Name(), Err: fmt.Errorf("decoding response: %w", err)}
	}
	if body.Result != "" && body.Result != "success" {
		log().Error("exchangerate-api error result",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error_type", body.ErrorType)
		return Rate{}, &ErrUnavailable{Source: e.Name(), Err: fmt.Errorf("API error: %s", body.ErrorType)}
	}

	raw, ok := body.Rates[quote]
	if !ok {
		log().Error("exchangerate-api missing quote rate",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: e.Name()}
	}
	mid, err := decimal.NewFromString(string(raw))
	if err != nil {
		log().Error("exchangerate-api rate parse failed",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String(),
			"error", err)
		return Rate{}, &ErrUnparseable{Source: e.Name(), Err: fmt.Errorf("parsing %s/%s rate %q: %w", base, quote, raw, err)}
	}
	// A rate of exactly zero is treated as absent rather than as a real
	// quote. Taking it at face value would make the spread calculation
	// divide by zero, or worse, report a route as infinitely good.
	if mid.IsZero() {
		log().Error("exchangerate-api zero rate",
			"service", e.Name(),
			"pair", base+"/"+quote,
			"duration", time.Since(started).Round(time.Millisecond).String())
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: e.Name()}
	}

	asOf := time.Now()
	if body.TimeLastUpdateUnix > 0 {
		asOf = time.Unix(body.TimeLastUpdateUnix, 0)
	}

	log().Debug("exchangerate-api rate fetched",
		"service", e.Name(),
		"pair", base+"/"+quote,
		"duration", time.Since(started).Round(time.Millisecond).String())

	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    mid,
		AsOf:   asOf,
		Source: e.Name(),
	}, nil
}
