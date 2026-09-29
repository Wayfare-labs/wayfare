package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestUIPreservesContentOnFetchError pins the contract from issue #293: a
// failed fetch must never empty the page. Previously measure() and
// loadTrend() cleared #out before the request, so an error left the reader
// staring at an empty page with nothing to recover, forcing them to re-enter
// everything. Now the last successful render stays visible and the error is
// shown as a banner above it.
//
// These assertions are on the UI source because the page is a single
// embedded file with no build step and no JS test runner in CI — the same
// approach as TestUITrendIsSelfContained.
func TestUIPreservesContentOnFetchError(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	// Nothing may blank #out before a fetch resolves. The only assignments
	// to #out.innerHTML allowed are writes of freshly rendered content.
	if strings.Contains(page, "$('out').innerHTML = '';") {
		t.Error("#out is cleared before a fetch; a failed request would leave an empty page")
	}

	for _, want := range []string{
		"function showErrorBanner",        // the shared preserve-and-banner path
		"insertAdjacentHTML('afterbegin'", // the banner goes above retained content
		`role="alert"`,                    // announced to assistive tech, not colour alone
		"prior.remove()",                  // retrying never stacks banners
		"Could not measure:",              // the measure failure keeps its text
		"Could not load history:",         // the trend failure keeps its text
		"Could not load corridor data:",   // the corridor failure keeps its text
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; an error would not preserve and explain the page", want)
		}
	}

	// A failed history load must not drop the last good trend state: the
	// only remaining assignment is the declaration itself.
	if n := strings.Count(page, "trendState = null"); n != 1 {
		t.Errorf(`found %d "trendState = null" assignments, want 1 (the declaration only); a failed trend load must keep the previous trend readable`, n)
	}

	// Definition plus at least the three fetch paths route errors through
	// the banner helper rather than replacing #out.
	if n := strings.Count(page, "showErrorBanner("); n < 4 {
		t.Errorf("showErrorBanner used %d times, want at least 4 (definition + three fetch paths)", n)
	}
}

// TestUISemanticHTMLPass pins the contract from issue #308: the results
// region is landmarked, sectioned and listed rather than a div soup, and
// the result is keyboard reachable. The page is asserted at the source
// level for the same reason as TestUIPreservesContentOnFetchError: one
// embedded file, no build step, no JS test runner in CI.
func TestUISemanticHTMLPass(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		// Landmarks and keyboard reachability.
		`<a class="skip-link" href="#out">Skip to results</a>`, // bypass the controls
		`<main id="out" tabindex="-1">`,                        // results are a landmark and a focus target
		`$('out').focus({ preventScroll: true });`,             // render moves focus to the results
		// Sections carry headings and accessible names; no bare panel divs
		// are produced by the renderers.
		`<section class="panel" aria-label="Corridor integrity and verdict">`,
		`<section class="panel" aria-label="Ladder measurements">`,
		`<section class="panel" aria-label="Counterparty checks">`,
		`<section class="panel" aria-label="Metrics">`,
		`<section class="panel" aria-label="Stored runs">`,
		// Findings and metrics are lists; evidence is a nested list.
		`<ul class="finding-list">`,
		`<li class="finding-row">`,
		`<ul class="f-evidence-list">`,
		`<li class="f-evidence">`,
		// The legend is a definition list: state → meaning, readable as pairs
		// with no stylesheet at all.
		`<dl class="legend-grid"`,
		`<dt><span class="f-state f-pass">PASS</span></dt>`,
		// Column headers are scoped, so every cell announces its column.
		`<th scope="col">Send</th>`,
		`<th scope="col">Recorded (UTC)</th>`,
		// The loading line is a live region, so "measuring live…" is
		// announced without stealing focus.
		`<span id="status" class="sub" role="status"></span>`,
		// Recommendation blocks are notes, not alerts: state, not an emergency.
		`class="rec-none" role="note"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; the results region would not be semantic", want)
		}
	}

	// Structural state chips must use the theme-aware unknown token, not the
	// raw brand navy/slate: those are dark-on-dark in dark mode (issue #308's
	// "works in light and dark"). DIRECT is a structural state, not a
	// severity, and must never render as an unreadable severity colour.
	for _, bad := range []string{
		".b-direct { color: var(--brand-navy)",
		".b-derivative { color: var(--brand-slate)",
		".f-unknown { color: var(--brand-slate)",
		".m-state {", // checked separately below for the token it colours with
	} {
		if strings.Contains(page, bad) && bad != ".m-state {" {
			t.Errorf("state chip style uses a raw brand token (%q); it is unreadable in dark mode", bad)
		}
	}
	if !strings.Contains(page, ".f-unknown { color: var(--unknown)") ||
		!strings.Contains(page, ".m-unknown { color: var(--unknown)") {
		t.Error("undetermined state styles do not use the theme-aware --unknown token")
	}
}
