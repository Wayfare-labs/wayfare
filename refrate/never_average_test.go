package refrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Issue #172: never average two provider mids, and test that we do not.
//
// The rule (ADR 001, docs/adr-reference-mids.md) is that a scored mid is
// always one named provider's own published figure. A blended number names no
// provider, so nobody can verify it. TestNeverAverageTwoProviderMids in
// cross_test.go already checks that the mid is not the arithmetic mean; the
// tests here close the remaining ways a blend could slip through: a mid that
// is one provider's figure but labelled with the other's name, a mid pulled
// toward a configured diagnostic provider, a weighted or three-way blend, a
// mid that loses precision on the way through, and the single-source
// fallbacks.

// dec parses a decimal string from a table case.
func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// assertNamedFigure fails unless r.Mid is exactly the mid published by the
// provider r.Source names, and the secondary fields carry the other one.
func assertNamedFigure(t *testing.T, r Rate, published map[string]string) {
	t.Helper()
	want, ok := published[r.Source]
	if !ok {
		t.Fatalf("Source = %q names no configured provider (have %v)", r.Source, published)
	}
	if !r.Mid.Equal(dec(want)) {
		t.Errorf("Mid = %s, but %s published %s; the scored mid must be the named provider's own figure",
			r.Mid, r.Source, want)
	}
	// String equality too: a figure re-derived through arithmetic can be
	// numerically equal yet differ in representation, and the rule is that the
	// provider's figure is carried, not recomputed.
	if r.Mid.String() != dec(want).String() {
		t.Errorf("Mid renders as %s, want the provider's figure %s verbatim", r.Mid.String(), dec(want).String())
	}
	if r.SecondarySource == "" {
		return
	}
	other, ok := published[r.SecondarySource]
	if !ok {
		t.Fatalf("SecondarySource = %q names no configured provider", r.SecondarySource)
	}
	if r.SecondarySource == r.Source {
		t.Errorf("Source and SecondarySource are both %q", r.Source)
	}
	if !r.SecondaryMid.Equal(dec(other)) {
		t.Errorf("SecondaryMid = %s, but %s published %s", r.SecondaryMid, r.SecondarySource, other)
	}
}

// TestNeverAverageMidMatchesTheProviderItNames covers both orderings of the
// two providers across the agreement bands. Whichever side is chosen, the
// label and the number must belong to the same provider.
func TestNeverAverageMidMatchesTheProviderItNames(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name               string
		primary, secondary string
	}{
		{"agree, primary higher", "1351.25", "1350.75"},
		{"agree, secondary higher", "1350.75", "1351.25"},
		{"disagree, primary higher", "1400", "1330"},
		{"disagree, secondary higher", "1330", "1400"},
		{"malfunction", "100", "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := rateOf(t, &Cross{
				Primary:   &fakeProvider{name: "alpha", mid: tc.primary, asOf: now},
				Secondary: &fakeProvider{name: "beta", mid: tc.secondary, asOf: now},
			})
			assertNamedFigure(t, r, map[string]string{"alpha": tc.primary, "beta": tc.secondary})
		})
	}
}

// TestNeverAverageWithADiagnosticProvider: a third provider is consulted on a
// disagreement to attribute it, never to move the mid. Every blend that could
// involve it — the three-way mean, either pairwise mean with the diagnostic,
// and the diagnostic figure itself — is ruled out explicitly.
func TestNeverAverageWithADiagnosticProvider(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	const primary, secondary, third = "1300", "1365", "1310"

	r := rateOf(t, &Cross{
		Primary:    &fakeProvider{name: "alpha", mid: primary, asOf: now},
		Secondary:  &fakeProvider{name: "beta", mid: secondary, asOf: now},
		Diagnostic: &fakeProvider{name: "gamma", mid: third, asOf: now},
	})
	if r.Agreement != AgreementDisagree {
		t.Fatalf("Agreement = %v, want DISAGREE so the diagnostic is consulted", r.Agreement)
	}
	if r.Diagnostic == nil {
		t.Fatal("no diagnostic recorded; the case does not exercise the third provider")
	}
	if r.Source == "gamma" {
		t.Fatal("the diagnostic provider became the scored source")
	}
	assertNamedFigure(t, r, map[string]string{"alpha": primary, "beta": secondary})

	p, s, g := dec(primary), dec(secondary), dec(third)
	two, three := decimal.NewFromInt(2), decimal.NewFromInt(3)
	for label, blend := range map[string]decimal.Decimal{
		"the diagnostic mid":              g,
		"the mean of all three":           p.Add(s).Add(g).Div(three),
		"the mean of primary and third":   p.Add(g).Div(two),
		"the mean of secondary and third": s.Add(g).Div(two),
	} {
		if r.Mid.Equal(blend) {
			t.Errorf("Mid = %s equals %s; the diagnostic provider must not participate in the scored mid", r.Mid, label)
		}
	}
	if !r.Diagnostic.Mid.Equal(g) {
		t.Errorf("Diagnostic.Mid = %s, want the third provider's own figure %s", r.Diagnostic.Mid, g)
	}
}

