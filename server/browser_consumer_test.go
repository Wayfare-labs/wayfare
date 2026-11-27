package server

// The browser consumer's contract (issue #928).
//
// examples/browser-consumer is a page served from another origin, so it
// depends on three things a Go consumer does not: a wildcard CORS policy, the
// ETag being exposed to cross-origin script, and a 304 that a browser's fetch
// can read. None of them is exercised by the Go example, so a regression in
// withCORS would only be noticed by a third party.
//
// This test serves the example from one httptest server and the API from
// another — genuinely different origins — and reads the page and the API the
// way the browser would. It pins the HTTP contract the page relies on, not the
// page's implementation: the page can be rewritten, and this test still has to
// pass.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// browserOrigin is the origin the page is served from. It is deliberately not
// the API's, which is the whole point of the test.
const browserOrigin = "https://consumer.example"

// pageRequest fetches a URL with an optional method and headers.
func pageRequest(t *testing.T, method, url string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, raw
}

// TestBrowserConsumerCORSAndConditionalRequests is the acceptance criterion:
// a test that pins the cross-origin contract examples/browser-consumer relies
// on — the preflight answer, the exposed ETag, and a 304 on revalidation.
func TestBrowserConsumerCORSAndConditionalRequests(t *testing.T) {
	page := httptest.NewServer(http.FileServer(http.Dir("../examples/browser-consumer")))
	t.Cleanup(page.Close)

	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	api := historyFirstServer(t, st)

	// 1. The example is served, and it is the page this test is about: one
	// that fetches the corridor, keeps the ETag and sends it back.
	status, header, raw := pageRequest(t, http.MethodGet, page.URL+"/index.html", nil)
	if status != http.StatusOK {
		t.Fatalf("GET the example = %d, want 200", status)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	for _, needle := range []string{"/api/corridor", "If-None-Match", "ETag", "Access-Control-Allow-Origin"} {
		if !strings.Contains(string(raw), needle) {
			t.Errorf("the example no longer mentions %q; it exists to exercise that part of the contract", needle)
		}
	}

	// 2. Preflight. A browser sends this before a GET that carries
	// If-None-Match, because that is not a CORS-safelisted request header.
	status, preflight, raw := pageRequest(t, http.MethodOptions, api.URL+"/api/corridor", map[string]string{
		"Origin":                         browserOrigin,
		"Access-Control-Request-Method":  http.MethodGet,
		"Access-Control-Request-Headers": "if-none-match",
	})
	if status != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204; body %s", status, raw)
	}
	if got := preflight.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin on preflight = %q, want *: a browser page on another origin cannot send If-None-Match otherwise", got)
	}
	if got := preflight.Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to include GET", got)
	}
	if got := strings.ToLower(preflight.Get("Access-Control-Allow-Headers")); !strings.Contains(got, "if-none-match") {
		t.Errorf("Access-Control-Allow-Headers = %q, want the requested If-None-Match reflected", got)
	}

	// 3. The first cross-origin read. The ETag has to be exposed, or script
	// cannot read the tag it is supposed to send back.
	status, first, raw := pageRequest(t, http.MethodGet, api.URL+"/api/corridor?to=NGNC&sizes=100", map[string]string{
		"Origin": browserOrigin,
	})
	if status != http.StatusOK {
		t.Fatalf("cross-origin read = %d, want 200; body %s", status, raw)
	}
	if got := first.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := first.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "ETag") {
		t.Errorf("Access-Control-Expose-Headers = %q, want it to name ETag: without it the browser hides the tag from the page", got)
	}
	etag := first.Get("ETag")
	if etag == "" {
		t.Fatal("the first response carries no ETag for the page to revalidate with")
	}

	// 4. The conditional read the page makes on its second request.
	status, second, raw := pageRequest(t, http.MethodGet, api.URL+"/api/corridor?to=NGNC&sizes=100", map[string]string{
		"Origin":        browserOrigin,
		"If-None-Match": etag,
	})
	if status != http.StatusNotModified {
		t.Fatalf("conditional read = %d, want 304; body %s", status, raw)
	}
	if len(raw) != 0 {
		t.Errorf("the 304 carried %d bytes; a browser keeps its copy and expects none", len(raw))
	}
	if got := second.Get("ETag"); got != etag {
		t.Errorf("ETag on the 304 = %q, want %q", got, etag)
	}
	if got := second.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin on the 304 = %q, want *: without it the browser rejects the revalidation it asked for", got)
	}
}
