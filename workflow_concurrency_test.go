package wayfare

import (
	"os"
	"strings"
	"testing"
)

// TestPullRequestRunsCancelOnSupersedingPushes pins the concurrency group on
// the workflows that answer for a pull request head.
//
// Without it, a new push starts a second run while the run of the replaced
// head keeps going. Both finish; the slower one lands last; and the checks a
// branch protection rule waits on then describe a commit that is no longer
// under review — a red head turning green, or a green one turning red, for
// reasons unrelated to the new code (#574).
//
// The group keys every run of one pull request together (the PR number for
// pull request events, the ref otherwise, so different branches never
// collide), and cancel-in-progress must be true so the newer push stops the
// obsolete run instead of racing it.
//
// Source-text assertions, like workflow_pins_test.go: a YAML parser would be
// a third-party dependency, and CONTRIBUTING.md keeps the module at two.
// The block is read positionally — top-level `concurrency:` up to the next
// top-level key — so prose in a comment or a nested key of the same name
// cannot satisfy the check.
func TestPullRequestRunsCancelOnSupersedingPushes(t *testing.T) {
	for _, file := range []string{
		".github/workflows/ci.yml",
		".github/workflows/govulncheck.yml",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		block, ok := topLevelBlock(string(raw), "concurrency:")
		if !ok {
			t.Errorf("%s has no top-level concurrency group, so a new push to a pull request "+
				"leaves the superseded run racing it for the checks", file)
			continue
		}
		flat := normalise(block)

		if !hasNormalisedLine(flat, "group:${{github.workflow}}-${{github.event.pull_request.number||github.ref}}") {
			t.Errorf("%s: concurrency group must key on the pull request number (the ref for "+
				"non-PR events) so one PR's runs cancel each other while other branches are "+
				"left alone; block reads:\n%s", file, block)
		}
		if !hasNormalisedLine(flat, "cancel-in-progress:true") {
			t.Errorf("%s: concurrency must set cancel-in-progress: true so a push cancels the "+
				"in-progress run of the head it replaced; block reads:\n%s", file, block)
		}
	}
}

// TestMeasureRunsQueueAndNeverOverlap pins the other half of the rule: the
// measure workflow serialises instead of cancelling (#574).
//
// Two concurrent sweeps would race on the same branch and the same
// hash-chained store — the second would fail to extend a chain the first has
// already moved. Queueing is the point, not a fallback: a cancelled sweep is
// a gap in the measurement history, which is the exact thing this workflow
// exists to prevent. So the group is shared by every trigger (schedule and
// workflow_dispatch alike) and cancel-in-progress is false.
//
// The check is positional (top-level `concurrency:` up to the next top-level
// key), so it cannot be satisfied by the comment above the block.
func TestMeasureRunsQueueAndNeverOverlap(t *testing.T) {
	const file = ".github/workflows/measure.yml"
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	block, ok := topLevelBlock(string(raw), "concurrency:")
	if !ok {
		t.Fatalf("%s has no top-level concurrency group; two measure runs would race on the "+
			"same branch and chain", file)
	}
	flat := normalise(block)

	if !hasNormalisedLine(flat, "group:measure") {
		t.Errorf("%s: every measure run must share one concurrency group so two runs never "+
			"overlap; block reads:\n%s", file, block)
	}
	if !hasNormalisedLine(flat, "cancel-in-progress:false") {
		t.Errorf("%s: measure must set cancel-in-progress: false and queue, because cancelling "+
			"a sweep would leave a gap in the hash-chained history; block reads:\n%s", file, block)
	}
}

// topLevelBlock returns the body of the workflow's top-level `key` block: the
// lines after the line that is exactly `key:` (zero indentation, so a nested
// key of the same name never matches) up to the next line at zero
// indentation that is neither blank nor a comment.
//
// The raw text is returned so the caller decides how to compare it; blocks
// are asserted through normalise, like every other workflow check here.
func topLevelBlock(workflow, key string) (string, bool) {
	lines := strings.Split(workflow, "\n")
	for i, line := range lines {
		if line != key {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if next == "" || strings.HasPrefix(next, "#") ||
				strings.HasPrefix(next, " ") || strings.HasPrefix(next, "\t") {
				body = append(body, next)
				continue
			}
			return strings.Join(body, "\n"), true
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// hasNormalisedLine reports whether the normalised text has a line equal to
// want, so an assertion cannot be met by a longer line that merely starts the
// same way (e.g. `group:measurements` against `group:measure`).
func hasNormalisedLine(flat, want string) bool {
	for _, line := range strings.Split(flat, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
