// CostDecomposition breaks the effective transfer cost into separately-reported
// components: FX loss, network fees, anchor fee, slippage, and expected
// failure cost.
//
// Currently, the verdict reports a single loss percentage against fair value.
// That number is useful but opaque. Showing the decomposition turns a single
// verdict into actionable information.
//
// Each component is computed and reported independently. Network fees and
// anchor fees are reported separately because they have different sources:
// network fees are Stellar base-fee charges per operation, while anchor fees
// are the anchor's own charge for a conversion, obtainable via SEP-38 when
// the anchor publishes an ANCHOR_QUOTE_SERVER. Expected failure cost stays
// explicitly unknown until failure history exists.
package route

import (
	"errors"

	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/shopspring/decimal"
)

// CostComponent names one piece of the effective transfer cost.
type CostComponent string

const (
	CostFXLoss          CostComponent = "fx_loss"
	CostNetworkFees     CostComponent = "network_fees"
	CostAnchorFee       CostComponent = "anchor_fee"
	CostSlippage        CostComponent = "slippage"
	CostExpectedFailure CostComponent = "expected_failure"
)

// CostPart is one component of the effective transfer cost.
type CostPart struct {
	Component  CostComponent
	Amount     decimal.Decimal
	Pct        decimal.Decimal
	Determined bool
	Reason     string
}

// CostDecomposition is the full breakdown of a route's effective transfer cost.
type CostDecomposition struct {
	Parts        []CostPart
	TotalLossPct decimal.Decimal
}

// Decompose splits a priced route's effective transfer cost into components.
func Decompose(q Quote, mid decimal.Decimal) CostDecomposition {
	parts := make([]CostPart, 0, 5)

	// FX loss: difference between effective rate and mid, as a percentage.
	fxLossPct := q.LossPct
	fxLossAmount := q.LossAmount

	anchorFeeReason := "anchor fee not available; the anchor does not publish an ANCHOR_QUOTE_SERVER, so its fee cannot be obtained programmatically"
	anchorFee := CostPart{
		Component:  CostAnchorFee,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     anchorFeeReason,
	}
	if q.Kind == KindAnchorSEP38 {
		anchorFee.Reason = "anchor fee available via SEP-38 but the normalized quote is not attached"
		if q.SEP38Quote != nil {
			feeAmount, feePct, reason := anchorFeeFromSEP38(q, mid)
			if reason == "" {
				anchorFee.Amount = feeAmount
				anchorFee.Pct = feePct
				anchorFee.Determined = true
				anchorFee.Reason = ""
				fxLossAmount = fxLossAmount.Sub(feeAmount)
				fxLossPct = fxLossPct.Sub(feePct)
			} else {
				anchorFee.Reason = reason
			}
		}
	}

	parts = append(parts, CostPart{
		Component:  CostFXLoss,
		Amount:     fxLossAmount,
		Pct:        fxLossPct,
		Determined: true,
	})

	// Network fees: undetermined. A Stellar path payment charges a base fee per
	// operation, and a multi-hop path is more operations than a direct one,
	// but Decompose sees only a Quote and has neither the path's operation
	// count nor a currently-effective base fee. Naming the gap keeps the
	// units honest: unavailable is unknown, not a default, and small is not
	// zero.
	parts = append(parts, CostPart{
		Component:  CostNetworkFees,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "network fee not measured; determining it requires the path's operation count and the current Stellar base fee",
	})

	// The anchor fee is the anchor's charge for conversion, normalized to
	// receive-asset units. DEX routes and anchor routes without a quote remain
	// undetermined rather than receiving a default zero.
	parts = append(parts, anchorFee)

	// Slippage: undetermined without a comparison across sizes.
	parts = append(parts, CostPart{
		Component:  CostSlippage,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "single quote available; slippage requires a comparison across sizes",
	})

	// Expected failure cost: explicitly unknown.
	parts = append(parts, CostPart{
		Component:  CostExpectedFailure,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "no failure history exists yet; runstore is collecting but has not accumulated enough observations",
	})

	return CostDecomposition{
		Parts:        parts,
		TotalLossPct: q.LossPct,
	}
}

