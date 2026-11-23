package automerge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Issue #319 — exercise the auto-merge gate deliberately.
//
// Every test here runs the real script out of .github/workflows/auto-merge.yml
// under bash, with a fake `gh` that answers from API responses recorded from
// this repository (testdata/recorded) and logs what it was asked to do. The
// gate is never modified: these tests pin its behaviour, including the
// behaviour nobody has observed yet.

const (
	scriptRel   = "../.github/scripts/auto-merge-gate.sh"
	recordedRel = "testdata/recorded"
	repoSlug    = "Wayfare-labs/wayfare"
	prNumber    = "517"
	issueNumber = "302"
	selfCheck   = "check the merge gates"
	holdMarker  = "<!-- auto-merge-gate -->"
)

// tickedBody is a description that satisfies the checklist gate and names the
// issue, so a scenario only fails the gate it means to fail.
const tickedBody = "Closes #" + issueNumber + "\n\n" +
	"## Confirmations\n\n" +
	"- [x] **Unknown is reported as unknown.**\n" +
	"- [x] **Tests run from recorded fixtures, no live network.**\n"

// --- the script under test -------------------------------------------------

// workflowScript reads the gate. It lives in a file because GitHub rejects a
// workflow whose single `run:` script passes 21,000 characters, which took
// the whole bot down once; TestWorkflowRunBlocksStayUnderGitHubsLimit keeps it
// out of the workflow.
func workflowScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(scriptRel)
	if err != nil {
		t.Fatalf("reading %s: %v", scriptRel, err)
	}
	script := string(raw)

	// Reading must not silently truncate: a test that passed against a
	// fragment of the gate would be worse than no test.
	for _, want := range []string{"GATE 1", "GATE 7", "gh pr merge", "needs-maintainer-review"} {
		if !strings.Contains(script, want) {
			t.Fatalf("%s is missing %q; it is not the whole gate", scriptRel, want)
		}
	}
	// A script file is not rendered by GitHub, so an expression in it would
	// reach bash literally. They belong in the workflow's env: block.
	if strings.Contains(script, "${{") {
		t.Fatalf("%s contains a GitHub expression; pass it through the workflow's env block instead", scriptRel)
	}
	return script
}

// TestWorkflowRunBlocksStayUnderGitHubsLimit is the regression test for the
// outage: auto-merge.yml's inline script grew to ~30,000 characters, GitHub
// refused the whole file ("workflow file issue"), and no pull request was
// gated until it was moved out. The ceiling is 21,000; this fails well before.
func TestWorkflowRunBlocksStayUnderGitHubsLimit(t *testing.T) {
	const limit = 18000
	matches, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil || len(matches) == 0 {
		t.Fatalf("finding workflows: %v (%d found)", err, len(matches))
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		for i := 0; i < len(lines); i++ {
			if !strings.HasSuffix(strings.TrimSpace(lines[i]), "run: |") {
				continue
			}
			indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			size := 0
			for _, l := range lines[i+1:] {
				if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= indent {
					break
				}
				size += len(l) + 1
			}
			if size > limit {
				t.Errorf("%s: the run block at line %d is %d characters; GitHub rejects the whole workflow past 21,000. Move it to .github/scripts/", path, i+1, size)
			}
		}
	}
}

// --- recorded fixtures ------------------------------------------------------

type userRef struct {
	Login string `json:"login"`
}

// prRecord holds the PR fields the gate reads. Anything else the API returns
// is decoded away on purpose: the test must not depend on a field the gate
// has never looked at.
type prRecord struct {
	Number int     `json:"number"`
	State  string  `json:"state"`
	Draft  bool    `json:"draft"`
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	User   userRef `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	ChangedFiles int `json:"changed_files"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

type fileRec struct {
	Filename string `json:"filename"`
	// Status and Patch are what GATE 0's malware scan reads.
	Status string `json:"status,omitempty"`
	Patch  string `json:"patch,omitempty"`
}

type checkRec struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type checkRecord struct {
	CheckRuns []checkRec `json:"check_runs"`
}

type reviewRec struct {
	User     userRef `json:"user"`
	State    string  `json:"state"`
	CommitID string  `json:"commit_id"`
	Body     string  `json:"body"`
}

type statusRec struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description,omitempty"`
}

