package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestUITypographicScale guards issue #282: every font size in the page is
// drawn from the declared scale, and the ad-hoc sizes the issue listed no
// longer exist as declarations.
//
// The regex covers both forms a stray size can take — `font-size: 12px`-style
// longhand and the `font: 700 .78rem/1` shorthand. em and px are exempt by
// design: inline code scales with its parent in em, and the SVG axis is in
// viewBox user units that scale with the chart (both documented on the scale
// itself).
func TestUITypographicScale(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, token := range []string{
		"--fs-3xs:", "--fs-2xs:", "--fs-xs:", "--fs-sm:",
		"--fs-md:", "--fs-lg:", "--fs-xl:", "--fs-2xl:",
	} {
		if !strings.Contains(page, token) {
			t.Errorf("UI missing scale token %q; the typographic scale is undefined", token)
		}
	}

	// The tokens must actually be used, not merely declared.
	if n := strings.Count(page, "var(--fs-"); n < 30 {
		t.Errorf("only %d font-size references use the scale; sizes are declared ad hoc again", n)
	}

	// No font declaration may carry a literal rem size: that is precisely the
	// behaviour the issue calls out.
	if m := regexp.MustCompile(`font(?:-size)?:\s*[0-9.]+rem`).FindString(page); m != "" {
		t.Errorf("literal rem size %q bypasses the typographic scale", m)
	}
}

// TestUIThemeToggle guards issue #287: a persisted light/dark/system choice
// that is keyboard reachable (a native select), keeps following the OS in
// system mode (the media query still exists), and applies before first paint
// so a reload does not flash the wrong scheme.
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
		// (an explicit light choice overrides it), not removed.
		"prefers-color-scheme",
		// Explicit choices override the OS in both directions.
		`:root[data-theme="dark"]`,
		`:root[data-theme="light"]`,
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
// disclosed rather than dumped, and the two disclosures the issue names both
// exist — a headline drawer carrying reference provenance (source, mid,
// fetched-at, paths), and per-check evidence lines that now include their
// observation timestamp alongside the source.
//
// Fields the response may not carry (an older record has no fetched-at) are
// read conditionally, so the test asserts the field is read, not that it is
// always printed — printing it unconditionally is the fabrication this
// issue exists to prevent.
func TestUIEvidenceDrawer(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		// The headline drawer.
		`<details class="evidence">`,
		"Evidence &amp; provenance",
		"d.reference_fetched_at",
		"d.reference_as_of",
		"d.reference_secondary_mid",
		"Paths tested",
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
// the gap between them get a visual form, built only from figures the engine
// published, and the panel disappears whenever the engine refused to score —
// a benchmark that could not be trusted must not be drawn as though it had.
func TestUIBenchmarkPanel(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		"Benchmark versus achieved",
		// Rendered from the same priced set as the curve, under the same gate.
		"benchmarkPanel(d, priced)",
		"d.scored && priced.length",
		// The visual primitives.
		"bench-track",
		"bench-fill",
		// The three engine figures printed together: mid, rate, gap.
		"d.reference_mid",
		"r.quote.effective_rate",
		"formatPct(r.quote.loss_pct)",
		// The fill width is derived from the engine's own loss figure, and
		// only from it.
		"100 - loss",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing %q; benchmark and executable would not be visualised", want)
		}
	}
}
