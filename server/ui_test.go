package server

import (
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
		"function showErrorBanner",              // the shared preserve-and-banner path
		"insertAdjacentHTML('afterbegin'",        // the banner goes above retained content
		`role="alert"`,                           // announced to assistive tech, not colour alone
		"prior.remove()",                         // retrying never stacks banners
		"Could not measure:",                     // the measure failure keeps its text
		"Could not load history:",                // the trend failure keeps its text
		"Could not load corridor data:",          // the corridor failure keeps its text
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