type statusRecord struct {
	State    string      `json:"state"`
	SHA      string      `json:"sha"`
	Statuses []statusRec `json:"statuses"`
}

type runRec struct {
	ID    int    `json:"id"`
	Event string `json:"event"`
}

type runsRecord struct {
	WorkflowRuns []runRec `json:"workflow_runs"`
}

type commentRec struct {
	ID   int64   `json:"id"`
	User userRef `json:"user"`
	Body string  `json:"body"`
}

type issueRec struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type commitRef struct {
	Number int `json:"number"`
}

// fixtures is one scenario's view of the world: the recorded responses,
// decoded and mutable.
type fixtures struct {
	pr          prRecord
	files       []fileRec
	checks      checkRecord
	reviews     []reviewRec
	comments    []commentRec
	issue       issueRec
	commitPulls []commitRef
	status      statusRecord
	runs        runsRecord
}

func loadFixtures(t *testing.T) *fixtures {
	t.Helper()
	f := &fixtures{}
	must := func(v any, name string) {
		b, err := os.ReadFile(filepath.Join(recordedRel, name))
		if err != nil {
			t.Fatalf("reading recorded fixture %s: %v", name, err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("recorded %s no longer matches the shape the gate reads: %v", name, err)
		}
	}
	must(&f.pr, "pr.json")
	must(&f.files, "files.json")
	must(&f.checks, "check-runs.json")
	must(&f.reviews, "reviews.json")
	must(&f.comments, "comments.json")
	must(&f.issue, "issue.json")
	must(&f.commitPulls, "commit-pulls.json")
	must(&f.status, "status.json")
	f.runs = runsRecord{WorkflowRuns: []runRec{}}

	// The recorded commit predates the malware scan, so its real status list
	// has no "Malware scan" entry. The gate treats an absent scan as "not
	// yet" and would wait forever, so the base scenario adds the passing
	// result the scan publishes today; scenarios override it.
	f.setMalware("success")
	return f
}

func (f *fixtures) write(t *testing.T, dir string) {
	t.Helper()
	put := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encoding %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	put("pr.json", f.pr)
	put("files.json", f.files)
	put("check-runs.json", f.checks)
	put("reviews.json", f.reviews)
	put("comments.json", f.comments)
	put("issue.json", f.issue)
	put("commit-pulls.json", f.commitPulls)
	put("status.json", f.status)
	put("runs.json", f.runs)
}

// setMalware sets the "Malware scan" commit status; "" removes it, which is
// what a commit the scan has not reported on looks like.
func (f *fixtures) setMalware(state string) {
	kept := make([]statusRec, 0, len(f.status.Statuses))
	for _, st := range f.status.Statuses {
		if st.Context != "Malware scan" {
			kept = append(kept, st)
		}
	}
	if state != "" {
		kept = append(kept, statusRec{Context: "Malware scan", State: state})
	}
	f.status.Statuses = kept
}

// label adds a label to the pull request.
func (f *fixtures) label(name string) {
	f.pr.Labels = append(f.pr.Labels, struct {
		Name string `json:"name"`
	}{Name: name})
}

// addFile clones a recorded file entry so a changed file arrives in exactly
// the shape the API sends it.
func (f *fixtures) addFile(name string) {
	seed := f.files[0]
	seed.Filename = name
	f.files = append(f.files, seed)
}

// dropHoldMarker removes the gate's own previous comment, so a held
// scenario can assert that the gate comments at all. The dedup scenario
// keeps it and asserts the opposite.
func (f *fixtures) dropHoldMarker() {
	kept := make([]commentRec, 0, len(f.comments))
	for _, c := range f.comments {
		if strings.Contains(c.Body, holdMarker) {
			continue
		}
		kept = append(kept, c)
	}
	f.comments = kept
}

// coderabbit appends a review summary comment. The gate reads the most
// recent CodeRabbit comment, so an appended one wins.
func (f *fixtures) coderabbit(body string) {
	f.comments = append(f.comments, commentRec{
		User: userRef{Login: "coderabbitai[bot]"},
		Body: body,
	})
}

// setFirstCheck changes the state of a real check run — never the gate's
// own, which the gate excludes from its count by name.
func (f *fixtures) setFirstCheck(t *testing.T, status, conclusion string) {
	t.Helper()
	for i := range f.checks.CheckRuns {
		if f.checks.CheckRuns[i].Name == selfCheck {
			continue
		}
		f.checks.CheckRuns[i].Status = status
		f.checks.CheckRuns[i].Conclusion = conclusion
		return
	}
	t.Fatal("the recorded check-runs fixture has no check other than the gate's own")
}

// --- running the gate -------------------------------------------------------

type scenario struct {
	name string
	// env overrides the environment the script runs with.
	env    map[string]string
	mutate func(t *testing.T, f *fixtures)

	stdout    []string // substrings that must appear on stdout
	holds     []string // substrings that must appear among the printed reasons
	merged    bool     // expect `gh pr merge --auto`
	labeled   bool     // expect `gh pr edit --add-label needs-maintainer-review`
	commented bool     // expect `gh pr comment`
	edited    bool     // expect the gate's own comment to be edited in place
	approved  bool     // expect a held workflow run to be approved
	noCalls   bool     // expect no gh invocation at all
	wantErr   bool     // expect a non-zero exit
}

type result struct {
	stdout string
	stderr string
	calls  []string
	err    error
}

// fakeGH is the offline stand-in for the GitHub CLI. It answers reads from
// the scenario's fixtures, appends every invocation to a log so the test can
// assert what the gate *did*, and fails merges on demand so the gate's own
// failure path is reachable without a real repository.
const fakeGH = `#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${GATE_CALLS:?GATE_CALLS must name the call log}"

cmd="${1:-}"; shift || true

# Drain a piped body so the writer never sees EPIPE under pipefail.
case " $* " in *" --body-file - "*) cat >/dev/null ;; esac

if [ "$cmd" != "api" ]; then
  if [ "${GATE_FAIL_MERGE:-0}" = "1" ] && [ "${1:-}" = "merge" ]; then
    echo "gh: refusing to merge (offline test)" >&2
    exit 1
  fi
  exit 0
fi

path=""; expr=""
while [ $# -gt 0 ]; do
  case "$1" in
    --paginate) ;;
    --jq) shift; expr="${1:-}" ;;
    -X|-f|-F) shift ;;
    *) [ -n "$path" ] || path="$1" ;;
  esac
  shift
done

rel="${path#repos/*/*/}"
case "$rel" in
  actions/runs/*/approve) echo '{}'; exit 0 ;;
  actions/runs\?*)        file="runs.json" ;;
  commits/*/status)     file="status.json" ;;
  issues/comments/*)
    jq -r --argjson id "${rel##*/}" ".[] | select(.id == \$id) | ${expr:-.}" "$GATE_FIXTURES/comments.json"
    exit 0 ;;
  pulls/*/commits)      echo '[]'; exit 0 ;;
  graphql)              exit 0 ;;
  */files)              file="files.json" ;;
  commits/*/check-runs) file="check-runs.json" ;;
  pulls/*/reviews)      file="reviews.json" ;;
  issues/*/comments)    file="comments.json" ;;
  commits/*/pulls)      file="commit-pulls.json" ;;
  issues/*)             file="issue.json" ;;
  pulls/*)              file="pr.json" ;;
  *) echo "gh (offline): no fixture for $path" >&2; exit 1 ;;
esac

src="$GATE_FIXTURES/$file"
if [ ! -f "$src" ]; then
  echo "gh (offline): missing fixture $file for $path" >&2
  exit 1
fi

if [ -n "$expr" ]; then jq -r "$expr" "$src"; else cat "$src"; fi
`

// mergeEnv overlays a map on the process environment without leaving a
// duplicate key: which value a child sees must not depend on how the OS
// resolves duplicates.
func mergeEnv(over map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(over))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			if _, ok := over[kv[:i]]; ok {
				continue
			}
		}
		out = append(out, kv)
	}
	keys := make([]string, 0, len(over))
	for k := range over {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+over[k])
	}
	return out
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func runGate(t *testing.T, s scenario) result {
	t.Helper()

	fx := loadFixtures(t)
	if s.mutate != nil {
		s.mutate(t, fx)
	}

	script := workflowScript(t)

	dir := t.TempDir()
	fx.write(t, dir)

	scriptPath := filepath.Join(dir, "gate.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("writing the extracted gate script: %v", err)
	}

	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("creating the fake gh directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatalf("installing the fake gh: %v", err)
	}

	// GATE 0 runs .github/scripts/malware_scan.py from the workspace root,
	// as it does in Actions.
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}

	callsPath := filepath.Join(dir, "calls.log")
	env := map[string]string{
		"GITHUB_WORKSPACE": root,
		"PATH":             bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GATE_FIXTURES":    dir,
		"GATE_CALLS":       callsPath,
		"GH_TOKEN":         "test-token",
		"REPO":             repoSlug,
		"EVENT_PR":         prNumber,
		"ISSUE_PR":         "",
		"IS_PR":            "",
		"HEAD_SHA":         fx.pr.Head.SHA,
		"SELF_CHECK":       selfCheck,
		// The two workflow expressions the script reads, supplied by the
		// workflow's env block in Actions.
		"WORKFLOW_RUN_EVENT": "pull_request",
		"COMMENT_USER":       "a-first-time-contributor",
	}
	for k, v := range s.env {
		env[k] = v
	}

	cmd := exec.CommandContext(context.Background(), "bash", scriptPath)
	cmd.Env = mergeEnv(env)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	return result{stdout: stdout.String(), stderr: stderr.String(), calls: readLines(callsPath), err: err}
}

