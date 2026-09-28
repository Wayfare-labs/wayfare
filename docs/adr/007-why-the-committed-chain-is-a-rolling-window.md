# ADR 007: Why the committed chain is a rolling window

**Status:** Accepted

## Context

Wayfare's measurements live in the repository (`data/`), appended by the
measure workflow every six hours and embedded into every build. Embedding the
history is the provenance story: anyone can clone the repository and verify
the hash chains without trusting a host.

That story has a price. At four records per corridor per day, a corridor grows
by roughly 1,460 records a year. Bottlenecked by the shortest-relevant history
(bottlenecked by the swap with the least history), a data directory that
eventually outgrows the repository is not a speculative concern — it is the
mechanism, running forever. The project needs a defined answer to what happens
when a chain outgrows the repository.

Storage backends (backlog #132) are out of scope: moving the history elsewhere
would trade the repository's strongest property — every reader can verify
every record — for a hosted service readers have to trust. The answer must keep
the history inside the repository.

## Decision

The committed `data/` is a **bounded, always-verifiable window**, not the
whole chain. When a corridor's chain exceeds the ceiling, the measure
workflow rotates it: the oldest records above the ceiling are dropped, and the
surviving newest records are re-sealed — the new window head's `prev_hash`
restarts from the genesis hash and every subsequent `prev_hash` is
re-derived — so the window verifies as a self-contained chain.

The ceiling is `runstore.MaxWindow` = **366 records per corridor** (one
calendar quarter at the six-hour cadence, matching the project's own 90-day
spike horizon).

The contract of rotation:

- **Measured contents are never touched.** Only `hash` and `prev_hash` move.
- **`seq` is preserved** and sequential, so a reader can tell where the window
  begins relative to the whole run.
- **Chains at or under the ceiling are untouched** — rotation only ever fires
  when the workflow has appended a record to an already-full window.
- **Dropped records are not destroyed.** The archive is the repository's own
  git history: every prior commit contains the chain as it stood, so a record
  dropped today remains at the commit it was swept into. Rotation is the
  mechanism that lets a data directory keep verifying forever without the
  repository growing forever.

## Consequences

- Good: the repository stays bounded — about one quarter of history online,
  growing no faster than the past business quarter slides.
- Good: the window always ends on the newest measurement; a sweep can never
  be rotated out before its successor exists.
- Good: the window always verifies, and rotation itself re-verifies before it
  writes.
- Good: older records still exist, in the exact bytes that were verified at
  their time — a reader who wants the deep history can walk the git log
  instead of one unbounded file.
- Cost: records older than the window are no longer in `data/` at HEAD, so a
  fresh clone sees the recent window and must visit history for the rest.
- Cost: a reader comparing HEAD with an old commit sees the head record at
  those two points differ in `hash` — the visible signature of a rotation,
  and deliberate: a re-sealed window must not masquerade as the original
  chain.

## Evidence

- `runstore/file.go` — `MaxWindow`, `Rotation`, `FileStore.Rotate`, `writeAll`
- `runstore/rotate_test.go` — rotation behavior (ceiling trim, re-seal,
  contents preserved, per-corridor isolation)
- `.github/workflows/measure.yml` — the rotate step between measure and verify
- `cmd/wayfared/main.go` — the `-rotate-store` / `-rotate-records` flags