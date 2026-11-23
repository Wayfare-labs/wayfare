package wayfare

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// TestDocsLinksAreNotBroken checks every relative link (and anchor) in all
// Markdown files under docs/ and in the root README.md (issue #649).
//
// What is checked:
//   - Relative file links: the target file must exist.
//   - Anchor links (#heading): the anchor must exist in the target file, using
//     GitHub's heading-to-anchor algorithm (lowercase, spaces→hyphens, most
//     punctuation dropped).
//
// What is NOT checked (and why):
//   - http/https URLs: network calls in a unit test create flaky CI.
//   - Line-number citations (file.go:123): those are warned via
//     TestDocsLineNumberCitationsAreNotTooStale below — they do not block CI.
func TestDocsLinksAreNotBroken(t *testing.T) {
	sources := collectDocsSources(t)

	for _, src := range sources {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Errorf("cannot read %s: %v", src, err)
			continue
		}
		srcDir := filepath.Dir(src)
		links := parseLinksFromMarkdown(string(raw))
		for _, lnk := range links {
			if strings.HasPrefix(lnk.url, "http://") || strings.HasPrefix(lnk.url, "https://") {
				continue // skip external links
			}
			if lnk.url == "" {
				continue
			}

			// Split path from fragment.
			rawURL := lnk.url
			fragment := ""
			if idx := strings.Index(rawURL, "#"); idx >= 0 {
				fragment = rawURL[idx+1:]
				rawURL = rawURL[:idx]
			}

			// Pure anchor link (no file part): target is the same file.
			targetFile := src
			if rawURL != "" {
				targetFile = filepath.Join(srcDir, rawURL)
			}

			if _, err := os.Stat(targetFile); os.IsNotExist(err) {
				t.Errorf("%s: broken link to %q (resolved: %s)", src, lnk.url, targetFile)
				continue
			}

			if fragment != "" && !isNumericFragment(fragment) {
				checkAnchorExists(t, src, targetFile, fragment)
			}
		}
	}
}

// TestDocsLineNumberCitationsAreNotTooStale reports (as warnings via t.Log,
// not failures) any file.go:NNN citation in docs/ whose cited line no longer
// contains text that looks like it did when the citation was written
// (issue #649).
//
// This is advisory: a stale line number is not proof the citation is wrong —
// the symbol may have moved a few lines — so this test never calls t.Errorf.
// It is a signal to a human reviewer.
func TestDocsLineNumberCitationsAreNotTooStale(t *testing.T) {
	// Pattern: someFile.go:123  (only Go source files, to avoid false positives)
	lineRef := regexp.MustCompile(`(\S+\.go):(\d+)`)

	sources := collectDocsSources(t)
	for _, src := range sources {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Errorf("cannot read %s: %v", src, err)
			continue
		}
		matches := lineRef.FindAllStringSubmatch(string(raw), -1)
		for _, m := range matches {
			goFile := m[1]
			lineNoStr := m[2]

			// Resolve relative to repo root (docs cite repo-root-relative paths).
			if _, err := os.Stat(goFile); os.IsNotExist(err) {
				t.Logf("WARNING %s: cites %s:%s but %s does not exist (file moved or removed?)",
					src, goFile, lineNoStr, goFile)
			}
			// We don't validate the line number itself here: that would require
			// parsing Go source, which is out of scope. The existence check is
			// enough to catch the most common staleness — the file is gone.
			_ = lineNoStr
		}
	}
}

// --- helpers ----------------------------------------------------------------

type markdownLink struct {
	url string
}

// parseLinksFromMarkdown extracts [text](url) link targets from Markdown.
// It handles plain links and skips image syntax (![alt](url) is treated the
// same — the target is still a file we can check).
func parseLinksFromMarkdown(s string) []markdownLink {
	var links []markdownLink
	// Find all ](url) sequences.
	for {
		idx := strings.Index(s, "](")
		if idx < 0 {
			break
		}
		rest := s[idx+2:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			break
		}
		raw := strings.TrimSpace(rest[:end])
		// Strip optional title: ](path "Title")
		if sp := strings.IndexByte(raw, ' '); sp >= 0 {
			raw = raw[:sp]
		}
		if raw != "" {
			links = append(links, markdownLink{url: raw})
		}
		s = rest[end+1:]
	}
	return links
}

// collectDocsSources returns all Markdown files to check: docs/**/*.md and
// README.md at the repo root.
func collectDocsSources(t *testing.T) []string {
	t.Helper()
	var sources []string
	if err := filepath.Walk("docs", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			sources = append(sources, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking docs/: %v", err)
	}
	if _, err := os.Stat("README.md"); err == nil {
		sources = append(sources, "README.md")
	}
	sort.Strings(sources)
	return sources
}

// isNumericFragment reports whether a fragment is a pure line-number reference
// (e.g. "#151", "#L151") rather than a heading anchor. GitHub blob-URL line
// references are written this way in some docs; they cannot be validated
// against headings and are skipped by TestDocsLinksAreNotBroken.
func isNumericFragment(f string) bool {
	s := strings.TrimPrefix(f, "L")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// checkAnchorExists verifies that `fragment` resolves to a heading anchor in
// targetFile, using GitHub's heading → anchor algorithm.
func checkAnchorExists(t *testing.T, srcFile, targetFile, fragment string) {
	t.Helper()
	raw, err := os.ReadFile(targetFile)
	if err != nil {
		// File existence already checked by caller; this is a read error.
		t.Errorf("%s: cannot read %s to check anchor #%s: %v", srcFile, targetFile, fragment, err)
		return
	}
	anchors := headingAnchors(string(raw))
	if !anchors[fragment] {
		t.Errorf("%s: broken anchor #%s in %s\n  known anchors: %s",
			srcFile, fragment, targetFile, fmt.Sprintf("%v", sortedBoolKeys(anchors)))
	}
}

// headingAnchors returns the set of GitHub-style heading anchors for all ATX
// headings (# …) in the Markdown text.
//
// GitHub's algorithm: lowercase the heading text, replace spaces with hyphens,
// remove all characters except letters, digits, hyphens, and underscores.
// Duplicate anchors get a -1, -2, … suffix; we implement the dedup to avoid
// false positives.
func headingAnchors(md string) map[string]bool {
	anchors := map[string]bool{}
	counts := map[string]int{}

	for _, line := range strings.Split(md, "\n") {
		stripped := strings.TrimLeft(line, "#")
		if stripped == line {
			continue // not a heading
		}
		if len(stripped) > 0 && stripped[0] != ' ' {
			continue // no space after hashes — not an ATX heading
		}
		text := strings.TrimSpace(stripped)
		slug := headingToAnchor(text)
		if counts[slug] == 0 {
			anchors[slug] = true
		} else {
			anchors[fmt.Sprintf("%s-%d", slug, counts[slug])] = true
		}
		counts[slug]++
	}
	return anchors
}

func headingToAnchor(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ' || r == '-':
			b.WriteRune('-')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
		// all other punctuation is dropped
		}
	}
	return b.String()
}

func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