func called(calls []string, substr string) bool {
	for _, c := range calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func TestAutoMergeGate(t *testing.T) {
	scenarios := []scenario{
		{
			// The path that has never been taken, per the audit in #48:
			// every gate satisfied, so the gate enqueues a squash merge.
			name: "all-gates-pass",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout:    []string{"all gates satisfied; handing off"},
			merged:    true,
			commented: true,
		},
		{
			// GATE 0 — malicious code is never merged, even when every other
			// gate would pass. The shape is the one use-assay/Assay#267 used:
			// a hidden editor task that runs a "font" file on folder open.
			name: "malware-detected",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.coderabbit("No actionable comments were generated for this review.")
				f.files = append(f.files, fileRec{
					Filename: ".vscode/tasks.json",
					Status:   "added",
					Patch: "@@ -0,0 +1,2 @@\n" +
						"+      \"command\": \"node ./public/fonts/fa-solid.lkf\",\n" +
						"+      \"runOptions\": { \"runOn\": \"folderOpen\" }",
				})
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"contains code matching known malicious patterns"},
			labeled: true, commented: true,
		},
		{
			// GATE 1 — maintainer-owned paths are never merged unreviewed.
			name: "maintainer-owned-path",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.addFile("route/route.go")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"touches maintainer-owned paths: route/route.go"},
			labeled: true, commented: true,
		},
		{
			// GATE 2 — a third dependency is a decision, not a detail.
			name: "dependency-change",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.addFile("go.mod")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"modifies go.mod or go.sum"},
			labeled: true, commented: true,
		},
		{
			// The PR description is not a gate. #517's description has no
			// checklist at all; what is judged is the diff against the issue
			// it resolves, so this merges.
			name: "description-is-not-a-gate",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = ""
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout: []string{"all gates satisfied; handing off"},
			merged: true, commented: true,
		},
		{
			// An open checklist box does not hold a PR either.
			name: "unticked-box-does-not-hold",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody + "- [ ] **`make fmt vet test race lint` is clean.**\n"
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout: []string{"all gates satisfied; handing off"},
			merged: true, commented: true,
		},
		{
			// GATE 4 — a red check stops the merge and says which one.
			name: "check-failed",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.setFirstCheck(t, "completed", "failure")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"checks are not green"},
			labeled: true, commented: true,
		},
		{
			// GATE 4 — a running check is "not yet", not "no": the gate waits
			// instead of deciding, and says so rather than going quiet.
			name: "check-pending",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.setFirstCheck(t, "in_progress", "")
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout: []string{"check(s) still running; will re-evaluate when they finish"},
		},
		{
			// GATE 5 — a reviewer has asked for changes.
			name: "changes-requested",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.reviews = append(f.reviews, reviewRec{
					User:     userRef{Login: "a-maintainer"},
					State:    "CHANGES_REQUESTED",
					CommitID: f.pr.Head.SHA,
					Body:     "One thing to fix.",
				})
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"a reviewer has requested changes"},
			labeled: true, commented: true,
		},
		{
			// GATE 6 — a review that raised comments is not clean.
			name: "coderabbit-actionable",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.coderabbit("Actionable comments posted: 2")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"CodeRabbit raised 2 actionable comment(s)"},
			labeled: true, commented: true,
		},
		{
			// GATE 6 — what the recorded CodeRabbit comment actually is: a
			// notice that it does not review this repository. That is not a
			// verdict and never will be, so it does not hold the PR, and the
			// merge comment must not claim a review happened.
			name: "coderabbit-skipped-is-not-a-hold",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
			},
			stdout: []string{"coderabbit: skipped for this repository", "all gates satisfied; handing off"},
			merged: true, commented: true,
		},
		{
			// GATE 6 — a CodeRabbit comment in a shape the gate does not
			// recognise (neither a verdict nor a skip notice) still holds:
			// merging on an unread review would claim it passed.
			name: "coderabbit-unparseable",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.coderabbit("Walkthrough: some prose with no verdict in it.")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"CodeRabbit's review could not be parsed for a verdict"},
			labeled: true, commented: true,
		},
		{
			// GATE 7 — a file the issue never named is scope creep, and it is
			// also how an unrelated change rides in on an approved one.
			name: "out-of-scope-file",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.addFile("monitor/scheduler.go")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"changes files outside the scope issue #" + issueNumber + " named"},
			labeled: true, commented: true,
		},
		{
			// GATE 7 — an issue that lists no paths cannot bound a code
			// change, so a human confirms it.
			name: "issue-without-paths-holds-code-change",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.issue.Body = "Make the thing better. No file paths here."
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"names no files or packages"},
			labeled: true, commented: true,
		},
		{
			// GATE 7 — but a change that is only documentation and tests
			// cannot alter behaviour, so a path-less issue does not hold it.
			name: "issue-without-paths-accepts-docs-only",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.issue.Body = "Document the thing. No file paths here."
				f.files = []fileRec{{Filename: "docs/glossary.md", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"}}
			},
			stdout: []string{"all gates satisfied; handing off"},
			merged: true, commented: true,
		},
		{
			// GATE 7 — the linked issue is found from the branch name when
			// the description never mentions it (#517's branch is
			// fix/issue-302-...), and the diff is judged against it.
			name: "issue-found-from-branch",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = "No closing keyword here.\n"
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout: []string{"all gates satisfied; handing off", "issue #" + issueNumber + " names"},
			merged: true, commented: true,
		},
		{
			// GATE 7 — with no issue anywhere (description, title, branch,
			// commits) there is no stated scope to check against.
			name: "no-issue-anywhere",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = "A description that never names the issue.\n"
				f.pr.Head.Ref = "fix/announce-results"
				f.dropHoldMarker()
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"could not be linked to an issue"},
			labeled: true, commented: true,
		},
		{
			// The hold comment is one comment per PR: a later evaluation
			// edits the gate's own comment in place (here the recorded one is
			// in the old format) instead of adding another.
			name: "hold-comment-edited-in-place",
			mutate: func(t *testing.T, f *fixtures) {
				f.coderabbit("Walkthrough: some prose with no verdict in it.")
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"CodeRabbit's review could not be parsed"},
			labeled: true, commented: false, edited: true,
		},
		{
			// MALWARE — a failed scan holds the PR, says so, and approves
			// nothing, even with a workflow run waiting.
			name: "malware-status-failed",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.setMalware("failure")
				f.runs.WorkflowRuns = []runRec{{ID: 99, Event: "pull_request"}}
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout:  []string{"not auto-merging:", "not approving CI runs"},
			holds:   []string{"failed the malware scan"},
			labeled: true, commented: true, approved: false,
		},
		{
			// MALWARE — the security-hold label alone is enough to hold, even
			// if the status reads success.
			name: "security-hold-label",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.label("security-hold")
				f.runs.WorkflowRuns = []runRec{{ID: 99, Event: "pull_request"}}
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout:  []string{"not auto-merging:", "not approving CI runs"},
			holds:   []string{"is under a security hold"},
			labeled: true, commented: true, approved: false,
		},
		{
			// MALWARE — a scan that has not reported is not a clean scan.
			// The gate waits (and approves nothing); it never merges.
			name: "malware-status-missing",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.setMalware("")
				f.runs.WorkflowRuns = []runRec{{ID: 99, Event: "pull_request"}}
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout: []string{"not deciding yet: the malware scan has not finished", "not approving CI runs"},
		},
		{
			// APPROVAL — a clean PR whose CI GitHub is holding gets it
			// released, once, for its own commit.
			name: "approves-held-ci-when-scan-clean",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.setFirstCheck(t, "queued", "")
				f.coderabbit("No actionable comments were generated for this review.")
				f.runs.WorkflowRuns = []runRec{{ID: 99, Event: "pull_request"}}
			},
			stdout:   []string{"approved workflow run 99"},
			approved: true,
		},
		{
			// APPROVAL — never for a PR that edits workflows.
			name: "no-approval-when-workflows-edited",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
				f.addFile(".github/workflows/ci.yml")
				f.runs.WorkflowRuns = []runRec{{ID: 99, Event: "pull_request"}}
			},
			stdout:  []string{"PR edits workflows; not approving"},
			holds:   []string{"touches maintainer-owned paths"},
			labeled: true, commented: true, approved: false,
		},
		{
			// A CI run that belongs to a merge-queue entry is not a pull
			// request; the gate says so before it touches the API.
			name: "merge-group-run-is-not-a-pr",
			env: map[string]string{
				"WORKFLOW_RUN_EVENT": "merge_group",
			},
			stdout:  []string{"CI run came from a merge_group entry; nothing to gate"},
			noCalls: true,
		},
		{
			// The gate's own comment must not re-trigger the gate.
			name: "own-comment-is-ignored",
			env: map[string]string{
				"COMMENT_USER": "github-actions[bot]",
			},
			stdout:  []string{"comment was ours; nothing to re-evaluate"},
			noCalls: true,
		},
		{
			// issue_comment also fires for plain issues. Those carry no
			// issue.pull_request.url, and looking them up as a pull request
			// 404s, which failed the run for every comment on every issue.
			name: "comment-on-plain-issue-is-not-a-pr",
			env: map[string]string{
				"EVENT_PR": "",
				"ISSUE_PR": "655",
				"IS_PR":    "",
				"HEAD_SHA": "",
			},
			stdout:  []string{"comment on issue #655, not a pull request; nothing to gate"},
			noCalls: true,
		},
		{
			// A comment on a pull request still re-evaluates that PR.
			name: "comment-on-pr-is-evaluated",
			env: map[string]string{
				"EVENT_PR": "",
				"ISSUE_PR": prNumber,
				"IS_PR":    "https://api.github.com/repos/" + repoSlug + "/pulls/" + prNumber,
				"HEAD_SHA": "",
			},
			stdout:    []string{"evaluating PR #" + prNumber},
			merged:    true,
			commented: true,
		},
		{
			// If the merge itself is refused, the gate fails loudly rather
			// than reporting an approval that landed nothing.
			name: "merge-refused",
			env: map[string]string{
				"GATE_FAIL_MERGE": "1",
			},
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.coderabbit("No actionable comments were generated for this review.")
			},
			stdout:    []string{"all gates satisfied; handing off", "could not enqueue PR #" + prNumber},
			merged:    true,
			commented: false,
			wantErr:   true,
		},
	}

	for _, s := range scenarios {
		s := s
		t.Run(s.name, func(t *testing.T) {
			got := runGate(t, s)

			for _, want := range s.stdout {
				if !strings.Contains(got.stdout, want) {
					t.Errorf("stdout does not contain %q\n--- stdout ---\n%s", want, got.stdout)
				}
			}
			for _, want := range s.holds {
				if !strings.Contains(got.stdout, want) {
					t.Errorf("the gate held the pull request without giving the reason %q\n--- stdout ---\n%s", want, got.stdout)
				}
			}

			if merge := called(got.calls, "pr merge "); merge != s.merged {
				t.Errorf("merged = %v, want %v (gh calls: %v)", merge, s.merged, got.calls)
			}
			if label := called(got.calls, "pr edit "); label != s.labeled {
				t.Errorf("labelled needs-maintainer-review = %v, want %v (gh calls: %v)", label, s.labeled, got.calls)
			}
			if comment := called(got.calls, "pr comment "); comment != s.commented {
				t.Errorf("commented = %v, want %v (gh calls: %v)", comment, s.commented, got.calls)
			}
			if edit := called(got.calls, "-X PATCH"); edit != s.edited {
				t.Errorf("edited own comment = %v, want %v (gh calls: %v)", edit, s.edited, got.calls)
			}
			if appr := called(got.calls, "-X POST") && called(got.calls, "/approve"); appr != s.approved {
				t.Errorf("approved a held run = %v, want %v (gh calls: %v)", appr, s.approved, got.calls)
			}
			if s.noCalls && len(got.calls) > 0 {
				t.Errorf("the gate called gh %d time(s) when it should not have called it at all: %v", len(got.calls), got.calls)
			}
			if s.wantErr && got.err == nil {
				t.Errorf("gate exited 0, want an error; output:\n%s%s", got.stdout, got.stderr)
			}
			if !s.wantErr && got.err != nil {
				t.Errorf("gate exited non-zero: %v\n--- stdout ---\n%s\n--- stderr ---\n%s", got.err, got.stdout, got.stderr)
			}
		})
	}
}

