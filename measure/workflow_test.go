package measure

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Issues #555, #556, #557, #558 — exercise the measure workflow deliberately.
//
// Every test here runs a real step out of .github/workflows/measure.yml under
// bash. The GitHub side is a fake `gh` that answers from fixtures and logs
// what it was asked to do; the git side is real git against a bare repository
// in a temporary directory. Nothing reaches the network.

const (
	workflowRel = "../.github/workflows/measure.yml"
	repoSlug    = "Wayfare-labs/wayfare"
	branch      = "automation/measure"
	alertMarker = "<!-- measure-alert -->"
	runURL      = "https://github.com/" + repoSlug + "/actions/runs/39063904218"
	thisRun     = "39063904218"
)

// --- extracting a step from the workflow ------------------------------------

// stepScript returns the `run:` block of the named step. A step that is not
// found, or found without a script, is a failure rather than a skip: these
// tests must not quietly stop covering a step somebody renamed.
func stepScript(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile(workflowRel)
	if err != nil {
		t.Fatalf("reading %s: %v", workflowRel, err)
	}
	lines := strings.Split(string(raw), "\n")

	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

	start, stepIndent := -1, 0
	for i, l := range lines {
		if strings.TrimSpace(l) == "- name: "+name {
			start, stepIndent = i, indentOf(l)
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no step named %q", workflowRel, name)
	}

	runAt := -1
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if indentOf(l) <= stepIndent && strings.HasPrefix(strings.TrimSpace(l), "- ") {
			break // the next step began
		}
		if strings.TrimSpace(l) == "run: |" {
			runAt = i
			break
		}
	}
	if runAt < 0 {
		t.Fatalf("step %q in %s has no `run: |` block to exercise", name, workflowRel)
	}

	bodyIndent, body := -1, []string(nil)
	for _, l := range lines[runAt+1:] {
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
			continue
		}
		n := indentOf(l)
		if bodyIndent < 0 {
			bodyIndent = n
		}
		if n < bodyIndent {
			break
		}
		body = append(body, l[bodyIndent:])
	}
	script := strings.Join(body, "\n")

	// A GitHub expression inside a `run:` block would be substituted before
	// bash ever saw it, so the harness cannot run the step honestly with one
	// present. Steps pass their values through `env:` instead.
	if strings.Contains(script, "${{") {
		t.Fatalf("step %q uses a ${{ }} expression inside its run block; pass it through env: so it can be exercised:\n%s", name, script)
	}
	return script
}

// --- the fake GitHub CLI ----------------------------------------------------

// fakeGH answers reads from the scenario's fixtures, records every invocation,
// and copies out every --body-file it is handed so a test can assert what the
// step actually wrote. Creation of a pull request can be made to fail with the
// exact message the live API returned on every run between 2026-08-22 and
// 2026-10-17.
const fakeGH = `#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${MEASURE_CALLS:?MEASURE_CALLS must name the call log}"

prev=""
for arg in "$@"; do
  if [ "$prev" = "--body-file" ] && [ -f "$arg" ]; then
    cat "$arg" >>"${MEASURE_BODIES:?MEASURE_BODIES must name the body log}"
  fi
  prev="$arg"
done

jq_over() {
  local src="$MEASURE_FIXTURES/$1"; shift
  local expr="$1"
  if [ ! -f "$src" ]; then
    echo "gh (offline): missing fixture $src" >&2
    exit 1
  fi
  if [ -n "$expr" ]; then jq -r "$expr" "$src"; else cat "$src"; fi
}

read_jq() {
  local expr=""
  while [ $# -gt 0 ]; do
    case "$1" in --jq) shift; expr="${1:-}" ;; esac
    shift
  done
  printf '%s' "$expr"
}

cmd="${1:-}"; shift || true

case "$cmd" in
  pr)
    sub="${1:-}"; shift || true
    case "$sub" in
      list) jq_over prs.json "$(read_jq "$@")"; exit 0 ;;
      view) jq_over pr.json "$(read_jq "$@")"; exit 0 ;;
      create)
        if [ "${MEASURE_PR_CREATE_FAILS:-0}" = "1" ]; then
          echo "pull request create failed: GraphQL: GitHub Actions is not permitted to create or approve pull requests (createPullRequest)" >&2
          exit 1
        fi
        echo "https://github.com/wayfare/pull/999"
        exit 0 ;;
      *) exit 0 ;;
    esac ;;
  issue) exit 0 ;;
  api) ;;
  *) exit 0 ;;
esac

path=""; expr=""
while [ $# -gt 0 ]; do
  case "$1" in
    --paginate) ;;
    --jq) shift; expr="${1:-}" ;;
    *) [ -n "$path" ] || path="$1" ;;
  esac
  shift
done

bare="${path%%\?*}"
rel="${bare#repos/*/*/}"
case "$rel" in
  actions/workflows/*/runs) jq_over runs.json "$expr" ;;
  issues)                   jq_over issues.json "$expr" ;;
  issues/*)                 jq_over issue.json "$expr" ;;
  *) echo "gh (offline): no fixture for $path" >&2; exit 1 ;;
esac
`

