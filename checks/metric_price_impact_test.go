package checks

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

func TestPriceImpactMetric_UnsetSizes(t *testing.T) {
	m := PriceImpactMetric{
		ProbeSize: decimal.Zero,
		FullSize:  decimal.Zero,
	}
	s := Subject{
		Send:    asset.Asset{Code: "USDC"},
		Receive: asset.Asset{Code: "NGNC"},
	}
	res := m.Run(context.Background(), s)
	if res.Verdict != VerdictUndetermined {
		Errorf(t, "expected undetermined for unset probe and full sizes, got %v", res.Verdict)
	}
}