// TestRecordedFixturesAreFromTheRealRepository keeps the fixtures honest: the
// base scenario has to describe a real pull request from this repository, or
// the tests above would be exercising a fiction.
func TestRecordedFixturesAreFromTheRealRepository(t *testing.T) {
	f := loadFixtures(t)

	if f.pr.Number != 517 || f.pr.User.Login == "" || f.pr.Head.SHA == "" {
		t.Errorf("pr.json does not describe PR #%d with an author and a head SHA: number=%d author=%q sha=%q",
			517, f.pr.Number, f.pr.User.Login, f.pr.Head.SHA)
	}
	if f.pr.State != "open" {
		t.Errorf("pr.json state = %q, want \"open\" (a merged or closed PR would exercise a different branch)", f.pr.State)
	}
	if len(f.files) == 0 || f.files[0].Filename != "server/index.html" {
		t.Errorf("files.json = %+v, want the recorded change to server/index.html", f.files)
	}
	if f.issue.Number != 302 || !strings.Contains(f.issue.Body, "`server/index.html`") {
		t.Errorf("issue.json does not describe #%d naming server/index.html (body %d bytes)", f.issue.Number, len(f.issue.Body))
	}
	if len(f.checks.CheckRuns) < 2 {
		t.Errorf("check-runs.json has %d runs; the gate's self-exclusion needs at least two", len(f.checks.CheckRuns))
	}
	// The reason this issue exists: the recorded verdict carries no
	// parseable outcome.
	for _, c := range f.comments {
		if strings.Contains(c.User.Login, "coderabbit") &&
			(strings.Contains(strings.ToLower(c.Body), "actionable comments posted") ||
				strings.Contains(strings.ToLower(c.Body), "no actionable comments")) {
			t.Error("the recorded CodeRabbit comment now carries a verdict; the coderabbit-unparseable scenario no longer describes reality")
		}
	}
}