// TestNeverAverageNoWeightedBlend rules out any blend at all, not only the
// even mean: for a disagreeing pair, every point strictly between the two
// mids is a blend, so the scored mid must sit on one of the endpoints.
func TestNeverAverageNoWeightedBlend(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	for _, pair := range [][2]string{{"1300", "1365"}, {"1365", "1300"}, {"1500.5", "1420.25"}} {
		r := rateOf(t, &Cross{
			Primary:   &fakeProvider{name: "alpha", mid: pair[0], asOf: now},
			Secondary: &fakeProvider{name: "beta", mid: pair[1], asOf: now},
		})
		lo, hi := dec(pair[0]), dec(pair[1])
		if lo.GreaterThan(hi) {
			lo, hi = hi, lo
		}
		if r.Mid.GreaterThan(lo) && r.Mid.LessThan(hi) {
			t.Errorf("pair %v: Mid = %s lies strictly between the two provider figures; that is a blend", pair, r.Mid)
		}
	}
}

// TestNeverAverageKeepsFullPrecision: a provider figure with more decimal
// places than any display rounding is carried exactly. Rounding it, or
// passing it through a float, would produce a number neither provider
// published — a blend with the rounding error.
func TestNeverAverageKeepsFullPrecision(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	const primary, secondary = "1350.123456789012345", "1350.987654321098765"
	r := rateOf(t, &Cross{
		Primary:   &fakeProvider{name: "alpha", mid: primary, asOf: now},
		Secondary: &fakeProvider{name: "beta", mid: secondary, asOf: now},
	})
	assertNamedFigure(t, r, map[string]string{"alpha": primary, "beta": secondary})
}

// TestNeverAverageSingleSourceFallback: when one provider fails, the rate is
// the survivor's own figure, under the survivor's name — not a mix with a
// cached or default value standing in for the missing side.
func TestNeverAverageSingleSourceFallback(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	down := errors.New("provider unavailable")

	cases := []struct {
		name                string
		primary, secondary  *fakeProvider
		wantSource, wantMid string
	}{
		{
			name:       "secondary down",
			primary:    &fakeProvider{name: "alpha", mid: "1350.5", asOf: now},
			secondary:  &fakeProvider{name: "beta", err: down},
			wantSource: "alpha", wantMid: "1350.5",
		},
		{
			name:       "primary down",
			primary:    &fakeProvider{name: "alpha", err: down},
			secondary:  &fakeProvider{name: "beta", mid: "1349.25", asOf: now},
			wantSource: "beta", wantMid: "1349.25",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := (&Cross{Primary: tc.primary, Secondary: tc.secondary}).Rate(context.Background(), "USD", "NGN")
			if err != nil {
				t.Fatalf("Cross.Rate: %v", err)
			}
			if r.Source != tc.wantSource {
				t.Errorf("Source = %q, want the surviving provider %q", r.Source, tc.wantSource)
			}
			if !r.Mid.Equal(dec(tc.wantMid)) {
				t.Errorf("Mid = %s, want the surviving provider's own figure %s", r.Mid, tc.wantMid)
			}
			if !r.SecondaryMid.IsZero() {
				t.Errorf("SecondaryMid = %s, but the other provider returned nothing; a missing side has no figure", r.SecondaryMid)
			}
		})
	}
}
