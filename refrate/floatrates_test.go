package refrate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// floatRatesFixture is shaped exactly like the live USD feed checked on
// 2026-10-19: one object per lowercase quote code, each carrying a string
// rate, an RFC 1123 GMT publication stamp, and an inverseRate this package
// does not use.
const floatRatesFixture = `{
  "ghs": {
    "code": "GHS",
    "alphaCode": "GHS",
    "numericCode": "936",
    "name": "Ghanaian Cedi",
    "rate": "11.64151782",
    "date": "Mon, 19 Oct 2026 07:55:04 GMT",
    "inverseRate": "0.08589939"
  },
  "kes": {
    "code": "KES",
    "alphaCode": "KES",
    "numericCode": "404",
    "name": "Kenyan Shilling",
    "rate": "129.86270328",
    "date": "Mon, 19 Oct 2026 07:55:04 GMT",
    "inverseRate": "0.00770044"
  },
  "ngn": {
    "code": "NGN",
    "alphaCode": "NGN",
    "numericCode": "566",
    "name": "Nigerian Naira",
    "rate": "1331.60997067",
    "date": "Mon, 19 Oct 2026 07:55:04 GMT",
    "inverseRate": "0.00075097"
  }
}`

// TestFloatRatesCoversNGNGHSKES is the coverage claim the diagnostic depends
// on: one USD feed answers all three corridors, with the published digits
// preserved exactly and the feed's own GMT stamp carried through.
func TestFloatRatesCoversNGNGHSKES(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daily/usd.json" {
			t.Errorf("request path = %q, want /daily/usd.json", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(floatRatesFixture))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/daily/"}
	want := map[string]string{
		"NGN": "1331.60997067",
		"GHS": "11.64151782",
		"KES": "129.86270328",
	}
	for quote, mid := range want {
		t.Run(quote, func(t *testing.T) {
			r, err := p.Rate(context.Background(), "USD", quote)
			if err != nil {
				t.Fatalf("Rate(USD/%s): %v", quote, err)
			}
			if got := r.Mid.String(); got != mid {
				t.Errorf("Mid = %s, want %s exactly as published", got, mid)
			}
			if r.Source != "floatrates" {
				t.Errorf("Source = %q, want floatrates", r.Source)
			}
			wantAsOf := time.Date(2026, 10, 19, 7, 55, 4, 0, time.UTC)
			if !r.AsOf.Equal(wantAsOf) {
				t.Errorf("AsOf = %s, want %s", r.AsOf, wantAsOf)
			}
		})
	}
}

func TestFloatRatesHTTPErrorIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for HTTP 503")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnavailable", err, err)
	}
}

func TestFloatRates429IsRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for HTTP 429")
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

func TestFloatRatesUnparseableEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnparseable", err, err)
	}
}

func TestFloatRatesMissingQuoteIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ngn":{"rate":"1331.6","date":"Mon, 19 Oct 2026 07:55:04 GMT"}}`))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "GHS")
	if err == nil {
		t.Fatal("expected an error for a missing quote")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("missing quote should produce *ErrNoRate, got %T", err)
	}
}

func TestFloatRatesUnparseableRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ngn":{"rate":"not-a-number","date":"Mon, 19 Oct 2026 07:55:04 GMT"}}`))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error")
	}
	var unparseable *ErrUnparseable
	if !errors.As(err, &unparseable) {
		t.Errorf("unparseable rate should produce *ErrUnparseable, got %T (%v)", err, err)
	}
}

func TestFloatRatesZeroRateIsErrNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ngn":{"rate":"0","date":"Mon, 19 Oct 2026 07:55:04 GMT"}}`))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a zero rate")
	}
	var noRate *ErrNoRate
	if !errors.As(err, &noRate) {
		t.Errorf("zero rate should produce *ErrNoRate, got %T", err)
	}
}

func TestFloatRatesMissingTimestampLeavesAsOfUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ngn":{"rate":"1331.6","date":"not a date"}}`))
	}))
	defer srv.Close()

	p := &FloatRates{BaseURL: srv.URL + "/"}
	r, err := p.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if !r.AsOf.IsZero() {
		t.Errorf("AsOf = %s, want zero: the stamp was unreadable, and unknown is not a guessed time", r.AsOf)
	}
	if r.Mid.String() != "1331.6" {
		t.Errorf("Mid = %s, want the rate still usable", r.Mid)
	}
}

func TestFloatRatesUnavailableNetwork(t *testing.T) {
	p := &FloatRates{BaseURL: "http://127.0.0.1:1/"}
	_, err := p.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error for a non-existent server")
	}
	var unavailable *ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Errorf("error %T (%[1]v) does not wrap to *ErrUnavailable", err, err)
	}
}

func TestFloatRatesName(t *testing.T) {
	if got := (&FloatRates{}).Name(); got != "floatrates" {
		t.Errorf("Name() = %q, want floatrates", got)
	}
}
