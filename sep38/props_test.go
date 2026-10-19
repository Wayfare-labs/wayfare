package sep38

// Property tests for quote normalisation (issue #278). Same convention as
// route/props_test.go: hand-rolled, fixed seed printed on failure, standard
// library + shopspring/decimal only, 1e-12 relative tolerance against the
// repo's 16-digit division.

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

func relErr(got, want decimal.Decimal) decimal.Decimal {
	diff := got.Sub(want).Abs()
	if want.IsZero() {
		return diff
	}
	return diff.Div(want.Abs())
}

var tol1e12 = decimal.New(1, -12)

func TestPropsNormalizeIdentities(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 500; i++ {
		price := randPositive(rng)
		gross := randPositive(rng)
		// A fee between zero and the gross, so the quote is internally
		// consistent by construction: sell = price * gross is exact
		// (multiplication never rounds).
		fee := gross.Mul(decimal.NewFromInt(int64(rng.Intn(10000))).Div(decimal.NewFromInt(10000)))
		q := Quote{
			Price:      price,
			SellAmount: price.Mul(gross),
			BuyAmount:  gross.Sub(fee),
		}
		if err := q.normalize(); err != nil {
			t.Fatalf("seed %d case %d: consistent quote refused: %v (price %s gross %s fee %s)",
				propSeed, i, err, price, gross, fee)
		}

		// Gross round-trips through the price. Division rounds at 16
		// decimal *places*, so the bound is absolute: the error of one
		// ulp of the quotient, scaled by the multiplier.
		if diff := q.GrossBuyAmount.Mul(price).Sub(q.SellAmount).Abs(); diff.GreaterThan(price.Mul(decimal.New(1, -16))) {
			t.Fatalf("seed %d case %d: GrossBuyAmount*Price off by %s (gross %s price %s sell %s)",
				propSeed, i, diff, q.GrossBuyAmount, price, q.SellAmount)
		}
		// The derived fee is the fee that was charged.
		if r := relErr(q.FeeInBuyAsset, fee); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: FeeInBuyAsset %s, want %s (err %s)", propSeed, i, q.FeeInBuyAsset, fee, r)
		}
		// The all-in rate never beats the advertised one.
		if q.TotalPrice.LessThan(price) {
			t.Fatalf("seed %d case %d: TotalPrice %s below advertised Price %s with fee %s",
				propSeed, i, q.TotalPrice, price, fee)
		}
		// And it round-trips: TotalPrice * Buy ≈ Sell, within one ulp of
		// the quotient times the multiplier.
		if diff := q.TotalPrice.Mul(q.BuyAmount).Sub(q.SellAmount).Abs(); diff.GreaterThan(q.BuyAmount.Mul(decimal.New(1, -16))) {
			t.Fatalf("seed %d case %d: TotalPrice*Buy off by %s (buy %s sell %s)", propSeed, i, diff, q.BuyAmount, q.SellAmount)
		}
	}
}

func TestPropsNormalizeZeroFee(t *testing.T) {
	// With no fee the recipient gets exactly the gross: the derived fee is
	// zero, not merely small.
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 100; i++ {
		price, gross := randPositive(rng), randPositive(rng)
		q := Quote{Price: price, SellAmount: price.Mul(gross), BuyAmount: gross}
		if err := q.normalize(); err != nil {
			t.Fatalf("seed %d case %d: zero-fee quote refused: %v", propSeed, i, err)
		}
		if !q.FeeInBuyAsset.IsZero() {
			t.Fatalf("seed %d case %d: zero fee derived as %s", propSeed, i, q.FeeInBuyAsset)
		}
		if r := relErr(q.TotalPrice, price); r.GreaterThan(tol1e12) {
			t.Fatalf("seed %d case %d: zero-fee TotalPrice %s vs Price %s (err %s)", propSeed, i, q.TotalPrice, price, r)
		}
	}
}

func TestPropsNormalizeRefusesBrokenQuotes(t *testing.T) {
	rng := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 200; i++ {
		positive := randPositive(rng)

		bad := []Quote{
			// Non-positive price: the division it feeds is undefined.
			{Price: decimal.Zero, SellAmount: positive, BuyAmount: positive},
			{Price: positive.Neg(), SellAmount: positive, BuyAmount: positive},
			// Negative amounts: an anchor cannot owe the user money.
			{Price: positive, SellAmount: positive.Neg(), BuyAmount: positive},
			{Price: positive, SellAmount: positive, BuyAmount: positive.Neg()},
			// buy_amount above the gross implied by the anchor's own price:
			// the anchor's numbers disagree with each other.
			{Price: positive, SellAmount: positive, BuyAmount: positive.Div(positive).Add(positive)},
		}
		for j, q := range bad {
			if err := q.normalize(); err == nil {
				t.Fatalf("seed %d case %d.%d: broken quote accepted (price %s sell %s buy %s)",
					propSeed, i, j, q.Price, q.SellAmount, q.BuyAmount)
			}
		}
	}
}
