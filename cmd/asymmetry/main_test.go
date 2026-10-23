package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/route"
)

// bookHandler serves canned strict-send and strict-receive answers.
//
// strict-send replies with delivered = amount × rate; strict-receive replies
// with required = destination ÷ rate × (1 + premium)/100, so a test can pin
// the asymmetry arithmetic against a known premium.
func bookHandler(t *testing.T, rate, premium string, noReceiveFor string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/hal+json")
		switch r.URL.Path {
		case "/paths/strict-send":
			amt, err := decimal.NewFromString(q.Get("source_amount"))
			if err != nil {
				t.Errorf("strict-send query missing source_amount: %v", err)
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			dest := amt.Mul(decimal.RequireFromString(rate)).Truncate(7)
			_, _ = fmt.Fprintf(w,
				`{"_embedded":{"records":[{"source_amount":%q,"destination_amount":%q,"path":[]}]}}`,
				amt.String(), dest.String())
		case "/paths/strict-receive":
			dest, err := decimal.NewFromString(q.Get("destination_amount"))
			if err != nil {
				t.Errorf("strict-receive query missing destination_amount: %v", err)
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			if dest.String() == noReceiveFor {
				_, _ = w.Write([]byte(`{"_embedded":{"records":[]}}`))
				return
			}
			required := dest.
				Div(decimal.RequireFromString(rate)).
				Mul(decimal.RequireFromString("1").Add(decimal.RequireFromString(premium))).
				Truncate(7)
			_, _ = fmt.Fprintf(w,
				`{"_embedded":{"records":[{"source_amount":%q,"destination_amount":%q,"path":[]}]}}`,
				required.String(), dest.String())
		default:
			http.Error(w, "unknown path", http.StatusNotFound)
		}
	})
}

// TestMeasureComputesAsymmetryAtMatchedNotional pins the arithmetic the
// #595 write-up reports: with a +0.5% strict-receive premium baked into the
// fake upstream, every sized row must show +0.5% asymmetry.
func TestMeasureComputesAsymmetryAtMatchedNotional(t *testing.T) {
	srv := httptest.NewServer(bookHandler(t, "1500", "0.005", ""))
	defer srv.Close()

	d := &dex.Client{HorizonURL: srv.URL}
	sizes := []decimal.Decimal{
		decimal.NewFromInt(1),
		decimal.NewFromInt(100),
		decimal.NewFromInt(5000),
	}
	rows := measure(context.Background(), d, sizes, asset.NGNC())

	if len(rows) != len(sizes) {
		t.Fatalf("got %d rows, want %d", len(rows), len(sizes))
	}
	for i, r := range rows {
		if r.Err != nil {
			t.Fatalf("row %d errored: %v", i, r.Err)
		}
		if r.NoMarket || r.State != "priced" {
			t.Errorf("row %d state = %q (NoMarket=%v), want priced", i, r.State, r.NoMarket)
		}
		// delivered = size × 1500
		wantDelivered := sizes[i].Mul(decimal.NewFromInt(1500)).Truncate(7)
		if !r.Delivered.Equal(wantDelivered) {
			t.Errorf("row %d delivered = %s, want %s", i, r.Delivered, wantDelivered)
		}
		// required = delivered ÷ 1500 × 1.005 = size × 1.005
		wantRequired := sizes[i].Mul(decimal.RequireFromString("1.005")).Truncate(7)
		if !r.Required.Equal(wantRequired) {
			t.Errorf("row %d required = %s, want %s", i, r.Required, wantRequired)
		}
		if got := r.AsymPct.StringFixed(4); got != "0.5000" {
			t.Errorf("row %d asymmetry = %s%%, want 0.5000%%", i, got)
		}
	}

	min, max := asymExtremes(rows)
	if min.StringFixed(4) != "0.5000" || max.StringFixed(4) != "0.5000" {
		t.Errorf("extremes = %s%%..%s%%, want 0.5000%%..0.5000%%", min, max)
	}
}

// TestMeasureReportsNoMarketOnEmptyReceive covers the one-sided finding: a
// strict-receive query that comes back with no records is NO-MARKET for the
// matched leg, never a zero-priced row pretending to be a measurement.
func TestMeasureReportsNoMarketOnEmptyReceive(t *testing.T) {
	// delivered for 1 USDC at rate 1500 is exactly 1500.
	srv := httptest.NewServer(bookHandler(t, "1500", "0.005", "1500"))
	defer srv.Close()

	d := &dex.Client{HorizonURL: srv.URL}
	rows := measure(context.Background(), d, []decimal.Decimal{decimal.NewFromInt(1)}, asset.NGNC())

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.NoMarket || r.State != "no-market" {
		t.Errorf("state = %q (NoMarket=%v), want no-market", r.State, r.NoMarket)
	}
	if !r.AsymPct.IsZero() {
		t.Errorf("asymmetry = %s, want zero on an unpriced row", r.AsymPct)
	}
	if !strings.Contains(r.Detail, "no path delivers 1500") {
		t.Errorf("detail = %q, want it to name the undeliverable amount", r.Detail)
	}
}

