package server

import (
	"strings"
	"testing"
)

// These tests pin issue #303 — keyboard navigation and visible focus
// throughout. The issue carries the shared UI template body, so the scope
// asserted here is the one docs/ui-states.md states: a skip link as the first
// tab stop, one focus indicator that clears 3:1 in both colour schemes, links
// that are not identified by colour alone, and a focus handoff so replacing
// the results never strands a keyboard reader on <body>.
//
// Source-text assertions, as everywhere else in this package: the page is one
// embedded file with no build step and no JS test runner in CI, so the
// stylesheet's and script's source is what is assertable.

// withoutComments strips CSS comments, so a selector match cannot be
// satisfied by prose that quotes it — this stylesheet explains itself at
// length and quotes its own selectors while doing so.
func withoutComments(css string) string {
	var b strings.Builder
	for {
		i := strings.Index(css, "/*")
		if i < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:i])
		j := strings.Index(css[i:], "*/")
		if j < 0 {
			return b.String()
		}
		css = css[i+j+2:]
	}
}

// rule extracts the declaration block of the first CSS rule whose selector
// contains sel, so an assertion is about that rule and not about a comment
// that happens to mention it.
func rule(t *testing.T, css, sel string) string {
	t.Helper()
	i := strings.Index(css, sel)
	if i < 0 {
		t.Fatalf("stylesheet has no rule matching %q", sel)
	}
	j := strings.Index(css[i:], "}")
	if j < 0 {
		t.Fatalf("rule %q has no closing brace", sel)
	}
	return css[i : i+j]
}

// TestUISkipLinkIsFirstAndReachable pins the entry point for a keyboard
// reader: a real link to the results region, ahead of every control in the
// source (and so in the tab order), styled so it is out of sight until it is
// focused rather than out of the tab order entirely.
func TestUISkipLinkIsFirstAndReachable(t *testing.T) {
	page := uiSource(t)
	css := withoutComments(styleBlock(t))

	const link = `<a class="skip-link" href="#out">`
	i := strings.Index(page, link)
	if i < 0 {
		t.Fatalf("page has no skip link (%q)", link)
	}
	if j := strings.Index(page, `<div class="controls">`); j < 0 || i > j {
		t.Error("skip link is not before the controls; a keyboard reader cannot pass them")
	}

	// Its target must take focus from the fragment navigation, and only from
	// it: tabindex="-1" makes #out focusable without adding a tab stop.
	if !strings.Contains(page, `<div id="out" tabindex="-1">`) {
		t.Error(`#out is missing tabindex="-1"; following the skip link would not move focus into the results`)
	}

	skip := rule(t, css, ".skip-link {")
	if !strings.Contains(skip, "position: absolute") {
		t.Error(".skip-link is not positioned; it would sit in the layout instead of over it")
	}
	if strings.Contains(skip, "display: none") || strings.Contains(skip, "visibility: hidden") {
		t.Error(".skip-link is hidden in a way that removes it from the tab order; it must only be moved out of view")
	}
	if !strings.Contains(rule(t, css, ".skip-link:focus"), "transform: none") {
		t.Error(".skip-link does not come into view when focused; a keyboard reader cannot see where they are")
	}
}

// TestUIFocusIndicatorClearsContrastInBothSchemes pins the visible half of
// the issue. Every focusable thing draws the same solid outline, from a token
// defined in both schemes, and no focus rule falls back to a bare
// `outline: none` — which is what the contract did before this issue, leaving
// a 28% halo as the only indication of focus.
func TestUIFocusIndicatorClearsContrastInBothSchemes(t *testing.T) {
	css := withoutComments(styleBlock(t))

	// Both schemes define the token: an indicator that clears 3:1 on light
	// and one that does not exist on dark is not an indicator in the dark
	// scheme.
	if n := strings.Count(css, "--focus-outline:"); n < 2 {
		t.Errorf("--focus-outline defined %d time(s), want at least 2 (light and dark)", n)
	}
	for _, want := range []string{"--focus-outline: #0F766E", "--focus-outline: #5EEAD4"} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}

	// Every :focus-visible rule in the stylesheet draws the outline. Scanned
	// exhaustively rather than selector by selector, so a rule added later
	// cannot quietly opt out.
	rest, found := css, 0
	for {
		i := strings.Index(rest, ":focus-visible")
		if i < 0 {
			break
		}
		rest = rest[i+len(":focus-visible"):]
		open := strings.Index(rest, "{")
		close := strings.Index(rest, "}")
		if close < 0 {
			break
		}
		if open < 0 || open > close {
			continue // prose, or a selector with no body of its own
		}
		found++
		block := strings.TrimSpace(rest[open+1 : close])
		if !strings.Contains(block, "var(--focus-outline)") {
			t.Errorf(":focus-visible rule does not draw var(--focus-outline): %q", block)
		}
		if strings.Contains(block, "outline: none") {
			t.Errorf(":focus-visible rule suppresses the outline with no replacement: %q", block)
		}
	}
	if found < 4 {
		t.Errorf("found %d :focus-visible rules with a body, want at least 4 (buttons, selects, links, results region)", found)
	}
}

// TestUILinksAreNotColourOnly pins SC 1.4.1/2.4.4 for the one inline link the
// page has: the reload link inside an error banner sits mid-sentence, so its
// underline — not its colour — is what says it goes somewhere.
func TestUILinksAreNotColourOnly(t *testing.T) {
	css := withoutComments(styleBlock(t))
	if !strings.Contains(rule(t, css, "a:not(.skip-link)"), "text-decoration: underline") {
		t.Error("inline links are not underlined; a link in running text would be identified by colour alone")
	}
}

// TestUIRenderKeepsFocusInsideTheResults pins the handoff: every result
// render goes through setOut(), which returns focus to #out when focus was
// inside it, and no render reaches for #out.innerHTML directly. Replacing
// focused content drops focus to <body>, which strands a keyboard reader at
// the top of the page.
func TestUIRenderKeepsFocusInsideTheResults(t *testing.T) {
	page := uiSource(t)

	if strings.Contains(page, "$('out').innerHTML") {
		t.Error("a render writes #out.innerHTML directly; it must go through setOut() so focus is not dropped")
	}

	start := strings.Index(page, "function setOut(html)")
	if start < 0 {
		t.Fatal("UI source has no setOut() definition; renders would drop focus to <body>")
	}
	end := strings.Index(page[start:], "\n}")
	if end < 0 {
		t.Fatal("setOut() definition has no closing brace")
	}
	fn := page[start : start+end]
	for _, want := range []string{
		"out.contains(document.activeElement)", // focus was inside the region
		"out.innerHTML = html",                 // the replacement itself
		"out.focus()",                          // and focus goes back to it
	} {
		if !strings.Contains(fn, want) {
			t.Errorf("setOut() missing %q", want)
		}
	}

	// Definition plus the six render sites (two empty states, three result
	// renders, one trend render).
	if n := strings.Count(page, "setOut("); n < 7 {
		t.Errorf("setOut( used %d times, want at least 7 (definition + six renders); a render is bypassing it", n)
	}
}
