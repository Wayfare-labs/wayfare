// Package measure exercises the scheduled measure workflow offline
// (issues #555, #556, #557, #558).
//
// .github/workflows/measure.yml is the project's clock. Until these tests it
// carried three decisions that nothing in the repository checked: which
// records a run inherits before it measures, what happens to the branch when
// a pull request cannot be opened, and whether a dead pipeline says anything
// to anybody. All three failed in production — twelve consecutive red runs
// between 2026-10-11 and 2026-10-14, each one force-pushing away the sweep
// before it.
//
// The tests take the workflow's shell steps out of the YAML and run them
// under bash. The GitHub side is a fake `gh` on PATH that answers from
// fixtures and logs every invocation, so a test asserts what the step *did* —
// created an issue, edited the open one, closed it, refused — and not only
// what it printed. The git side is real git against a bare repository in a
// temporary directory: the accumulation property #556 asks for is proved by
// running two sweeps without a merge and reading the branch afterwards.
//
// Nothing here touches the network, so the suite runs in CI's no-network job.
// `go run ./cmd/wayfared` is stubbed in the git tests — what those tests pin
// is the workflow's handling of a verification result, not the verifier,
// which runstore's own tests and embed_test.go already cover.
package measure
