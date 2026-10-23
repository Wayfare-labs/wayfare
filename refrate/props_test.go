package refrate

// Property tests for the divergence arithmetic (issue #278). Same shape as
// route/props_test.go: hand-rolled generation, fixed seed printed on
// failure, standard library + shopspring/decimal only. See that file for
// why tolerances are 1e-12 relative against 16-digit division.

import (
	"math/rand"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const propSeed = 20260926

func randPositive(rng *rand.Rand) decimal.Decimal {
	mant := decimal.NewFromInt(int64(rng.Intn(999) + 1))
	exp := int32(rng.Intn(15) - 7)
	return mant.Shift(exp)
}

func relErr(got, want decimal.Decimal) decimal.Decimal {
	diff := got.Sub(want).Abs()
	if want.IsZero() {
		return diff
	}
	return diff.Div(want.Abs())
}

var tol1e12 = decimal.New(1, -12)

// sameMoment keeps the STALE band out of the property: every case below
// uses identical AsOf values, so the band under test is decided by
// divergence alone.
var sameMoment = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func rates(a, b decimal.Decimal) (Rate, Rate) {
	return Rate{Base: "USD", Quote: "NGN", Mid: a, Source: "primary", AsOf: sameMoment, FetchedAt: sameMoment},
		Rate{Base: "USD", Quote: "NGN", Mid: b, Source: "secondary", AsOf: sameMoment, FetchedAt: sameMoment}
}

func TestPropsReconcileDivergence(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 500; i++ {
		a := randPositive(rng)
		b := randPositive(rng)
		p, s := rates(a, b)
		out := reconcile(p, s)
		flip := reconcile(s, p)

		// Divergence is never negative and never depends on which
		// provider happens to be primary.
		if out.DivergencePct.IsNegative() {
			t.Fatalf("seed %d case %d: negative divergence %s (a %s b %s)", propSeed, i, out.DivergencePct, a, b)
		}
		if !out.DivergencePct.Equal(flip.DivergencePct) {
			t.Fatalf("seed %d case %d: divergence not symmetric: %s vs %s (a %s b %s)",
				propSeed, i, out.DivergencePct, flip.DivergencePct, a, b)
		}

		// The figure describes the gap: divergence * lo ≈ hi − lo.
		lo, hi := a, b
		if hi.LessThan(lo) {
			lo, hi = hi, lo
		}
		if r := relErr(out.DivergencePct.Div(decimal.NewFromInt(100)).Mul(lo), hi.Sub(lo)); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: divergence %s%% inconsistent with gap %s/%s (err %s)",
				propSeed, i, out.DivergencePct, hi.Sub(lo), lo, r)
		}

		// Identical quotes cannot diverge.
		if a.Equal(b) && !out.DivergencePct.IsZero() {
			t.Fatalf("seed %d case %d: identical mids diverge by %s", propSeed, i, out.DivergencePct)
		}

		// Band placement follows the documented thresholds, boundaries
		// inclusive on the agree side (the code uses GreaterThan).
		var want Agreement
		switch {
		case out.DivergencePct.GreaterThan(DivergenceMalfunction):
			want = AgreementMalfunction
		case out.DivergencePct.GreaterThan(DivergenceAgree):
			want = AgreementDisagree
		default:
			want = AgreementAgree
		}
		if out.Agreement != want {
			t.Fatalf("seed %d case %d: divergence %s%% gave %v, want %v", propSeed, i, out.DivergencePct, out.Agreement, want)
		}

		// Conservative scoring: when the providers genuinely disagree, the
		// scored mid is the larger of the two — the one that makes the
		// corridor look worse, never the flattering one.
		if out.Agreement == AgreementDisagree && !out.Mid.Equal(hi) {
			t.Fatalf("seed %d case %d: DISAGREE scored against %s, want the larger mid %s", propSeed, i, out.Mid, hi)
		}
		// When they agree, the primary is kept.
		if out.Agreement == AgreementAgree && !out.Mid.Equal(a) {
			t.Fatalf("seed %d case %d: AGREE replaced the primary mid %s with %s", propSeed, i, a, out.Mid)
		}
	}
}

func TestPropsReconcileZeroMidIsMalfunction(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 50; i++ {
		m := randPositive(rng)
		for _, pair := range [][2]decimal.Decimal{{decimal.Zero, m}, {m, decimal.Zero}} {
			p, s := rates(pair[0], pair[1])
			out := reconcile(p, s)
			if out.Agreement != AgreementMalfunction {
				t.Fatalf("seed %d case %d: zero mid %s/%s gave %v, want MALFUNCTION", propSeed, i, pair[0], pair[1], out.Agreement)
			}
			// The refusal must be stated, not left for the caller to
			// infer: the note names the broken feed.
			if out.Note == "" {
				t.Fatalf("seed %d case %d: MALFUNCTION without a note", propSeed, i)
			}
		}
	}
}

func TestPropsReconcileStalePicksFresher(t *testing.T) {
	// A gap beyond StaleGap with different AsOf values is labelled STALE,
	// and STALE still scores — against the fresher of the two.
	old := sameMoment.Add(-3 * 24 * time.Hour)
	p, s := rates(decimal.NewFromInt(1500), decimal.NewFromInt(1510))
	s.AsOf = old
	out := reconcile(p, s)
	if out.Agreement != AgreementStale {
		t.Fatalf("seed %d: %v apart by 3 days gave %v, want STALE", propSeed, out.DivergencePct, out.Agreement)
	}
	if !out.Mid.Equal(p.Mid) || out.Source != "primary" {
		t.Fatalf("seed %d: STALE must score against the fresher rate, got %s via %s", propSeed, out.Mid, out.Source)
	}
	// Flipping which side is fresh flips the scored rate.
	p2, s2 := rates(decimal.NewFromInt(1500), decimal.NewFromInt(1510))
	p2.AsOf = old
	out2 := reconcile(p2, s2)
	if !out2.Mid.Equal(s2.Mid) || out2.Source != "secondary" {
		t.Fatalf("seed %d: STALE with stale primary scored %s via %s, want the fresher secondary", propSeed, out2.Mid, out2.Source)
	}
}
