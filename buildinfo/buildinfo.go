// Package buildinfo reports which build produced the running binary.
//
// Reproducing a published figure needs more than the code: it needs the
// measurement history that was embedded in the binary at build time, because
// a trend read depends on the records it was drawn from. Wayfare therefore has
// two provenances to name, and `make build-stamped` records both with
// -ldflags.
//
// Those stamps are empty in a dev or test build, so this package falls back to
// what the Go toolchain records for itself (the VCS settings `go build` embeds
// from a git checkout) and says which source a value came from. A fallback
// reported as if it were the stamp would be the one thing this package exists
// to prevent: a reader who believes a provenance that was never recorded.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// codeCommit is the git commit the binary was built from. It is stamped at
// build time with
//
//	-ldflags -X github.com/Wayfare-labs/wayfare/buildinfo.codeCommit=$(git rev-parse HEAD)
//
// and is empty for unstamped (dev/test) builds.
var codeCommit string

// dataCommit is the git commit of data/ that was embedded in the binary at
// build time. It is stamped at build time with
//
//	-ldflags -X github.com/Wayfare-labs/wayfare/buildinfo.dataCommit=$(git log -1 --format=%H -- data/)
//
// and is empty for unstamped (dev/test) builds.
var dataCommit string

// Unknown renders a provenance this build did not record. It is a word rather
// than an empty string because -version is read by a person trying to
// reproduce a figure, and a blank field cannot tell them the value is absent
// from a build that simply did not print it.
const Unknown = "unknown"

// Source names where a provenance value came from, so a reader can judge how
// much to trust it.
type Source string

const (
	// FromStamp means -ldflags recorded it at build time, which is the only
	// source that survives `make build-reproducible` (-buildvcs=false).
	FromStamp Source = "stamped"

	// FromVCS means the Go toolchain's own build settings supplied it. That
	// matches the checkout the binary was built from, but not necessarily the
	// tree that was published: it is absent when the working tree was dirty
	// beyond the revision, and stripped by -buildvcs=false.
	FromVCS Source = "vcs"

	// FromNone means neither source had it, so the value is Unknown.
	FromNone Source = "unstamped"
)

// Info is the provenance of the binary that read it.
type Info struct {
	// CodeCommit is the git commit of the source, or Unknown.
	CodeCommit string

	// CodeSource says where CodeCommit came from.
	CodeSource Source

	// DataCommit is the git commit of the embedded data/ history, or Unknown.
	// No fallback exists for this one: the toolchain records the commit of the
	// whole checkout, not of one directory inside it, so only a stamped build
	// can name the history it carries.
	DataCommit string

	// DataSource says where DataCommit came from.
	DataSource Source

	// GoVersion is the toolchain that compiled the binary, as runtime.Version
	// reports it.
	GoVersion string

	// BuildTime is the commit timestamp of the source, from the toolchain.
	// Empty when no source recorded it.
	BuildTime string

	// Modified reports that the toolchain saw uncommitted changes in the tree
	// the binary was built from, which makes CodeCommit an approximation.
	Modified bool
}

// CodeCommit and DataCommit return the stamps alone, with no fallback and no
// substitution: an unstamped build reports the empty string. The HTTP surface
// reports provenance this way because a wire field is parsed by a program, and
// "" is what that program already reads as "this build does not know".
func CodeCommit() string { return codeCommit }

// DataCommit returns the stamped commit of the embedded history, empty when
// the build was not stamped.
func DataCommit() string { return dataCommit }

// Get reads the provenance of the running binary.
func Get() Info {
	var settings []debug.BuildSetting
	if build, ok := debug.ReadBuildInfo(); ok {
		settings = build.Settings
	}
	return resolve(codeCommit, dataCommit, settings, runtime.Version())
}

// resolve composes the two provenance sources, stamp first and toolchain
// second.
//
// The precedence lives here rather than inline in Get so it can be tested as a
// fact about this package: a test that called Get would pass or fail on whether
// `go test` happens to embed VCS settings in the test binary, which is a
// toolchain behaviour and not what this package promises.
func resolve(code, data string, settings []debug.BuildSetting, goVersion string) Info {
	info := Info{
		CodeCommit: Unknown,
		CodeSource: FromNone,
		DataCommit: Unknown,
		DataSource: FromNone,
		GoVersion:  goVersion,
	}

	if code != "" {
		info.CodeCommit, info.CodeSource = code, FromStamp
	} else if revision := setting(settings, "vcs.revision"); revision != "" {
		info.CodeCommit, info.CodeSource = revision, FromVCS
	}

	// The data commit deliberately gets no fallback. The toolchain records the
	// commit of the whole checkout, never of one directory inside it, so only a
	// stamped build can name the history it carries; reusing the source revision
	// here would print a data provenance that was never recorded.
	if data != "" {
		info.DataCommit, info.DataSource = data, FromStamp
	}

	// The tree state is read whatever supplied the commit. A stamp is
	// `git rev-parse HEAD`, which says nothing about uncommitted changes, so a
	// stamped build from a dirty tree would otherwise print a commit it did not
	// actually build.
	info.Modified = setting(settings, "vcs.modified") == "true"
	info.BuildTime = setting(settings, "vcs.time")
	return info
}

// Line renders the provenance as the one line -version prints. The labels are
// part of the contract: a reader pastes this into an issue, and a field whose
// name has to be guessed from its position is a field that gets copied wrong.
func (i Info) Line(prog string) string {
	parts := []string{
		prog,
		"code=" + i.CodeCommit + " (" + string(i.CodeSource) + ")",
		"data=" + i.DataCommit + " (" + string(i.DataSource) + ")",
		"go=" + i.GoVersion,
	}
	if i.BuildTime != "" {
		parts = append(parts, "committed="+i.BuildTime)
	}
	if i.Modified {
		parts = append(parts, "tree=modified")
	}
	return strings.Join(parts, " ")
}

// setting returns the build setting with this key, or "" when the toolchain
// did not record one.
func setting(settings []debug.BuildSetting, key string) string {
	for _, s := range settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