// TestErroredNamesOnlyTransportFailures keeps NO-MARKET (a finding) out of
// the set that makes a recording unfit: only sizes whose request failed have
// a hole in the snapshot.
func TestErroredNamesOnlyTransportFailures(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
	defer failing.Close()

	d := &dex.Client{HorizonURL: failing.URL}
	rows := measure(context.Background(), d, []decimal.Decimal{decimal.NewFromInt(10)}, asset.NGNC())

	failed := errored(rows)
	if len(failed) != 1 || failed[0] != "10" {
		t.Fatalf("errored = %v, want [10]", failed)
	}

	// The healthy no-market case must not be treated as a hole.
	srv := httptest.NewServer(bookHandler(t, "1500", "0.005", "1500"))
	defer srv.Close()
	good := measure(context.Background(), &dex.Client{HorizonURL: srv.URL},
		[]decimal.Decimal{decimal.NewFromInt(1)}, asset.NGNC())
	if holes := errored(good); len(holes) != 0 {
		t.Fatalf("errored = %v, want none; NO-MARKET is a finding, not a hole", holes)
	}
}

// TestResolveCorridorRequiresAFiatPeg guards the comparison's meaning: a
// token without a published peg has no matched benchmark story, so -to must
// reject it rather than measure something uninterpretable.
func TestResolveCorridorRequiresAFiatPeg(t *testing.T) {
	if _, peg, err := resolveCorridor("NGNC"); err != nil || peg != "NGN" {
		t.Errorf("NGNC -> peg %q, err %v; want NGN, nil", peg, err)
	}
	if _, _, err := resolveCorridor("NOPE"); err == nil {
		t.Error("unknown destination accepted")
	}
}

// TestParseSizesSkipsMalformed mirrors cmd/ladder's contract: a sweep
// measures what it can instead of aborting on one typo.
func TestParseSizesSkipsMalformed(t *testing.T) {
	sizes, err := parseSizes("1,abc,100")
	if err != nil {
		t.Fatalf("parseSizes: %v", err)
	}
	if len(sizes) != 2 || sizes[0].String() != "1" || sizes[1].String() != "100" {
		t.Errorf("sizes = %v, want [1 100]", sizes)
	}
}

// TestDefaultSizesFlagMatchesSharedLadder keeps the cross-check on the same
// ladder the corridor figures come from (#390).
func TestDefaultSizesFlagMatchesSharedLadder(t *testing.T) {
	sizes, err := parseSizes(defaultSizesFlag())
	if err != nil {
		t.Fatalf("parseSizes(defaultSizesFlag()): %v", err)
	}
	if len(sizes) != len(route.DefaultSizes) {
		t.Fatalf("parsed %d sizes, want the shared ladder's %d", len(sizes), len(route.DefaultSizes))
	}
	for i := range sizes {
		if !sizes[i].Equal(route.DefaultSizes[i]) {
			t.Errorf("size %d = %s, want %s", i, sizes[i], route.DefaultSizes[i])
		}
	}
}

// ensure the fake server route shapes stay what dex.Client sends.
func TestMeasureQueryShapes(t *testing.T) {
	var seenSend, seenRecv bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/hal+json")
		switch r.URL.Path {
		case "/paths/strict-send":
			seenSend = true
			// Strict-send fixes source_amount and names the destination by
			// full asset encoding; it carries no source_assets parameter.
			if q.Get("source_amount") == "" || q.Get("destination_assets") == "" {
				t.Errorf("strict-send query incomplete: %v", q)
			}
			_, _ = w.Write([]byte(`{"_embedded":{"records":[{"source_amount":"1","destination_amount":"1500","path":[]}]}}`))
		case "/paths/strict-receive":
			seenRecv = true
			// Strict-receive fixes destination_amount and names the
			// destination by its asset fields, mirroring strict-send.
			if q.Get("source_assets") == "" || q.Get("destination_asset_code") == "" || q.Get("destination_amount") == "" {
				t.Errorf("strict-receive query incomplete: %v", q)
			}
			if q.Get("destination_amount") != "1500" {
				t.Errorf("destination_amount = %q, want the strict-send delivered amount", q.Get("destination_amount"))
			}
			_, _ = w.Write([]byte(`{"_embedded":{"records":[{"source_amount":"1","destination_amount":"1500","path":[]}]}}`))
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	d := &dex.Client{HorizonURL: srv.URL}
	rows := measure(context.Background(), d, []decimal.Decimal{decimal.NewFromInt(1)}, asset.NGNC())
	if !seenSend || !seenRecv {
		t.Fatalf("both directions must be queried (send=%v receive=%v)", seenSend, seenRecv)
	}
	if len(rows) != 1 || rows[0].Err != nil {
		t.Fatalf("rows = %+v", rows)
	}
}

// A nil-context deadline must not be needed: measure propagates ctx to both
// legs, so a cancelled sweep stops instead of leaking goroutines.
func TestMeasureHonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rows := measure(ctx, &dex.Client{HorizonURL: srv.URL},
		[]decimal.Decimal{decimal.NewFromInt(1)}, asset.NGNC())
	if len(rows) != 1 || rows[0].Err == nil {
		t.Fatalf("rows = %+v, want one errored row on deadline", rows)
	}
}
