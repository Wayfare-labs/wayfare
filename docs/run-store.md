# Run store

A tamper-evident history of corridor measurements.

**Schema version 3.** Implemented by `runstore/`. Version 1 and Version 2
chains still load and still verify — see [Migration to version 2](#migration-to-version-2)
and [Migration to version 3](#migration-to-version-3).

---

## What problem this solves

Wayfare's claims look like *"measured live on 2026-08-08, USDC → NGNC lost
25.02% at 0.1 USDC"*. A reader has to trust two separate things:

1. **That the measurement happened as described.** Snapshots address this —
   see [snapshot-format.md](snapshot-format.md).
2. **That nobody adjusted it afterwards.** That is this package.

Each record carries the hash of the record before it, so a stored history is a
chain rather than a pile. Editing any past record changes its hash, which
breaks every record after it. `Verify` walks the chain and names the first
record that does not reconcile.

**What this does not prove:** that a measurement was *correct*. It proves the
stored history is the one that was written. There is one writer and the chain
lives in a file, so anyone with write access can rewrite the whole chain from
any point. What they cannot do is edit one record and leave the rest intact —
which is the realistic failure mode: a number quietly improved long after it
was published.

This is not a blockchain and makes no distributed-consensus claim.

---

## Record shape

```json
{
  "version": 3,
  "seq": 42,
  "recorded_at": "2026-08-21T22:30:40Z",
  "corridor": "USDC-NGNC",
  "integrity": "DIRECT",
  "depends_on": [],
  "reference": {
    "mid": "1350.2568",
    "source": "currency-api",
    "as_of": "2026-08-21T00:00:00Z",
    "fetched_at": "2026-08-21T22:28:00Z",
    "secondary_mid": "1348.0585",
    "secondary_source": "exchangerate-api",
    "divergence_pct": "0.16",
    "scored_against": "currency-api"
  },
  "floor_loss_pct": "25.02", "floor_size": "0.1",
  "worst_loss_pct": "97.68", "worst_size": "5000",
  "recommended": null,
  "rungs": [
    {"send_amount": "0.1", "priced": true, "integrity": "DIRECT",
     "receive_amount": "102.78", "effective_rate": "1027.84",
     "loss_pct": "24.65", "verdict": "UNUSABLE", "path": "USDC -> NGNC"}
  ],
  "checks": [
    {"id": "sep10.endpoint-responds", "scope": "anchor",
     "subject": "ngnc.online", "severity": "warning",
     "determined": false, "passed": false,
     "reason": "no sep10 web-auth endpoint declared",
     "summary": "could not determine: no sep10 web-auth endpoint declared",
     "evidence": [{"source": "ngnc.online/.well-known/stellar.toml",
                    "observed": "NO WEB_AUTH_ENDPOINT"}],
     "observed_at": "2026-08-21T22:28:00Z"}
  ],
  "metrics": [
    {"id": "spread.bid-ask", "scope": "asset", "subject": "USDC",
     "determined": true, "value": "0.0004", "unit": "ratio",
     "summary": "bid-ask spread on the USDC/NGNC book",
     "evidence": [{"source": "https://horizon.stellar.org/order_book",
                    "observed": "bid=1350.1000 ask=1350.6400"}],
     "observed_at": "2026-08-21T22:28:00Z"}
  ],
  "prev_hash": "sha256:0000…0000",
  "hash": "sha256:6b1f…"
}
```

Records are derived from `route.CorridorJSON` — the same shape the HTTP API
and `ladder -json` emit — so the stored record and the served record cannot
drift into two schemas that disagree about the same measurement.

Four pieces are worth calling out:

**`recommended` is `null`, not omitted,** when no size produced an acceptable
route. That is the normal shape of a broken corridor, and storing it as an
explicit null means a history can never be read as though the monitor made a
recommendation it refused to make.

**`reference` carries both mids, always,** even when the providers agree. A
corridor's numbers moving because the benchmark changed is a completely
different event from the corridor moving, and a history recording only the mid
it scored against cannot distinguish them afterwards. `scored_against` names
which mid produced the verdicts in that record. Since Version 3 it also
carries `fetched_at` — when this project last obtained the rate, which can be
older than `recorded_at` when a cached rate was reused — so a reader can tell
how old the benchmark was when the reading was taken.

**`checks` and `metrics` carry the findings taken with the measurement.** A
check result preserves its tri-state — `determined: false` is *not* a failure —
plus its `reason`, `summary`, evidence and timestamp, exactly as a live
response serves them. A metric carries its `value` and `unit` as decimal
strings. In Version 2 a record may also omit either block entirely, meaning no
checks or no metrics ran.

**All money is a decimal string.** Never a JSON number, never a `float64`.

---

## The preimage rule

```
hash = sha256(preimage)
```

The preimage is the record's JSON encoding **with the `hash` field omitted**,
produced by Go's `encoding/json` over `runstore.Record` with:

- `SetEscapeHTML(false)`
- no indentation
- fields in struct-declaration order (which is what `encoding/json` emits)
- the trailing newline `Encoder.Encode` appends

`prev_hash` is **inside** the preimage. That is what chains the records:
altering an earlier record changes its hash, which changes the next record's
preimage, and so on to the end of the file.

Genesis `prev_hash` is `sha256:` followed by 64 zeros.

`Record.Preimage()` is exported so verification is reproducible by anyone,
using the same bytes the writer used rather than a reimplementation that might
differ in a detail like HTML escaping.

### Changing the record shape is a version bump

Adding, removing, or reordering a field changes the preimage of **every record
ever written**. A reader verifying last month's history against the new build
would be told it had been tampered with.

So a shape change is a `Version` bump plus a migration — never a compatible
change, and never a tidy-up.

This is enforced, not merely agreed. `TestRecordHashIsPinned` freezes the hash
of a fixed record; a purely cosmetic field reorder fails it. The test's own
comment says what to do when it goes red, because the tempting response —
updating the constant — is exactly the mistake it exists to prevent.

### Migration to version 3

Version 2 records had no `fetched_at` on the reference block. Version 3 adds
it, so a stored reading can tell a reader how old the benchmark was when the
reading was taken — the live wire already publishes `reference_fetched_at`,
and a replay that dropped it made stored history look current no matter how
reused the rate behind it was.

This is still a Version bump — the field set and the field order are part of
every hash — **but the migration is byte-for-byte invisible to existing
chains**, exactly like the Version 2 migration:

- The new field is declared `omitempty` and placed **after every Version 2
  field** of `runstore.Reference`.
- `encoding/json` emits struct fields in declaration order, so a record with
  an empty `fetched_at` encodes to exactly the same JSON — same field order,
  same contents — it did as a Version 2 record.
- Therefore a Version 2 record's hash is *unchanged* under Version 3, and a
  stored Version 2 chain still loads and still verifies with no rewriting.

Concretely:

- A record written as Version 1 or Version 2 stays that version on disk
  forever. Nothing is relabelled or rewritten — the store is append-only.
- New records are written `version: 3`, carrying `fetched_at` when the
  measurement knew when its rate was fetched and omitting it when it did not
  (an older build's record, or a provider that left the stamp unset — the
  honest "unknown", never a fabricated time).
- A corridor's file can therefore become a **mixed-version chain** (older
  Version 1 or Version 2 records, newer Version 3 records). `Open` and
  `Verify` walk all three; each record verifies against its own `version`
  field, and the chain links across every boundary because each record
  carries the full hash of the one before it regardless of who wrote it.

Verification, not rewriting, is what makes a mixed chain safe: the new build
*understands* Version 1 and Version 2 records rather than silently upgrading
them, so it can tell a genuine legacy record from a tampered one — and it
refuses any other version (see [the version-mismatch rule](#the-version-mismatch-rule)).

### Migration to version 2

Version 1 records had no `checks` or `metrics` block. Version 2 adds both.

This is still a Version bump — the field set and the field order are part of
every hash — **but the migration is byte-for-byte invisible to existing
chains**, and intentionally so:

- The two new fields are declared `omitempty` and placed **after every Version
  1 field**.
- `encoding/json` emits struct fields in declaration order, so a record with
  no findings encodes to exactly the same JSON — same field order, same
  contents — it did as a Version 1 record.
- Therefore a Version 1 record's hash is *unchanged* under Version 2, and a
  stored Version 1 chain still loads and still verifies with no rewriting.

Concretely:

- A record written as Version 1 stays `version: 1` on disk forever. Nothing
  is relabelled or rewritten — the store is append-only.
- New records are written `version: 2`, with a `checks`/`metrics` block when
  checks ran and both blocks omitted when none did.
- A corridor's file can therefore become a **mixed-version chain** (older
  Version 1 records, newer Version 2 records). `Open` and `Verify` walk both;
  each record verifies against its own `version` field, and the chain links
  across the boundary because every record carries the full hash of the one
  before it regardless of who wrote it.

Verification, not rewriting, is what makes a mixed chain safe: the new build
*understands* Version 1 records rather than silently upgrading them, so it can
tell a genuine legacy record from a tampered one — and it refuses any other
version (see [the version-mismatch rule](#the-version-mismatch-rule)).

### The version-mismatch rule

A record version this build does not recognise is an error, never a best-effort
parse: `runstore` accepts exactly `1`, `2` and `3`. Relabelling a record to
make it parse is falsification, not migration — a record that says `version: 9`
is a schema the build cannot speak, and guessing at it would hide the mismatch.

---

## Storage

NDJSON, one file per corridor, opened `O_APPEND`:

```
<dir>/USDC-NGNC.ndjson
<dir>/USDC-GHSC.ndjson
```

No database, deliberately. The access pattern is *append one record every few
hours, read the last one or two*. A file per corridor serves that exactly, is
inspectable with tools everyone already has, and makes the chain verifiable
with `sha256sum` and a text editor rather than a client library.

One file per corridor because the chains are independent; `Verify` walks one
at a time.

Every write is `fsync`ed. At one write per few hours durability matters more
than throughput, and a record acknowledged but lost in the page cache would
leave the in-memory tip ahead of the file — so the next `Open` would report a
broken chain for a record that was never really there.

### Read path

`Open` scans each file once, verifies the chain, and indexes the tail.
Thereafter:

- `Latest(corridor)` is a map read.
- `Recent(corridor, 2)` is the whole read path for integrity-change detection
  (issue #24).
- `Verify(corridor)` re-reads and rechecks the full chain.

`Open` **fails** if any existing chain is broken. A store that quietly loaded a
broken chain and appended to it would bury the break under valid records.

### Degrading gracefully

A `Nop` store discards everything and is safe everywhere, so callers never
branch on a nil store. `wayfared` with no history configured serves live
measurements exactly as it did before this package existed.

`Append` failures are logged, never propagated to the caller: a measurement
that succeeded should still reach the reader even if recording it failed.

---

## Verifying a chain

```bash
wayfared -verify-store          # walks every corridor, exits non-zero on failure
```

Run it after any deploy, and after any restore from backup. For the full
output — including what a broken chain looks like, exit codes, and every
failure mode — see [verify-store.md](verify-store.md).

A failure names the corridor and the `seq` of the first record that does not
reconcile:

```
runstore: USDC-NGNC: record seq 3 has hash 1872c8f15412… but its contents
hash to 8a4eecd77b48…; it was modified after it was written
```

### Backups

The chain proves nothing was edited. It does **not** survive the volume dying.
Copy the NDJSON files off-box on a schedule, and verify after restoring —
a backup nobody has restored from is a hypothesis. See
[deployment.md](deployment.md).

---

## Related

- [snapshot-format.md](snapshot-format.md) — recorded upstream bytes
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
