package wayfare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The stamp identities -version resolves, in the package that reads them.
//
// They are repeated here on purpose: the Makefile and the Dockerfile each
// spell them out again, and all four have to agree. If the package moves and a
// build file keeps the old path, `go build` accepts an unknown -X target
// silently, the stamp lands nowhere, and a production binary reports
// code=unknown while every gate stays green. These tests are what turns that
// into a failure.
const (
	stampCodeVar = "github.com/Wayfare-labs/wayfare/buildinfo.codeCommit"
	stampDataVar = "github.com/Wayfare-labs/wayfare/buildinfo.dataCommit"
)

// wayfaredCommit and ladderCommit are the values the stamped build under test
// injects. They are full 40-hex strings because that is what
// `git rev-parse HEAD` supplies in a real stamp, and a test value shaped
// differently would not catch a build that truncated one.
const (
	stampCodeValue = "1111111111111111111111111111111111111111"
	stampDataValue = "2222222222222222222222222222222222222222"
)

// TestBuildStampsTargetTheProvenancePackage checks the two places that stamp a
// release build still name the package that reads the stamps (issue #707).
func TestBuildStampsTargetTheProvenancePackage(t *testing.T) {
	for _, file := range []string{"Makefile", "Dockerfile"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		content := string(raw)
		for _, want := range []string{"-X " + stampCodeVar, "-X " + stampDataVar} {
			if !strings.Contains(content, want) {
				t.Errorf("%s does not stamp %q: the value would be dropped and -version would report it unknown", file, want)
			}
		}
	}
}

// TestVersionFlagPrintsBothProvenances builds each binary the way a release is
// built, runs its -version, and requires the stamped commits to come back out
// (issue #707: "Both binaries print version info").
//
// It runs the binary rather than calling into it because the acceptance is a
// property of the command line: the flag has to be registered, print on stdout,
// exit 0, and do all of that before the program opens a run store or reaches
// the network. None of that is observable from a unit test of the formatter.
func TestVersionFlagPrintsBothProvenances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build test in short mode")
	}

	for _, tc := range []struct {
		pkg string
		exe string
	}{
		{pkg: "./cmd/wayfared", exe: "wayfared"},
		{pkg: "./cmd/ladder", exe: "ladder"},
	} {
		t.Run(tc.exe, func(t *testing.T) {
			line := runVersion(t, tc.pkg, tc.exe, map[string]string{
				stampCodeVar: stampCodeValue,
				stampDataVar: stampDataValue,
			})

			// The binary names itself, so a copy of this line from the other
			// command cannot be mistaken for its own output.
			if !strings.HasPrefix(line, tc.exe+" ") {
				t.Errorf("-version printed %q, which does not start with %q", line, tc.exe)
			}
			// Each value carries the source that supplied it: a stamp must read
			// as stamped, not as a value whose origin has to be inferred.
			for _, want := range []string{
				"code=" + stampCodeValue + " (stamped)",
				"data=" + stampDataValue + " (stamped)",
				"go=go",
			} {
				if !strings.Contains(line, want) {
					t.Errorf("-version printed %q, missing %q", line, want)
				}
			}
			if strings.Contains(line, "unknown") {
				t.Errorf("-version printed %q for a stamped build; a stamp was dropped", line)
			}
		})
	}
}

// TestVersionFlagFallsBackToTheToolchain builds without stamps and requires the
// answer to say which source it used. In a git checkout that is the toolchain's
// own revision; with -buildvcs=false there is no source left and the word
// "unknown" has to appear rather than a blank.
func TestVersionFlagFallsBackToTheToolchain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build test in short mode")
	}

	line := runVersion(t, "./cmd/ladder", "ladder", nil)

	// Exactly one of the three provenances is claimed for the code commit, and
	// the line always says which: this is the assertion that a fallback is
	// never presented as if it were the release stamp.
	var claimed string
	for _, source := range []string{"stamped", "vcs", "unstamped"} {
		if strings.Contains(line, "("+source+")") {
			claimed = source
			break
		}
	}
	if claimed == "" {
		t.Fatalf("-version printed %q with no labelled provenance source", line)
	}
	if claimed == "stamped" {
		t.Errorf("-version claims %q for an unstamped build; a fallback was presented as a stamp", line)
	}
	if !strings.Contains(line, "go=go") {
		t.Errorf("-version printed %q without the Go toolchain version", line)
	}
}

