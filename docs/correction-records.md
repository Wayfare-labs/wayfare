# Correction Records: Fixing a Wrong Published Figure in an Append-Only Chain

**Status:** Implemented (issue #729, Schema Version 5).
**Related runbooks:** [wrong-published-figure.md](wrong-published-figure.md), [run-store.md](run-store.md), [verify-store.md](verify-store.md).

---

## 1. Context and Problem Statement

Wayfare records corridor pricing and market structure measurements into an append-only cryptographic hash chain (`data/<CORRIDOR>.ndjson`). Each record seals its contents and links to the previous record's hash via `prev_hash`.

When a defect in code (such as an arithmetic error in a route fee denominator or an asset misclassification) causes an erroneous figure to be committed to the chain, maintainers cannot edit or delete the faulty record in place:
1. Modifying bytes on disk breaks `hash` verification for that record (`ErrHashMismatch`).
2. Modifying `hash` invalidates `prev_hash` on every subsequent record in the chain (`ErrPrevHashMismatch`).
3. Binary releases embed the historical chain; rewriting past lines invalidates reproducibility and breaks independent auditor verification.

Before issue #729, the interim practice was manual disclosure in GitHub issues without an on-chain correction mechanism. Issue #729 defines and implements append-only correction records that amend published figures while keeping the cryptographic chain intact.

---

## 2. Specification: Schema Version 5

### The `Correction` Struct

Version 5 introduces the `Correction` struct on `runstore.Record`:

```go
type Correction struct {
    TargetSeq int64  `json:"target_seq"`
    Reason    string `json:"reason"`
}
```

- `TargetSeq`: The positive 1-based sequence number (`seq`) of the earlier record in the corridor's chain being corrected.
- `Reason`: A concise, human-readable justification stating why the target record was wrong and what bug or condition was addressed.

### Placement in `runstore.Record`

In `runstore/runstore.go`:

```go
type Record struct {
    Version    int       `json:"version"`
    Seq        int64     `json:"seq"`
    RecordedAt time.Time `json:"recorded_at"`
    Corridor   string    `json:"corridor"`
    ...
    Snapshot        string                 `json:"snapshot,omitempty"`
    DependencyChain *route.DependencyChain `json:"dependency_chain,omitempty"`

    // Correction describes an erratum against an earlier sequence number (issue #729).
    // Optional; omitted when empty so pre-Version 5 record hashes verify unchanged.
    Correction *Correction `json:"correction,omitempty"`

    PrevHash string `json:"prev_hash"`
    Hash     string `json:"hash"`
}
```

### Preimage Invariance and Backward Compatibility

`Correction` is declared with `omitempty` and positioned after every Version 4 field:
- A legacy record (Version 1 through 4) without a correction has `Correction == nil`.
- Go's `encoding/json` omits nil `omitempty` fields.
- Therefore, existing chains with Version 1–4 records produce the exact same JSON preimage bytes and cryptographic hashes under this Version 5 build.
- `TestRecordHashIsPinned` and `TestVersion1RecordStillVerifies` confirm bit-for-bit backward compatibility.

---

## 3. Chain Verification Rules

Verification (`store.Verify` and `wayfared -verify-store`) treats correction records as ordinary chained records, asserting:
1. **Cryptographic continuity:** `r.VerifySelf()` passes (record contents hash to `r.Hash`), and `r.PrevHash == prev.Hash`.
2. **Sequential progression:** `r.Seq == prev.Seq + 1`.
3. **Valid backward reference:** If `r.Correction != nil`:
   - `r.Correction.TargetSeq >= 1`
   - `r.Correction.TargetSeq < r.Seq` (a correction cannot reference a future or current sequence number).
   - `strings.TrimSpace(r.Correction.Reason) != ""` (an empty correction reason is rejected).

---

## 4. Effect on Trend, Transitions, and Movements

### Effective History Resolution (`EffectiveRecords`)

To ensure historical analysis is not distorted by retracted bugs, `runstore.EffectiveRecords` provides a normalized view:
- Maps each `TargetSeq` to its amending correction record.
- Replaces target records with their corrected measurement figures (rungs, floor loss, integrity, finding) while preserving the target's original measurement timestamp (`RecordedAt`) and sequence number (`Seq`).
- Excludes purely retrospective correction records from being treated as duplicate time-series measurement events.

### Transition Detection & Movement Decomposition
- `TransitionsFromRecords(records)` resolves effective records before comparing consecutive states. Retracted false alarms (such as a temporary `DIRECT -> UNKNOWN` or `BROKEN` caused by a calculation defect) are eliminated.
- `MovementsFromRecords(records)` computes benchmark and corridor movements based on corrected rates rather than buggy outputs.

---

## 5. API and UI Exposure: Surfacing Both Values

The core design principle is transparency: **both the original published value and the corrected figure must be visible.**

### HTTP API (`/api/corridor/trend` and `/api/corridor`)

In `TrendRunJSON` and `route.CorridorJSON`:

```json
{
  "seq": 10,
  "floor_loss_pct": "80.00",
  "worst_loss_pct": "95.00",
  "correction": {
    "corrected": true,
    "corrected_by_seq": 15,
    "reason": "fixed decimal precision error in quote calculation",
    "original_floor_loss_pct": "80.00",
    "corrected_floor_loss_pct": "20.00",
    "original_worst_loss_pct": "95.00",
    "corrected_worst_loss_pct": "25.00"
  }
}
```

And on the correction record itself (seq 15):

```json
{
  "seq": 15,
  "floor_loss_pct": "20.00",
  "worst_loss_pct": "25.00",
  "correction": {
    "is_correction": true,
    "target_seq": 10,
    "reason": "fixed decimal precision error in quote calculation",
    "original_floor_loss_pct": "80.00",
    "corrected_floor_loss_pct": "20.00"
  }
}
```

### Dashboard UI (`server/index.html`)

In the stored runs table (`runsTable`):
- Corrected runs display a badge with the correcting sequence number and reason tooltip.
- The floor loss and worst loss cells display both values:
  `<del>80.00%</del> 20.00%`
- The correction record in the table indicates which run it amends and shows what the original figure was.
