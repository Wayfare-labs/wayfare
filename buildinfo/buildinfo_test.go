package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// settings builds the toolchain build settings a git checkout produces. The
// keys are the exact strings `go version -m` prints, because resolve matches on
// them and a typo here would silently disable the fallback it is meant to test.
func settings(revision, time string, modified bool) []debug.BuildSetting {
	mark := "false"
	if modified {
		mark = "true"
	}
	return []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.time", Value: time},
		{Key: "vcs.modified", Value: mark},
	}
}

// ---------------------------------------------------------------------------
// Which source supplies each value
// ---------------------------------------------------------------------------

func TestResolveReportsNeitherSourceAsUnknown(t *testing.T) {
	// An unstamped build with no VCS settings must say so in words. The
	// alternative — a blank field — is indistinguishable from a build that
	// forgot to print the field at all.
	got := resolve("", "", nil, "go1.27.4")

	if got.CodeCommit != Unknown || got.CodeSource != FromNone {
		t.Errorf("code = %q (%s), want %q (%s)", got.CodeCommit, got.CodeSource, Unknown, FromNone)
	}
	if got.DataCommit != Unknown || got.DataSource != FromNone {
		t.Errorf("data = %q (%s), want %q (%s)", got.DataCommit, got.DataSource, Unknown, FromNone)
	}
	if got.GoVersion != "go1.27.4" {
		t.Errorf("GoVersion = %q, want go1.27.4", got.GoVersion)
	}
	if got.Modified {
		t.Error("Modified = true with no settings to say the tree was dirty")
	}
}

func TestResolvePrefersTheStampOverTheToolchain(t *testing.T) {
	// Both sources are present and disagree, which is the normal case for
	// `make build-stamped`: the stamp names what was published and the VCS
	// settings name the machine that ran make. The stamp has to win, or the
	// value a reader matches against a release is the wrong one.
	got := resolve("aaaaaaaa", "bbbbbbbb", settings("cccccccc", "2026-01-01T00:00:00Z", false), "go1.27.4")

	if got.CodeCommit != "aaaaaaaa" || got.CodeSource != FromStamp {
		t.Errorf("code = %q (%s), want the stamp aaaaaaaa (%s)", got.CodeCommit, got.CodeSource, FromStamp)
	}
	if got.DataCommit != "bbbbbbbb" || got.DataSource != FromStamp {
		t.Errorf("data = %q (%s), want the stamp bbbbbbbb (%s)", got.DataCommit, got.DataSource, FromStamp)
	}
}

func TestResolveFallsBackToVCSOnlyForCode(t *testing.T) {
	// The data commit has no fallback: the toolchain records the commit of the
	// whole checkout, never of one directory inside it. Reusing the source
	// revision for data/ would print a history provenance that was never
	// recorded, which is the one failure this package exists to prevent.
	got := resolve("", "", settings("deadbeef", "2026-01-01T00:00:00Z", false), "go1.27.4")

	if got.CodeCommit != "deadbeef" || got.CodeSource != FromVCS {
		t.Errorf("code = %q (%s), want deadbeef (%s)", got.CodeCommit, got.CodeSource, FromVCS)
	}
	if got.DataCommit != Unknown || got.DataSource != FromNone {
		t.Errorf("data = %q (%s), want %q (%s): the source revision is not a data commit",
			got.DataCommit, got.DataSource, Unknown, FromNone)
	}
	if got.BuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("BuildTime = %q, want the vcs.time the toolchain recorded", got.BuildTime)
	}
}

func TestResolveFillsOnlyTheMissingCommit(t *testing.T) {
	// A build stamped for code but not data is what `make build-stamped`
	// produces when one -X line is dropped. The stamp wins where it exists and
	// the fallback fills only the gap; neither is allowed to overwrite the other.
	got := resolve("stampedcode", "", settings("vcsrevision", "", false), "go1.27.4")

	if got.CodeCommit != "stampedcode" || got.CodeSource != FromStamp {
		t.Errorf("code = %q (%s), want the stamp, not the fallback", got.CodeCommit, got.CodeSource)
	}
	if got.DataCommit != Unknown || got.DataSource != FromNone {
		t.Errorf("data = %q (%s), want %q (%s)", got.DataCommit, got.DataSource, Unknown, FromNone)
	}
}

func TestResolveReportsDirtyTreeBesideAStamp(t *testing.T) {
	// A stamp is `git rev-parse HEAD`, which is silent about uncommitted
	// changes. Without this, a build made from a dirty tree prints a commit it
	// did not build and reads as reproducible.
	got := resolve("stampedcode", "stampeddata", settings("ignored", "", true), "go1.27.4")

	if !got.Modified {
		t.Error("Modified = false for a stamp built from a dirty tree; the commit is an approximation")
	}
	if got.CodeSource != FromStamp {
		t.Errorf("CodeSource = %s, want the stamp to stay the source of the commit", got.CodeSource)
	}
}

func TestResolveTreatsAnAbsentRevisionAsNoFallback(t *testing.T) {
	// `go build -buildvcs=false` and a non-git tree both leave vcs.revision
	// empty, which must land on unknown rather than on the empty string.
	got := resolve("", "", settings("", "", false), "go1.27.4")

	if got.CodeCommit != Unknown || got.CodeSource != FromNone {
		t.Errorf("code = %q (%s), want %q (%s)", got.CodeCommit, got.CodeSource, Unknown, FromNone)
	}
}

