package server

import (
	"strings"
	"testing"
)

// TestUICostDecompositionPinsDisclosureAndDisciplines asserts that index.html
// renders a disclosure per rung listing cost components, labels undetermined
// components as undetermined (never zero), and mentions cost decomposition
// in the curve caption text alternative (issue #629).
func TestUICostDecompositionPinsDisclosureAndDisciplines(t *testing.T) {
	page := uiPage(t)

	for _, want := range []string{
		"costDecompositionBlock",
		"details.cost-decomp",
		"cost-decomp",
		"Marginal cost",
		"Total loss (reconciled)",
		"cost-undetermined",
		"undetermined",
		"Detailed cost decompositions (FX loss, fees, slippage, expected failure)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html missing %q for rung cost decomposition display", want)
		}
	}
}