func anchorFeeFromSEP38(q Quote, mid decimal.Decimal) (decimal.Decimal, decimal.Decimal, string) {
	quote := q.SEP38Quote
	switch {
	case quote == nil:
		return decimal.Zero, decimal.Zero, "SEP-38 quote is unavailable"
	case !q.SendAsset.Equal(quote.SellAsset):
		return decimal.Zero, decimal.Zero, "SEP-38 sell asset does not match the route quote"
	case !q.ReceiveAsset.Equal(quote.BuyAsset):
		return decimal.Zero, decimal.Zero, "SEP-38 buy asset does not match the route quote"
	case !q.SendAmount.Equal(quote.SellAmount):
		return decimal.Zero, decimal.Zero, "SEP-38 sell amount does not match the route quote"
	case !q.ReceiveAmount.Equal(quote.BuyAmount):
		return decimal.Zero, decimal.Zero, "SEP-38 buy amount does not match the route quote"
	case !quote.Price.IsPositive():
		return decimal.Zero, decimal.Zero, "SEP-38 quote has no positive price"
	case !quote.SellAmount.IsPositive():
		return decimal.Zero, decimal.Zero, "SEP-38 quote has no positive sell amount"
	case !quote.BuyAmount.IsPositive():
		return decimal.Zero, decimal.Zero, "SEP-38 quote has no positive buy amount"
	case !quote.GrossBuyAmount.Equal(quote.SellAmount.Div(quote.Price)):
		return decimal.Zero, decimal.Zero, "SEP-38 gross buy amount does not match sell_amount / price"
	case !quote.FeeInBuyAsset.Equal(quote.GrossBuyAmount.Sub(quote.BuyAmount)):
		return decimal.Zero, decimal.Zero, "SEP-38 fee does not match the fee-denomination identity"
	case quote.FeeInBuyAsset.IsNegative():
		return decimal.Zero, decimal.Zero, "SEP-38 quote has a negative buy-asset fee"
	case quote.Fee.Total.IsNegative():
		return decimal.Zero, decimal.Zero, "SEP-38 quote has a negative fee total"
	case !mid.IsPositive():
		return decimal.Zero, decimal.Zero, "reference mid unavailable; cannot reconcile anchor fee with route loss"
	}

	switch {
	case quote.Fee.Total.IsZero():
		if !quote.FeeInBuyAsset.IsZero() {
			return decimal.Zero, decimal.Zero, "SEP-38 quote has a buy-asset fee but no disclosed fee total"
		}
	case quote.Fee.Asset.Equal(quote.SellAsset):
		netSell := quote.SellAmount.Sub(quote.Fee.Total)
		if !netSell.IsPositive() {
			return decimal.Zero, decimal.Zero, "SEP-38 sell-asset fee is not less than the sell amount"
		}
		wantFeeInBuy := quote.BuyAmount.Mul(quote.Fee.Total).Div(netSell)
		if !quote.FeeInBuyAsset.Equal(wantFeeInBuy) {
			return decimal.Zero, decimal.Zero, "SEP-38 converted fee does not match its sell-asset denomination"
		}
	case quote.Fee.Asset.Equal(quote.BuyAsset):
		if !quote.FeeInBuyAsset.Equal(quote.Fee.Total) {
			return decimal.Zero, decimal.Zero, "SEP-38 converted fee does not match its buy-asset denomination"
		}
	default:
		return decimal.Zero, decimal.Zero, "SEP-38 fee asset is neither the sell nor buy asset"
	}

	referenceAmount := q.SendAmount.Mul(mid)
	if !referenceAmount.IsPositive() {
		return decimal.Zero, decimal.Zero, "reference amount unavailable; cannot reconcile anchor fee with route loss"
	}
	if !q.ReferenceMid.Equal(mid) {
		return decimal.Zero, decimal.Zero, "route reference mid does not match the decomposition reference mid"
	}
	effectiveRate := q.ReceiveAmount.Div(q.SendAmount)
	if !q.EffectiveRate.Equal(effectiveRate) {
		return decimal.Zero, decimal.Zero, "route effective rate does not match its amounts"
	}
	wantLossPct := mid.Sub(effectiveRate).Div(mid).Mul(decimal.NewFromInt(100))
	if wantLossPct.IsNegative() {
		wantLossPct = decimal.Zero
	}
	if !q.LossPct.Equal(wantLossPct) {
		return decimal.Zero, decimal.Zero, "route loss percentage does not match its reference mid and effective rate"
	}
	wantLossAmount := referenceAmount.Sub(q.ReceiveAmount)
	if wantLossAmount.IsNegative() {
		wantLossAmount = decimal.Zero
	}
	if !q.LossAmount.Equal(wantLossAmount) {
		return decimal.Zero, decimal.Zero, "route loss amount does not match its reference mid and receive amount"
	}
	feePct := quote.FeeInBuyAsset.Div(referenceAmount).Mul(decimal.NewFromInt(100))
	return quote.FeeInBuyAsset, feePct, ""
}

// SweepCostClass classifies the upstream request cost of a metric sweep
// across sizes, distinguishing CostOneRequest from CostExpensive in accordance
// with checks.Descriptor.Cost.
func SweepCostClass(sizeCount int) checks.Cost {
	if sizeCount <= 1 {
		return checks.CostOneRequest
	}
	return checks.CostExpensive
}

// ErrComponentSumMismatch is returned when the sum of determined and undetermined
// cost components does not match the published total loss within the stated tolerance.
var ErrComponentSumMismatch = errors.New("component sum does not match total loss within tolerance")

// ReconcileComponents asserts that the sum of components and undetermined amount
// equals the total loss within the stated tolerance.
func ReconcileComponents(total decimal.Decimal, comps map[string]decimal.Decimal, undetermined decimal.Decimal, tolerance decimal.Decimal) error {
	sum := undetermined
	for _, val := range comps {
		sum = sum.Add(val)
	}
	diff := sum.Sub(total).Abs()
	if diff.GreaterThan(tolerance) {
		return ErrComponentSumMismatch
	}
	return nil
}
