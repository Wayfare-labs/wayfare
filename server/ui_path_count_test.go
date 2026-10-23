package server

import (
	"strings"
	"testing"
)

// TestUIRenderDistinctPathCount pins issue #630:
// The measurements table renders a "Paths" column displaying the distinct candidate
// path count per rung (from r.path_count or r.quote.path_count), carries a tooltip
// explaining what counts as distinct, and handles absent/zero counts for older records
// without crashing or printing zero (displaying &mdash;).
func TestUIRenderDistinctPathCount(t *testing.T) {
	page := uiHTML(t)

	// Verify header definition with tooltip explaining distinct candidate paths
	for _, want := range []string{
		`th scope="col" class="num" title=`,
		`distinct candidate payment paths`,
		`>Paths</th>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI measurements table header is missing %q", want)
		}
	}

	// Verify path count extraction handles present count as well as absent/older records
	for _, want := range []string{
		"r.path_count",
		"q.path_count",
		"&mdash;",
		"<td class=\"num\" data-label=\"Paths\">${pathCount}</td>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI measurements table row rendering is missing %q", want)
		}
	}

	// Verify th[title] styling exists for tooltip affordance
	if !strings.Contains(page, "th[title] { cursor: help; }") {
		t.Error("UI missing th[title] { cursor: help; } tooltip affordance style")
	}

	// Verify both scored and unscored tables include the Paths header
	scoredTable := page[strings.Index(page, "if (scored) {"):strings.Index(page, "return `<table>\n    <caption>Every size measured. No loss")]
	if !strings.Contains(scoredTable, "${pathCountHeader}") {
		t.Error("scored measurements table header missing pathCountHeader")
	}
	unscoredTable := page[strings.Index(page, "<caption>Every size measured. No loss"):strings.Index(page, "function findings(f)")]
	if !strings.Contains(unscoredTable, "${pathCountHeader}") {
		t.Error("unscored measurements table header missing pathCountHeader")
	}
}