func TestResolveIgnoresUnrelatedBuildSettings(t *testing.T) {
	// The toolchain records many settings (GOOS, CGO_ENABLED, ...) and the
	// lookup must key exactly, not on a prefix or a substring.
	got := resolve("", "", []debug.BuildSetting{
		{Key: "vcs.revision.extra", Value: "should-not-be-used"},
		{Key: "notvcs.modified", Value: "true"},
		{Key: "GOOS", Value: "linux"},
	}, "go1.27.4")

	if got.CodeCommit != Unknown {
		t.Errorf("code = %q, want %q: a setting that merely starts with the key is not the key",
			got.CodeCommit, Unknown)
	}
	if got.Modified {
		t.Error("Modified = true from a key that is not vcs.modified")
	}
}

func TestGetReadsTheStampsOfThisBinary(t *testing.T) {
	// The package vars are what -ldflags -X writes, so Get has to actually read
	// them rather than only the toolchain. Assigning them here is the only way
	// to exercise the stamped branch from inside the package.
	originalCode, originalData := codeCommit, dataCommit
	t.Cleanup(func() { codeCommit, dataCommit = originalCode, originalData })

	codeCommit, dataCommit = "codedeployed", "datadeployed"
	got := Get()

	if got.CodeCommit != "codedeployed" || got.CodeSource != FromStamp {
		t.Errorf("code = %q (%s), want the stamp the build set", got.CodeCommit, got.CodeSource)
	}
	if got.DataCommit != "datadeployed" || got.DataSource != FromStamp {
		t.Errorf("data = %q (%s), want the stamp the build set", got.DataCommit, got.DataSource)
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want the running toolchain %q", got.GoVersion, runtime.Version())
	}
}

func TestStampsReturnEmptyWhenUnstamped(t *testing.T) {
	// CodeCommit and DataCommit back the HTTP surface, where an unstamped build
	// must report "" and not the toolchain's guess: a wire field is parsed by a
	// program that already reads "" as "this build does not know".
	originalCode, originalData := codeCommit, dataCommit
	t.Cleanup(func() { codeCommit, dataCommit = originalCode, originalData })

	codeCommit, dataCommit = "", ""
	if got := CodeCommit(); got != "" {
		t.Errorf("CodeCommit() = %q, want empty for an unstamped build", got)
	}
	if got := DataCommit(); got != "" {
		t.Errorf("DataCommit() = %q, want empty for an unstamped build", got)
	}

	codeCommit, dataCommit = "stampcode", "stampdata"
	if got := CodeCommit(); got != "stampcode" {
		t.Errorf("CodeCommit() = %q, want stampcode", got)
	}
	if got := DataCommit(); got != "stampdata" {
		t.Errorf("DataCommit() = %q, want stampdata", got)
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func TestLineNamesEveryFieldItPrints(t *testing.T) {
	// -version is pasted into issues. A field whose name has to be guessed from
	// its position is a field that gets copied wrong, so each value carries its
	// label and its source.
	info := resolve("abc123", "def456", settings("ignored", "2026-01-01T00:00:00Z", false), "go1.27.4")
	got := info.Line("wayfared")

	for _, want := range []string{
		"wayfared",
		"code=abc123 (stamped)",
		"data=def456 (stamped)",
		"go=go1.27.4",
		"committed=2026-01-01T00:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "tree=modified") {
		t.Errorf("line %q reports a clean tree as modified", got)
	}
}

func TestLineMarksTheUnknownProvenance(t *testing.T) {
	// The reader of an unstamped build still needs to know which half is
	// missing, and needs the word "unknown" rather than two adjacent spaces.
	got := resolve("", "", nil, "go1.27.4").Line("ladder")

	if !strings.HasPrefix(got, "ladder ") {
		t.Errorf("line %q does not name the binary that printed it", got)
	}
	for _, want := range []string{
		"code=" + Unknown + " (unstamped)",
		"data=" + Unknown + " (unstamped)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "committed=") {
		t.Errorf("line %q prints a commit time the build did not record", got)
	}
}

func TestLineMarksAVCSFallbackAndADirtyTree(t *testing.T) {
	got := resolve("", "", settings("ff00", "2026-02-02T00:00:00Z", true), "go1.27.4").Line("ladder")

	for _, want := range []string{
		"code=ff00 (vcs)",
		"data=" + Unknown + " (unstamped)",
		"tree=modified",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q is missing %q", got, want)
		}
	}
}

func TestLineIsOneLine(t *testing.T) {
	// Both binaries print it with Fprintln into a shell pipeline; a newline
	// inside the line would break `wayfared -version | cut -d= -f2`.
	info := resolve("a", "b", settings("c", "2026-01-01T00:00:00Z", true), "go1.27.4")
	if strings.Contains(info.Line("wayfared"), "\n") {
		t.Error("Line returned more than one line")
	}
}

func TestSettingReturnsFirstMatchOrEmpty(t *testing.T) {
	set := []debug.BuildSetting{{Key: "k", Value: "first"}, {Key: "k", Value: "second"}}
	if got := setting(set, "k"); got != "first" {
		t.Errorf("setting = %q, want first", got)
	}
	if got := setting(set, "absent"); got != "" {
		t.Errorf("setting = %q, want empty for a key that is not recorded", got)
	}
	if got := setting(nil, "k"); got != "" {
		t.Errorf("setting = %q, want empty for no settings at all", got)
	}
}
