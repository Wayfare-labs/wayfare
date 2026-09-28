package server

import (
	"strings"
	"testing"
)

// These tests pin the interactive state contract (issue #285) as
// source-text assertions on the single embedded UI file — the same approach
// as TestUIPreservesContentOnFetchError: no build step and no JS test runner
// in CI, so the stylesheet's source is what is assertable. They cannot prove
// how a panel renders; docs/qa/README.md is the harness for that.

func uiSource(t *testing.T) string {
	t.Helper()
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// styleBlock extracts the <style>…</style> block so assertions are on the
// stylesheet and cannot be satisfied by stray words elsewhere in the page.
func styleBlock(t *testing.T) string {
	t.Helper()
	page := uiSource(t)
	open := strings.Index(page, "<style>")
	close := strings.Index(page, "</style>")
	if open < 0 || close < 0 || close < open {
		t.Fatal("index.html must contain a <style> block")
	}
	return page[open:close]
}

func TestUIFocusStatesExistForButtonsAndSelects(t *testing.T) {
	css := styleBlock(t)
	for _, want := range []string{
		"button:focus-visible", "select:focus-visible",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q; keyboard focus would fall back to the browser default", want)
		}
	}
	// The focus ring must come from the token so both colour schemes render
	// the same contract.
	if !strings.Contains(css, "var(--focus-ring)") {
		t.Error("focus styling must use the --focus-ring token, not a hard-coded colour")
	}
}

// TestUIActiveStateExists pins the state the issue found missing entirely.
func TestUIActiveStateExists(t *testing.T) {
	css := styleBlock(t)
	if !strings.Contains(css, "button:active") {
		t.Error("stylesheet missing button:active; a pressed control gives no feedback")
	}
	// The depression must be a shadow, not a transform: moving the element
	// under the finger shifts the press target mid-press.
	if !strings.Contains(css, "var(--press-shadow)") {
		t.Error("button:active must use the --press-shadow token")
	}
	// Find the active rule and forbid transforms in it.
	idx := strings.Index(css, "button:active")
	rule := css[idx : idx+strings.Index(css[idx:], "}")]
	if strings.Contains(rule, "transform") || strings.Contains(rule, "translate") {
		t.Errorf("button:active moves the element (rule %q); a press must not shift its own target", rule)
	}
}

// TestUIHoverAppliesToBothControls and is guarded for touch: the hover rule
// must cover select as well as button, and must sit inside a (hover: hover)
// media guard so a touch device never gets a sticky hover.
func TestUIHoverAppliesToBothControls(t *testing.T) {
	css := styleBlock(t)
	if !strings.Contains(css, "button:hover:not(:disabled)") ||
		!strings.Contains(css, "select:hover:not(:disabled)") {
		t.Error("hover must be defined for both button and select on enabled controls")
	}
	if !strings.Contains(css, "@media (hover: hover)") {
		t.Error("hover styling must be guarded by @media (hover: hover) for touch devices")
	}
}

// TestUIDisabledStateCancelsHoverFeedback: disabled is the end of the
// interaction story. A control that cannot act must not look about to.
func TestUIDisabledStateCancelsHoverFeedback(t *testing.T) {
	css := styleBlock(t)
	if !strings.Contains(css, "button:disabled, select:disabled") {
		t.Error("the disabled rule must cover both button and select")
	}
	// The cancel rule must exist and reset all three feedback properties.
	if !strings.Contains(css, "button:disabled:hover") ||
		!strings.Contains(css, "select:disabled:hover") {
		t.Error("a pointer resting on a disabled control must see rest styling, not hover styling")
	}
	idx := strings.Index(css, "button:disabled:hover")
	rule := css[idx : idx+strings.Index(css[idx:], "}")]
	for _, want := range []string{"border-color", "background", "box-shadow"} {
		if !strings.Contains(rule, want) {
			t.Errorf("disabled hover cancel resets %q (rule %q); the hover look would leak through", want, rule)
		}
	}
}

