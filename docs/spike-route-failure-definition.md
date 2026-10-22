# Spike: what a route failure actually is, observationally

**Issue:** [#339](https://github.com/Wayfare-labs/wayfare/issues/339)
**Backlog entry:** #141, Initiative E / E2 — Predictive intelligence (V5)
**Date:** 2026-10-22
**Status:** Finding written. Inconclusive on the core question; the reasons are explained.

---

## The question

Failure prediction needs observed failures. The store records unpriced rungs.
Whether an unpriced rung is a *failure* is a definitional question nobody has
answered. This spike answers it — or, more precisely, establishes that the
question cannot be answered from what the store currently contains, and states
exactly what is missing.

---

## What the code actually records

All claims in this section are checked against the code at the commit current
on 2026-10-22.

### The in-memory distinction the engine makes

`route.Rung` carries three distinguishable terminal states for a given size
(checked: `route/ladder.go`, lines 60–75):

| State | `Rung.Err` | `Rung.Result` | `len(Result.Quotes)` | `Result.Integrity` |
|---|---|---|---|---|
| **Priced** | nil | non-nil | > 0 | DIRECT or DERIVATIVE |
| **No-market** | nil | non-nil | 0 | `IntegrityNoMarket` |
| **Upstream error** | non-nil | any | — | — |
| **Unknown** | nil | non-nil | 0 | `IntegrityUnknown` |

`Rung.Priced()` returns true only in the first state. `Rung.Unmeasured()`
returns true for the upstream-error and unknown states, but not for no-market.
This is intentional and documented in the `Failed()` comment
(`route/ladder.go:228–230`):

> NO-MARKET is a finding: the request succeeded and Horizon answered that no
> path exists. Failed means the question was never put.

### How the store serialises those states

`runstore.FromCorridorJSON` converts `route.CorridorJSON` to a `runstore.Record`
(`runstore/convert.go`, lines 15–110, checked 2026-10-22). Each rung is
serialised to `runstore.Rung` with:

- `Priced bool` — copied from `RungJSON.Priced`
- `Integrity string` — copied from `RungJSON.Integrity`
- `EffectiveRate string` — copied from `RungJSON.Quote.EffectiveRate` when
  `Quote != nil`, otherwise empty
- `LossPct`, `Verdict`, `Path`, `ReceiveAmount` — same rule

`countPricedRungs` in `runstore/measurability.go` (lines 192–200) counts a rung
as priced when `Priced == true && EffectiveRate != ""`. The `MeasurabilitySummary`
built on top of this calls any run with at least one such rung *measurable*.

### What the store does not record

The `runstore.Rung` struct has no field for the cause of `Priced == false`.
The four distinct engine states above — priced, no-market, upstream-error,
unknown — collapse to one of two stored values:

| Engine state | `runstore.Rung.Priced` | `runstore.Rung.Integrity` |
|---|---|---|
| Priced | `true` | `"DIRECT"` or `"DERIVATIVE"` |
| No-market | `false` | `"NO-MARKET"` |
| Upstream error | `false` | `"UNKNOWN"` |
| Unknown (upstream reached, no path, no market signal) | `false` | `"UNKNOWN"` |

`runstore.Rung.Integrity` is `"UNKNOWN"` for both the upstream-error and the
generic-unknown cases because both arrive from a rung whose `Result.Integrity`
was `IntegrityUnknown`. The error string that distinguishes them (`Rung.Err`)
is not stored.

At the corridor level `runstore.Record.Integrity` can be read to tell whether
the whole ladder was NO-MARKET or UNKNOWN. But there is no per-rung reason
field, so a record in which three rungs are UNKNOWN and nine are priced cannot
say which three were errors versus which three returned no paths.

---

## What "failure" could mean, and which meanings the store can currently support

### Meaning 1: the upstream was unreachable for this size

This is an operational event. Horizon returned a non-200, or the request timed
out, or the response was malformed. The engine records this as `Rung.Err != nil`
(`route/ladder.go:41-56`). The stored record does not carry it.

**Conclusion:** not observable from stored records as they exist today.

### Meaning 2: no path exists at this size

This is a market-structure fact. Horizon returned a 200 with an empty result
set. The engine sets `IntegrityNoMarket` (`route/route.go:621-693`). The stored
record carries `Integrity: "NO-MARKET"` at the rung level.

**Conclusion:** observable, but it is explicitly not a failure in the engine's
own terms — it is a *measurement* of the corridor's structure at that size. A
no-market rung is a successful observation; there is just nothing to price.

### Meaning 3: the reference rate was unscorable

The two reference providers disagreed past the 10% MALFUNCTION threshold, or
both failed. `route.Engine.Quote` returns an `unscored` result rather than an
error (`route/route.go:376-383`). The stored `CorridorJSON.Scored` is `false`.
This is a corridor-level fact, not a rung-level one.

**Conclusion:** observable at corridor level from the `ScoredAgainst` field in
`runstore.Reference`. Not expressible per-rung because a mal­functioning
reference makes the whole ladder unscored simultaneously.

### Meaning 4: every path found was priced UNUSABLE

The rung priced correctly; the loss was greater than 20%. This is a verdict, not
a failure — it is the monitor's thesis made concrete. The stored record carries
`Verdict: "UNUSABLE"` per rung.

**Conclusion:** fully observable. This is the most common terminal state on the
corridors currently measured.

### Meaning 5: the rung partially priced (priced at some sizes, not others)

`LadderResult.PartiallyFailed()` (`route/ladder.go:247-258`) reports this
condition; `UnmeasuredSizes()` names the gaps. The stored record carries
`Priced: false` for those rungs with `Integrity: "UNKNOWN"`, and separately
`Priced: true` for the rungs that answered.

**Conclusion:** partially observable. Whether a `Priced: false, Integrity:
"UNKNOWN"` stored rung was an upstream error or a genuine absence of path is
not recoverable from the stored record alone.

---

## The core finding

The question "was this an observed failure?" requires distinguishing two states
that produce identical stored records:

- Horizon was unreachable or returned an error for this size at this moment
- Horizon was reachable but gave no path and no integrity signal

Both produce `Priced: false, Integrity: "UNKNOWN"` in storage. Neither carries
a reason.

Failure prediction in the sense of "the request did not succeed" therefore
cannot be built from the current record shape. The data the model would train
on does not exist in the store.

This is not a gap that can be filled by accumulating more history under the
current schema. No matter how long the store runs, `Priced: false, Integrity:
"UNKNOWN"` will remain ambiguous between "network problem" and "genuine
absence". The fix is a schema change: a per-rung reason field that preserves
the `Err.Error()` string, or at minimum a boolean distinguishing
`IntegrityUnknown-due-to-error` from `IntegrityUnknown-due-to-absence`.

---

## What the store does support for a future model

The following are determinately observable from records as they exist today:

1. **No-market incidence per size** — `Integrity == "NO-MARKET"` per rung is
   a real observation. A model predicting when a corridor becomes unroutable
   could train on this.

2. **Corridor-level measurability rate** — `runstore.MeasurabilitySummary`
   (`runstore/measurability.go`) already computes the fraction of sweeps that
   produced any priced rung. This is a failure-frequency fact at the corridor
   level.

3. **Loss trajectories** — `LossPct` and `Verdict` per rung, per sweep, are
   fully stored. A model of "when does loss worsen" has its inputs.

4. **Integrity transitions** — `runstore.DetectTransitions` reconstructs
   changes between consecutive records. A DIRECT → NO-MARKET transition is the
   clearest corridor-level failure event and is already detectable.

5. **Reference quality** — `ScoredAgainst` and `DivergencePct` per record. A
   model could treat MALFUNCTION runs as a separate class rather than as
   failures of the corridor.

None of these require the store to distinguish error from absence at the
rung level.

---

## What would need to change before failure prediction is possible

If "failure" is defined as *the Horizon request did not complete* (meaning 1
above), the record schema needs a per-rung reason field. The field set and
declaration order of `runstore.Rung` are part of the hash preimage, so adding
a field is a version bump with a migration — not a tidy-up. This is not proposed
here; it is a design decision for the maintainer when the V4+ work is active.

If "failure" is defined as *no market exists at this size* (meaning 2), the
data is already in the store and the work is modelling, not schema work. This is
the safer definition and is recommended as the starting point.

If "failure" is defined as *the trade would have destroyed value* (meaning 4),
the data is also fully present. This is the definition most directly aligned
with the project's thesis.

---

## Relationship to the V4+ roadmap

The README (checked 2026-10-22) states that V4 is blocked on months of history
and on *what* is being collected. This spike confirms the second blocker is
real: the records do not distinguish error from absence at the rung level, so
a failure-prediction model trained on the current schema would be training on
ambiguous labels. Accumulating more history does not resolve this.

The no-market and loss-trajectory definitions of failure are unambiguous in the
current schema and would support a model. Whether they are the right definition
for the prediction this project wants to make is a design question beyond the
scope of this spike.

---

## Summary

| Question | Answer |
|---|---|
| Does the store record unpriced rungs? | Yes: `Priced: false` |
| Can a stored record distinguish an upstream error from a genuine absence? | No — both produce `Integrity: "UNKNOWN"` with no reason |
| Is an unpriced rung a failure in the engine's own terms? | Depends on the cause. NO-MARKET is a measurement, not a failure. UNKNOWN may be either |
| What definitions of failure are unambiguous in the current store? | No-market per size; loss verdict UNUSABLE; corridor-level integrity transitions |
| What schema change would be required to observe Meaning 1 failures? | A per-rung reason field, which is a `runstore.Record` version bump |
| Is accumulating more history sufficient? | No, for Meaning 1. Yes, for meanings 2 and 4 |

This is an inconclusive finding on the original question as stated. The store
records unpriced rungs; whether they are failures depends on a definition the
project has not settled, and the most operationally useful definition (upstream
errors) cannot be recovered from the current record shape.
