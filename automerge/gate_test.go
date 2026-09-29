package automerge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	workflowRel = "../.github/workflows/auto-merge.yml"
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

// workflowScript extracts the `run: |` block of the evaluate step. The
// workflow's `env:` block is deliberately not rendered — those expressions
// become environment variables, which the test sets itself.
func workflowScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(workflowRel)
	if err != nil {
		t.Fatalf("reading %s: %v", workflowRel, err)
	}
	lines := strings.Split(string(raw), "\n")

	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "run: |" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no `run: |` step to exercise", workflowRel)
	}

	indent := -1
	var block []string
	for _, l := range lines[start+1:] {
		if strings.TrimSpace(l) == "" {
			block = append(block, l)
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent < 0 {
			indent = n
		}
		if n < indent {
			break
		}
		block = append(block, l)
	}
	if indent <= 0 {
		t.Fatalf("the run block in %s is not indented; extraction cannot work", workflowRel)
	}

	out := make([]string, len(block))
	for i, l := range block {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out[i] = l[indent:]
	}
	script := strings.Join(out, "\n")

	// Extraction must not silently truncate: a test that passed against a
	// fragment of the gate would be worse than no test.
	for _, want := range []string{"GATE 1", "GATE 7", "gh pr merge", "needs-maintainer-review"} {
		if !strings.Contains(script, want) {
			t.Fatalf("the extracted script is missing %q; it is not the whole gate", want)
		}
	}
	return script
}

var exprRe = regexp.MustCompile(`\$\{\{\s*([^{}]+?)\s*\}\}`)

// render replaces the expressions GitHub replaces before running a step.
// An expression the harness does not know is a failure, not a passthrough:
// a future contributor adding one to the workflow should meet these tests.
func render(t *testing.T, script string, vars map[string]string) string {
	t.Helper()
	out := exprRe.ReplaceAllStringFunc(script, func(m string) string {
		key := strings.TrimSpace(exprRe.FindStringSubmatch(m)[1])
		v, ok := vars[key]
		if !ok {
			t.Fatalf("the workflow uses the unmapped GitHub expression %q; add it to the harness before it can be exercised", key)
		}
		return v
	})
	if strings.Contains(out, "${{") {
		t.Fatalf("an unrendered GitHub expression survived substitution:\n%s", out)
	}
	return out
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
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	ChangedFiles int `json:"changed_files"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

type fileRec struct {
	Filename string `json:"filename"`
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

type commentRec struct {
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
	// vars override the GitHub expressions rendered into the script.
	vars map[string]string
	// env overrides the environment the script runs with.
	env    map[string]string
	mutate func(t *testing.T, f *fixtures)

	stdout    []string // substrings that must appear on stdout
	holds     []string // substrings that must appear among the printed reasons
	merged    bool     // expect `gh pr merge --auto`
	labeled   bool     // expect `gh pr edit --add-label needs-maintainer-review`
	commented bool     // expect `gh pr comment`
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
    *) [ -n "$path" ] || path="$1" ;;
  esac
  shift
done

rel="${path#repos/*/*/}"
case "$rel" in
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

	vars := map[string]string{
		"github.repository":                   repoSlug,
		"github.event.workflow_run.event":     "pull_request",
		"github.event.comment.user.login":     "a-first-time-contributor",
		"github.event.pull_request.number":    prNumber,
		"github.event.issue.number":           "",
		"github.event.issue.pull_request.url": "",
		"github.event.workflow_run.head_sha":  fx.pr.Head.SHA,
		"secrets.GITHUB_TOKEN":                "test-token",
	}
	for k, v := range s.vars {
		vars[k] = v
	}
	script := render(t, workflowScript(t), vars)

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

	callsPath := filepath.Join(dir, "calls.log")
	env := map[string]string{
		"PATH":          bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GATE_FIXTURES": dir,
		"GATE_CALLS":    callsPath,
		"GH_TOKEN":      "test-token",
		"REPO":          repoSlug,
		"EVENT_PR":      prNumber,
		"ISSUE_PR":      "",
		"IS_PR":         "",
		"HEAD_SHA":      fx.pr.Head.SHA,
		"SELF_CHECK":    selfCheck,
	}
	for k, v := range s.env {
		env[k] = v
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = mergeEnv(env)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

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
			// GATE 3, recorded as it actually happened: PR #517's description
			// carries no checklist at all, and the gate held it for exactly
			// this reason.
			name: "checklist-absent",
			mutate: func(t *testing.T, f *fixtures) {
				f.dropHoldMarker()
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"no checklist items are ticked"},
			labeled: true, commented: true,
		},
		{
			// GATE 3 — one box left open is a request for a human to look.
			name: "checklist-unticked",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody + "- [ ] **`make fmt vet test race lint` is clean.**\n"
				f.dropHoldMarker()
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"left unticked"},
			labeled: true, commented: true,
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
			// GATE 6 — the behaviour this exercise actually found. The
			// recorded CodeRabbit comment carries neither of the two verdict
			// phrases the gate parses, because this repository's reviews are
			// the "not configured" summary rather than a verdict. The gate
			// holds rather than guessing, which is the safe direction, and it
			// is why no pull request has ever been auto-merged.
			name: "coderabbit-unparseable",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = tickedBody
				f.dropHoldMarker()
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
			// GATE 7 — with no linked issue there is no stated scope to check
			// against, so the gate cannot verify one.
			name: "no-issue-reference",
			mutate: func(t *testing.T, f *fixtures) {
				f.pr.Body = "A description that never names the issue.\n\n- [x] **Confirmed.**\n"
				f.dropHoldMarker()
			},
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"does not say which issue it closes"},
			labeled: true, commented: true,
		},
		{
			// The hold comment is posted once: a second evaluation of an
			// already-held pull request relabels but stays silent, so a reader
			// is not followed round by the same message. The recorded
			// description (no checklist) is what held it, and the recorded
			// comments already carry the gate's own marker.
			name:    "hold-comment-deduplicated",
			stdout:  []string{"not auto-merging:"},
			holds:   []string{"no checklist items are ticked"},
			labeled: true, commented: false,
		},
		{
			// A CI run that belongs to a merge-queue entry is not a pull
			// request; the gate says so before it touches the API.
			name: "merge-group-run-is-not-a-pr",
			vars: map[string]string{
				"github.event.workflow_run.event": "merge_group",
			},
			stdout:  []string{"CI run came from a merge_group entry; nothing to gate"},
			noCalls: true,
		},
		{
			// The gate's own comment must not re-trigger the gate.
			name: "own-comment-is-ignored",
			vars: map[string]string{
				"github.event.comment.user.login": "github-actions[bot]",
			},
			stdout:  []string{"comment was ours; nothing to re-evaluate"},
			noCalls: true,
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
