package wayfare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// coverage-floors.txt is a policy file, and a policy file that nothing reads is
// a comment. These tests check the two ways this one could stop doing work
// without anybody noticing: a package quietly disappearing from the policy, and
// the CI job that enforces it being removed or disconnected from the script.
// The behavioural tests below run the real script, because an enforcement step
// that is wired up but does not fail is the same failure one layer down.
//
// #666 asked for floors on the packages whose arithmetic publishes the
// project's figures. CONTRIBUTING.md lists the same four as maintainer-owned,
// so the set here is not arbitrary: it is the blast radius.

// maintainerPackages is the set the policy must cover. Listing it here rather
// than deriving it means removing a floor requires editing a second place that
// says why the package mattered, which is the review conversation the change
// should have.
var maintainerPackages = []string{"route", "dex", "refrate", "sep38"}

func TestCoverageFloorPolicyCoversTheMaintainerPackages(t *testing.T) {
	floors, err := readCoverageFloors("coverage-floors.txt")
	if err != nil {
		t.Fatalf("reading coverage-floors.txt: %v", err)
	}

	for _, pkg := range maintainerPackages {
		floor, ok := floors[pkg]
		if !ok {
			t.Errorf("coverage-floors.txt has no floor for %q; the package is "+
				"maintainer-owned (CONTRIBUTING.md) and its numbers are published", pkg)
			continue
		}
		if floor <= 0 || floor >= 100 {
			t.Errorf("floor for %q is %v%%, which cannot be a floor on a real "+
				"package; 0 enforces nothing and 100 demands perfect coverage", pkg, floor)
		}
	}

	for pkg := range floors {
		known := false
		for _, want := range maintainerPackages {
			if pkg == want {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("coverage-floors.txt names %q, which is not a maintainer-owned "+
				"package; an unowned floor will be lowered by whoever it inconveniences", pkg)
		}
	}

	if len(floors) != len(maintainerPackages) {
		t.Errorf("coverage-floors.txt carries %d floors, want %d",
			len(floors), len(maintainerPackages))
	}
}

// TestCIRunsTheCoverageFloorGate keeps the mechanism attached. The script and
// the policy file can both be perfect while nothing invokes them.
func TestCIRunsTheCoverageFloorGate(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("reading the CI workflow: %v", err)
	}
	workflow := string(raw)

	for _, want := range []string{
		"coverage-floors:",
		"scripts/check-coverage-floors.sh",
		"coverage-floors.txt",
		"go test -coverprofile=coverage.out",
		"./route/...",
		"./dex/...",
		"./refrate/...",
		"./sep38/...",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf(".github/workflows/ci.yml is missing %q; the coverage floors "+
				"are committed but nothing enforces them", want)
		}
	}
}

// TestCoverageFloorScriptEnforcesThePolicy runs the real script against
// synthetic profiles. Each case is a way the gate could report the wrong
// verdict: passing a package below its floor, passing a package it did not
// measure, and passing a file that is not a coverage profile at all.
func TestCoverageFloorScriptEnforcesThePolicy(t *testing.T) {
	script, err := filepath.Abs("scripts/check-coverage-floors.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the enforcement script is not in the tree: %v", err)
	}

	const module = "github.com/Wayfare-labs/wayfare"

	// A profile block is either executed or not, so partial coverage is two
	// blocks: the statements a test reached and the ones it did not. Both
	// counts are explicit so the expectation is statement-weighted rather
	// than a count of functions.
	lines := func(pkg string, hit, miss int) string {
		return module + "/" + pkg + "/hit.go:1.1,2.2 " + strconv.Itoa(hit) + " 1\n" +
			module + "/" + pkg + "/miss.go:1.1,2.2 " + strconv.Itoa(miss) + " 0\n"
	}

	cases := []struct {
		name       string
		floors     string
		profile    string
		wantStatus int
		wantText   string
	}{
		{
			name:   "all floors met",
			floors: "route 80\n",
			profile: "mode: atomic\n" +
				lines("route", 9, 1),
			wantStatus: 0,
			wantText:   "ok",
		},
		{
			name:   "one floor missed",
			floors: "route 80\n",
			profile: "mode: atomic\n" +
				lines("route", 5, 5),
			wantStatus: 1,
			wantText:   "BELOW FLOOR",
		},
		{
			// The failure this case guards is the vacuous pass: a profile
			// that measured a different package must not satisfy a floor
			// because nothing contradicted it.
			name:   "floored package absent from the profile",
			floors: "route 80\nsep38 50\n",
			profile: "mode: atomic\n" +
				lines("route", 9, 1),
			wantStatus: 1,
			wantText:   "MISSING FROM PROFILE",
		},
		{
			name:       "not a coverage profile",
			floors:     "route 80\n",
			profile:    "this is not a profile\n",
			wantStatus: 2,
			wantText:   "not a Go coverage profile",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			floors := filepath.Join(dir, "floors.txt")
			profile := filepath.Join(dir, "coverage.out")
			if err := os.WriteFile(floors, []byte(tc.floors), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(profile, []byte(tc.profile), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("bash", script, profile, floors)
			out, err := cmd.CombinedOutput()
			status := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				status = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running the script: %v", err)
			}

			if status != tc.wantStatus {
				t.Errorf("exit status = %d, want %d; output:\n%s",
					status, tc.wantStatus, out)
			}
			if !strings.Contains(string(out), tc.wantText) {
				t.Errorf("output does not mention %q:\n%s", tc.wantText, out)
			}
			// A miss has to point at the policy file a maintainer edits, not
			// at the profile the script happened to read last.
			if tc.wantStatus == 1 && !strings.Contains(string(out), "Coverage floors are in "+floors) {
				t.Errorf("the failure hint does not name the floors file %s:\n%s", floors, out)
			}
		})
	}
}

// TestCoverageFloorScriptWeightsByStatements pins the property that makes the
// floor the same number `go test -cover` prints. A per-function average is the
// obvious wrong implementation and produces a different verdict here: one
// covered ten-statement file and one uncovered single-statement file average
// to 50% by function, but sit at 91% of statements.
func TestCoverageFloorScriptWeightsByStatements(t *testing.T) {
	script, err := filepath.Abs("scripts/check-coverage-floors.sh")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	floors := filepath.Join(dir, "floors.txt")
	profile := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(floors, []byte("route 85\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profileBody := "mode: atomic\n" +
		"github.com/Wayfare-labs/wayfare/route/a.go:1.1,2.2 10 10\n" +
		"github.com/Wayfare-labs/wayfare/route/b.go:1.1,2.2 1 0\n"
	if err := os.WriteFile(profile, []byte(profileBody), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("bash", script, profile, floors).CombinedOutput()
	if err != nil {
		t.Fatalf("the script rejected 90.9%% statement coverage against an 85%% "+
			"floor, so it is not statement-weighted: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "90.9%") {
		t.Errorf("the reported coverage is not the statement-weighted 90.9%%:\n%s", out)
	}
}

// readCoverageFloors parses the policy file. Comments and blank lines are
// ignored, matching the script.
func readCoverageFloors(path string) (map[string]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, line := range strings.Split(string(raw), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			continue
		}
		floor, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, err
		}
		out[fields[0]] = floor
	}
	return out, nil
}
