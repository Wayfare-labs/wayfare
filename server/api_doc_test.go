package server

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// TestAPIDocRoutesParity asserts that all eleven routes registered by the server
// have an exact matching "## `GET <path>`" section in docs/api.md, and vice versa.
func TestAPIDocRoutesParity(t *testing.T) {
	expectedRoutes := []string{
		"/api/assets",
		"/api/chain-heads",
		"/api/corridor",
		"/api/corridor/image",
		"/api/corridor/trend",
		"/api/market-structure",
		"/api/thresholds",
		"/healthz",
		"/metrics",
		"/og/corridor.svg",
		"/readyz",
	}
	sort.Strings(expectedRoutes)

	docPath := filepath.Join("..", "docs", "api.md")
	f, err := os.Open(docPath)
	if err != nil {
		t.Fatalf("opening %s: %v", docPath, err)
	}
	defer f.Close()

	headingRe := regexp.MustCompile("^## `GET ([^`]+)`")
	var docRoutes []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := headingRe.FindStringSubmatch(line)
		if len(matches) > 1 {
			docRoutes = append(docRoutes, matches[1])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", docPath, err)
	}
	sort.Strings(docRoutes)

	if len(docRoutes) != len(expectedRoutes) {
		t.Errorf("documented route count mismatch: got %d, want %d\ngot: %v\nwant: %v",
			len(docRoutes), len(expectedRoutes), docRoutes, expectedRoutes)
	}

	docMap := make(map[string]bool)
	for _, r := range docRoutes {
		docMap[r] = true
	}
	for _, expected := range expectedRoutes {
		if !docMap[expected] {
			t.Errorf("route %s is registered by Server.Handler() but missing from docs/api.md", expected)
		}
	}

	expMap := make(map[string]bool)
	for _, r := range expectedRoutes {
		expMap[r] = true
	}
	for _, doc := range docRoutes {
		if !expMap[doc] {
			t.Errorf("route %s is documented in docs/api.md but not in registered route list", doc)
		}
	}

	// Verify that Server.Handler() serves all eleven routes
	srv := testServer(t, liveNGNCPaths, "1580.00")

	for _, path := range expectedRoutes {
		resp, err := httpGet(srv.URL + path)
		if err != nil {
			t.Errorf("probing registered route %s: %v", path, err)
			continue
		}
		_ = resp.Body.Close()
	}
}
