# What a corridor "baseline of normal" would require

**Status:** completed spike. **Finding:** inconclusive — the stored history cannot define a baseline today, and defining one for a corridor that has never been acceptable is a design question this document poses rather than answers.

Backlog #138 (issue #336), Initiative E1 — Historical intelligence (V4). Nothing here is implemented, and nothing here should be read as describing a shipped capability: anomaly detection is **future** (V4+, not active).

All checks below were made against this repository's tree on **2026-11-18** (`main` at the commit dated 2026-11-10). No live network source was read; every figure is from files in the repository, cited by path.

## The question

Anomaly detection needs a reference for "normal". A corridor that has never been acceptable — one whose every recorded run recommends nothing — has no good state to be compared against. What would a baseline have to contain before a deviation from it could be claimed?

## What exists today (checked 2026-11-18)

| Claim | Source |
|:---|:---|
| Each stored run carries integrity state, floor and worst loss, per-rung loss and verdict, reference mid and its source, and optional check findings and metrics. | `runstore/runstore.go`, `Record` |
| Loss figures are decimal strings, not floats. | `runstore/runstore.go`, `Record`, `Rung` |
| The monitor sweeps every registered corridor every 6 hours by default. | `monitor/monitor.go`, `DefaultInterval` |
| A committed chain is trimmed to the newest 366 records per corridor (one quarter). | `runstore/file.go`, `MaxWindow`; [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md) |
| The only change detection that exists compares **consecutive** runs for an integrity-state change (or a changed dependency set while DERIVATIVE), and skips any pair involving UNKNOWN. It uses no history beyond the previous run. | `runstore/transition.go`, `compareRuns` |
| The corridor health score exists in code but is unreachable and deliberately off the wire. | [maintainer-owned-areas.md](maintainer-owned-areas.md) §6; [ADR 009](adr/009-why-the-health-score-stays-off-the-wire.md) |

## What the committed history contains

Counted from `data/*.ndjson` on 2026-11-18:

| Corridor file | Records | First / last `recorded_at` | Integrity | Recommended a route |
|:---|---:|:---|:---|:---|
| `USDC-NGNC` | 1 | 2026-08-22T12:09:59Z | DIRECT | no |
| `USDC-GHSC` | 1 | 2026-08-22T12:10:05Z | DERIVATIVE | no |
| `USDC-KESC` | 1 | 2026-08-22T12:10:09Z | NO-MARKET | no |
| `USDC-NGNT` | 1 | 2026-10-17T23:36:46Z | DIRECT | yes |

**Finding 1 (negative).** Every committed corridor has exactly one record. A baseline is a distribution over time; one observation has no spread, no trend and no seasonality. No baseline can be derived from the committed data, and none should be claimed.

**Finding 2 (consequence for the question asked).** Three of the four corridors recommended nothing in their only run. For those, the only observable "normal" is "unacceptable", and a baseline built from it would classify a persistently broken corridor as unremarkable. Whether that is the intended meaning of "normal" is not decided here.

## What a baseline would require (all future)

These are requirements to decide, not designs.

1. **A defined unit of comparison.** Per corridor, per trade size, and per metric (floor loss, worst loss, per-rung loss, reference divergence) are different baselines. The record stores all of them, so none is excluded by the data model; the choice is open.
2. **A minimum number of observations.** Tracked separately as backlog #139 (issue #337). Until that number is stated with its reasoning, "deviation" has no defensible threshold, and the project's rule of reporting unknown as unknown applies: the output would be *unable to determine*, not "normal".
3. **A rule for runs that measured nothing.** UNKNOWN runs are real records. `compareRuns` already skips them for transitions; a baseline must say whether they are excluded, counted as a gap, or reported separately. Silently dropping them would hide upstream failures.
4. **A rule for a changing benchmark.** Each record names the reference source it was scored against. A move in the reference is not a move in the corridor (see `BenchmarkMoved` in `runstore/movement.go`), so a baseline must not mix runs scored against different sources without saying so.
5. **A rule for structural change.** A corridor moving between DIRECT, DERIVATIVE and NO-MARKET changes what a loss figure means; a baseline spanning such a change compares unlike things.
6. **An answer for the never-acceptable corridor.** Options to evaluate, none chosen: report only the corridor's own drift from its observed state; report no baseline until the corridor has a non-empty recommendation history; or define "normal" externally rather than from history. Each has a cost: the first normalises failure, the second never speaks for a broken corridor, the third needs a source of truth the project's measurement rules do not currently allow (no encoded remembered values).
7. **A retention decision.** With the 366-record window, a baseline can only ever describe the newest quarter. Any longer baseline needs history kept elsewhere, which ADR 007 decided against.

## Constraints on whoever builds it

- Anything that publishes a "normal" figure is a judgement of the same class as the verdict thresholds and the health score, both maintainer-owned. A baseline score or anomaly flag would need that review bar.
- It must not alter the headline verdict or the check composition rule ([ADR 002](adr/002-why-checks-never-move-the-headline.md)).

## Result

Inconclusive. The data to define a baseline does not exist yet (one record per corridor), and the central design question — what "normal" means for a corridor that has never been acceptable — is open. Revisit once chains hold enough history and #139 states a minimum observation count.
