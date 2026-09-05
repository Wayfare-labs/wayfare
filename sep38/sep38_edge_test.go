package sep38

// Edge-case coverage for the SEP-38 client's failure modes: what happens when
// the anchor sends back something other than a clean, well-formed quote. These
// tests deliberately assert on the client's *actual* contract — malformed data
// is rejected, never silently repaired into a plausible-looking quote — rather
// than on any hoped-for behaviour. None of them touch sep38.go.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestGetPriceRejectsMalformedJSON verifies that a 200 response whose body is
// not valid JSON surfaces a decode error and yields no quote. A partially
// populated Quote here would be worse than an error: corridor analysis would
// proceed on fabricated pricing.
func TestGetPriceRejectsMalformedJSON(t *testing.T) {
	cases := map[string]string{
		"truncated object":   `{"price": "5.00", "sell_amount":`,
		"not json":           `not json at all`,
		"price wrong type":   `{"price": 5.00}`, // number where a string is expected
		"array not object":   `["price", "5.00"]`,
		"trailing garbage":   `{"price":"5.00"} and then some`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, body)
			defer srv.Close()

			c := &Client{BaseURL: srv.URL}
			q, err := c.GetPrice(context.Background(),
				asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
			if err == nil {
				t.Fatalf("expected a decode error, got quote %+v", q)
			}
			if q != nil {
				t.Errorf("expected nil quote on malformed JSON, got %+v", q)
			}
		})
	}
}

// TestGetPriceTimeoutReturnsError verifies that a client-side deadline produces
// a clear, wrapped error rather than hanging or panicking. The server blocks
// until the client abandons the request, so the only way out is the timeout.
func TestGetPriceTimeoutReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never respond; unblock only when the client gives up
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(ctx, asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
	if err == nil {
		t.Fatalf("expected a timeout error, got quote %+v", q)
	}
	if q != nil {
		t.Errorf("expected nil quote on timeout, got %+v", q)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error %v does not wrap context.DeadlineExceeded", err)
	}
}

// TestGetPriceRejectsEmptyResponse covers empty and contentless bodies.
//
// This client exposes no pair-listing endpoint, so the "empty pairs array"
// failure mode from the issue has no literal analogue here; the equivalent is
// an empty or field-less price response. The property under test is the same
// one that matters for an empty pairs list: the client must refuse rather than
// fabricate a zero-valued quote and present it as a real price.
func TestGetPriceRejectsEmptyResponse(t *testing.T) {
	cases := map[string]string{
		"empty body":       ``,
		"empty object":     `{}`,
		"whitespace only":  "   \n\t ",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, body)
			defer srv.Close()

			c := &Client{BaseURL: srv.URL}
			q, err := c.GetPrice(context.Background(),
				asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
			if err == nil {
				t.Fatalf("expected an error for an empty response, got quote %+v", q)
			}
		})
	}
}

