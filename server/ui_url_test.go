package server

import (
	"strings"
	"testing"
)

// These tests pin the URL-addressable state contract (issue #292) as
// source-text assertions on the single embedded UI file — the same approach
// as TestUIPreservesContentOnFetchError: the page has no build step and no
// JS test runner in CI, so the script's source is what is assertable. They
// cannot prove how the page behaves; the QA browser harness is for that.
//
// The contract: the measurement state (?to=, ?sizes=, ?live=) lives in the
// address bar, so a measurement can be linked, reloaded and shared. Two
// rules matter enough to pin. First, "the URL wins": a parseable ?to= on
// boot must override the has-history-first selector sort, because a link's
// reader chose that corridor. Second, the serialised URL is always a request
// the API would accept — the default ladder is never written into the bar,
// so a shared link can never 400 on sizes the UI invented.

func TestUIWritesURLStateOnMeasurement(t *testing.T) {
	page := uiSource(t)
	for _, want := range []string{
		"function writeURLState",             // the single writer
		"history.replaceState",               // replace, not push: back stays a state history
		"function readURLState",              // the single reader
		"window.addEventListener('popstate'", // back/forward re-render from the bar
		"await navigateTo()",                 // boot routes through the shared path
		"function corridorQuery",             // one request-URL builder for every caller
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; corridor state would not be URL-addressable", want)
		}
	}
	// The writer must go through replaceState, never pushState: an innerHTML
	// render per keystroke of history would make the back button useless.
	if strings.Contains(page, "history.pushState") {
		t.Error("UI pushes state per render; back-button history would fill with intermediate states")
	}
}

func TestUIURLStateOverridesBootSelection(t *testing.T) {
	page := uiSource(t)
	// Boot must consult the address bar before falling back to the
	// has-history-first sort, so ?to=GHSC opens GHSC even when another
	// corridor has history and GHSC does not.
	if !strings.Contains(page, "const url = readURLState();") {
		t.Fatal("boot does not read the URL state; a linked corridor would be ignored")
	}
	if !strings.Contains(page, "if (url) {\n    await navigateTo();") {
		t.Error("a parseable ?to= must navigate before the default sort is applied")
	}
}

func TestUISerialisedURLOmitsDefaultSizes(t *testing.T) {
	page := uiSource(t)
	// The serialised form is the wire shape: sizes= is written only when it
	// differs from the default ladder, so a shared URL is always a request
	// the API accepts. The constant and both guard sites must agree.
	for _, want := range []string{
		"const DEFAULT_SIZES_STR = '0.1,1,5,10,25,50,100,250,500,1000,2500,5000'",
		"if (sizes && sizes !== DEFAULT_SIZES_STR) q.set('sizes', sizes);",
		"if (sizes && sizes.ok && sizes.norm !== DEFAULT_SIZES_STR)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; a serialised URL could carry sizes the API would refuse", want)
		}
	}
}

func TestUISizesInputValidatedBeforeRequest(t *testing.T) {
	page := uiSource(t)
	// The UI mirrors the server's sizes= validation (decimal, positive, at
	// most 24) so an invalid value is refused before a request is spent. The
	// server still validates — the UI check is affordance, not the boundary.
	for _, want := range []string{
		"function parseSizesInput",
		"parts.length > 24",       // mirrors maxSizes in api.go
		"is not a decimal number", // a person-readable reason, in text
		"aria-invalid",            // the field, not just the banner, carries the failure
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; an invalid sizes value would not be explained before the request", want)
		}
	}
	// The sizes field participates in the interactive state contract.
	css := styleBlock(t)
	for _, want := range []string{
		".sizes-input:focus-visible", ".sizes-input:hover:not(:disabled)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q; the sizes input would fall outside the focus/hover contract", want)
		}
	}
	if !strings.Contains(css, "var(--focus-ring)") {
		t.Error("sizes-input focus must use the shared --focus-ring token")
	}
}
