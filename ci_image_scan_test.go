package wayfare

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ciImageScan pins the CI job that inspects the deployed artifact.
//
// The Dockerfile says what the image is made of. Nothing else in the
// repository said whether any of it is known-bad, and a Dockerfile cannot:
// whether a base image or a compiled-in standard library has a published
// advisory changes without a line of this repository changing. So the rule this
// test protects is not "the image is clean" — that is a measurement with a date
// on it, and today it is not — but "something inspects the image, in CI,
// against the artifact CI built".
//
// These are source-text assertions rather than a YAML parse, deliberately:
// gopkg.in/yaml.v3 would be a third-party dependency, and CONTRIBUTING.md keeps
// the module surface at two. The same reasoning is why the UI's own tests
// assert on server/index.html's text. Whitespace and quoting are normalised
// away first, so reformatting the workflow does not fail a test about what the
// workflow does.
func TestCIImageIsScannedForKnownVulnerabilities(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("reading the CI workflow: %v", err)
	}
	workflow := string(raw)

	scans := scanSteps(workflow)
	if len(scans) == 0 {
		t.Fatal("no step in .github/workflows/ci.yml uses aquasecurity/trivy-action, " +
			"so nothing inspects the container image CI builds")
	}

	var gates, libraryCoverage int
	for i, s := range scans {
		where := "scan step " + strconv.Itoa(i+1) + ": " + firstLineContaining(s.raw, "trivy-action")

		// A floating ref would let the scanner change what "clean" means
		// between two runs of the same commit. The lint job pins its action
		// for exactly this reason.
		if !pinnedTrivyAction.MatchString(s.flat) {
			t.Errorf("%s does not pin the action to a version", where)
		}

		// It must inspect the image this workflow built, not a registry tag
		// that could be something else entirely.
		if !strings.Contains(s.flat, "image-ref:wayfare:ci") {
			t.Errorf("%s does not scan wayfare:ci, the image the docker job builds", where)
		}

		if strings.Contains(s.flat, "vuln-type:os,library") ||
			strings.Contains(s.flat, "vuln-type:library,os") {
			libraryCoverage++
		}

		if !strings.Contains(s.flat, "exit-code:1") {
			continue
		}
		gates++

		// The gate must not fire on an advisory with no available fix: it
		// cannot be acted on, and a job that is red for a reason nobody can
		// change is a job people learn to ignore.
		if !strings.Contains(s.flat, "ignore-unfixed:true") {
			t.Errorf("%s gates but does not set ignore-unfixed, so it would fail on "+
				"advisories with no available fix", where)
		}
		if !strings.Contains(s.flat, "severity:HIGH,CRITICAL") &&
			!strings.Contains(s.flat, "severity:CRITICAL,HIGH") {
			t.Errorf("%s gates but does not cover HIGH and CRITICAL severities", where)
		}
		// The image's own packages are the part a change in this repository
		// can act on: a newer base image fixes them.
		if !strings.Contains(s.flat, "vuln-type:os") {
			t.Errorf("%s gates but does not cover the image's own packages", where)
		}
	}

	if gates == 0 {
		t.Error("every trivy step in .github/workflows/ci.yml sets exit-code 0, so a " +
			"known vulnerability in the deployed image cannot fail the build")
	}
	// The standard library compiled into the binary carries advisories today
	// that no change here can fix. They are recorded rather than gated, and
	// dropping the language packages entirely would hide them.
	if libraryCoverage == 0 {
		t.Error("no trivy step covers language packages (vuln-type: library), so " +
			"advisories in the Go standard library compiled into the image are " +
			"inspected by nothing and recorded nowhere")
	}

	// The scan is only meaningful against an image that exists: the build must
	// come first, in the same file.
	flatWorkflow := normalise(workflow)
	build := strings.Index(flatWorkflow, "dockerbuild-twayfare:ci")
	scan := strings.Index(flatWorkflow, "aquasecurity/trivy-action@")
	if build < 0 {
		t.Fatal("the docker job no longer builds wayfare:ci; this test and the scan it " +
			"protects both assume that image name")
	}
	if scan < build {
		t.Error("the image scan runs before the image is built; a scan of an image from " +
			"an earlier build is not a scan of this commit")
	}
}

// pinnedTrivyAction matches the action reference with an explicit version,
// which rejects @master, @main and @latest.
//
// The leading `v` is required, not cosmetic: that is how this action tags its
// releases (its container image, confusingly, is tagged without one). A
// reference that merely looks like a version — `@0.36.0` against `v0.36.0` —
// fails the job before a single scan runs, which is a failure mode worth a
// test rather than a review.
var pinnedTrivyAction = regexp.MustCompile(`aquasecurity/trivy-action@v\d+\.\d+\.\d+`)

// scanStep is one workflow step that runs the container scanner: the text as
// written, for a failure message a reader can act on, and the same text with
// whitespace and quotes removed, for assertions.
type scanStep struct {
	raw  string
	flat string
}

// scanSteps returns the workflow steps that use the container scanner.
//
// Steps in this workflow are the entries indented six spaces under `steps:`.
// Splitting on that boundary keeps each step's inputs together, so a check can
// reason about one invocation rather than about the whole file.
func scanSteps(workflow string) []scanStep {
	var out []scanStep
	for _, chunk := range strings.Split(workflow, "\n      - ") {
		if strings.Contains(chunk, "aquasecurity/trivy-action@") {
			out = append(out, scanStep{raw: chunk, flat: normalise(chunk)})
		}
	}
	return out
}

// normalise strips whitespace and quotes so an assertion is about what a step
// says, not how it is formatted.
func normalise(s string) string {
	r := strings.NewReplacer(" ", "", "\t", "", "\r", "", `"`, "", "'", "")
	return r.Replace(s)
}

// firstLineContaining returns the first line naming needle, so a failure
// message points at the step rather than at the file.
func firstLineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return "(step not found)"
}
