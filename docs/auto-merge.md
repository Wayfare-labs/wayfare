# The auto-merge gate

`auto-merge.yml` decides, mechanically, whether a pull request may land
without a human. It is deliberately narrow: it merges the boring,
provably-safe changes and hands everything else to a maintainer. Nothing in
it forms a judgement about whether a change is *good*.

**Status: exercised by tests.** The decision logic runs offline from recorded
API responses under `go test ./automerge` (issue #319); the workflow file
itself was not changed.

---

## When it runs

| Trigger | Why it exists |
|:---|:---|
| `pull_request_target` (opened, synchronize, reopened, ready_for_review, labeled, unlabeled, edited) | the first evaluation, and the one that re-runs when the description or labels change |
| `workflow_run` — CI completed | the run that sees the final check-run state. An earlier `check_suite: completed` trigger never fired: GitHub does not fire it for check suites created by GitHub Actions, which is what CI is |
| `pull_request_review` | a review arriving after CI would otherwise never be noticed |
| `issue_comment` | a bot comment finishing after CI, same reason |

It runs the **base branch's** definition with a write token, so it never
checks out or executes pull request code — it only reads metadata through the
API. Adding a checkout of the head would turn it into a code-execution vector
for anyone who can open a pull request.

## The seven gates

| # | Gate | Holds when |
|:--|:---|:---|
| 1 | Maintainer-owned paths | the diff touches `route/route.go`, `route/ladder.go`, `dex/`, `sep38/`, `runstore/runstore.go`, `checks/engine.go`, `.github/workflows/` or `data/` |
| 2 | No new dependencies | `go.mod` or `go.sum` changed |
| 3 | The contributor checklist is ticked | the description is empty, has no ticked box, or leaves one open |
| 4 | Every check is green | a check failed, or one is still running (that is a *wait*, not a hold) |
| 5 | No reviewer has asked for changes | a `CHANGES_REQUESTED` review exists |
| 6 | CodeRabbit reviewed this commit | no parseable verdict, or the verdict has actionable comments (no verdict at all is a *wait*) |
| 7 | The change matches what its issue named | the description does not name an issue, the issue names no files, or a changed file falls outside them (test files, `docs/` and `*.md` accompanying in-scope work are allowed) |

**Wait vs hold.** A pending check or a review that has not arrived is
*wait*: the gate says so and exits, and a later trigger re-evaluates. A
reason is *hold*: the pull request is labelled `needs-maintainer-review` and
commented **once**, with the exact reasons — the comment carries a
`<!-- auto-merge-gate -->` marker so re-evaluations never repeat it.

**When every gate passes** it enqueues the squash merge (`--auto`, which is
both the merge-queue path and the plain one), then comments what it did —
including what it did *not* verify. It forms no opinion on the substance of a
change, which is why it declines anything touching pricing, thresholds, the
integrity taxonomy or the published chain.

## How it is tested

`automerge/` takes the script out of the workflow file, substitutes the
GitHub expressions GitHub itself would substitute, and runs it under `bash`
with a fake `gh` on PATH. The fake answers from API responses **recorded from
this repository** (`automerge/testdata/recorded` — PR #517 and its issue
#302) and appends every invocation to a log, so a test asserts what the gate
*did*, not only what it printed.

```bash
go test ./automerge
```

Sixteen scenarios cover each gate, both wait paths, the two no-op paths
(a `merge_group` CI run, and the gate's own comment), comment de-duplication,
and a refused merge — which must fail loudly rather than report an approval
that landed nothing. The suite needs no network, so it runs in CI's
no-network job.

The recorded fixtures also carry the two facts that motivated the issue:

- **PR #517 was held for "no checklist items are ticked"** — its description
  has no checkboxes at all. The scenario `checklist-absent` reproduces it.
- **The gate has never merged anything, and gate 6 is why.** The recorded
  CodeRabbit comment on a repository with fewer than 10 stars is the
  "this repository does not receive automatic reviews" summary: it carries
  neither `Actionable comments posted: N` nor `No actionable comments`, so
  the parse fails and the gate holds. Holding is the safe direction, and the
  scenario `coderabbit-unparseable` pins it as the behaviour *today*.

The second finding is reported, not fixed: whether an absent CodeRabbit
verdict should hold a merge, pass it, or fall back to something else is a
decision about what the gate may assume, and that is a maintainer's call.

## Related

- [CONTRIBUTING.md](../CONTRIBUTING.md) — what every pull request confirms
- [docs/maintainer-owned-areas.md](maintainer-owned-areas.md) — what gate 1 protects, and why
- [docs/development-loop.md](development-loop.md) — the `make` targets CI runs
