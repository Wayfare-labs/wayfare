# ADR 008: Why measurements stay on main

**Status:** Accepted

## Context

Measurements reach readers by being committed to `data/` and embedded into the
binary at build time ([embedded-history.md](../embedded-history.md)). Since the
switch to pull-request commits, every scheduled sweep has opened a pull request
against `main` — four a day, each through the required checks and a CodeRabbit
review, each held for a human because `data/` is a maintainer-owned path in the
auto-merge gate.

That cost is not hypothetical. It has broken the pipeline twice: the push
rejection of [#63](https://github.com/Wayfare-labs/wayfare/issues/63), and then
[#555](https://github.com/Wayfare-labs/wayfare/issues/555) — *GitHub Actions is
not permitted to create or approve pull requests* — which failed every run from
2026-08-22 to 2026-10-17. Fifty-six days, no records. Each corridor's committed
chain still holds exactly one record, so every trend, movement and transition
the project computes is built on a single point.

[ADR 007](007-why-the-committed-chain-is-a-rolling-window.md) already settled
that the committed chain is a rolling window rather than the whole history.
That makes the chain's *location* a decision rather than a law, and
[#557](https://github.com/Wayfare-labs/wayfare/issues/557) asks for the
decision to be made explicitly rather than inherited from whatever the
workflow happened to do.

Three candidates, judged on four things the project already cares about:
**integrity** (a published chain head must be checkable — `/api/chain-heads`,
[#332](https://github.com/Wayfare-labs/wayfare/issues/332)), **reproducibility**
(a given build's history must be derivable from what it was built from),
**review load**, and **redeploy freshness**.

### A. Pull requests to `main` (status quo)

- *Integrity.* A pinned head resolves to a commit on `main`. `api.md` tells a
  third party to "obtain the corresponding NDJSON history from the
  repository's committed data and verify its links" — on this route that
  history is on the default branch, at a commit with a parent chain nobody can
  rewrite without it being visible.
- *Reproducibility.* One commit is one image. `COPY . .` and `//go:embed
  all:data` make the embedded history a function of the source commit, which
  `TestEmbeddedHistoryVerifies` checks on every build.
- *Review load.* The real cost. Every sweep is a pull request a maintainer must
  look at, because the auto-merge gate holds anything touching `data/`.
- *Redeploy freshness.* Records are on `main` the moment they merge, so the
  next image built carries them.

### B. An orphan `data` branch, pinned by the build

- *Integrity.* The chain is still in the repository and still verifiable, but
  the branch is rewritten by the writer and protected by nothing. A head pinned
  from a deployment resolves to a branch tip, which is a moving target; the
  reader has to be told which commit, and that commit is no longer reachable
  from anything the project's review process guards.
- *Reproducibility.* This is where it fails. The pin has to live somewhere. If
  it lives on `main`, every bump is a pull request to `main` — the same traffic
  with a smaller diff, not less of it. If instead the build resolves the branch
  tip at build time, two builds of the same source commit embed different
  history, and the project loses the property that a build is determined by
  what it was built from. `rollback.md` already warns that chain heads are not
  comparable across images built at different times; making the data float
  would make them incomparable across images built from the *same commit*.
- *Review load.* Genuinely lower, which is the whole appeal.
- *Redeploy freshness.* Unchanged — the deployment still only refreshes when a
  new image is built.
- *Operational cost.* The Dockerfile would need git and network access during
  the build to fetch the branch, in an image whose current virtue is that it
  needs neither.

### C. Release assets

Worse than B on every axis above and adds one more: a release asset is mutable
by whoever can edit the release and has no commit history behind it, so a
reader who fetches one cannot tell whether it is the artefact that was
verified. It also requires a release cadence the project does not have.

### What the review load actually is

The pull-request count is a consequence of the workflow opening one per sweep,
not of the records living on `main`. A branch that accumulates (#556) collapses
four pull requests a day into **one standing pull request** that grows until it
is merged — the reviewer sees one diff whenever they get to it, and no sweep is
lost while they do not. That removes most of B's advantage without paying any
of its costs.

## Decision

**The committed chain stays on `main`, reached by one standing pull request
from `automation/measure`.** The dedicated data branch and release assets are
rejected.

Concretely:

- Each run adopts `automation/measure`'s `data/` before measuring, so sweeps
  accumulate on the branch instead of replacing one another.
- The branch carries at most one open pull request at a time; a sweep that
  arrives while it waits for review updates that pull request rather than
  opening another.
- The pull request is opened by a token that is not `GITHUB_TOKEN`, because
  `GITHUB_TOKEN` can neither create it nor cause CI to run on it
  ([deployment.md](../deployment.md#running-the-ci-path)).
- The build keeps embedding `data/` from the source tree. Nothing fetches
  history at build time.

**What would reopen this.** The decision rests on review load being tolerable
and on the window staying bounded. It should be revisited if either stops
holding:

- the standing pull request routinely waits longer than the rotation window
  (366 records, about one quarter) so that sweeps are rotated out before they
  are ever merged; or
- `data/` at the ceiling becomes large enough to make a clone or an image build
  materially more expensive than it is at three corridors.

Neither is true today: three corridors at the ceiling is roughly a megabyte.

## Consequences

- Good: a build's history remains a function of its source commit, and
  `TestEmbeddedHistoryVerifies` keeps proving it on every build.
- Good: a pinned chain head still resolves to a commit on the default branch,
  which is what `api.md` tells third parties to go and check.
- Good: the Dockerfile stays network-free and git-free.
- Good: review load drops from four pull requests a day to one open at a time,
  without moving the data.
- Cost: measurements still cannot merge themselves. Latency to `main` is
  bounded by how quickly a maintainer merges, not by the cadence — accepted
  deliberately, because `data/` is exactly the path whose unreviewed change
  would alter a published figure.
- Cost: the scheduled workflow now needs one credential (`MEASURE_TOKEN`),
  where the CI path previously advertised none. That trade is recorded in
  [deployment.md](../deployment.md#running-the-ci-path); the alternative is a
  repository-wide setting that lets *every* workflow create and approve pull
  requests, which is a wider grant for the same capability.
- Cost: the branch is force-pushed on every run. Non-destructive by
  construction — the tree being pushed contains every record the branch
  carried — and leased against the SHA the run fetched, so a branch that moved
  under the run fails the push instead of losing the other writer's records.

## Evidence

- `.github/workflows/measure.yml` — adoption, the lease, the standing pull
  request, and the alert
- `measure/workflow_test.go` — two sweeps without a merge both survive; a moved
  branch is not overwritten; a broken inherited chain stops the run
- `embed.go`, `embed_test.go`, `Dockerfile` — history embedded from the source
  tree at build time
- `docs/api.md` (`GET /api/chain-heads`) — what a pinned head obliges the
  project to keep resolvable
- `.github/workflows/auto-merge.yml` — `data/` as a maintainer-owned path, the
  source of the review load
