package wayfare

import (
	"os"
	"strings"
	"testing"
)

// TestStrayWorkspaceFilesStayRemovedAndIgnored keeps two files that were
// committed by mistake out of the tree, and keeps them out for good.
//
// PR_DESCRIPTION.md was a pull request description that tooling wrote to the
// workspace root: it reads like project documentation and is not, so a
// contributor has to discover that before trusting it (#570).
// .claude/settings.json is personal AI-tool configuration, and worse than
// noise — it pre-approves shell commands for anyone using that tool in a
// clone, so the repository was granting permissions on someone's behalf
// without their review (#571).
//
// The removal alone would not hold: a file that tooling dropped once can be
// dropped again, and a tracked file reappears in every clone. So this asserts
// both halves of each fix — the file is gone, and .gitignore says it does not
// come back.
func TestStrayWorkspaceFilesStayRemovedAndIgnored(t *testing.T) {
	stray := []struct {
		path    string
		whyGone string
	}{
		{"PR_DESCRIPTION.md", "a PR description, not project documentation (#570)"},
		{".claude/settings.json", "personal tooling config that pre-approves shell commands (#571)"},
	}
	for _, f := range stray {
		if _, err := os.Stat(f.path); !os.IsNotExist(err) {
			t.Errorf("%s exists at the repository root; it is %s", f.path, f.whyGone)
		}
	}

	ignored := ignoredPatterns(t)
	for _, want := range []string{"/PR_DESCRIPTION.md", ".claude/"} {
		if !ignored[want] {
			t.Errorf(".gitignore does not contain %q, so %s can be committed again by the "+
				"same mistake that put it there", want, want)
		}
	}
}

// TestRenderIsTheOnlyRootDeploymentDescriptor makes "which deployment is
// live?" answerable from the root directory alone.
//
// fly.toml described a Fly.io deployment that does not exist; render.yaml
// describes the real one (#144 fixed the docs, but the file itself still
// suggested otherwise). Two descriptors at the root mean every contributor
// has to guess — and guessing wrong means deploying to the wrong platform.
// So the root carries exactly one: render.yaml, with render.yaml present
// (the check is not satisfiable by deleting both) and no fly.toml (#572).
func TestRenderIsTheOnlyRootDeploymentDescriptor(t *testing.T) {
	if _, err := os.Stat("render.yaml"); err != nil {
		t.Errorf("render.yaml, the production deployment descriptor, is missing from the "+
			"repository root: %v", err)
	}
	if _, err := os.Stat("fly.toml"); !os.IsNotExist(err) {
		t.Error("fly.toml is still at the repository root; render.yaml is the only production " +
			"descriptor, and a second one makes contributors guess which is live (#572)")
	}
}

// ignoredPatterns returns .gitignore's effective patterns: comments and blank
// lines dropped, whitespace trimmed, so a test compares what the file ignores
// rather than how it is formatted.
//
// An empty result is a fatal error rather than a pass: every assertion below
// depends on reading a real pattern list, and a vacuous one would prove
// nothing.
func ignoredPatterns(t *testing.T) map[string]bool {
	t.Helper()

	raw, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	patterns := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns[line] = true
	}
	if len(patterns) == 0 {
		t.Fatal(".gitignore contains no effective patterns; nothing could be ignored")
	}
	return patterns
}
