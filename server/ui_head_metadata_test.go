package server

import (
	"strings"
	"testing"
)

// TestUIHeadMetadata guards issue #635: the page must carry an inline SVG
// favicon (no external icon request, nothing third-party to block or track)
// and static Open Graph tags so shared links unfurl with a name and a plain
// description instead of a bare URL. The rendered tab title is set per
// corridor from client-side code — these assertions pin that the fallback
// <title> and the OG tags exist and the title is updated where a measurement
// and a trend document are shown.
func TestUIHeadMetadata(t *testing.T) {
	page := uiSource(t)

	headEnd := strings.Index(page, "</head>")
	if headEnd < 0 {
		t.Fatal("page has no </head>; metadata placement cannot be verified")
	}
	head := page[:headEnd]

	if !strings.Contains(head, `rel="icon"`) {
		t.Error("head has no favicon link; the tab falls back to a default icon")
	}
	if !strings.Contains(head, `href="data:image/svg+xml,`) {
		t.Error("favicon is not an inline data: URI; the browser would make an extra request for the icon")
	}

	for _, tag := range []string{
		`<meta property="og:type"`,
		`<meta property="og:site_name"`,
		`<meta property="og:title"`,
		`<meta property="og:description"`,
		`<meta property="og:url"`,
	} {
		if !strings.Contains(head, tag) {
			t.Errorf("head missing the %s Open Graph tag; shared links will not unfurl with a name and description", tag)
		}
	}

	// The per-corridor title: set from render() with the corridor and the
	// verdict the reader just received, and from the trend view with the
	// corridor and the window's latest integrity.
	if !strings.Contains(page, "const setDocTitle") {
		t.Error("UI missing setDocTitle; the tab title never tracks the corridor shown")
	}
	if n := strings.Count(page, "setDocTitle("); n < 2 {
		// definition + render() + renderTrend()
		t.Errorf("setDocTitle call sites = %d, want at least 2 (definition plus render and/or renderTrend)", n)
	}
}