// fakeGo stands in for the toolchain in the git tests. The only thing the
// workflow asks of it is a verification verdict, and that verdict is what
// these tests vary; runstore's own tests cover the verifier itself.
const fakeGo = `#!/usr/bin/env bash
printf 'go %s\n' "$*" >>"${MEASURE_CALLS:?}"
exit "${FAKE_GO_EXIT:-0}"
`

// --- running a step ---------------------------------------------------------

type step struct {
	name     string            // the workflow step to run
	workdir  string            // where bash runs; a temp dir when empty
	fixtures map[string]string // fixture file -> JSON body
	env      map[string]string
	withGo   bool // install the toolchain stand-in
	failGo   bool // and make its verification fail
}

type stepResult struct {
	stdout, stderr string
	calls          []string
	bodies         string
	env            map[string]string // what the step appended to $GITHUB_ENV
	outputs        map[string]string // what it appended to $GITHUB_OUTPUT
	err            error
}

func (r stepResult) called(substr string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// all returns stdout and stderr together: a step is allowed to report on
// either, and a test asserting on its reasoning should not care which.
func (r stepResult) all() string { return r.stdout + r.stderr }

func runStep(t *testing.T, s step) stepResult {
	t.Helper()

	root := t.TempDir()
	fixtures := filepath.Join(root, "fixtures")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{fixtures, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("creating %s: %v", d, err)
		}
	}
	for name, body := range s.fixtures {
		if err := os.WriteFile(filepath.Join(fixtures, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing fixture %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatalf("installing the fake gh: %v", err)
	}
	if s.withGo {
		if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fakeGo), 0o755); err != nil {
			t.Fatalf("installing the toolchain stand-in: %v", err)
		}
	}

	scriptPath := filepath.Join(root, "step.sh")
	if err := os.WriteFile(scriptPath, []byte(stepScript(t, s.name)), 0o600); err != nil {
		t.Fatalf("writing the extracted step: %v", err)
	}

	calls := filepath.Join(root, "calls.log")
	bodies := filepath.Join(root, "bodies.log")
	ghEnv := filepath.Join(root, "github_env")
	ghOut := filepath.Join(root, "github_output")
	runnerTemp := filepath.Join(root, "runner_temp")
	if err := os.MkdirAll(runnerTemp, 0o755); err != nil {
		t.Fatalf("creating RUNNER_TEMP: %v", err)
	}

	env := map[string]string{
		"PATH":                bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"MEASURE_FIXTURES":    fixtures,
		"MEASURE_CALLS":       calls,
		"MEASURE_BODIES":      bodies,
		"REPO":                repoSlug,
		"MEASURE_BRANCH":      branch,
		"ALERT_MARKER":        alertMarker,
		"ALERT_THRESHOLD":     "2",
		"GH_TOKEN":            "test-token",
		"RUN_URL":             runURL,
		"THIS_RUN":            thisRun,
		"GITHUB_RUN_ID":       thisRun,
		"GITHUB_ENV":          ghEnv,
		"GITHUB_OUTPUT":       ghOut,
		"RUNNER_TEMP":         runnerTemp,
		"GIT_CONFIG_GLOBAL":   filepath.Join(root, "gitconfig-none"),
		"GIT_CONFIG_SYSTEM":   filepath.Join(root, "gitconfig-none"),
		"GIT_TERMINAL_PROMPT": "0",
	}
	if s.failGo {
		env["FAKE_GO_EXIT"] = "1"
	}
	for k, v := range s.env {
		env[k] = v
	}

	workdir := s.workdir
	if workdir == "" {
		workdir = root
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = workdir
	cmd.Env = mergeEnv(env)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	return stepResult{
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		calls:   readLines(calls),
		bodies:  readFile(bodies),
		env:     parseKeyFile(t, ghEnv),
		outputs: parseKeyFile(t, ghOut),
		err:     err,
	}
}

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

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readLines(path string) []string {
	s := strings.TrimRight(readFile(path), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// parseKeyFile reads the $GITHUB_ENV / $GITHUB_OUTPUT format, including the
// heredoc form a multi-line value uses.
func parseKeyFile(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	lines := readLines(path)
	for i := 0; i < len(lines); i++ {
		k, v, ok := strings.Cut(lines[i], "=")
		if ok {
			out[k] = v
			continue
		}
		k, delim, ok := strings.Cut(lines[i], "<<")
		if !ok {
			continue
		}
		var block []string
		for i++; i < len(lines) && lines[i] != delim; i++ {
			block = append(block, lines[i])
		}
		out[k] = strings.Join(block, "\n")
	}
	return out
}

// --- #558: alerting ---------------------------------------------------------

// runsFixture builds an Actions API page, newest first. The failing run that
// is alerting is excluded by the step itself, so it is never in this list.
func runsFixture(conclusions ...string) string {
	var b strings.Builder
	b.WriteString(`{"workflow_runs":[`)
	for i, c := range conclusions {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"conclusion":%q}`, 100+i, c)
	}
	b.WriteString("]}")
	return b.String()
}

func openAlert(number int, body string) string {
	return fmt.Sprintf(`[{"number":%d,"body":%q}]`, number, body)
}

const priorAlertBody = alertMarker + "\nThe scheduled `measure` workflow has failed **2 time(s) in a row**.\n\n### Failing runs\n\n- https://github.com/" + repoSlug + "/actions/runs/38992725069\n"

func alertStep(t *testing.T, runs, issues, issue string) stepResult {
	t.Helper()
	return runStep(t, step{
		name: "Alert on repeated failure",
		fixtures: map[string]string{
			"runs.json":   runs,
			"issues.json": issues,
			"issue.json":  issue,
		},
	})
}

// A single red run is not yet a dead pipeline. The alert exists because
// twelve failures went unnoticed, not because one upstream hiccup deserves an
// issue — and an alert that fires on the first flake is the one people mute.
func TestAlertStaysSilentBelowTheThreshold(t *testing.T) {
	got := alertStep(t, runsFixture("success", "success"), "[]", "{}")
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if !strings.Contains(got.stdout, "failed 1 time(s) in a row") {
		t.Errorf("the streak was not reported as 1:\n%s", got.stdout)
	}
	if got.called("issue create") || got.called("issue edit") {
		t.Errorf("an issue was opened below the threshold:\n%s", strings.Join(got.calls, "\n"))
	}
}

func TestAlertOpensOneIssueAtTheThreshold(t *testing.T) {
	got := alertStep(t, runsFixture("failure", "success"), "[]", "{}")
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if !got.called("issue create") {
		t.Fatalf("no issue was opened at the threshold:\n%s", strings.Join(got.calls, "\n"))
	}
	if got.called("issue edit") {
		t.Errorf("an issue was edited when none existed:\n%s", strings.Join(got.calls, "\n"))
	}
	for _, want := range []string{alertMarker, "failed **2 time(s) in a row**", runURL} {
		if !strings.Contains(got.bodies, want) {
			t.Errorf("the issue body is missing %q:\n%s", want, got.bodies)
		}
	}
}

// "Consecutive failures update one issue." The marker is what makes that
// true: a second issue would be the alert becoming the noise it replaced.
func TestAlertUpdatesTheOneOpenIssue(t *testing.T) {
	got := alertStep(t,
		runsFixture("failure", "failure", "success"),
		openAlert(77, priorAlertBody),
		fmt.Sprintf(`{"body":%q}`, priorAlertBody))
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if got.called("issue create") {
		t.Errorf("a second issue was opened while one was already tracking this:\n%s", strings.Join(got.calls, "\n"))
	}
	if !got.called("issue edit 77") {
		t.Fatalf("issue 77 was not updated:\n%s", strings.Join(got.calls, "\n"))
	}
	if !strings.Contains(got.bodies, "failed **3 time(s) in a row**") {
		t.Errorf("the updated body does not carry the new count:\n%s", got.bodies)
	}
	// The run that was already listed must survive the update, or the issue
	// forgets the history it exists to carry.
	if !strings.Contains(got.bodies, "runs/38992725069") {
		t.Errorf("the earlier run was dropped from the body:\n%s", got.bodies)
	}
	if !strings.Contains(got.bodies, runURL) {
		t.Errorf("this run was not appended to the body:\n%s", got.bodies)
	}
}

// A success is the only thing that resets the count. Were it otherwise the
// streak would restart every time GitHub dropped a scheduled run.
func TestAlertCountsOnlyConsecutiveFailures(t *testing.T) {
	got := alertStep(t,
		runsFixture("failure", "failure", "success", "failure", "failure"),
		"[]", "{}")
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if !strings.Contains(got.stdout, "failed 3 time(s) in a row") {
		t.Errorf("the failures before the last success were counted:\n%s", got.stdout)
	}
}

// A cancelled run is neither a failure nor evidence of recovery. Counting it
// would overstate the streak; treating it as a reset would hide a pipeline
// that has been dead throughout.
func TestAlertStepsOverCancelledRuns(t *testing.T) {
	got := alertStep(t,
		runsFixture("cancelled", "failure", "skipped", "failure", "success"),
		"[]", "{}")
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if !strings.Contains(got.stdout, "failed 3 time(s) in a row") {
		t.Errorf("cancelled or skipped runs were counted or treated as a reset:\n%s", got.stdout)
	}
}

// Pull requests are issues to this endpoint. A measurement pull request whose
// body quotes the marker must not be mistaken for the tracking issue and
// edited into an alert.
func TestAlertIgnoresPullRequestsCarryingTheMarker(t *testing.T) {
	issues := fmt.Sprintf(
		`[{"number":42,"pull_request":{"url":"x"},"body":%q},{"number":77,"body":%q}]`,
		alertMarker+" quoted in a pull request", priorAlertBody)
	got := alertStep(t, runsFixture("failure"), issues, fmt.Sprintf(`{"body":%q}`, priorAlertBody))
	if got.err != nil {
		t.Fatalf("the alert step failed: %v\n%s", got.err, got.all())
	}
	if !got.called("issue edit 77") {
		t.Errorf("the tracking issue was not the one updated:\n%s", strings.Join(got.calls, "\n"))
	}
	if got.called("issue edit 42") {
		t.Errorf("a pull request was edited as though it were the tracking issue:\n%s", strings.Join(got.calls, "\n"))
	}
}

func TestSuccessClosesTheOpenAlert(t *testing.T) {
	got := runStep(t, step{
		name: "Clear the failure alert",
		fixtures: map[string]string{
			"issues.json": openAlert(77, priorAlertBody),
		},
	})
	if got.err != nil {
		t.Fatalf("the clear step failed: %v\n%s", got.err, got.all())
	}
	if !got.called("issue comment 77") {
		t.Errorf("the recovery was not recorded on the issue:\n%s", strings.Join(got.calls, "\n"))
	}
	if !got.called("issue close 77") {
		t.Errorf("the alert was not closed on a successful run:\n%s", strings.Join(got.calls, "\n"))
	}
}

func TestSuccessIsQuietWhenNothingIsTracking(t *testing.T) {
	got := runStep(t, step{
		name:     "Clear the failure alert",
		fixtures: map[string]string{"issues.json": "[]"},
	})
	if got.err != nil {
		t.Fatalf("the clear step failed: %v\n%s", got.err, got.all())
	}
	if got.called("issue close") || got.called("issue comment") {
		t.Errorf("a healthy run touched an issue that does not exist:\n%s", strings.Join(got.calls, "\n"))
	}
}

// --- #555: the pull request -------------------------------------------------

func prStep(t *testing.T, prs, pr string, createFails bool) stepResult {
	t.Helper()
	env := map[string]string{
		"TOTAL":     "5",
		"UNMERGED":  "4",
		"CORRIDORS": "- `data/USDC-NGNC.ndjson` — 5 record(s), newest `2026-10-18T06:00:00Z`",
	}
	if createFails {
		env["MEASURE_PR_CREATE_FAILS"] = "1"
	}
	return runStep(t, step{
		name:     "Open or update the measurement pull request",
		fixtures: map[string]string{"prs.json": prs, "pr.json": pr},
		env:      env,
	})
}

func TestPullRequestIsOpenedWhenNoneIs(t *testing.T) {
	got := prStep(t, "[]", "{}", false)
	if got.err != nil {
		t.Fatalf("the pull request step failed: %v\n%s", got.err, got.all())
	}
	if !got.called("pr create") {
		t.Fatalf("no pull request was opened:\n%s", strings.Join(got.calls, "\n"))
	}
	for _, want := range []string{"4 of which are not yet on `main`", runURL} {
		if !strings.Contains(got.bodies, want) {
			t.Errorf("the pull request body is missing %q:\n%s", want, got.bodies)
		}
	}
}

// One standing pull request, not one per sweep. A second would put the same
// records in front of a reviewer twice and make the merge order matter.
func TestPullRequestIsUpdatedWhenOneIsOpen(t *testing.T) {
	prior := "### Sweeps on this branch\n\n- https://github.com/" + repoSlug + "/actions/runs/38992725069\n"
	got := prStep(t, `[{"number":42}]`, fmt.Sprintf(`{"body":%q}`, prior), false)
	if got.err != nil {
		t.Fatalf("the pull request step failed: %v\n%s", got.err, got.all())
	}
	if got.called("pr create") {
		t.Errorf("a second pull request was opened for the same branch:\n%s", strings.Join(got.calls, "\n"))
	}
	if !got.called("pr edit 42") {
		t.Fatalf("the open pull request was not updated:\n%s", strings.Join(got.calls, "\n"))
	}
	if !strings.Contains(got.bodies, "runs/38992725069") {
		t.Errorf("the sweep already listed was dropped from the body:\n%s", got.bodies)
	}
	if !strings.Contains(got.bodies, runURL) {
		t.Errorf("this sweep was not added to the body:\n%s", got.bodies)
	}
}

// The failure that stopped the pipeline for eight weeks. The run must go red
// — the records reached the branch but nothing reaches main — and the error
// has to name the fix rather than leaving the next person to rediscover it.
func TestRefusedPullRequestFailsLoudlyAndNamesTheFix(t *testing.T) {
	got := prStep(t, "[]", "{}", true)
	if got.err == nil {
		t.Fatalf("a refused pull request was reported as a successful run:\n%s", got.all())
	}
	out := got.all()
	if !strings.Contains(out, "not permitted to create or approve pull requests") {
		t.Errorf("the refusal from the API was swallowed:\n%s", out)
	}
	for _, want := range []string{"MEASURE_TOKEN", "safe on " + branch, "docs/deployment.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("the error does not mention %q:\n%s", want, out)
		}
	}
}

// --- #556: the branch accumulates -------------------------------------------

type repo struct {
	t      *testing.T
	bare   string
	record int
}

func (r *repo) git(dir string, args ...string) string {
	r.t.Helper()
	full := append([]string{
		"-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(map[string]string{
		"GIT_CONFIG_GLOBAL":   filepath.Join(r.bare, "..", "gitconfig-none"),
		"GIT_CONFIG_SYSTEM":   filepath.Join(r.bare, "..", "gitconfig-none"),
		"GIT_TERMINAL_PROMPT": "0",
	})
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// record writes one plausible NDJSON line. Only its shape matters here: the
// steps count lines and read recorded_at out of the newest one.
func (r *repo) line() string {
	r.record++
	return fmt.Sprintf(
		`{"version":1,"seq":%d,"recorded_at":"2026-10-18T%02d:00:00Z","corridor":"USDC-NGNC"}`,
		r.record, r.record)
}

// newRepo builds a bare origin holding a main branch with one committed
// record, which is exactly the state the live repository is in.
func newRepo(t *testing.T) *repo {
	t.Helper()
	root := t.TempDir()
	r := &repo{t: t, bare: filepath.Join(root, "origin.git")}

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(filepath.Join(seed, "data"), 0o755); err != nil {
		t.Fatalf("creating the seed tree: %v", err)
	}
	r.git(root, "init", "--bare", "-b", "main", r.bare)
	r.git(root, "init", "-b", "main", seed)
	if err := os.WriteFile(filepath.Join(seed, "data", "USDC-NGNC.ndjson"), []byte(r.line()+"\n"), 0o644); err != nil {
		t.Fatalf("writing the seed record: %v", err)
	}
	r.git(seed, "add", "-A")
	r.git(seed, "commit", "-m", "seed")
	r.git(seed, "remote", "add", "origin", r.bare)
	r.git(seed, "push", "origin", "main")
	return r
}

func (r *repo) clone() string {
	r.t.Helper()
	dir := r.t.TempDir()
	work := filepath.Join(dir, "work")
	r.git(dir, "clone", r.bare, work)
	return work
}

func (r *repo) show(ref, path string) string {
	r.t.Helper()
	return r.git(r.bare, "show", ref+":"+path)
}

func (r *repo) lines(ref, path string) int {
	r.t.Helper()
	return len(strings.Split(strings.TrimRight(r.show(ref, path), "\n"), "\n"))
}

// sweep performs one run of the workflow: adopt, measure, push. The measure
// step is the append — what the real `wayfared -once` does to the file.
func (r *repo) sweep(t *testing.T, opts ...func(*repo, string)) (adopt, push stepResult) {
	t.Helper()
	work := r.clone()

	adopt = runStep(t, step{name: "Adopt any unmerged measurements", workdir: work, withGo: true})
	if adopt.err != nil {
		return adopt, stepResult{}
	}

	f := filepath.Join(work, "data", "USDC-NGNC.ndjson")
	existing, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("reading the adopted store: %v", err)
	}
	if err := os.WriteFile(f, append(existing, []byte(r.line()+"\n")...), 0o644); err != nil {
		t.Fatalf("appending the sweep: %v", err)
	}

	for _, o := range opts {
		o(r, work)
	}

	push = runStep(t, step{
		name:    "Commit and push the sweep",
		workdir: work,
		env:     adopt.env, // $GITHUB_ENV carries MAIN_RECORDS and PENDING_SHA
	})
	return adopt, push
}

// The headline of #556: a run that cannot be merged must not cost the
// previous run's measurement. Two sweeps, no merge in between, both on the
// branch afterwards.
func TestTwoSweepsWithoutAMergeBothSurvive(t *testing.T) {
	r := newRepo(t)

	if _, push := r.sweep(t); push.err != nil {
		t.Fatalf("the first sweep failed to push: %v\n%s", push.err, push.all())
	}
	if got := r.lines(branch, "data/USDC-NGNC.ndjson"); got != 2 {
		t.Fatalf("after one sweep the branch holds %d record(s), want 2", got)
	}

	adopt, push := r.sweep(t)
	if push.err != nil {
		t.Fatalf("the second sweep failed to push: %v\n%s", push.err, push.all())
	}
	if !strings.Contains(adopt.stdout, "adopted 2 record(s)") {
		t.Errorf("the second run did not adopt the pending sweep:\n%s", adopt.all())
	}
	if got := r.lines(branch, "data/USDC-NGNC.ndjson"); got != 3 {
		t.Fatalf("after two sweeps the branch holds %d record(s), want 3 — a sweep was discarded", got)
	}
	if got := r.lines("main", "data/USDC-NGNC.ndjson"); got != 1 {
		t.Errorf("main gained %d record(s) without a merge, want 1", got)
	}
	// One commit ahead of main, as before: accumulating records must not mean
	// accumulating commits for a reviewer to read.
	if ahead := r.git(r.bare, "rev-list", "--count", "main.."+branch); ahead != "1" {
		t.Errorf("the branch is %s commits ahead of main, want 1", ahead)
	}
	if push.outputs["unmerged"] != "2" {
		t.Errorf("the pull request would claim %q unmerged record(s), want 2", push.outputs["unmerged"])
	}
}

// Once the measurement pull request merges, the branch is rebuilt on the new
// main and keeps counting from there.
func TestASweepAfterAMergeRebasesOntoMain(t *testing.T) {
	r := newRepo(t)
	if _, push := r.sweep(t); push.err != nil {
		t.Fatalf("the first sweep failed to push: %v\n%s", push.err, push.all())
	}

	// Squash the branch into main, the way the project merges.
	merge := r.clone()
	r.git(merge, "fetch", "origin", branch)
	r.git(merge, "merge", "--squash", "FETCH_HEAD")
	r.git(merge, "commit", "-m", "measure: corridor sweep")
	r.git(merge, "push", "origin", "main")

	adopt, push := r.sweep(t)
	if push.err != nil {
		t.Fatalf("the sweep after the merge failed to push: %v\n%s", push.err, push.all())
	}
	if adopt.env["MAIN_RECORDS"] != "2" {
		t.Errorf("the merged records were not counted as main's: %q", adopt.env["MAIN_RECORDS"])
	}
	if got := r.lines(branch, "data/USDC-NGNC.ndjson"); got != 3 {
		t.Fatalf("the branch holds %d record(s) after the merge, want 3", got)
	}
	if ahead := r.git(r.bare, "rev-list", "--count", "main.."+branch); ahead != "1" {
		t.Errorf("the branch is %s commits ahead of the merged main, want 1", ahead)
	}
	if push.outputs["unmerged"] != "1" {
		t.Errorf("the pull request would claim %q unmerged record(s), want 1", push.outputs["unmerged"])
	}
}

// The lease is what makes the force push safe. A branch that moved after this
// run read it must stop the push, not be overwritten by it — the whole point
// of adopting the branch is defeated if a racing writer's records are lost.
func TestAMovedBranchIsNotOverwritten(t *testing.T) {
	r := newRepo(t)
	if _, push := r.sweep(t); push.err != nil {
		t.Fatalf("the first sweep failed to push: %v\n%s", push.err, push.all())
	}
	before := r.git(r.bare, "rev-parse", branch)

	// Somebody else advances the branch between this run's fetch and its push.
	_, push := r.sweep(t, func(r *repo, _ string) {
		other := r.clone()
		r.git(other, "fetch", "origin", branch)
		r.git(other, "checkout", "-B", branch, "FETCH_HEAD")
		f := filepath.Join(other, "data", "USDC-NGNC.ndjson")
		existing, err := os.ReadFile(f)
		if err != nil {
			r.t.Fatalf("reading the racing clone: %v", err)
		}
		if err := os.WriteFile(f, append(existing, []byte(r.line()+"\n")...), 0o644); err != nil {
			r.t.Fatalf("writing the racing sweep: %v", err)
		}
		r.git(other, "add", "-A")
		r.git(other, "commit", "-m", "a concurrent sweep")
		r.git(other, "push", "origin", branch)
	})

	if push.err == nil {
		t.Fatalf("the push overwrote a branch that had moved:\n%s", push.all())
	}
	after := r.git(r.bare, "rev-parse", branch)
	if after == before {
		t.Fatal("the racing writer never landed; the test proves nothing")
	}
	if got := r.lines(branch, "data/USDC-NGNC.ndjson"); got != 3 {
		t.Errorf("the racing writer's records were lost: the branch holds %d, want 3", got)
	}
}

// A chain that does not verify is a hard stop. Starting over from main would
// be the data loss of #556 wearing a different hat, and appending would bury
// the break under valid records.
func TestABrokenInheritedChainStopsTheRun(t *testing.T) {
	r := newRepo(t)
	if _, push := r.sweep(t); push.err != nil {
		t.Fatalf("the first sweep failed to push: %v\n%s", push.err, push.all())
	}

	work := r.clone()
	adopt := runStep(t, step{
		name:    "Adopt any unmerged measurements",
		workdir: work,
		withGo:  true,
		failGo:  true,
	})
	if adopt.err == nil {
		t.Fatalf("a branch whose chain does not verify was adopted anyway:\n%s", adopt.all())
	}
	if !strings.Contains(adopt.all(), "does not verify") {
		t.Errorf("the run did not say why it stopped:\n%s", adopt.all())
	}
	if !strings.Contains(adopt.all(), "must never be thrown away") {
		t.Errorf("the error does not warn against deleting the branch to get past it:\n%s", adopt.all())
	}
}

// With no branch on the remote there is nothing to inherit, and the sweep
// starts from main — the state the repository is in today.
func TestTheFirstSweepStartsFromMain(t *testing.T) {
	r := newRepo(t)
	work := r.clone()

	adopt := runStep(t, step{name: "Adopt any unmerged measurements", workdir: work, withGo: true})
	if adopt.err != nil {
		t.Fatalf("the first sweep could not start: %v\n%s", adopt.err, adopt.all())
	}
	if !strings.Contains(adopt.stdout, "no "+branch+" on origin") {
		t.Errorf("an absent branch was not reported as such:\n%s", adopt.all())
	}
	if adopt.env["PENDING_SHA"] != "" {
		t.Errorf("a lease was recorded against a branch that does not exist: %q", adopt.env["PENDING_SHA"])
	}
	if adopt.env["MAIN_RECORDS"] != "1" {
		t.Errorf("main's records were miscounted: %q", adopt.env["MAIN_RECORDS"])
	}
	if adopt.called("go run") {
		t.Errorf("the verifier ran with no inherited chain to verify:\n%s", strings.Join(adopt.calls, "\n"))
	}
}

// `cat data/*.ndjson` on an unmatched glob exits non-zero, and under
// `pipefail` that would end the step with no explanation at all. No corridor
// files is a count of zero — an odd state to be in, but one the run should
// say out loud rather than die of.
func TestAnEmptyStoreIsCountedNotCrashedOn(t *testing.T) {
	r := newRepo(t)
	work := r.clone()
	if err := os.Remove(filepath.Join(work, "data", "USDC-NGNC.ndjson")); err != nil {
		t.Fatalf("emptying the store: %v", err)
	}

	adopt := runStep(t, step{name: "Adopt any unmerged measurements", workdir: work, withGo: true})
	if adopt.err != nil {
		t.Fatalf("an empty store ended the step: %v\n%s", adopt.err, adopt.all())
	}
	if adopt.env["MAIN_RECORDS"] != "0" {
		t.Errorf("MAIN_RECORDS=%q, want 0", adopt.env["MAIN_RECORDS"])
	}
}

// A sweep that measured nothing new must not push, open or touch anything.
func TestASweepWithNoNewRecordsPushesNothing(t *testing.T) {
	r := newRepo(t)
	work := r.clone()

	adopt := runStep(t, step{name: "Adopt any unmerged measurements", workdir: work, withGo: true})
	if adopt.err != nil {
		t.Fatalf("adopting failed: %v\n%s", adopt.err, adopt.all())
	}
	push := runStep(t, step{name: "Commit and push the sweep", workdir: work, env: adopt.env})
	if push.err != nil {
		t.Fatalf("the push step failed: %v\n%s", push.err, push.all())
	}
	if push.outputs["pushed"] != "false" {
		t.Errorf("pushed=%q, want false", push.outputs["pushed"])
	}
	refs := r.git(r.bare, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if strings.Contains(refs, branch) {
		t.Errorf("a branch was created for a sweep that produced nothing: %s", refs)
	}
}
