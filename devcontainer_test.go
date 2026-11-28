package wayfare

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The devcontainer (#664) reads every version it installs from the workflows
// rather than restating it (#467). These tests keep that true: if a workflow
// is reformatted so .devcontainer/install-toolchain.sh can no longer read it,
// or a version is written into the container definition directly, the build
// goes red here instead of a contributor's container silently drifting.
//
// Source-text assertions, like workflow_pins_test.go: a YAML or JSON-with-
// comments parser would be a third-party dependency.

// workflowEnv mirrors install-toolchain.sh's workflow_env: a two-space
// indented, double-quoted top-level env entry.
func workflowEnv(t *testing.T, path, key string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(key) + `: "([^"]+)"[ \t]*$`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, m[1])
	}
	return out
}

func TestDevcontainerVersionSourcesAreReadable(t *testing.T) {
	for _, c := range []struct{ file, key string }{
		{".github/workflows/ci.yml", "GO_VERSION"},
		{".github/workflows/qa-accessibility.yml", "NODE_VERSION"},
	} {
		got := workflowEnv(t, c.file, c.key)
		if len(got) != 1 {
			t.Errorf("%s: want exactly one `  %s: \"...\"` line for install-toolchain.sh to read, found %d",
				c.file, c.key, len(got))
		}
	}

	ci, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	lint := regexp.MustCompile(`(?s)golangci-lint-action@.*?\n\s+version:\s*(v[0-9.]+)\s*\n`).FindSubmatch(ci)
	if lint == nil {
		t.Error("ci.yml: no `version: vX.Y.Z` under golangci-lint-action for install-toolchain.sh to read")
	}
}

// TestDevcontainerGoSatisfiesGoMod: the container sets GOTOOLCHAIN=local, so
// a CI toolchain older than go.mod's go line would fail to build rather than
// quietly download a different one. Fail here first, where the cause is named.
func TestDevcontainerGoSatisfiesGoMod(t *testing.T) {
	ciGo := workflowEnv(t, ".github/workflows/ci.yml", "GO_VERSION")
	if len(ciGo) != 1 {
		t.Skip("covered by TestDevcontainerVersionSourcesAreReadable")
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go ([0-9.]+)\s*$`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod has no go directive")
	}
	if versionLess(ciGo[0], string(m[1])) {
		t.Errorf("ci.yml GO_VERSION %s is older than go.mod's go %s; the devcontainer (GOTOOLCHAIN=local) could not build",
			ciGo[0], m[1])
	}
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.27.4", "1.27.4", false},
		{"1.27.3", "1.27.4", true},
		{"1.27", "1.27.4", true},
		{"1.28.0", "1.27.4", false},
		{"1.9.0", "1.27.4", true},
	} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestDevcontainerRestatesNoVersion: a literal Go or Node version in the
// container definition is a second source of truth, which is what #467 removed.
func TestDevcontainerRestatesNoVersion(t *testing.T) {
	literal := regexp.MustCompile(`(?i)(golang:|go_?version\s*[:=]\s*"?\d|node_?version\s*[:=]\s*"?\d|features/(go|node))`)
	for _, f := range []string{".devcontainer/Dockerfile", ".devcontainer/devcontainer.json"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if literal.MatchString(line) {
				t.Errorf("%s:%d restates a toolchain version: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestDevcontainerContextAllowsEveryCopy: the root .dockerignore excludes
// .github/ and docs/, so the devcontainer has its own allow-list. A COPY whose
// source is not on it fails only when someone builds the container; this
// fails on the next go test instead.
func TestDevcontainerContextAllowsEveryCopy(t *testing.T) {
	ignore, err := os.ReadFile(".devcontainer/Dockerfile.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, line := range strings.Split(string(ignore), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "!"); ok {
			allowed[p] = true
		}
	}

	dockerfile, err := os.ReadFile(".devcontainer/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	copies := 0
	for _, line := range strings.Split(string(dockerfile), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "COPY" {
			continue
		}
		for _, src := range fields[1 : len(fields)-1] {
			copies++
			if _, err := os.Stat(src); err != nil {
				t.Errorf("Dockerfile copies %s, which does not exist", src)
			}
			if !allowed[src] {
				t.Errorf("Dockerfile copies %s, which Dockerfile.dockerignore does not allow", src)
			}
		}
	}
	if copies == 0 {
		t.Error("found no COPY sources; the parser no longer matches the Dockerfile")
	}
}
