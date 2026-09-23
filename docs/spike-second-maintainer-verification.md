# Spike: how would a second maintainer verify a published claim?

Issue [\#223](https://github.com/Wayfare-labs/wayfare/issues/223), backlog `#163`.

**Status: completed.** The reproducibility story is strong in code and — as of
this spike, actually run the way a stranger would — it holds: a second
maintainer following the documented commands can independently verify (a) that
no recorded figure was edited after writing, (b) that recorded-run figures are
derived by the pinned code from pinned input bytes, and (c) which claims are
**deliberately not** reproducible from the tree and must be re-trusted live.
The story's one genuine weakness is documented honesty: several relationships a
verifier would want drawn together (figure → snapshot, figure → run record)
are manual today. No code was written; findings beat on the tree as it is.

---

## Why this matters

Wayfare's every published figure ("USDC → NGNC lost 25.02% at 0.1 USDC,
measured live on 2026-08-21") rolls up to a reader trusting **two separable
things**: the measurement happened as described, and nobody adjusted it
afterwards ([docs/run-store.md](run-store.md) states this split explicitly,
`docs/run-store.md:11-18`). A second maintainer, a migration, a backup restore,
or an auditor all need to re-establish those two claims without trusting the
person who wrote the numbers. The compatibility of the code claims to make this
possible; this spike runs the scenario end-to-end and reports what a stranger
can and cannot verify.

---

## What is verifiable today, with sources

All commands, flag names, and file layouts were checked against the current
tree on 2026-09-23.

### 1. "No figure was edited after it was recorded"

- **Command:** `go run ./cmd/wayfared -verify-store -data ./data`
  (`docs/verify-store.md:12-24`).
- **Mechanism:** every record's `hash` is `sha256` of the record JSON with the
  `hash` field omitted; every record links to its predecessor via `prev_hash`
  (preimage rule in `docs/run-store.md:118-130`, record shape
  `docs/run-store.md:36-114`). Editing any past record changes its own hash and
  breaks every record that follows.
- **Exit codes make this scriptable:** 0 = all chains clean, 1 = at least one
  chain failed or the store would not open, 2 = verify requested without a data
  directory (`docs/verify-store.md:194-200`).
- **The measure workflow treats it as a gate, not a nicety:** the scheduled
  sweep runs `wayfared -verify-store` *before* opening its pull request, so a
  broken chain never lands (`/measure.yml:45-58`).
- **CI also runs it on every push** (`docs/verify-store.md:172-173`, CI
  workflow `offline-tests` job — `.github/workflows/ci.yml`).

### 2. "The measurement happened as described, on this code, from these bytes"

- **Record-and-replay is byte-pinned.** `ladder -record` captures the verbatim
  upstream responses (`docs/snapshot-record-replay.md:25-52`); bodies are
  pinned by `sha256` in the manifest and re-verified on load
  (`docs/snapshot-format.md`, manifest files under `testdata/snapshots/*/manifest.json`
  carry `body_sha256` per interaction).
- **Snapshots name the code that produced them.** `git_revision` is written into
  the manifest and recording is refused on a dirty tree unless
  `-allow-dirty` is passed (`docs/snapshot-record-replay.md:48-52`). The
  committed snapshots of 2026-08-21 carry revisions `ee3d2b3` (GHSC, KESC) and
  `b36a3af` (NGNC) (`testdata/snapshots/*/manifest.json`, checked 2026-09-23).
- **Replay never falls through to the network.** The replayer serves recorded
  bytes only; a request for something not recorded returns an explicit
  not-recorded result, so a replay test cannot silently "measure live"
  (`docs/snapshot-format.md`, runner wiring in `checks/runner.go:82-87`).
- **There is a working reproducibility precedent in the tree.** `cmd/hop-analysis`
  replays the recorded snapshots and `TestAnalyseReproducesDocFigures` pins the
  exact figures that `docs/native-xlm-routing.md` publishes
  (`cmd/hop-analysis/main_test.go:8-60`, `cmd/hop-analysis/main.go:1-17`). A
  stranger can satisfy themselves that a *published table in a doc* was produced
  by the recorded bytes without touching the network — this is the strongest
  reproducibility link that exists.

### 3. "The figures reported are the ones the wire would have returned"

- Run records are derived from `route.CorridorJSON` — "the same shape the HTTP
  API and `ladder -json` emit" (`docs/run-store.md:87-89`) — and the stale-path
  HTTP reader reconstructs a live-identical document from storage
  (`server/api.go:470-597`). Verdict/threshold pins live in `route/route.go`
  tests and are part of the ordinary `go test` suite.

---

## What a second maintainer actually does (the run-through)

Checked end-to-end against the current tree on 2026-09-23:

1. Clone at the pinned revision (or the revision a published figure names).
2. Run `go test ./...` (includes the offline-replay suite; CI enforces the no
   network expectation in `offline-tests`, `.github/workflows/ci.yml:95-126`).
3. Run `go run ./cmd/wayfared -verify-store -data ./data` and read the per
   corridor `ok`/`FAIL` lines and exit code (`docs/verify-store.md:47-109`). In
   this checkout the committed `data/*.ndjson` each hold **exactly one record**
   (`seq 1`, `version 1`, recorded `2026-08-22`; `data/` scanned 2026-09-23) —
   so the chain verifies trivially, but see the limits section below.
4. For a figure backed by a committed snapshot: run the replay path (e.g.
   `go run ./cmd/hop-analysis -snapshots ./testdata/snapshots` —
   `cmd/hop-analysis/main.go:85-93`) and confirm the published figure matches
   the replay-derived report. This re-derives the figure from recorded bytes
   and pinned hashes with zero network.
5. For a **live** figure (one not recorded offline): re-measure live with the
   same corridor pair, sizes, and reference provider (`ladder`/API) and compare
   — accepting the documented rule that live re-measurement **differs by
   design**; what is verified is *attribution and direction and method*, not
   bit-equality (see limits below).

---

## What a stranger cannot verify from the tree (limits, reported as such)

These are honest gaps, found by actually running the scenario, **not** design
regrets. All checked against the current checkout on 2026-09-23.

1. **The committed history here is one record per corridor.** The initial sweep
   ran 2026-08-22 and this repo's `data/` contains only that first record per
   corridor (`data/USDC-*.ndjson`, seq 1 each). A two-red-record chain cannot
   demonstrate much about tamper-evidence; the *code* supports it
   (`docs/run-store.md`, `docs/verify-store.md` show multi-record examples from
   the 2026-08-21 deployment), but a stranger whose only copy is this checkout
   cannot exercise a broken-chain case end-to-end without the older data. That
   is a property of the demo data, not of the chain.
2. **Live figures are not reproducible bit-for-bit, and the code says so.**
   `docs/verify-store.md:177-190` is explicit: verification proves the stored
   history is the one written, not that a measurement was *correct*; run-store's
   "what this does not prove" section is equally explicit
   (`docs/run-store.md:25-32`). A live figure from a moment with no snapshot has
   no byte record in the tree; the second maintainer verifies source, method,
   and plausibility, then must *trust* the recorder for correctness of that
   instant.
3. **Figure → snapshot and figure → record are manually joined.** Nothing
   currently auto-links "the 2026-08-21 figure in `corridor-measurements.md` was
   produced by the NGNC snapshot of `2026-08-21T22:30:40Z` at revision b36a3af".
   `hop-analysis` proves the *method* reproduces the doc table, and the record's
   `recorded_at`/`reference` fields correlate, but the join is inference, not
   machinery. A future issue could add a `figure_sourced_from` back-reference.
4. **The snapshot set is curated, not exhaustive.** `testdata/snapshots/` holds
   eight fixtures (USDC→NGNC/GHSC/KESC 2026-08-21 and BRL/INR/MXN/PHP
   2026-08-23 plus a malformed-strict-send fixture) — the corridors the project
   has explicitly exercised, not every corridor ever measured
   (`testdata/snapshots/` listing, 2026-09-23). A figure for an un-snapshotted
   corridor/reference pair at an unrecorded moment has no replay route.
5. **Snapshot manifest `git_revision` is only present where committed
   cleanly.** The 2026-08-21 fixtures carry revisions; the 2026-08-23
   research-corridor fixtures do not include the key at all
   (`testdata/snapshots/usdc-brlc-20260823T000000Z/manifest.json` shows no
   `git_revision`), consistent with the `-allow-dirty`/manual research path.
   A verifier of those fixtures learns *what* and *when* but not *which commit
   produced them*. Noted as-is, not as a defect in the documented workflow.

---

## Verdict

**Positive with a documented tail.** The central claims a second maintainer
would want to re-establish — "nobody edited recorded figures" and "the replay
path reproduces published numbers from pinned bytes" — are verified by two
short commands and run as gates in the project's own workflows
(`measure.yml`, `ci.yml`). The figure-reproduction precedent
(`hop-analysis` ↔ `native-xlm-routing.md`) is the single most convincing piece
of evidence in the tree. What is **not** reproducible (limit 2) is not a gap
but a boundary the project documents on purpose: correctness-at-the-instant of a
live measurement is recorded as trust, not re-derivable as arithmetic. The
actionable follow-ups — a machine-readable figure→snapshot link, and
`git_revision` on every snapshot — are small and could be scheduled as backlog
items, but **no implementation was attempted as part of this spike.**

## Related

- [verify-store.md](verify-store.md), [run-store.md](run-store.md) — the two
  trust claims and each one's mechanism
- [snapshot-format.md](snapshot-format.md), [snapshot-record-replay.md](snapshot-record-replay.md)
  — byte-pinning and the record/replay loop
- `cmd/hop-analysis/` — the working figure-reproduction precedent
- `.github/workflows/ci.yml` (`offline-tests` job), `.github/workflows/measure.yml`
  — verification as a gate
- `data/`, `testdata/snapshots/` — the current store and fixture set this spike
  inspected