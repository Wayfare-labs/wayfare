package server

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestUISecurityHeaders verifies all required security headers on UI responses (issue #611):
// Content-Security-Policy (with self and hashes for inline scripts, and frame-ancestors 'none'),
// X-Content-Type-Options: nosniff, Referrer-Policy: no-referrer, and X-Frame-Options: DENY.
func TestUISecurityHeaders(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// 1. X-Content-Type-Options: nosniff
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}

	// 2. Referrer-Policy: no-referrer
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want %q", got, "no-referrer")
	}

	// 3. X-Frame-Options: DENY
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
	}

	// 4. Content-Security-Policy
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}

	for _, want := range []string{
		"default-src 'self'",
		"frame-ancestors 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"connect-src 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP = %q missing directive %q", csp, want)
		}
	}

	// Verify that CSP includes sha256 hashes for all inline scripts in index.html.
	body, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	hashes := computeScriptHashes(body)
	if len(hashes) == 0 {
		t.Fatal("expected at least one inline script hash from index.html")
	}
	for _, h := range hashes {
		if !strings.Contains(csp, h) {
			t.Errorf("CSP missing script hash %s", h)
		}
	}
}

// TestJSONResponsesCarryNosniff checks that every JSON endpoint and error response
// sets X-Content-Type-Options: nosniff (issue #611).
func TestJSONResponsesCarryNosniff(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	endpoints := []string{
		"/healthz",
		"/api/assets",
		"/api/market-structure",
		"/api/chain-heads",
		"/api/corridor?to=NGNC",
		"/api/corridor/trend?to=NGNC",
		// Error cases (deterministic 400s)
		"/api/corridor?to=UNKNOWN",
		"/api/corridor?to=NGNC&sizes=bad",
		"/api/corridor?unknown=1",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(srv.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			defer resp.Body.Close()

			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("%s: X-Content-Type-Options = %q, want %q", ep, got, "nosniff")
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("%s: Content-Type = %q, want application/json", ep, ct)
			}
		})
	}
}

// TestUINotFoundHasNosniff asserts that non-root paths on uiHandler also set nosniff.
func TestUINotFoundHasNosniff(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	resp, err := http.Get(srv.URL + "/not-found-page")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

// TestComputeScriptHashes tests the script hashing logic with synthetic HTML.
func TestComputeScriptHashes(t *testing.T) {
	snippet := "console.log('hello world');"
	sum := sha256.Sum256([]byte(snippet))
	wantHash := fmt.Sprintf("'sha256-%s'", base64.StdEncoding.EncodeToString(sum[:]))

	html := fmt.Sprintf(`<!doctype html><html><head><script>%s</script></head><body><script>   </script></body></html>`, snippet)
	hashes := computeScriptHashes([]byte(html))
	if len(hashes) != 1 {
		t.Fatalf("got %d hashes, want 1 (empty script should be skipped)", len(hashes))
	}
	if hashes[0] != wantHash {
		t.Errorf("hash = %q, want %q", hashes[0], wantHash)
	}
}

// TestNoCloudflareScriptInIndexHTML coordinates with #477 and verifies that
// the accidental Cloudflare challenge script is removed from index.html.
func TestNoCloudflareScriptInIndexHTML(t *testing.T) {
	body, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	content := string(body)
	for _, forbidden := range []string{
		"challenge-platform",
		"/cdn-cgi/",
		"__CF$cv$params",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("index.html still contains Cloudflare artifact %q (issue #477)", forbidden)
		}
	}
}
