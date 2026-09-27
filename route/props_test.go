package route

// Property tests for the scoring arithmetic (issue #278): rate, loss and
// percentage conversions must hold for arbitrary decimals, not only the
// values someone thought to write down.
//
// Hand-rolled rather than a library: the repo carries no third-party test
// dependencies (CONTRIBUTING), so this file uses only the standard library
// and shopspring/decimal. Generation is deterministic — a fixed seed per
// run, printed on every failure — so any counterexample shown here is
// reproducible with `go test ./route -run TestPropsScore -seed=<printed>`.
//
// Tolerances: decimal division in this repo runs at the default
// DivisionPrecision of 16 digits, so round-trip identities (multiply after
// dividing) are asserted to a relative error of 1e-12, which is orders of
// magnitude tighter than any figure the product displays and far looser
// than the arithmetic's own error.

import (
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
)

// propSeed is fixed so a run is reproducible; failures print it.
const propSeed = 20260926

// randPositive renders a positive decimal spanning many magnitudes:
// 1..999 mantissa scaled by 10^e, e in [-7, 7]. Money here lives between
// dust (0.1 USDC) and the ladder top (5000), with rates in the thousands,
// so this band covers the real domain with room either side.
func randPositive(rng *rand.Rand) decimal.Decimal {
	mant := decimal.NewFromInt(int64(rng.Intn(999) + 1))
	exp := int32(rng.Intn(15) - 7)
	return mant.Shift(exp)
}

// relErr reports |got-want| relative to |want|, or absolute when want is 0.
func relErr(got, want decimal.Decimal) decimal.Decimal {
	diff := got.Sub(want).Abs()
	if want.IsZero() {
		return diff
	}
	return diff.Div(want.Abs())
}

var tol1e12 = decimal.New(1, -12)

func TestPropsScoreRateAndLoss(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 500; i++ {
		send := randPositive(rng)
		mid := randPositive(rng)
		// Receive anywhere from a fraction of fair value to above it.
		receive := mid.Mul(send).Mul(decimal.NewFromInt(int64(rng.Intn(15000))).Div(decimal.NewFromInt(10000)))

		q := Quote{SendAmount: send, ReceiveAmount: receive}
		q.score(mid, "prop-test")

		if q.Verdict == VerdictUnknown {
			t.Fatalf("seed %d case %d: positive send and mid must produce a verdict, got UNKNOWN (send %s mid %s)",
				propSeed, i, send, mid)
		}
		// Effective rate round-trips: send * rate ≈ receive.
		if r := relErr(send.Mul(q.EffectiveRate), receive); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: send*EffectiveRate off by %s (send %s receive %s rate %s)",
				propSeed, i, r, send, receive, q.EffectiveRate)
		}
		// Loss is clamped at zero, never negative.
		if q.LossPct.IsNegative() {
			t.Fatalf("seed %d case %d: negative LossPct %s (eff %s mid %s)", propSeed, i, q.LossPct, q.EffectiveRate, mid)
		}
		if q.LossAmount.IsNegative() {
			t.Fatalf("seed %d case %d: negative LossAmount %s", propSeed, i, q.LossAmount)
		}
		// Zero loss exactly when the route pays at least mid — modulo the
		// 16-digit division rounding, which cannot represent a shortfall
		// below ~1e-15 relative and rounds it to a zero loss.
		beatsMid := !q.EffectiveRate.LessThan(mid)
		if beatsMid && !q.LossPct.IsZero() {
			t.Fatalf("seed %d case %d: loss %s reported for eff %s >= mid %s",
				propSeed, i, q.LossPct, q.EffectiveRate, mid)
		}
		if !beatsMid && q.LossPct.IsZero() {
			if r := relErr(q.EffectiveRate, mid); r.GreaterThan(decimal.New(1, -15)) {
				t.Fatalf("seed %d case %d: zero loss hides a %s relative shortfall (eff %s mid %s)",
					propSeed, i, r, q.EffectiveRate, mid)
			}
		}
		// The loss percentage describes the same shortfall as the amounts:
		// (1 - LossPct/100) * mid ≈ EffectiveRate.
		implied := decimal.NewFromInt(100).Sub(q.LossPct).Div(decimal.NewFromInt(100)).Mul(mid)
		want := q.EffectiveRate
		if beatsMid {
			// LossPct was clamped, so the identity holds only in the
			// direction the clamp guarantees.
			if implied.GreaterThan(want.Add(tol1e12.Mul(want.Abs()))) {
				t.Fatalf("seed %d case %d: clamped loss overstates shortfall (implied %s eff %s)", propSeed, i, implied, want)
			}
		} else if r := relErr(implied, want); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: LossPct and EffectiveRate disagree by %s (loss %s eff %s mid %s)",
				propSeed, i, r, q.LossPct, q.EffectiveRate, mid)
		}
		// LossAmount says the same thing in money: ≈ send*mid − receive,
		// clamped at zero.
		wantAmt := send.Mul(mid).Sub(receive)
		if wantAmt.IsNegative() {
			wantAmt = decimal.Zero
		}
		if r := relErr(q.LossAmount, wantAmt); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: LossAmount off by %s (got %s want %s)", propSeed, i, r, q.LossAmount, wantAmt)
		}
	}
}

func TestPropsVerdictThresholdsAndMonotonicity(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	cases := []decimal.Decimal{}
	for i := 0; i < 400; i++ {
		cases = append(cases, randPositive(rng))
	}
	// Land exactly on each boundary too — thresholds are inclusive there.
	for _, b := range []string{"3", "8", "20"} {
		v, err := decimal.NewFromString(b)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, v, v.Sub(decimal.New(1, -15)))
	}

	wantVerdict := func(loss decimal.Decimal) Verdict {
		switch {
		case loss.LessThanOrEqual(ThresholdGood):
			return VerdictGood
		case loss.LessThanOrEqual(ThresholdFair):
			return VerdictFair
		case loss.LessThanOrEqual(ThresholdPoor):
			return VerdictPoor
		default:
			return VerdictUnusable
		}
	}
	for _, loss := range cases {
		if got := verdictFor(loss); got != wantVerdict(loss) {
			t.Fatalf("seed %d: verdictFor(%s) = %v, want %v", propSeed, loss, got, wantVerdict(loss))
		}
	}

	// Monotonicity: more loss never buys a better (lower-numbered) verdict.
	for i := 0; i < 500; i++ {
		a := randPositive(rng)
		b := a.Add(randPositive(rng).Abs()) // b > a
		if verdictFor(a) > verdictFor(b) {
			t.Fatalf("seed %d case %d: verdict got better with more loss (%s→%s gives %v→%v)",
				propSeed, i, a, b, verdictFor(a), verdictFor(b))
		}
	}
}

func TestPropsScoreUnknownInputs(t *testing.T) {
	// A zero mid or zero send is unscoreable: the answer must be UNKNOWN,
	// never a verdict computed from a division that cannot be made.
	for _, q := range []Quote{
		{SendAmount: decimal.Zero, ReceiveAmount: decimal.NewFromInt(10)},
		{SendAmount: decimal.NewFromInt(10), ReceiveAmount: decimal.NewFromInt(10)},
	} {
		mid := decimal.Zero
		if q.SendAmount.IsZero() {
			mid = decimal.NewFromInt(1500)
		}
		s := q
		s.score(mid, "prop-test")
		if s.Verdict != VerdictUnknown {
			t.Fatalf("seed %d: send %s mid %s gave %v, want UNKNOWN", propSeed, q.SendAmount, mid, s.Verdict)
		}
	}
}