// TestUIStatesNeverUseVerdictColour is the issue's hardest constraint: no
// state rule may borrow a colour that carries financial meaning, so hover or
// focus can never read as GOOD, UNUSABLE, or UNDETERMINED. The interactive
// tokens are affordance-only.
func TestUIStatesNeverUseVerdictColour(t *testing.T) {
	css := styleBlock(t)
	// The verdict/integrity palette.
	verdictTokens := []string{
		"var(--bad)", "var(--warn)", "var(--ok)",
		"var(--critical)", "var(--warning)", "var(--success)",
		"var(--bad-soft)", "var(--ok-soft)", "var(--unknown)",
		"var(--unknown-soft)", "var(--brand-amber)", "var(--brand-coral)",
	}
	// The interactive-state rules, as a single region between the contract
	// marker and the reduced-motion block that follows it.
	start := strings.Index(css, "Interactive state contract")
	end := strings.Index(css, "prefers-reduced-motion")
	if start < 0 || end < 0 || end < start {
		t.Fatal("the interactive state contract block is missing from the stylesheet")
	}
	states := css[start:end]
	for _, tok := range verdictTokens {
		if strings.Contains(states, tok) {
			t.Errorf("an interactive state rule uses %q; state styling is affordance-only and must not borrow verdict colour", tok)
		}
	}
}

// TestUIStateTokensDefinedInBothSchemes: the states are one contract
// rendered twice, so both colour schemes must carry the interactive tokens.
func TestUIStateTokensDefinedInBothSchemes(t *testing.T) {
	page := uiSource(t)
	darkStart := strings.Index(page, "prefers-color-scheme: dark")
	if darkStart < 0 {
		t.Fatal("no dark scheme block found")
	}
	light := page[:darkStart]
	dark := page[darkStart:]

	for _, tok := range []string{"--focus-ring:", "--press-shadow:"} {
		if !strings.Contains(light, tok) {
			t.Errorf("light scheme missing %s", tok)
		}
		if !strings.Contains(dark, tok) {
			t.Errorf("dark scheme missing %s; the state contract must render in both schemes", tok)
		}
	}
}

// TestUIReducedMotionHonoured is the I5 motion half: the interface must
// collapse its animation for readers whose OS asks for reduced motion, while
// keeping the state changes themselves (the feedback is the point).
func TestUIReducedMotionHonoured(t *testing.T) {
	css := styleBlock(t)
	if !strings.Contains(css, "prefers-reduced-motion: reduce") {
		t.Error("stylesheet must honour prefers-reduced-motion: reduce")
	}
	if !strings.Contains(css, "animation-iteration-count: 1") ||
		!strings.Contains(css, "transition-duration") {
		t.Error("the reduced-motion block must still both animations and transitions")
	}
	// The loading dots must freeze visible, not disappear: find the
	// reduced-motion override and check it sets a readable opacity.
	idx := strings.Index(css, "prefers-reduced-motion: reduce")
	block := css[idx:]
	if !strings.Contains(block, ".loading-dots span") {
		t.Error("the loading dots must be given a static form under reduced motion")
	}
	if dot := strings.Index(block, ".loading-dots span"); dot >= 0 {
		rule := block[dot : dot+strings.Index(block[dot:], "}")]
		if strings.Contains(rule, "opacity: 0") {
			t.Error("reduced motion freezes the dots at opacity 0; the cold-start state must stay visible")
		}
	}
}

// TestUISelectsGetDisabledParity: the fetch lifecycle disables buttons; the
// trend size select and corridor select must be covered by the same visual
// disabled contract so a disabled control is recognisable as one thing.
func TestUISelectsGetDisabledParity(t *testing.T) {
	css := styleBlock(t)
	if !strings.Contains(css, "select:disabled") {
		t.Error("select:disabled must exist; a disabled select must read as disabled like a button")
	}
}
