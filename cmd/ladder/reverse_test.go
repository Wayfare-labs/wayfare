package main

import (
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/route"
)

// TestResolveDirectionForward pins the historical corridor: USDC in, the
// registered token out, benchmarked against the token's peg.
func TestResolveDirectionForward(t *testing.T) {
	d, err := resolveDirection("USDC", "NGNC")
	if err != nil {
		t.Fatalf("resolveDirection: %v", err)
	}
	if d.send.Code != "USDC" || d.recv.Code != "NGNC" {
		t.Errorf("corridor = %s -> %s, want USDC -> NGNC", d.send.Code, d.recv.Code)
	}
	if d.pair() != "USD/NGN" {
		t.Errorf("pair = %s, want USD/NGN", d.pair())
	}
	if d.reverse {
		t.Error("forward corridor marked reverse")
	}
	if d.fiatToken().Code != "NGNC" {
		t.Errorf("fiat token = %s, want NGNC", d.fiatToken().Code)
	}
}

// TestResolveDirectionReverse pins the exit leg (#596): the token is sent,
// USDC is received, and the benchmark is the inverted peg pair.
func TestResolveDirectionReverse(t *testing.T) {
	d, err := resolveDirection("NGNC", "USDC")
	if err != nil {
		t.Fatalf("resolveDirection: %v", err)
	}
	if d.send.Code != "NGNC" || d.recv.Code != "USDC" {
		t.Errorf("corridor = %s -> %s, want NGNC -> USDC", d.send.Code, d.recv.Code)
	}
	if d.pair() != "NGN/USD" {
		t.Errorf("pair = %s, want NGN/USD", d.pair())
	}
	if !d.reverse {
		t.Error("reverse corridor not marked reverse")
	}
	// The counterparty the checks describe is the token, not the settlement
	// asset, in both directions.
	if d.fiatToken().Code != "NGNC" {
		t.Errorf("fiat token = %s, want NGNC", d.fiatToken().Code)
	}
}

// TestResolveDirectionRejects covers the shapes that must not be interpreted
// into a measurement: an unknown token, a non-fiat send asset, and a
// token-to-token corridor.
func TestResolveDirectionRejects(t *testing.T) {
	cases := []struct {
		name       string
		from, to   string
		wantSubstr string
	}{
		{"unknown destination", "USDC", "NOPE", "unknown destination"},
		{"unknown source", "NOPE", "USDC", "unknown -from"},
		{"token to token", "NGNC", "GHSC", "a reverse corridor measures NGNC -> USDC"},
		{"settlement asset as destination", "USDC", "USDC", "unknown destination"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveDirection(tc.from, tc.to)
			if err == nil {
				t.Fatalf("resolveDirection(%q, %q) accepted; want an error", tc.from, tc.to)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantSubstr)
			}
		})
	}
}

// TestReverseSizesPreserveLadderDesign keeps the reverse ladder the forward
// ladder's notional twin: same rung count, ascending, scaled into NGN.
func TestReverseSizesPreserveLadderDesign(t *testing.T) {
	sizes := reverseSizes()
	if len(sizes) != len(route.DefaultSizes) {
		t.Fatalf("reverse ladder has %d rungs, want %d", len(sizes), len(route.DefaultSizes))
	}
	for i := 1; i < len(sizes); i++ {
		if !sizes[i].GreaterThan(sizes[i-1]) {
			t.Errorf("rung %d = %s is not greater than %s; the ladder must ascend",
				i, sizes[i], sizes[i-1])
		}
	}
	wantFirst := route.DefaultSizes[0].Mul(reverseScale).Truncate(0)
	if !sizes[0].Equal(wantFirst) {
		t.Errorf("first rung = %s, want %s", sizes[0], wantFirst)
	}
	wantLast := route.DefaultSizes[len(route.DefaultSizes)-1].Mul(reverseScale).Truncate(0)
	if !sizes[len(sizes)-1].Equal(wantLast) {
		t.Errorf("last rung = %s, want %s", sizes[len(sizes)-1], wantLast)
	}
}

// TestReverseDefaultSizesFlag proves -from alone selects the reverse ladder,
// so `ladder -from NGNC -to USDC` never measures NGNC at the USD ladder's
// unit sizes by accident.
func TestReverseDefaultSizesFlag(t *testing.T) {
	d, err := resolveDirection("NGNC", "USDC")
	if err != nil {
		t.Fatalf("resolveDirection: %v", err)
	}
	got, err := parseSizes(d.defaultSizesFlag())
	if err != nil {
		t.Fatalf("parseSizes(reverse default): %v", err)
	}
	want := reverseSizes()
	if len(got) != len(want) {
		t.Fatalf("parsed %d rungs, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("rung %d = %s, want %s", i, got[i], want[i])
		}
	}
}
