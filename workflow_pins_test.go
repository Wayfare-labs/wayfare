package wayfare

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWorkflowActionsArePinnedByCommitSHA makes the pin rule a red build,
// rather than a convention a reviewer has to catch.
//
// The workflows here carry `contents: write` and `pull-requests: write`
// (measure.yml opens measurement pull requests, auto-merge.yml merges them).
// A tag is a mutable pointer: whoever controls an action's repository can
// move `@v4` onto different code after this repository reviewed it, and that
// code would then run with those permissions on the next pull request or
// schedule. A full commit SHA removes the moving part. The trailing version
// comment is what lets a human — and Dependabot's SHA bump — read which
// release the SHA is.
//
// Source-text assertions, like ci_image_scan_test.go: a YAML parser would be
// a third-party dependency, and CONTRIBUTING.md keeps the module at two.
// Whitespace and quotes are normalised away first, so reformatting a workflow
// does not fail a test about what the workflow says.
func TestWorkflowActionsArePinnedByCommitSHA(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatalf("globbing the workflow directory: %v", err)
	}
	more, err := filepath.Glob(".github/workflows/*.yaml")
	if err != nil {
		t.Fatalf("globbing the workflow directory: %v", err)
	}
	files = append(files, more...)
	if len(files) == 0 {
		t.Fatal("no workflow files found under .github/workflows; a check with nothing " +
			"to read would pass vacuously")
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			ref, comment, ok := usesReference(line)
			if !ok {
				continue
			}
			where := fmt.Sprintf("%s:%d", file, i+1)

			switch {
			case strings.HasPrefix(ref, "./"):
				// A local action is this repository at this commit; it cannot
				// float independently of the checkout that runs it.
				continue
			case strings.HasPrefix(ref, "docker://"):
				if !dockerDigestReference.MatchString(ref) {
					t.Errorf("%s: %q is not pinned by image digest; a registry tag is "+
						"mutable, so pin the image as @sha256:<digest>", where, ref)
				}
			default:
				if !shaPinnedAction.MatchString(ref) {
					t.Errorf("%s: %q does not name a full 40-character commit SHA; a tag "+
						"or branch is a mutable pointer and would run with this workflow's "+
						"permissions", where, ref)
					continue
				}
				if !versionComment.MatchString(comment) {
					t.Errorf("%s: %q is pinned by SHA but has no version comment "+
						"(e.g. `# v4.2.0`), so neither a reader nor Dependabot can tell "+
						"which release it is", where, ref)
				}
			}
		}
	}
}

var (
	// shaPinnedAction matches `owner/repo[/subdir]@<40 lowercase hex>`, the
	// only form that cannot move under the repository's feet.
	shaPinnedAction = regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)

	// dockerDigestReference matches a container action pinned by its image
	// digest, which is the immutable form of `docker://`.
	dockerDigestReference = regexp.MustCompile(`^docker://[^@\s]+@sha256:[0-9a-f]{64}$`)

	// versionComment matches a release-shaped token in the trailing comment:
	// `v5.1.0`, `0.36.0`, and the like.
	versionComment = regexp.MustCompile(`v?[0-9]+\.[0-9]+`)
)

// usesReference extracts the value of a step's `uses:` and its trailing
// comment, if the line is one. It returns false for every other line.
//
// A YAML comment only starts at " #" — the space matters — so a reference
// that merely contains a hash is not truncated.
func usesReference(line string) (ref, comment string, ok bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	if !strings.HasPrefix(trimmed, "uses:") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "uses:"))
	if i := strings.Index(rest, " #"); i >= 0 {
		comment = strings.TrimSpace(rest[i:])
		rest = strings.TrimSpace(rest[:i])
	}
	return strings.Trim(rest, `"'`), comment, true
}
