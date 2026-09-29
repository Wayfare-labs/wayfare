package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestUISpacingAndRadiusScale guards issue #283: the spacing and radius scales
// are declared once, and every padding, margin, gap and corner in the single
// embedded stylesheet is drawn from them rather than the dozen ad-hoc rem and
// px values the issue found scattered across the rule bodies.
//
// Like the other UI contracts this is a source-text assertion: the page is one
// embedded file with no build step and no JS test runner in CI. It cannot prove
// how a panel renders — docs/qa/README.md is the harness for that — only that
// the scale is declared and actually used.
func TestUISpacingAndRadiusScale(t *testing.T) {
	css := styleBlock(t)

	for _, token := range []string{
		"--space-1:", "--space-2:", "--space-3:", "--space-4:",
		"--space-5:", "--space-6:", "--space-7:", "--space-8:",
		"--space-10:", "--space-12:", "--space-20:",
		"--radius-xs:", "--radius-sm:", "--radius-md:", "--radius-lg:",
	} {
		if !strings.Contains(css, token) {
			t.Errorf("stylesheet missing scale token %q; the spacing/radius scale is undefined", token)
		}
	}

	// The tokens must actually be used, not merely declared.
	if n := strings.Count(css, "var(--space-"); n < 20 {
		t.Errorf("only %d spacing references use the scale; spacing is declared ad hoc again", n)
	}
	if n := strings.Count(css, "var(--radius-"); n < 5 {
		t.Errorf("only %d corner references use the scale; corners are declared ad hoc again", n)
	}

	// No padding, margin or gap may carry a literal rem length: that is the
	// behaviour the issue calls out. Zero and auto are layout keywords, not
	// steps, so they are exempt by construction (they carry no rem unit).
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?:padding|margin)(?:-[a-z]+)?:\s*[^;{}]*[0-9.]+rem`),
		regexp.MustCompile(`gap:\s*[^;{}]*[0-9.]+rem`),
	} {
		if m := re.FindString(css); m != "" {
			t.Errorf("literal rem spacing %q bypasses the spacing scale", strings.TrimSpace(m))
		}
	}

	// Every corner is a step, except a deliberate circle (50%).
	for _, m := range regexp.MustCompile(`border-radius:\s*([^;{}]+)`).FindAllStringSubmatch(css, -1) {
		v := strings.TrimSpace(m[1])
		if v == "50%" || strings.Contains(v, "var(--radius-") {
			continue
		}
		t.Errorf("border-radius %q is not drawn from the radius scale", v)
	}
}
