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
	_ = mid // reserved: will carry the independent mid-market rate for future FX-loss-from-mid calculation
	parts := make([]CostPart, 0, 5)

	// FX loss: difference between effective rate and mid, as a percentage.
	fxLossPct := q.LossPct
	fxLossAmount := q.LossAmount
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

	// Anchor fee: the anchor's own charge for a conversion, obtainable via
	// SEP-38 when the anchor publishes an ANCHOR_QUOTE_SERVER in its
	// stellar.toml. A DEX-only route has no anchor involved, and an anchor
	// that does not publish a quote server has no machine-readable rate — the
	// absence is a fact about the anchor rather than a zero fee.
	//
	// When a KindAnchorSEP38 quote is available, its FeeInBuyAsset (already
	// normalised for denomination by sep38.Quote) can be wired here.
	anchorFeeReason := "anchor fee not available; the anchor does not publish an ANCHOR_QUOTE_SERVER, so its fee cannot be obtained programmatically"
	if q.Kind == KindAnchorSEP38 {
		// TODO: extract anchor fee from a sep38.Quote when a corridor with
		// SEP-38 support is wired in. The sep38.Quote.FeeInBuyAsset field
		// already carries the converted fee.
		anchorFeeReason = "anchor fee available via SEP-38 but no corridor with a published quote server has been priced yet"
	}
	parts = append(parts, CostPart{
		Component:  CostAnchorFee,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     anchorFeeReason,
	})

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
