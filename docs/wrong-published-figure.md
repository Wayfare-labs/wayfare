# Responding to a wrong published figure

How to respond when a measurement already written to `data/` or served by the
API is found to be wrong: who decides, how the error is recorded without
breaking hash-chain verification, and how consumers are told.

[rollback.md](rollback.md) covers returning a deployment to an earlier binary
image. Rolling back an image changes which code serves the chain; it does not
retract a measurement that was already recorded. This runbook covers data
correctness when a published figure itself is wrong.

**Status: interim procedure (manual disclosure; append-only correction records
tracked in issue #729).** The current `runstore.Record` schema in
`runstore/runstore.go` has no correction record type yet. Until issue #729
lands, the repository handles wrong figures through the interim procedure
below rather than silently editing history.

---

## What counts as a wrong published figure

A published measurement is wrong when the recorded figure does not reflect what
the upstream venues actually returned at `recorded_at` under the repository's
documented rules, for example:

- **Arithmetic or unit defect in Wayfare code:** a bug in `dex/`, `sep38/`,
  `refrate/`, or `route/` miscomputed an effective rate, fee, spread, or loss
  percentage from valid inputs.
- **Mis-recorded asset or provider identity:** a corridor measured the wrong
  issuer account, wrong SEP-38 quote direction, or a stale reference-rate scale.
- **Corrupted capture treated as priced:** a malformed or truncated response
  was recorded with `priced: true` instead of an unpriced rung or
  `UNABLE-TO-DETERMINE`.

What does **not** count as a wrong figure:

- **Unusual but genuine market state:** a wide spread, an empty book (`NO-MARKET`),
  or a `BROKEN` verdict that accurately reflects the order book at
  `recorded_at` is a valid observation, not an error to retract.
- **Explicit unknown state:** a record carrying `UNABLE-TO-DETERMINE` because a
  reference provider or Horizon endpoint failed is working as designed.

---

## 1. Who decides

A maintainer with write access to `Wayfare-labs/wayfare` confirms that a
published figure is wrong before any public notice or chain action is taken.

Anyone — a contributor, an API consumer, or an automated check — may report a
suspected bad figure by opening an issue that cites:

1. **Corridor and sequence number:** e.g. `USDC-NGNC`, `seq`, and `recorded_at`
   timestamp from `data/<CORRIDOR>.ndjson` or `/api/corridor`.
2. **Record hash and source commit:** the `hash` field of the affected record
   and the git commit on `main` that introduced or recorded it.
3. **Reproduction:** the command, test, or recorded snapshot under
   `testdata/snapshots/` showing the discrepancy between the published figure
   and the verified calculation.

The maintainer verifies the reproduction against the code that produced the
record and decides whether the record is erroneous.

---

## 2. How it is recorded

### Why `data/*.ndjson` is never edited in place

Every line in `data/<CORRIDOR>.ndjson` commits to the previous record's `hash`
through `prev_hash` (`runstore/runstore.go`). Modifying or deleting a historical
line in place invalidates its hash and breaks `prev_hash` for every subsequent
record in that corridor, causing `wayfared -verify-store -data ./data` and
`TestEmbeddedHistoryVerifies` (`embed_test.go`) to fail with `ErrHashMismatch`
or `ErrPrevHashMismatch`.

### Target design (issue #729 — not yet implemented)

Issue #729 specifies an append-only correction record type that references an
earlier record's `seq`, records what was wrong and why, and allows `/api/corridor`,
`/api/corridor/trend`, and the UI to surface both the original and corrected
values while keeping the hash chain intact.

### Interim practice (today)

Until issue #729 is implemented, follow these steps in order:

1. **Stop producing new wrong records first.**
   - If a recent code deploy introduced the calculation bug, roll the image back
     immediately following [rollback.md](rollback.md) or merge a fix with a
     regression test before the next scheduled `measure` sweep runs.
2. **Preserve chain verification (`wayfared -verify-store -data ./data`).**
   - Do **not** hand-edit historical lines in `data/<CORRIDOR>.ndjson`.
   - If the bad measurement is only at the very tip of `data/<CORRIDOR>.ndjson`
     (no valid subsequent record links to its `hash` yet), a maintainer may
     revert that specific `data/` commit on `main` and run:
     ```bash
     go run ./cmd/wayfared -verify-store -data ./data
     ```
     to confirm every corridor's chain still exits `0`.
   - If valid records have already been appended after the bad record, leave the
     bytes in `data/<CORRIDOR>.ndjson` untouched so cryptographic continuity is
     preserved, and record the correction in the disclosure ledger below.
3. **Record the erratum in the repository.**
   - Open or update a dedicated GitHub issue labelled `area:data` documenting:
     - Corridor (`FROM-TO`), affected `seq` range, `recorded_at` timestamps, and
       record `hash` values.
     - The faulty code path or input (file, function, and commit SHA).
     - The published figure vs. the recomputed figure (or `UNABLE-TO-DETERMINE`
       if the true figure cannot be recovered from recorded inputs).
   - Cross-reference the erratum from the relevant corridor case study in `docs/`
     (for example, `docs/ngnc-structural-floor.md`) if any prose or table cites
     the affected window.

---

## 3. How consumers are told

Because `/api/corridor` and `/api/corridor/trend` do not yet emit machine-readable
correction records (issue #729), consumers must be notified visibly where they
discover and track the project:

1. **Public GitHub issue and cross-link:** Keep the erratum issue open (or
   linked from `docs/`) until the affected records age out of the rolling
   embedded window ([adr/007-why-the-committed-chain-is-a-rolling-window.md](adr/007-why-the-committed-chain-is-a-rolling-window.md))
   or are superseded by a correction record under issue #729.
2. **Exact consumer fields named:** State clearly which API fields were affected
   (`rungs[].effective_rate`, `rungs[].loss_pct`, `verdict`, `integrity`, or
   `reference_rate`), the exact UTC time window (`recorded_at` start and end),
   and the commit SHA where the fix deployed.
3. **No silent backfill:** Never overwrite an embedded window and redeploy
   without a commit message and issue reference explaining why the window tip
   changed.

---

## Related

- [rollback.md](rollback.md) — rolling back a deployed binary image and verifying the store
- [verify-store.md](verify-store.md) — how `wayfared -verify-store` checks hash links
- [run-store.md](run-store.md) — the append-only hash chain and preimage rules
- [api-consumer.md](api-consumer.md) — what downstream consumers read from the HTTP API
- [adr/007-why-the-committed-chain-is-a-rolling-window.md](adr/007-why-the-committed-chain-is-a-rolling-window.md) — how rolling window rotation re-seals surviving records
