package server

import (
	"strings"
	"testing"
)

// TestUIHasDistinctEmptyStates guards the empty-state component added for
// issue #300 the way the other UI tests guard their surfaces: the whole point
// of the issue is that a reader can tell apart six different absences, so a
// change that collapsed them back into one generic message must fail here.
//
// These are source-text assertions, not rendering ones — they cannot tell you
// how a panel paints. That check belongs to the browser harness
// (docs/qa/README.md), not to go test.
func TestUIHasDistinctEmptyStates(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	// The shared component and its parts must exist.
	for _, want := range []string{
		"const emptyState", "capabilityGap",
		"es-glyph", "es-title", "es-body", "class=\"empty-state",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the UI has no %q; the empty-state component is missing", want)
		}
	}

	// Each distinct cause is named by its own title, so "empty" is not one
	// ambiguous message reused everywhere.
	for _, want := range []string{
		"No corridor selected",        // nothing chosen yet
		"No market exists",            // structural absence (NO-MARKET)
		"No stored runs yet",          // insufficient observations (trend)
		"Not available in this build", // capability gap (unsupported request)
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the UI has no empty state %q; distinct absences would collapse", want)
		}
	}

	// NO-MARKET must render the neutral empty state, not a red recommendation
	// box: a structural absence is not a severity (docs/state-vocabulary.md).
	noMarket := strings.Index(page, "d.integrity === 'NO-MARKET'")
	blockEnd := strings.Index(page, "if (!d.recommended)")
	if noMarket == -1 || blockEnd == -1 || noMarket > blockEnd {
		t.Fatal("recommendationBlock no longer special-cases NO-MARKET before the severity path")
	}
	if !strings.Contains(page[noMarket:blockEnd], "emptyState") {
		t.Error("the NO-MARKET branch does not use emptyState; a no-market corridor " +
			"would render as a failure rather than an absence")
	}

	// The component stays neutral: it is painted with the availability role,
	// never a severity/bad token. (Guards the one invariant that matters most.)
	esStart := strings.Index(page, ".empty-state .es-glyph")
	esEnd := strings.Index(page, ".empty-state .es-copy")
	if esStart == -1 || esEnd == -1 || esStart > esEnd {
		t.Fatal("the empty-state glyph style block is not findable")
	}
	glyphCSS := page[esStart:esEnd]
	if !strings.Contains(glyphCSS, "var(--availability-undetermined)") {
		t.Error("the empty state is not coloured by the availability-undetermined role")
	}
	for _, forbidden := range []string{"--bad", "--severity-unusable", "--check-fail"} {
		if strings.Contains(glyphCSS, forbidden) {
			t.Errorf("the empty state uses %q; an absence of data must not read as failure", forbidden)
		}
	}
}
