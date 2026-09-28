package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestUITypographicScale guards issue #282: font sizes must come from a
// declared scale, not be re-declared ad hoc at every rule.
//
// The original implementation of this PR declared an --fs-* token scale and
// rewritten every rule against it. Main has since moved to its own
// convention: a --text-* scale for common text sizes plus deliberate
// literal rem values for the small mono/badge sizes, never px. This test
// pins the merged convention: the --text-* scale exists and is used, and no
// rule declares a px font size (the original issue's core complaint).
func TestUITypographicScale(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, token := range []string{
		"--text-xs:", "--text-sm:", "--text-md:", "--text-lg:", "--text-xl:",
	} {
		if !strings.Contains(page, token) {
			t.Errorf("UI missing scale token %q; the typographic scale is undefined", token)
		}
	}

	// The tokens must actually be used, not merely declared.
	if n := strings.Count(page, "var(--text-"); n < 3 {
		t.Errorf("only %d rules use the --text-* scale; sizes are declared ad hoc again", n)
	}

	// No rule may carry a px font size: that is the behaviour the issue
	// calls out (px sizes do not respect the reader's font-size setting).
	// The one exemption is the SVG axis: inside the chart, px units are
	// viewBox user units that scale with the graphic, not CSS px — the same
	// exemption the original scale documented.
	svgAxis := regexp.MustCompile(`\.axis\s*\{[^}]*\}`).ReplaceAllString(page, "")
	if m := regexp.MustCompile(`font(?:-size)?:\s*[0-9.]+px`).FindString(svgAxis); m != "" {
		t.Errorf("literal px size %q bypasses the reader's font-size setting", m)
	}
}

// TestUIThemeToggle guards issue #287: a persisted light/dark/system choice
// that is keyboard reachable (a native select), keeps following the OS in
// system mode (the media query still exists, narrowed by the explicit-light
// exclusion), and applies before first paint so a reload does not flash the
// wrong scheme.
func TestUIThemeToggle(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		// The three states the selector offers.
		`<select id="theme"`,
		`<option value="system">`,
		`<option value="light">`,
		`<option value="dark">`,
		// System mode still follows the OS: the media query was narrowed
		// (an explicit light choice overrides it via the :root:not(...)
		// selector), not removed.
		"prefers-color-scheme",
		`:root:not([data-theme="light"])`,
		// Explicit dark overrides the OS in the other direction.
		`:root[data-theme="dark"]`,
		// Persistence, and the pre-paint application that avoids a flash.
		"localStorage.getItem('wayfare-theme')",
		"localStorage.setItem('wayfare-theme'",
		"setAttribute('data-theme'",
		"removeAttribute('data-theme')",
		// Native form controls follow the chosen scheme.
		"color-scheme",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; the theme toggle would not work", want)
		}
	}
}

// TestUIEvidenceDrawer guards issue #294: evidence is progressively
// disclosed rather than dumped, and provenance is stated next to the
// headline.
//
// The original implementation built a headline <details> drawer from fields
// the server did not yet publish (reference_fetched_at, reference_as_of).
// Main's headline publishes provenance inline instead — pair, mid, provider
// source and measurement time — which satisfies the issue without inventing
// fields, and adds dual-provider provenance (reference_secondary_*). This
// test pins main's headline provenance plus the per-check disclosure, and
// requires every evidence line to carry source AND observation time.
func TestUIEvidenceDrawer(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		// Headline provenance, stated next to the finding: pair, mid,
		// provider and time. Main also carries the secondary provider.
		"d.reference_pair",
		"d.reference_mid",
		"d.reference_source",
		"d.measured_at",
		"d.reference_secondary_mid",
		// Per-check evidence behind its own disclosure.
		`<details class="f-ev">`,
		// Each check evidence line carries source AND timestamp. This exact
		// arrow form belongs to the checks renderer; metrics uses entities
		// and already had timestamps.
		"→ ${esc(e.observed)} · ${esc(e.observed_at)}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; evidence would not be disclosed progressively", want)
		}
	}
}

// TestUIBenchmarkPanel guards issue #295: reference mid, achieved rate and
// the loss between them get a visual form built only from figures the
// engine published, and the visual disappears whenever the engine refused
// to score — a benchmark that could not be trusted must not be drawn as
// though it had.
//
// The original implementation drew bar tracks per size. Main renders the
// same figures as an SVG curve against the size axis, with the engine's
// refusal threshold drawn as its own line, so the "why this size fails"
// boundary is visible rather than only the losses. The gate is unchanged:
// no scored data, no chart.
func TestUIBenchmarkPanel(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		// Rendered from the same priced set as the table, under the same gate.
		"Loss against mid, by size",
		"curve(priced)",
		"d.scored && priced.length",
		// The visual primitives: the SVG loss curve and the refusal
		// threshold line, painted from chart role tokens.
		"chart-threshold",
		"chart-point",
		// The engine's own loss figure drives the geometry; nothing else.
		"parseFloat(r.quote.loss_pct)",
		// Reference provenance printed with the headline (same figures the
		// original bench panel printed).
		"d.reference_mid",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; benchmark and executable would not be visualised", want)
		}
	}
}

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
