# Workflow permissions and supply-chain path

The measure and auto-merge workflows can write to the repository. Because
their output is embedded into the deployed binary, a compromised or
misconfigured run of either is a supply-chain path for published figures.

This section records each workflow's effective permissions, what it can change,
and how a compromise would show up. It is descriptive, not prescriptive — the
workflows are already pinned and least-privileged where possible.

## Scope of this section

This section covers the **writing** workflows only: the ones that touch
repository contents or pull-request state and whose output can reach the
deployed binary.

- `.github/workflows/measure.yml` — continuous measurement
- `.github/workflows/auto-merge.yml` — mechanical merge gate
- `.github/workflows/ci.yml` — build and test (read-only)

CI is listed for contrast. It has no write permission and its outputs never
reach the deployed binary.

## Permissions per workflow

### `.github/workflows/measure.yml` — `measure`

**Events.** `schedule` (every six hours), `workflow_dispatch`.

**Declared permissions.**

- `contents: write`
- `pull-requests: write`

**What it can do.** It measures the configured corridors, rotates chains that
have grown past the window ceiling, verifies the resulting chain, and — if the
chain verifies — opens a pull request (`automation/measure`) with the new
`data/` contents via `peter-evans/create-pull-request`. The PR is created with
the default `GITHUB_TOKEN` and is subject to the same branch-protection rules
as other PRs.

**What it cannot do.** It does not merge its own PR, it does not run any
arbitrary code from a PR head, and it does not write outside `data/`.

**Why this is a supply-chain path.** The deployed instance runs with
`-schedule=0 -history-first`, so its served readings come from the hash-chained
store committed by this workflow. A run that wrote a corrupt or tampered record
would, if that record reached a redeploy, publish wrong figures under the
project's name — and the hash chain is the thing that makes that detectable.

**How a compromise would show up.**

- The pre-commit verification step (`wayfared -verify-store`) rejects chains
  that do not verify. A tampered or corrupted record produced by this workflow
  lands as a PR that fails its own checks, not as merged history.
- The store is append-only and hash-chained. Any edit to a previously committed
  record changes its hash and breaks the chain from that point forward, which
  `wayfared -verify-store` reports on load and `docs/verify-store.md` describes.
- Because the PR is created by a well-known action with an explicit add-paths of
  `data/`, unexpected files in the PR are visible in the diff rather than
  silently committed.
- A push straight to `main` from this workflow is not possible — its write
  path is through a PR, and `main` is protected by a pull-request ruleset.

**Current state.** The workflow is currently unable to push and so cannot open
its measurement PR. The served history is therefore frozen at its last
successful sweep ([#63](https://github.com/Wayfare-labs/wayfare/issues/63)).
While that is a reliability problem rather than a security one, it also means
freshness is gated by redeploy rather than by the scheduler until the push path
is restored.

### `.github/workflows/auto-merge.yml` — `auto-merge`

**Events.** `pull_request_target` (opened, synchronize, reopened,
ready_for_review, labeled, unlabeled, edited), `workflow_run` on CI completion,
`pull_request_review`, `issue_comment`.

**Declared permissions.**

- `contents: write`
- `pull-requests: write`
- `checks: read`
- `issues: write`

**What it can do.** It evaluates an open pull request against seven mechanical
gates and, if all pass, enqueues a squash merge with the default token. It can
label a PR `needs-maintainer-review`, add a comment, and — when it merges — close
a linked issue that the merge body references.

**What it cannot do.** It never checks out or executes the PR head's code. It
reads metadata through the GitHub API only — diff files, check-run state,
reviews, comments, and issue bodies. The workflow file states this constraint
explicitly and the gate is exercised by `go test ./automerge`.

**Why this is a supply-chain path.** It is not a path for arbitrary code: it
will not merge anything touching maintainer-owned paths, any new dependency, or
anything whose checks are not green. But it *is* a path that can land a PR with
a write token, and the merge comment it posts is an authoritative-looking
approval. A misconfigured rule in the gate — or a token with more scope than the
workflow declares — could make the gate land something it should not.

**How a compromise would show up.**

- The gate is mechanical and inspectable: it fails closed on anything it cannot
  verify, so ambiguity produces a hold, not a merge.
- Any merge it performs is recorded as a squash-merged PR with the merge
  comment describing exactly what was and was not verified. A merge that skipped
  review would still leave that comment, which says plainly that the gate formed
  no opinion on substance.
- A token with larger scope than the workflow's declared permissions would not
  by itself change the gate's logic, but it would enlarge what a compromised run
  could do after landing. The declared permissions are therefore the binding
  constraint to check.
- Because `pull_request_target` and `workflow_run` run the base branch's
  workflow definition with a write token, the relevant threat is not a PR
  contributor adding code to a run — that is blocked by the no-checkout rule —
  but a change to the workflow file itself landing through some other path. That
  is exactly why `.github/workflows/` is a maintainer-owned path in gate 1 of
  the same workflow.

### `.github/workflows/ci.yml` — `CI`

**Events.** push to `main`, pull request to `main`, merge-group.

**Declared permissions.**

- `contents: read`

**What it can do.** It builds, tests, lints, verifies tests run with no network,
builds the container image, and scans it. It cannot write to the repository.

**Why it is listed.** For contrast: this is the workflow whose outputs a reader
might assume are the supply-chain path. They are not. CI validates changes; it
never produces content that reaches the deployed binary, and it has no write
token.

**How a compromise would show up.** A CI compromise would show up as a failed
or unexpected check on a pull request, or as an image scan finding — both are
visible in the PR and in the actions run log. CI itself cannot mute those.

## What is not covered here

This section does not cover:

- the contents of the embedded history itself — that is the subject of
  `docs/embedded-history.md` and `docs/verify-store.md`;
- dependency supply-chain policy — that is `docs/dependency-policy.md`;
- image vulnerability findings — that is `SECURITY.md`.
- vulnerability reporting — that is `SECURITY.md`.

## Acceptance criteria

- [x] Section added.
- [x] Each workflow's permissions listed.
- [x] The permission and blast radius per workflow documented for the two writing
      workflows (`measure`, `auto-merge`), with CI listed for contrast as the
      read-only workflow.
