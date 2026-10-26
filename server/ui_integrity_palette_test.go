package server

import (
	"strings"
	"testing"
)

func TestUIIntegrityStatesUseStructuralPalette(t *testing.T) {
	css := styleBlock(t)

	for _, role := range []string{"direct", "derivative", "nomarket"} {
		token := "--structural-" + role
		if !strings.Contains(css, token+":") {
			t.Errorf("missing structural palette token %q", token)
		}
	}

	for _, scheme := range []struct {
		name  string
		start string
	}{
		{name: "system dark", start: "@media (prefers-color-scheme: dark)"},
		{name: "explicit dark", start: ":root[data-theme=\"dark\"]"},
	} {
		start := strings.Index(css, scheme.start)
		if start < 0 {
			t.Fatalf("missing %s palette", scheme.name)
		}
		end := strings.Index(css[start:], "}")
		if end < 0 {
			t.Fatalf("unterminated %s palette", scheme.name)
		}
		block := css[start : start+end]
		for _, role := range []string{"direct", "derivative", "nomarket"} {
			if !strings.Contains(block, "--structural-"+role+":") {
				t.Errorf("%s palette is missing structural %s", scheme.name, role)
			}
		}
	}

	for _, role := range []string{"direct", "derivative", "nomarket"} {
		token := "--structural-" + role
		selector := ".b-" + role + " { color: var(" + token + "); background: var(--neutral-soft); }"
		if !strings.Contains(css, selector) {
			t.Errorf("integrity badge %q must use its structural token on a neutral background", role)
		}
	}

	for _, selector := range []string{
		".b-direct {", ".b-derivative {", ".b-nomarket {",
		".cs-info .cs-state.cs-direct {", ".cs-info .cs-state.cs-derivative {",
		".cs-info .cs-state.cs-nomarket {", ".integrity-dependency {",
		".integrity-cell .integrity-via {",
	} {
		start := strings.Index(css, selector)
		if start < 0 {
			t.Fatalf("missing integrity style %q", selector)
		}
		end := strings.Index(css[start:], "}")
		if end < 0 {
			t.Fatalf("unterminated integrity style %q", selector)
		}
		rule := css[start : start+end]
		for _, forbidden := range []string{"var(--ok)", "var(--warn)", "var(--bad)", "var(--success)", "var(--warning)", "var(--critical)"} {
			if strings.Contains(rule, forbidden) {
				t.Errorf("integrity style %q borrows financial severity token %q", selector, forbidden)
			}
		}
	}
	for _, selector := range []string{".integrity-dependency {", ".integrity-cell .integrity-via {"} {
		start := strings.Index(css, selector)
		end := strings.Index(css[start:], "}")
		if !strings.Contains(css[start:start+end], "var(--structural-derivative)") {
			t.Errorf("derivative detail %q must use the structural palette", selector)
		}
	}

	if !strings.Contains(css, ".m-state {") || !strings.Contains(css, ".m-unknown { color: var(--unknown); }") {
		t.Error("undetermined metrics must keep their neutral unknown styling")
	}
}
