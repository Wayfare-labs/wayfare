package refrate

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSamePeg(t *testing.T) {
	cases := []struct {
		base, quote string
		want        bool
	}{
		{"USD", "USD", true},
		{"usd", " USD ", true},
		{"USD", "NGN", false},
		{"EUR", "USD", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := SamePeg(c.base, c.quote); got != c.want {
			t.Errorf("SamePeg(%q, %q) = %v, want %v", c.base, c.quote, got, c.want)
		}
	}
}

// TestIdentityIsDefinitionalNotObserved pins the shape of the same-peg
// reference: exactly 1, sourced to no provider, and carrying no timestamp
// that would make it look like a fresh observation.
func TestIdentityIsDefinitionalNotObserved(t *testing.T) {
	r, err := Identity("usd", "USD")
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if !r.Mid.Equal(decimal.NewFromInt(1)) {
		t.Errorf("Mid = %s, want exactly 1", r.Mid)
	}
	if !r.Definitional() || r.Kind.String() != "definitional" {
		t.Errorf("Kind = %v, want definitional", r.Kind)
	}
	if r.Source != DefinitionalSource {
		t.Errorf("Source = %q, want %q: an identity must not name a provider", r.Source, DefinitionalSource)
	}
	if !r.AsOf.IsZero() || !r.FetchedAt.IsZero() {
		t.Errorf("AsOf=%v FetchedAt=%v; a definition has no moment and was never fetched", r.AsOf, r.FetchedAt)
	}
	if r.Pair() != "USD/USD" {
		t.Errorf("Pair = %q, want USD/USD", r.Pair())
	}
	if r.Note == "" {
		t.Error("Note is empty; the wire must explain why the mid is 1")
	}
	if !r.Scorable() {
		t.Error("an identity reference must be scorable: loss is deviation from par")
	}
}

// TestIdentityRefusesARealFXPair guards against Identity being used to
// manufacture a constant for a pair that has a market rate.
func TestIdentityRefusesARealFXPair(t *testing.T) {
	if _, err := Identity("USD", "NGN"); err == nil {
		t.Fatal("Identity(USD, NGN) succeeded; it must only answer a currency against itself")
	}
}

// TestObservedIsTheZeroKind keeps every provider-built rate, including ones
// constructed before Kind existed, reading as an observation.
func TestObservedIsTheZeroKind(t *testing.T) {
	var r Rate
	if r.Definitional() || r.Kind.String() != "observed" {
		t.Errorf("zero Rate kind = %q, want observed", r.Kind)
	}
}

func TestKindOfSource(t *testing.T) {
	if KindOfSource(DefinitionalSource) != KindDefinitional {
		t.Error("the definitional source must recover KindDefinitional")
	}
	for _, s := range []string{"exchangerate-api", "currency-api", "static", ""} {
		if KindOfSource(s) != KindObserved {
			t.Errorf("KindOfSource(%q) is not observed", s)
		}
	}
}
