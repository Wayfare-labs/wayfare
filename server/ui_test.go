package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// uiSource returns the source text of the whole UI: every embedded file,
// concatenated in the order the embed declares them.
//
// The UI's source-text tests assert statements about the UI as a whole —
// which finding states it renders, what its trend view promises — not about
// one file, so they read all of it. Before the split that meant grepping a
// single index.html; the property under test has not changed, only the file
// layout, and this helper is the one place that difference lives.
func uiSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{"index.html", "app.css", "app.js"} {
		raw, err := uiFS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading embedded %s: %v", name, err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestUISplitFilesAreEmbedded guards the split itself: all three files must
// be present and non-empty in the binary. An empty asset would serve a blank
// or unstyled page while every request still returned 200.
func TestUISplitFilesAreEmbedded(t *testing.T) {
	for _, name := range []string{"index.html", "app.css", "app.js"} {
		raw, err := uiFS.ReadFile(name)
		if err != nil {
			t.Errorf("%s not embedded: %v", name, err)
			continue
		}
		if len(raw) == 0 {
			t.Errorf("%s is embedded but empty", name)
		}
	}
}

// TestUIPageReferencesItsAssets pins the wiring between the three files: the
// page must load the stylesheet and the script, and must carry no inline
// style or script. Inline content would silently reintroduce the single-file
// problem the split exists to remove — and with it the need for CSP hashes.
func TestUIPageReferencesItsAssets(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	for _, want := range []string{
		`<link rel="stylesheet" href="/app.css">`,
		`<script src="/app.js"></script>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html does not reference %s", want)
		}
	}
	if strings.Contains(page, "<style") {
		t.Error("index.html has an inline <style> block; styles belong in app.css")
	}
	// A <script src=...> tag is external and fine; a bare <script> open
	// tag is inline script, which strict CSP without hashes would block.
	if strings.Contains(page, "<script>") {
		t.Error("index.html has an inline <script> block; behaviour belongs in app.js")
	}
}

// TestUISplitAssetsAreServed checks each file reaches the browser at its own
// URL with the content type that makes it take effect: a stylesheet served
// as text/plain, or a script without a JavaScript MIME type, is inert — the
// page would render unstyled or dead while every request returned 200.
func TestUISplitAssetsAreServed(t *testing.T) {
	h := uiHandler()
	cases := map[string]struct {
		contentType string
		bodyHas     string
	}{
		"/":        {"text/html", "corridor integrity monitor"},
		"/app.css": {"text/css", "--bad:"},
		"/app.js":  {"text/javascript", "/api/corridor/trend"},
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, want.contentType) {
			t.Errorf("GET %s: Content-Type = %q, want prefix %q", path, ct, want.contentType)
		}
		body, err := io.ReadAll(rec.Result().Body)
		if err != nil {
			t.Fatalf("GET %s: reading body: %v", path, err)
		}
		if !strings.Contains(string(body), want.bodyHas) {
			t.Errorf("GET %s: body does not contain %q", path, want.bodyHas)
		}
	}

	// Anything other than the three paths still 404s, as the single-file
	// handler did: the UI has no directory to list and no assets beyond
	// the ones embedded.
	for _, path := range []string{"/nope", "/index.html", "/app.css.map"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, rec.Code)
		}
	}
}
