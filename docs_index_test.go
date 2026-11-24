package wayfare

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDocsIndexCoversEveryDoc enforces that docs/README.md lists every
// Markdown file under docs/ (issue #648).
//
// The purpose is structural: when a new doc is committed and the author
// forgets to add it to the index, CI tells them immediately rather than
// letting it accumulate as an undiscoverable file. The test is intentionally
// narrow — it only checks that every path is mentioned somewhere in the index,
// not that the description is accurate.
//
// docs/README.md is not required to list itself; it is the index, not an
// entry in the index.
//
// Non-Markdown files (JSON schemas, etc.) are out of scope for this test;
// they appear in the index as a convenience but are not enforced here.
func TestDocsIndexCoversEveryDoc(t *testing.T) {
	indexPath := "docs/README.md"
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", indexPath, err)
	}
	indexText := string(raw)

	var missing []string
	err = filepath.Walk("docs", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// The index is not required to list itself.
		if path == indexPath {
			return nil
		}
		// Convert to the relative form used in the index (strip "docs/").
		rel := strings.TrimPrefix(path, "docs/")
		if !strings.Contains(indexText, rel) {
			missing = append(missing, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking docs/: %v", err)
	}

	sort.Strings(missing)
	for _, path := range missing {
		t.Errorf("orphan doc not listed in %s: %s", indexPath, path)
	}
	if len(missing) > 0 {
		t.Logf("add each orphan doc to %s with a one-line description", indexPath)
	}

	// Also assert the index has at least one entry per audience group, to
	// guard against an accidentally empty index.
	for _, heading := range []string{"## Reader", "## Consumer", "## Contributor", "## Operator", "## Researcher"} {
		if !strings.Contains(indexText, heading) {
			t.Errorf("%s is missing audience group heading %q", indexPath, heading)
		}
	}
}

// TestDocsIndexHasNoDeadLinks ensures every relative Markdown link in
// docs/README.md points to a file that actually exists (issue #648).
// This is the complement to TestDocsIndexCoversEveryDoc: the index must list
// every doc and must not point to a doc that does not exist.
func TestDocsIndexHasNoDeadLinks(t *testing.T) {
	indexPath := "docs/README.md"
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", indexPath, err)
	}

	// Extract all Markdown links: [text](path). Skip http/https and anchors.
	links := extractMarkdownLinks(string(raw))
	for _, link := range links {
		if strings.HasPrefix(link, "http") || strings.HasPrefix(link, "#") {
			continue
		}
		target := filepath.Join("docs", link)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			t.Errorf("%s: broken link: %s (resolved to %s)", indexPath, link, target)
		}
	}
}

// extractMarkdownLinks returns the URL part of every [text](url) in s.
func extractMarkdownLinks(s string) []string {
	var links []string
	for {
		open := strings.Index(s, "](")
		if open < 0 {
			break
		}
		rest := s[open+2:]
		close := strings.Index(rest, ")")
		if close < 0 {
			break
		}
		link := rest[:close]
		// Strip any fragment (#anchor).
		if hash := strings.Index(link, "#"); hash >= 0 {
			link = link[:hash]
		}
		if link != "" {
			links = append(links, link)
		}
		s = rest[close+1:]
	}
	return links
}

// TestDocsIndexDocCount is a canary: if the number of Markdown files in docs/
// changes without a corresponding update to this test, the test fails.
// Update the constant when you add or remove docs.
//
// The count is the number of .md files found under docs/ minus docs/README.md
// itself (which is the index, not a doc entry).
func TestDocsIndexDocCount(t *testing.T) {
	var paths []string
	err := filepath.Walk("docs", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") && path != "docs/README.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking docs/: %v", err)
	}

	// If this number is wrong after you added or removed a doc, update it
	// and also update docs/README.md to list the new doc.
	const wantCount = 150
	if got := len(paths); got != wantCount {
		sort.Strings(paths)
		t.Errorf("docs/ contains %d .md files (excluding README.md), want %d; "+
			"if you added a new doc, list it in docs/README.md and update this constant; "+
			"if you removed one, remove it from docs/README.md and update this constant\n%s",
			got, wantCount, fmt.Sprintf("found:\n  %s", strings.Join(paths, "\n  ")))
	}
}