// TestVersionFlagNeedsNoDataDirectoryOrPort runs -version with the flags a
// misconfigured service would have, and still expects the provenance and exit
// 0. An operator reaches for -version precisely when the service will not
// start, so the answer cannot depend on it starting.
func TestVersionFlagNeedsNoDataDirectoryOrPort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build test in short mode")
	}

	bin := buildBinary(t, "./cmd/wayfared", "wayfared", nil)

	cmd := exec.Command(bin, "-version", "-data", filepath.Join(t.TempDir(), "does", "not", "exist"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("-version with an unavailable data dir failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "wayfared code=") {
		t.Errorf("-version with an unavailable data dir printed %q", strings.TrimSpace(string(out)))
	}
}

// TestMakeBuildStampedProducesAStampedBinary runs the documented release target
// and requires the binary it writes to report the commits it claims (issue #707).
//
// The unit tests above cannot catch this target's own failure modes, and both of
// them are quiet:
//
//   - flags placed after the package list make `go build` read -ldflags as an
//     import path, which fails the build loudly but only for whoever runs the
//     target, which no test job does;
//   - a stamp value quoted inside the already-double-quoted -ldflags string
//     keeps its quotes, so the binary stamps `'abc…'` and -version prints a
//     commit that matches no `git log` output.
//
// Only running the recipe and reading the answer back checks both.
func TestMakeBuildStampedProducesAStampedBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the make target build in short mode")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}

	// BIN is overridden rather than left at ./bin so the test does not write
	// into the developer's tree. -e makes the override win over the makefile's
	// own `BIN := bin` assignment.
	outDir := t.TempDir()
	build := exec.Command("make", "-e", "BIN="+outDir, "build-stamped")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build-stamped failed: %v\n%s", err, output)
	}

	wantCode := gitOutput(t, "rev-parse", "HEAD")
	wantData := gitOutput(t, "log", "-1", "--format=%H", "--", "data/")

	for _, exe := range []string{"wayfared", "ladder"} {
		t.Run(exe, func(t *testing.T) {
			bin := filepath.Join(outDir, exe)
			if _, err := os.Stat(bin); err != nil {
				t.Fatalf("make build-stamped did not write %s: %v", bin, err)
			}
			line := strings.TrimSuffix(mustOutput(t, bin, "-version"), "\n")

			// Equal to what git answers right now, quoted-free and
			// prefix-free: the field is only useful if a reader can paste it
			// into `git show`.
			for _, want := range []string{
				"code=" + wantCode + " (stamped)",
				"data=" + wantData + " (stamped)",
			} {
				if !strings.Contains(line, want) {
					t.Errorf("%s -version printed %q\nwant it to contain %q", exe, line, want)
				}
			}
			if strings.ContainsAny(line, "'\"") {
				t.Errorf("%s -version printed %q: a stamp value carries quotes", exe, line)
			}
		})
	}
}

// gitOutput runs git in the module root and returns its trimmed stdout.
func gitOutput(t *testing.T, args ...string) string {
	t.Helper()

	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// mustOutput runs a built binary and returns its stdout, failing the test on a
// non-zero exit.
func mustOutput(t *testing.T, name string, args ...string) string {
	t.Helper()

	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("%s %s failed: %v", name, strings.Join(args, " "), err)
	}
	return string(out)
}

// runVersion builds one command with the given stamps, runs -version and
// returns its single stdout line.
func runVersion(t *testing.T, pkg, exe string, stamps map[string]string) string {
	t.Helper()

	bin := buildBinary(t, pkg, exe, stamps)
	line := strings.TrimSuffix(mustOutput(t, bin, "-version"), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("%s -version printed more than one line:\n%s", exe, line)
	}
	return line
}

// buildBinary compiles one command into a temp dir, injecting stamps as
// `make build-stamped` does, and returns its path.
func buildBinary(t *testing.T, pkg, exe string, stamps map[string]string) string {
	t.Helper()

	args := []string{"build", "-trimpath"}
	if len(stamps) > 0 {
		var ldflags []string
		for identity, value := range stamps {
			ldflags = append(ldflags, "-X "+identity+"="+value)
		}
		args = append(args, "-ldflags", strings.Join(ldflags, " "))
	}
	out := filepath.Join(t.TempDir(), exe)
	args = append(args, "-o", out, pkg)

	cmd := exec.Command("go", args...)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", pkg, err, buildOutput)
	}
	return out
}
