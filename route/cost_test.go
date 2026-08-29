package route

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestDecomposeExpectedFailureCostUndetermined(t *testing.T) {
	q := Quote{
		LossPct:    decimal.NewFromFloat(1.25),
		LossAmount: decimal.NewFromFloat(0.50),
	}
	mid := decimal.NewFromFloat(100.0)

	decomp := Decompose(q, mid)

	var found bool
	for _, part := range decomp.Parts {
		if part.Component == CostExpectedFailure {
			found = true
			if part.Determined {
				t.Error("expected_failure component must stay undetermined, but Determined is true")
			}
			if part.Reason == "" {
				t.Error("expected_failure component must provide a reason why it is undetermined, but reason is empty")
			}
		}
	}

	if !found {
		t.Fatal("CostDecomposition missing expected_failure component")
	}
}
