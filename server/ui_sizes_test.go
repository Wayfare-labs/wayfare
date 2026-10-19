package server

import (
	"strings"
	"testing"
)

// The page is a single embedded file with no build step and no JS test runner
// in CI, so these tests assert on the UI source itself — the same approach as
// TestUIPreservesContentOnFetchError and the other ui_*_test.go files. Each
// test pins one property of the reader-chosen-sizes contract (issue #291).

func uiHTML(t *testing.T) string {
	t.Helper()
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestUISizesControlExists pins that the measure controls expose a sizes
// input, labelled and keyboard reachable like the corridor select beside it
// (both are a <label for>/<select> pair on the same ids).
func TestUISizesControlExists(t *testing.T) {
	page := uiHTML(t)

	if !strings.Contains(page, `input id="sizes"`) {
		t.Error(`no <input id="sizes">; the reader cannot choose a transfer size`)
	}
	if !strings.Contains(page, `<label for="sizes">`) {
		t.Error(`no <label for="sizes">; the control must be labelled, not colour- or placeholder-only`)
	}
	if !strings.Contains(page, `aria-describedby="sizes-help"`) {
		t.Error(`sizes input does not describe itself via sizes-help; the default ladder must be stated in words`)
	}
	if !strings.Contains(page, `placeholder="Default: 0.1 to 5000"`) {
		t.Error("sizes input does not state the default ladder it replaces")
	}
}

// TestUIMeasureSendsSizes pins that the live measurement actually forwards
// the reader's sizes to /api/corridor, and sends none when the field is
// empty — so an empty field keeps the default ladder rather than degrading
// to something else.
func TestUIMeasureSendsSizes(t *testing.T) {
	page := uiHTML(t)

	if !strings.Contains(page, "q.set('sizes', sizesRaw)") {
		t.Error("measure() never sets sizes on the request; the ladder stays fixed at the default")
	}
	if !strings.Contains(page, "if (sizesRaw !== '') q.set('sizes', sizesRaw);") {
		t.Error("empty input must send no sizes parameter, not an empty one")
	}
	// The fetch itself must go through the params object, so the sizes value
	// cannot drift out of the URL the server actually receives.
	if !strings.Contains(page, "const q = new URLSearchParams({ to })") ||
		!strings.Contains(page, "fetch(`/api/corridor?${q}&live=1`)") {
		t.Error("measure() does not build its query from URLSearchParams; sizes would not reach /api/corridor")
	}
}

// TestUIMirrorsParseSizesContract pins the client-side validation to the
// server's contract in parseSizes (server/api.go): positive decimal amounts,
// at most maxSizes of them. MAX_SIZES is asserted against the real constant
// so the two cannot drift.
func TestUIMirrorsParseSizesContract(t *testing.T) {
	page := uiHTML(t)

	if !strings.Contains(page, "const MAX_SIZES = 24;") {
		t.Error("MAX_SIZES is not declared or does not match the API limit of 24")
	}
	if MAX_SIZES_DECL := "const MAX_SIZES = 24;"; !strings.Contains(page, MAX_SIZES_DECL) || maxSizes != 24 {
		t.Errorf("UI limit and server limit disagree: MAX_SIZES=24 in the page, maxSizes=%d in api.go", maxSizes)
	}
	for _, want := range []string{
		"function sizesValidation",              // the one validator
		"is not a number",                       // non-numeric parts are named, not swallowed
		"Every size must be a positive amount.", // zero and negatives are refused
		"At most ${MAX_SIZES} sizes can be measured in one request.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; validation would not mirror parseSizes' contract", want)
		}
	}
	// Validation fires in measure(), before anything is sent, and names
	// itself through aria-invalid rather than colour alone.
	if !strings.Contains(page, "sizesInput.setAttribute('aria-invalid', 'true')") {
		t.Error("invalid sizes are not marked aria-invalid; the failure would rely on colour alone")
	}
	if idxValidation := strings.Index(page, "sizesValidation(sizesRaw)"); idxValidation >= 0 {
		if idxFetch := strings.Index(page, "fetch(`/api/corridor?${q}&live=1`)"); idxFetch >= 0 && idxValidation > idxFetch {
			t.Error("sizes are validated after the request is built; an invalid input could still be sent")
		}
	}
}

// TestUIStatesTheMeasuredLadder pins that the rendered result says which
// ladder was measured — the reader's sizes, or the default set when none
// were chosen — so a curve is never presented without its sizes.
func TestUIStatesTheMeasuredLadder(t *testing.T) {
	page := uiHTML(t)

	for _, want := range []string{
		"at the sizes you asked for",
		"at the default sizes (0.1 to 5000)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("render() does not state %q; a curve must always name the ladder it was measured at", want)
		}
	}
	// The default-ladder label matches what the engine actually runs when no
	// sizes are sent (dex.DefaultSizes, documented in docs/ladder-sizes.md).
	if !strings.Contains(page, "docs/ladder-sizes.md") {
		t.Error("the sizes contract is not attributed to docs/ladder-sizes.md")
	}
}

// TestUISizesHelpIsSelfContained mirrors TestUIIsServed's rule at the source
// level: the page must not reference external assets, so the help text
// explains the ladder in words rather than linking out.
func TestUISizesHelpIsSelfContained(t *testing.T) {
	page := uiHTML(t)

	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, "sizes-help") && strings.Contains(line, "<a href=\"http") {
			t.Errorf("the sizes help links to an external asset; the page must be self-contained: %s", strings.TrimSpace(line))
		}
	}
}

// TestUITrendUnaffectedBySizes pins the scope boundary: the trend endpoint
// has no sizes parameter, and the sizes control must not claim otherwise.
func TestUITrendUnaffectedBySizes(t *testing.T) {
	page := uiHTML(t)

	trendFetch := "/api/corridor/trend?to="
	idx := strings.Index(page, trendFetch)
	if idx < 0 {
		t.Fatal("loadTrend() no longer fetches /api/corridor/trend")
	}
	// Within the trend fetch there is no sizes interpolation.
	window := page[idx : idx+120]
	if strings.Contains(window, "sizes") {
		t.Error("the trend request references sizes; the trend endpoint has no sizes parameter")
	}
}
