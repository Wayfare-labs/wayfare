package refrate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCurrencyAPISuccess covers the normal path so a baseline exists.
func TestCurrencyAPISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"date":"2026-08-21",
			"usd":{"ngn":1350.2568}
		}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	r, err := p.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mid.String() != "1350.2568" {
		t.Errorf("Mid = %s, want 1350.2568", r.Mid)
	}
}

// TestCurrencyAPIUnavailableNetwork covers a provider that did not answer at
// all — connection refused, timeout, or DNS failure.
func TestCurrencyAPIUnavailableNetwork(t *testing.T) {
	p := &CurrencyAPI{BaseURL: "http://127.0.0.1:1/"}

	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a non-existent server")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnavailable", err, err)
	}
}

// TestCurrencyAPIUnavailableHTTP covers a provider that answered with a
// non-2xx HTTP status.
func TestCurrencyAPIUnavailableHTTP(t *testing.T) {
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

			p := &CurrencyAPI{BaseURL: srv.URL + "/"}
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

// TestCurrencyAPINotRateLimited distinguishes 429 from other HTTP errors.
func TestCurrencyAPINotRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
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

// TestCurrencyAPIUnparseableBody covers a provider that answered with a body
// that cannot be decoded as the expected envelope.
func TestCurrencyAPIUnparseableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnparseable", err, err)
	}
}

// TestCurrencyAPIMissingBaseIsErrNoRate covers a valid envelope that does not
// contain the base currency at all.
func TestCurrencyAPIMissingBaseIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"date":"2026-08-21","eur":{"ngn":1500}}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a missing base")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("missing base should produce *ErrNoRate, got %T", err)
	}
}

// TestCurrencyAPIUnparseableRates covers a base entry that is not a valid
// JSON object of currency codes.
func TestCurrencyAPIUnparseableRates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"date":"2026-08-21","usd":"not-an-object"}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("unparseable rates should produce *ErrUnparseable, got %T (%v)", err, err)
	}
}

// TestCurrencyAPIMissingQuoteIsErrNoRate covers a valid rates object that
// does not contain the requested quote currency.
func TestCurrencyAPIMissingQuoteIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"date":"2026-08-21","usd":{"eur":0.92}}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a missing quote")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("missing quote should produce *ErrNoRate, got %T", err)
	}
}

// TestCurrencyAPIUnparseableRate covers a rate field that is present but not
// a valid decimal.
func TestCurrencyAPIUnparseableRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"date":"2026-08-21","usd":{"ngn":"not-a-number"}}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("unparseable rate should produce *ErrUnparseable, got %T (%v)", err, err)
	}
}

// TestCurrencyAPIZeroRateIsErrNoRate covers a rate of exactly zero, which is
// treated as absence.
func TestCurrencyAPIZeroRateIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"date":"2026-08-21","usd":{"ngn":0}}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a zero rate")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("zero rate should produce *ErrNoRate, got %T", err)
	}
}

// TestCurrencyAPIContextCancelled covers context cancellation before the
// request completes.
func TestCurrencyAPIContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
	_, err := p.Rate(ctx, "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("cancelled context should produce *ErrUnavailable, got %T", err)
	}
}

// TestCurrencyAPISuccessFields verifies the returned Rate carries all
// expected fields on the normal path.
func TestCurrencyAPISuccessFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"date":"2026-08-21",
			"usd":{"ngn":1350.2568}
		}`))
	}))
	defer srv.Close()

	p := &CurrencyAPI{BaseURL: srv.URL + "/"}
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
	if r.Source != "currency-api" {
		t.Errorf("Source = %s, want currency-api", r.Source)
	}
	if r.AsOf.IsZero() {
		t.Error("AsOf must be set from the date field")
	}
}
