package server

import (
	"strings"
	"testing"
)

// TestUIAmountFormatting guards issue #634: received amounts like
// 62900.1234567 must display with locale-aware digit grouping so NGN
// five- and six-digit figures are not misread by an order of magnitude —
// without ever changing a digit. The grouping is applied to the integer
// part only; the exact string survives in the element's tooltip.
func TestUIAmountFormatting(t *testing.T) {
	page := uiSource(t)

	if !strings.Contains(page, "const formatAmount") {
		t.Error("UI missing the formatAmount helper; large received amounts stay ungrouped")
	}
	if !strings.Contains(page, "Intl.NumberFormat") {
		t.Error("UI missing Intl.NumberFormat; the grouping separator is hard-coded instead of locale-aware")
	}
	if !strings.Contains(page, "const amountCell") {
		t.Error("UI missing the amountCell wrapper; the exact string is not preserved in a tooltip")
	}
	if !strings.Contains(page, `class="amount" title="`) {
		t.Error("amountCell does not render a title attribute; the exact server string would be lost")
	}

	// The helper must never round: the decimal tail is concatenated, not
	// re-formatted through toFixed or parseFloat-to-display.
	if strings.Contains(page, "formatAmount") && strings.Contains(page, "toFixed") {
		// toFixed appears for percentages elsewhere; pin that formatAmount
		// itself does not call it by inspecting the helper body.
		start := strings.Index(page, "const formatAmount")
		end := strings.Index(page[start:], "const amountCell")
		if end < 0 {
			t.Fatal("could not find the formatAmount helper body")
		}
		body := page[start : start+end]
		if strings.Contains(body, "toFixed") {
			t.Error("formatAmount rounds via toFixed; the helper must preserve every published digit")
		}
		if strings.Contains(body, "parseFloat") {
			t.Error("formatAmount parses to a float; precision must be preserved by string operations only")
		}
	}

	// Both receive-amount table cells render through the wrapper.
	if n := strings.Count(page, "${amountCell(q.receive_amount)}"); n != 2 {
		t.Errorf("receive-amount cells using amountCell = %d, want 2 (scored and unscored tables)", n)
	}
	if strings.Contains(page, "${esc(q.receive_amount)}") {
		t.Error("a receive-amount cell still renders the raw string; grouping would not be applied")
	}
}
