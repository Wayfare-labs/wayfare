// Package automerge exercises this repository's auto-merge gate offline
// (issue #319).
//
// .github/workflows/auto-merge.yml carries the whole merge decision — seven
// gates in ~350 lines of shell — and until now nothing had run it: per the
// audit in #48 it had never merged a pull request. A gate that is never
// exercised fails the first time it is trusted, and for this repository the
// two failure directions are symmetric and both bad: a plausible-looking
// change lands unreviewed, or a safe one is held for a reason no reader can
// see.
//
// The tests take the script out of the workflow file, substitute the GitHub
// expressions GitHub itself would substitute, and run it under bash with a
// fake `gh` on PATH. The fake answers from API responses recorded from the
// live repository (testdata/recorded) and appends every invocation to a log,
// so a test can assert not only what the gate printed but what it actually
// did — merged, labelled, commented, or stayed silent.
//
// Nothing here touches the network, so the suite runs in CI's no-network job.
// The workflow itself is deliberately untouched: these tests pin the gate as
// it is, including the behaviours nobody has observed yet.
package automerge
