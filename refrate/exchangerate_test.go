package refrate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestExchangeRateAPISuccess covers the normal path so a baseline exists.
func TestExchangeRateAPISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1755763200,
			"rates":{"NGN":1348.0585}
		}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	r, err := p.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mid.String() != "1348.0585" {
		t.Errorf("Mid = %s, want 1348.0585", r.Mid)
	}
}

// TestExchangeRateAPIUnavailableNetwork covers a provider that did not answer
// at all — connection refused, timeout, or DNS failure.
func TestExchangeRateAPIUnavailableNetwork(t *testing.T) {
	p := &ExchangeRateAPI{BaseURL: "http://127.0.0.1:1/"}

	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a non-existent server")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnavailable", err, err)
	}
}

// TestExchangeRateAPIUnavailableHTTP covers a provider that answered with a
// non-2xx HTTP status.
func TestExchangeRateAPIUnavailableHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"500", http.StatusInternalServerError},
		{"502", http.StatusBadGateway},
		{"503", http.StatusServiceUnavailable},
		{"403", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
			_, err := p.Rate(context.Background(), "USD", "NGN")
			if err == nil {
				t.Fatal("expected an error")
			}
			var unavailable *ErrUnavailable
			if !errors.As(err, &unavailable) {
				t.Errorf("HTTP %d: error %T does not wrap to *ErrUnavailable", tc.status, err)
			}
		})
	}
}

// TestExchangeRateAPINotRateLimited distinguishes 429 from other HTTP errors.
func TestExchangeRateAPINotRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var limited *ErrRateLimited
	if !errors.As(err, &limited) {
		t.Errorf("429 should produce ErrRateLimited, got %T", err)
	}
	var unavailable *ErrUnavailable
	if errors.As(err, &unavailable) {
		t.Error("429 must not produce ErrUnavailable — the remedy is different")
	}
}

// TestExchangeRateAPIUnparseableBody covers a provider that answered with a
// body that cannot be decoded as a rate response.
func TestExchangeRateAPIUnparseableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnparseable", err, err)
	}
}

// TestExchangeRateAPIErrorResult covers a provider that answered with an
// explicit error in the JSON payload.
func TestExchangeRateAPIErrorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":"error","error-type":"unknown-code"}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("API error result should produce *ErrUnavailable, got %T", err)
	}
}

// TestExchangeRateAPIUnparseableRate covers a rate field that is present but
// not a valid decimal.
func TestExchangeRateAPIUnparseableRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1755763200,
			"rates":{"NGN":"not-a-number"}
		}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("unparseable rate should produce *ErrUnparseable, got %T (%v)", err, err)
	}
}

// TestExchangeRateAPIRateLimitedIsDistinctFromUnavailable checks that a 429
// never wraps to ErrUnavailable even when it is the only HTTP error the
// taxonomy must handle.
func TestExchangeRateAPIRateLimitedIsDistinctFromUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var limited *ErrRateLimited
	var unavailable *ErrUnavailable
	if errors.As(err, &limited) && errors.As(err, &unavailable) {
		t.Error("a 429 must not be both ErrRateLimited and ErrUnavailable")
	}
}

// TestExchangeRateAPIContextCancelled covers context cancellation before the
// request completes.
func TestExchangeRateAPIContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(ctx, "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("cancelled context should produce *ErrUnavailable, got %T", err)
	}
}

// TestExchangeRateAPISuccessFields verifies the returned Rate carries all
// expected fields on the normal path.
func TestExchangeRateAPISuccessFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1755763200,
			"rates":{"NGN":1348.0585}
		}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	r, err := p.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Base != "USD" {
		t.Errorf("Base = %s, want USD", r.Base)
	}
	if r.Quote != "NGN" {
		t.Errorf("Quote = %s, want NGN", r.Quote)
	}
	if r.Source != "exchangerate-api" {
		t.Errorf("Source = %s, want exchangerate-api", r.Source)
	}
	want := time.Unix(1755763200, 0)
	if !r.AsOf.Equal(want) {
		t.Errorf("AsOf = %s, want %s", r.AsOf, want)
	}
}

// TestExchangeRateAPIRateLimitHTTP429 is a focused check that the specific
// HTTP status 429 produces ErrRateLimited and not any other error type.
func TestExchangeRateAPIRateLimitHTTP429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}

	var limited *ErrRateLimited
	if !errors.As(err, &limited) {
		t.Errorf("429 should produce *ErrRateLimited, got %T (%v)", err, err)
	}
	if limited != nil && limited.Source != "exchangerate-api" {
		t.Errorf("Source = %s, want exchangerate-api", limited.Source)
	}
}

// TestExchangeRateAPINoQuoteIsErrNoRate covers a valid response that does not
// contain the requested quote currency.
func TestExchangeRateAPINoQuoteIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1755763200,
			"rates":{"EUR":0.92}
		}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a missing quote")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("missing quote should produce *ErrNoRate, got %T", err)
	}
}

// TestExchangeRateAPIZeroRateIsErrNoRate covers a rate of exactly zero, which
// is treated as absence.
func TestExchangeRateAPIZeroRateIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result":"success",
			"base_code":"USD",
			"time_last_update_unix":1755763200,
			"rates":{"NGN":0}
		}`))
	}))
	defer srv.Close()

	p := &ExchangeRateAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a zero rate")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("zero rate should produce *ErrNoRate, got %T", err)
	}
}
