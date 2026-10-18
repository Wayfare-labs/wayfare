package wayfare

import (
	"os"
	"strings"
	"testing"
)

// ciDataDeploy pins the workflow that redeploys the hosted instance when a
// measurement commit lands on main.
//
// Freshness advances by redeploy, not by the scheduler (docs/embedded-history.md),
// so data/ must be a deploy trigger. The deploy hook is a repository secret and
// must be called (referenced) but never echoed or printed to the job log.
func TestDataCommitTriggersRenderDeploy(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/deploy.yml")
	if err != nil {
		t.Fatalf("reading the deploy workflow: %v", err)
	}
	workflow := string(raw)
	flat := normalise(workflow)

	// A push to data/ on the default branch must start the redeploy. normalise
	// strips quotes, so the bare path is what remains in `flat`. `on:` and
	// `push:` sit on separate lines in YAML, so check them individually.
	if !strings.Contains(flat, "on:") ||
		!strings.Contains(flat, "push:") ||
		!strings.Contains(flat, "branches:[main]") ||
		!strings.Contains(flat, "paths:") ||
		!strings.Contains(flat, "data/**") {
		t.Error(".github/workflows/deploy.yml does not trigger on pushes to data/** on main")
	}

	// The deploy hook must come from a secret so it is never committed in plaintext.
	// GitHub Actions syntax uses dots: ${{ secrets.RENDER_DEPLOY_HOOK }}.
	if !strings.Contains(flat, "secrets.RENDER_DEPLOY_HOOK") {
		t.Error("the deploy workflow does not call the RENDER_DEPLOY_HOOK secret")
	}

	// The secret may be referenced (to call it) but must not be echoed or
	// printed: any line naming the secret must not also emit it to stdout.
	for _, line := range strings.Split(flat, "\n") {
		if strings.Contains(line, "secrets.RENDER_DEPLOY_HOOK") &&
			(strings.Contains(line, "echo") || strings.Contains(line, "printf")) {
			t.Errorf("the deploy hook secret is echoed to the log: %s", line)
		}
	}
}

// ciGovulncheck pins the source-level vulnerability job.
//
// It must run on pull requests and on a weekly schedule, install govulncheck
// at a pinned version, and fail on a vulnerability reachable from this code.
func TestGovulncheckRunsOnPRsAndWeekly(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/govulncheck.yml")
	if err != nil {
		t.Fatalf("reading the govulncheck workflow: %v", err)
	}
	flat := normalise(string(raw))

	// Runs on pull requests against main.
	if !strings.Contains(flat, "pull_request:") ||
		!strings.Contains(flat, "branches:[main]") {
		t.Error(".github/workflows/govulncheck.yml does not run on pull requests to main")
	}

	// Runs on a weekly schedule.
	if !strings.Contains(flat, "schedule:") || !strings.Contains(flat, "cron:") {
		t.Error(".github/workflows/govulncheck.yml has no weekly schedule")
	}

	// Installs govulncheck at a pinned version.
	if !strings.Contains(flat, "golang.org/x/vuln/cmd/govulncheck@v1.9.0") {
		t.Error("the govulncheck job does not install a pinned govulncheck version")
	}

	// Runs against the module source; govulncheck exits non-zero on a
	// reachable vulnerability, failing the job.
	if !strings.Contains(flat, "govulncheck./...") {
		t.Error("the govulncheck job does not run govulncheck ./...")
	}
}
