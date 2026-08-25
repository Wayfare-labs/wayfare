package refrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func officialRate(mid string) Rate {
	return Rate{
		Base:   "USD",
		Quote:  "NGN",
		Mid:    decimal.RequireFromString(mid),
		Source: "official",
	}
}

// TestParallelRate covers a parallel source that answers with a defensible
// mid: it is reported as a separate dimension and the gap to the official mid
// is derived rather than blended.
func TestParallelRate(t *testing.T) {
	official := officialRate("1350")
	street := &fakeProvider{name: "street", mid: "1600"}

	p := ParallelAgainst(context.Background(), street, official)

	if !p.Reported() || p.Status != ParallelReported {
		t.Fatalf("status = %s, want REPORTED", p.Status)
	}
	if p.Status.String() != "REPORTED" {
		t.Fatalf("Status.String() = %q, want REPORTED", p.Status.String())
	}
	if p.Source != "street" {
		t.Fatalf("Source = %q, want street", p.Source)
	}
	if !p.Mid.Equal(decimal.RequireFromString("1600")) {
		t.Fatalf("Mid = %s, want 1600", p.Mid)
	}
	// (1600 - 1350) / 1350 * 100 = 18.5185…%, positive because the street
	// value is weaker than the official one.
	want := decimal.RequireFromString("18.5185")
	if p.GapPct.Round(4).String() != want.String() {
		t.Fatalf("GapPct = %s, want %s", p.GapPct.Round(4), want)
	}
	// The official mid is untouched: the parallel dimension is additive.
	if !official.Mid.Equal(decimal.RequireFromString("1350")) {
		t.Fatalf("official mid mutated to %s", official.Mid)
	}
}

// TestParallelRateUnavailable covers every way a parallel mid can fail to be
// reportable. In all of them the status is UNABLE-TO-DETERMINE and a reason is
// attached, and no number is fabricated.
func TestParallelRateUnavailable(t *testing.T) {
	official := officialRate("1350")

	cases := []struct {
		name       string
		provider   Provider
		official   Rate
		wantReason string
	}{
		{
			name:       "no source configured",
			provider:   nil,
			official:   official,
			wantReason: "no parallel-rate source configured",
		},
		{
			name:       "source fails",
			provider:   &fakeProvider{name: "street", err: errors.New("boom")},
			official:   official,
			wantReason: "boom",
		},
		{
			name:       "zero parallel mid",
			provider:   &fakeProvider{name: "street", mid: "0"},
			official:   official,
			wantReason: "zero mid",
		},
		{
			name:       "zero official mid",
			provider:   &fakeProvider{name: "street", mid: "1600"},
			official:   officialRate("0"),
			wantReason: "zero mid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ParallelAgainst(context.Background(), tc.provider, tc.official)

			if p.Reported() || p.Status != ParallelUnavailable {
				t.Fatalf("status = %s, want UNABLE-TO-DETERMINE", p.Status)
			}
			if p.Status.String() != "UNABLE-TO-DETERMINE" {
				t.Fatalf("Status.String() = %q, want UNABLE-TO-DETERMINE", p.Status.String())
			}
			if !p.Mid.IsZero() {
				t.Fatalf("Mid = %s, want zero on an unavailable parallel rate", p.Mid)
			}
			if !p.GapPct.IsZero() {
				t.Fatalf("GapPct = %s, want zero on an unavailable parallel rate", p.GapPct)
			}
			if !strings.Contains(p.Reason, tc.wantReason) {
				t.Fatalf("Reason = %q, want it to contain %q", p.Reason, tc.wantReason)
			}
		})
	}
}