// TestGetPriceRejectsMissingRequiredFields verifies that a response missing the
// field the derivation cannot proceed without — price — errors instead of
// dividing by an implicit zero.
//
// sell_amount is intentionally absent from these cases: the client documents
// that it backfills sell_amount from the request input when the anchor omits
// it. That is a stated tolerance, not a silent repair of malformed data, so a
// missing sell_amount is not a failure mode and is not asserted here.
func TestGetPriceRejectsMissingRequiredFields(t *testing.T) {
	cases := map[string]string{
		"no price field":     `{"sell_amount":"542","buy_amount":"100","fee":{"total":"42","asset":"iso4217:BRL"}}`,
		"empty price string":  `{"price":"","sell_amount":"542","buy_amount":"100"}`,
		"fee only":            `{"fee":{"total":"42","asset":"iso4217:BRL"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, body)
			defer srv.Close()

			c := &Client{BaseURL: srv.URL}
			_, err := c.GetPrice(context.Background(),
				asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
			if err == nil {
				t.Fatalf("expected an error for a response missing price")
			}
		})
	}
}

// TestGetPriceStatusCodesAreDistinguishable verifies that transient upstream
// failures (429 rate limiting, 503 unavailable) each surface an error that
// names the status code, so a caller can tell one from the other and decide
// whether to back off or fail over.
func TestGetPriceStatusCodesAreDistinguishable(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))

		c := &Client{BaseURL: srv.URL}
		_, err := c.GetPrice(context.Background(),
			asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
		srv.Close()

		if err == nil {
			t.Fatalf("HTTP %d: expected an error", code)
		}
		if want := fmt.Sprintf("%d", code); !strings.Contains(err.Error(), want) {
			t.Errorf("HTTP %d: error %q does not name the status code", code, err.Error())
		}
	}
}

// TestGetPriceSurfacesErrorBodyOnStatusCode verifies that when a 429/503 also
// carries the anchor's JSON {"error": ...} body, that message reaches the
// caller — the same contract the 400 path already relies on.
func TestGetPriceSurfacesErrorBodyOnStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"quote server under maintenance"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "quote server under maintenance") {
		t.Errorf("error %q does not surface the anchor's message", got)
	}
}

// TestGetPricePartialResponses draws the line between a legitimately partial
// response and a malformed one.
func TestGetPricePartialResponses(t *testing.T) {
	// The fee is an optional section. A response that omits it is partial but
	// valid: the fee is parsed as zero, which is a faithful reading of what was
	// sent — not a repair of something broken.
	t.Run("omitted fee parses as zero", func(t *testing.T) {
		srv := newTestServer(t, `{"price":"5.00","sell_amount":"542","buy_amount":"100"}`)
		defer srv.Close()

		c := &Client{BaseURL: srv.URL}
		q, err := c.GetPrice(context.Background(),
			asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
		if err != nil {
			t.Fatalf("GetPrice: %v", err)
		}
		if !q.Fee.Total.IsZero() {
			t.Errorf("Fee.Total = %s, want 0 for an omitted fee", q.Fee.Total)
		}
	})

	// A field that is present but malformed must error, not be silently coerced
	// to zero: a non-numeric fee total is bad data, not a missing section.
	t.Run("malformed fee total errors", func(t *testing.T) {
		srv := newTestServer(t, `{"price":"5.00","sell_amount":"542","buy_amount":"100","fee":{"total":"not-a-number"}}`)
		defer srv.Close()

		c := &Client{BaseURL: srv.URL}
		_, err := c.GetPrice(context.Background(),
			asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
		if err == nil {
			t.Fatal("expected an error for a non-numeric fee total")
		}
	})
}

// TestGetPriceHandlesLargeResponseBody verifies that a well-formed but very
// large body parses in full without panicking or truncating. Thousands of
// line-item fee details are the natural way for a response to balloon.
func TestGetPriceHandlesLargeResponseBody(t *testing.T) {
	const n = 2000

	var b strings.Builder
	b.WriteString(`{"price":"5.00","sell_amount":"542","buy_amount":"100",`)
	b.WriteString(`"fee":{"total":"42","asset":"iso4217:BRL","details":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"charge-%d","description":"padding padding padding","amount":"0.01"}`, i)
	}
	b.WriteString(`]}}`)

	srv := newTestServer(t, b.String())
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice on large body: %v", err)
	}
	if got := len(q.Fee.Details); got != n {
		t.Errorf("parsed %d fee details, want %d (body truncated?)", got, n)
	}
}

// TestGetPriceIgnoresUnknownFields verifies forward compatibility: a newer
// anchor that adds fields an older client does not know about must remain
// readable, not rejected wholesale.
func TestGetPriceIgnoresUnknownFields(t *testing.T) {
	body := `{
	  "price": "5.00",
	  "sell_amount": "542",
	  "buy_amount": "100",
	  "fee": {"total": "42", "asset": "iso4217:BRL", "surcharge_v2": {"x": 1}},
	  "total_price": "5.42",
	  "future_field": {"nested": [1, 2, 3]},
	  "experimental_flags": ["a", "b"],
	  "extra_number": 99
	}`
	srv := newTestServer(t, body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	q, err := c.GetPrice(context.Background(),
		asset.Fiat("BRL"), asset.USDC(), mustDec(t, "542"), ContextSEP6)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if got, want := q.Price.String(), "5"; got != want {
		t.Errorf("Price = %s, want %s", got, want)
	}
	if got, want := q.BuyAmount.String(), "100"; got != want {
		t.Errorf("BuyAmount = %s, want %s", got, want)
	}
}

// TestPostQuoteRejectsMalformedJSON pins the firm-quote path separately from
// GetPrice. It shares transport, but it is the authenticated call, and a
// malformed 200 body must still fail closed rather than return a half-built
// quote that carries an ID and nothing trustworthy after it.
func TestPostQuoteRejectsMalformedJSON(t *testing.T) {
	srv := newTestServer(t, `{"id": "q-1", "price":`) // truncated mid-object
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, AuthToken: "test-jwt"}
	q, err := c.PostQuote(context.Background(),
		asset.USDC(), asset.NGN(), mustDec(t, "100"), ContextSEP31)
	if err == nil {
		t.Fatalf("expected a decode error, got quote %+v", q)
	}
	if q != nil {
		t.Errorf("expected nil quote on malformed JSON, got %+v", q)
	}
}
