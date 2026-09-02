# Verifying a store

How to prove the measurement history has not been edited after it was written,
and what each failure mode looks like.

**Status: implemented.** This document covers the `-verify-store` flag on
`wayfared` as it exists in the current tree. All claims below were verified
against the code at time of writing (2026-08-27).

---

## What `-verify-store` does

`-verify-store` walks every corridor's hash chain in the run store, recomputes
every record's hash, checks every link, and reports pass or fail for each
corridor. It then exits with a code the caller can test.

```bash
wayfared -verify-store -data ./data
```

That is the entire command. It reads the NDJSON files under `-data`, verifies
each chain, prints a result per corridor, and exits. It does not start an HTTP
server, does not measure, and does not write anything.

---

## How to run it

```bash
# against the committed history
go run ./cmd/wayfared -verify-store -data ./data

# or, if you built the binary
./wayfared -verify-store -data ./data

# on a deployed Fly instance
fly ssh console -C "/wayfared -verify-store -data /data"
```

The command needs a `-data` directory that contains `.ndjson` files — one per
corridor. An empty or missing directory is valid; there is simply nothing to
verify.

---

## What the output looks like

### All chains pass

When every corridor's chain verifies clean:

```
no corridor history to verify
```

This is printed when the data directory exists but contains no `.ndjson` files
yet.

```
ok   USDC-GHSC: 30 records, latest 2026-08-21T22:29:49Z
ok   USDC-NGNC: 42 records, latest 2026-08-21T22:30:40Z
```

Each `ok` line names the corridor, the number of records in its chain, and the
timestamp of the most recent record. The exit code is **0**.

### Some chains fail

When one or more corridors fail:

```
ok   USDC-GHSC: 30 records, latest 2026-08-21T22:29:49Z
FAIL USDC-NGNC: runstore: USDC-NGNC: record seq 3 expects prev_hash
    sha256:1872c8f15412… but the previous record hashes to
    sha256:8a4eecd77b48…; the chain is broken at position 2

1 of 2 chains failed verification
```

The exit code is **1**. The `FAIL` line carries the corridor name and the
underlying error from `runstore`. The summary line at the end counts how many
out of the total failed.

### Store cannot open

If the data directory is missing, unreadable, or contains a file the store
cannot parse, the failure is reported before any chain is walked:

```
FAIL runstore: reading ./data: <system error>
```

or, if a file exists but is not valid JSON or uses a version the build does
not recognise:

```
FAIL runstore: USDC-NGNC line 3 has record version 9, this build
    understands 2; refusing to guess at a schema it does not know
```

or, if a record's JSON is malformed:

```
FAIL runstore: USDC-NGNC line 1: invalid character ',' looking for
    beginning of value
```

In every case the exit code is **1**.

---

## What a broken chain is

A chain is a sequence of NDJSON records, each carrying the SHA-256 hash of the
record before it. Two things can go wrong:

### A record was modified after it was written

Every record's hash is computed over its own contents (the "preimage"). If
anybody edits a field — a loss percentage, a timestamp, a verdict — the
stored hash no longer matches. `VerifySelf` detects this:

```
runstore: USDC-NGNC: record seq 3 has hash sha256:1872c8f15412…
but its contents hash to sha256:8a4eecd77b48…; it was modified
after it was written
```

The two truncated hashes let you see which record stored one hash but hashes
to another. The record at `seq 3` was tampered with.

### The chain link is broken

Each record also carries `prev_hash`, which must equal the `hash` of the
record before it. If an earlier record was modified (or removed, or reordered),
the chain snaps at the next record:

```
runstore: USDC-NGNC: record seq 4 expects prev_hash
sha256:1872c8f15412… but the previous record hashes to
sha256:8a4eecd77b48…; the chain is broken at position 3
```

Record `seq 4` says its predecessor hashed to `1872c…`, but record `seq 3`
actually hashes to `8a4e…`. The break is at position 3 (0-indexed) in the
chain — the record right before the one that detected the mismatch.

### Why both errors matter

A "modified after written" error means a single record was edited. A "chain
broken" error means an earlier record was edited, which invalidated every
record after it. Both are evidence of tampering, but the second implies more
of the history was affected. The command reports the first failure it finds
and stops — fixing the earlier record is required before the later ones can
verify.

---

## When to run it

The command exists for two moments:

1. **After a deploy.** Every new build opens the store and verifies every
   chain at startup (see `Open` in `runstore/file.go`). `-verify-store` lets
   you run the same check without starting the server.

2. **After restoring from backup.** A backup is only as trustworthy as the
   last time someone restored it and verified it. Copy the NDJSON files
   off-box, restore them, and run `-verify-store` before trusting the result.

CI runs this check on every push (`.github/workflows/ci.yml`), and the measure
workflow runs it after writing new records (`.github/workflows/measure.yml`).

---

## What verification does not prove

Verification proves the stored history is the one that was written. It does
**not** prove that a measurement was correct, that a corridor was priced
fairly, or that the numbers in a record reflect reality. Those are separate
claims — integrity (the measurement happened as described) and correctness
(the measurement described reality accurately).

There is one writer and the chain lives in a file. Someone with write access
to the data directory can rewrite the entire chain from any point. What they
cannot do is edit one record and leave the rest intact — which is the
realistic failure mode the chain is designed to detect.

This is not a blockchain and makes no distributed-consensus claim.

---

## Exit codes

| Code | Meaning |
|:-----|:--------|
| 0 | All chains verified clean, or no corridor history exists |
| 1 | At least one chain failed, or the store could not be opened |
| 2 | `-verify-store` was requested but no data directory is configured |

---

## Backups

The chain proves nothing was edited. It does **not** survive the volume dying.
Copy the NDJSON files off-box on a schedule, and verify after restoring — a
backup nobody has restored from is a hypothesis. See
[deployment.md](deployment.md).

---

## Related

- [run-store.md](run-store.md) — record shape, preimage rule, storage format
- [deployment.md](deployment.md) — how to run continuously and verify after
  restore
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
