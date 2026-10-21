package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// glossaryHeadingAnchor reduces a markdown heading to the anchor GitHub gives
// it: lowercased, spaces to dashes. It is deliberately simple — the headings
// this page links to are plain words, and the point is to catch a link whose
// section was renamed or never written, not to reimplement GitHub's slugger.
func slugHeading(h string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(h)), " ", "-")
}

// TestUIGlossaryLinksResolveToHeadings pins the other half of the glossary
// links (#641): every anchor the page links to must name a heading that
// actually exists in docs/glossary.md. A link to a missing anchor is worse
// than no link — it promises an explanation and delivers the top of a file.
func TestUIEveryGlossaryLinkResolves(t *testing.T) {
	page := uiHTML(t)

	raw, err := os.ReadFile("../docs/glossary.md")
	if err != nil {
		t.Fatalf("reading glossary: %v", err)
	}
	glossary := string(raw)

	headings := map[string]bool{}
	for _, line := range strings.Split(glossary, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		headings[slugHeading(title)] = true
	}

	anchors := glossaryAnchors(page)
	if len(anchors) == 0 {
		t.Fatal("the page links to no glossary entry; the help links have been removed")
	}
	for _, a := range anchors {
		if !headings[a] {
			t.Errorf("help link points at #%s, but docs/glossary.md has no heading that anchors there", a)
		}
	}
}

// TestUIGlossaryLinksAreSelfContainedExceptions pins that the one outbound
// link the page carries is the documented glossary URL — and that it is the
// only kind of external reference, so the self-contained rule (TestUIIsServed)
// is not silently broken by a second one.
func TestUIGlossaryLinkTarget(t *testing.T) {
	page := uiHTML(t)
	if !strings.Contains(page, "https://github.com/Wayfare-labs/wayfare/blob/main/docs/glossary.md") {
		t.Error("the page does not link to the repository glossary")
	}
	if strings.Count(page, "href=\"http") != 0 {
		t.Error("a help link uses a bare href to an external URL; glossary links must go through GLOSSARY_BASE")
	}
}

// TestUIStreamsAndCancels pins the streaming UI (#639/#640): the page opens
// the stream endpoint, parses SSE, can cancel an in-flight measurement with an
// AbortController, and keeps the non-streaming fallback.
func TestUIStreamsAndCancels(t *testing.T) {
	page := uiHTML(t)
	for _, want := range []string{
		"/api/corridor/stream",    // the streaming endpoint
		"readCorridorStream",      // the SSE reader
		"parseSSE",                // the frame parser
		"fetchCorridorJSON",       // the non-streaming fallback
		"activeMeasurement",       // the in-flight tracking
		"cancelActiveMeasurement", // the cancel entry point
		"AbortController",         // how the fetch is cancelled
		"id=\"cancel-measure\"",   // the control the reader presses
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q; streaming or cancellation would be incomplete", want)
		}
	}
}

// TestUIMeasurabilityBlock pins the trend's uptime rendering (#638): the block
// exists, renders only when the API sent one, always shows the denominator,
// and warns on a low sample rather than presenting a precise-looking rate.
func TestUIMeasurabilityBlock(t *testing.T) {
	page := uiHTML(t)
	for _, want := range []string{
		"measurabilityBlock", // the renderer
		"d.measurability",    // gated on the API's block
		"low_sample",         // the caveat
		"measurable_runs",    // the numerator
		"total_runs",         // the denominator the rate stands on
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the trend view lacks %q; measurability would render incompletely", want)
		}
	}
}

// glossaryAnchors returns every anchor the page passes to helpLink/helpMark.
func glossaryAnchors(page string) []string {
	re := regexp.MustCompile(`help(?:Link|Mark)\('([a-z0-9-]+)'`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(page, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
