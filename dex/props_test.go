package dex

// Property tests for path rate arithmetic (issue #278). Same convention as
// route/props_test.go: hand-rolled, fixed seed printed on failure, standard
// library + shopspring/decimal only. Division rounds at 16 decimal places,
// so round-trip bounds are absolute (one ulp of the quotient), not relative.

import (
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
)

const propSeed = 20260926

func randPositive(rng *rand.Rand) decimal.Decimal {
	mant := decimal.NewFromInt(int64(rng.Intn(999) + 1))
	exp := int32(rng.Intn(15) - 7)
	return mant.Shift(exp)
}

func TestPropsPathRate(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	ulp := decimal.New(1, -16)

	for i := 0; i < 500; i++ {
		src := randPositive(rng)
		dst := randPositive(rng)
		p := Path{SourceAmount: src, DestAmount: dst}
		rate := p.Rate()

		// The rate is the destination amount per source unit: multiplying
		// back reaches the destination within one ulp of the quotient.
		if diff := rate.Mul(src).Sub(dst).Abs(); diff.GreaterThan(ulp.Mul(src)) {
			t.Fatalf("seed %d case %d: Rate*src off by %s (src %s dst %s rate %s)", propSeed, i, diff, src, dst, rate)
		}

		// Scaling both sides by the same factor is the same market: the
		// rate must not move more than the two roundings combined.
		k := randPositive(rng)
		scaled := Path{SourceAmount: src.Mul(k), DestAmount: dst.Mul(k)}
		if diff := scaled.Rate().Sub(rate).Abs(); diff.GreaterThan(ulp) {
			t.Fatalf("seed %d case %d: rate moved %s under uniform scaling by %s (%s/%s → %s/%s)",
				propSeed, i, diff, k, src, dst, src.Mul(k), dst.Mul(k))
		}
	}
}

func TestPropsPathRateZeroSource(t *testing.T) {
	// A zero source amount has no rate. The documented answer is zero —
	// no division is attempted, so no panic and no invented figure.
	p := Path{SourceAmount: decimal.Zero, DestAmount: decimal.NewFromInt(100)}
	if got := p.Rate(); !got.IsZero() {
		t.Fatalf("Rate with zero source = %s, want zero", got)
	}
}
