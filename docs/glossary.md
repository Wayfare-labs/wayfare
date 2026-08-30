# Glossary

Every state a reader can meet in a Wayfare response, across five vocabularies.
Each vocabulary answers a different question; none is a substitute for another.

**Verified against:** the code at commit `b36a3af`, 2026-08-25.

---

## Vocabulary map

| Vocabulary | Question answered | Where it lives |
|:---|:---|:---|
| [Verdict](#verdict) | How far below fair value does this route fall? | `route/route.go` |
| [Integrity](#integrity) | Does this corridor have an independent market? | `route/route.go` |
| [Reference agreement](#reference-agreement) | Do the two benchmark providers agree? | `refrate/cross.go` |
| [Check result](#check-result) | Did the check establish the fact, and what was it? | `checks/checks.go` |
| [Freshness](#freshness) | Is this measurement live or served from history? | `route/wire.go` |

Two supplementary vocabularies appear on the wire but are not primary
classifications:

| Vocabulary | Question answered | Where it lives |
|:---|:---|:---|
| [Severity](#severity) | How bad is a check failure? | `checks/checks.go` |
| [Parallel status](#parallel-status) | Was a street-market rate available? | `refrate/parallel.go` |

---

## Verdict

**Question:** How far below fair value does this route fall?

Grades a single route's effective rate against the independent mid-market
rate. Loss is measured as the percentage gap between what the route delivers
and what the mid-market rate promises.

| Verdict | Loss range | Meaning |
|:---|:---|:---|
| `GOOD` | ≤ 3% | Comparable to a competitive remittance service |
| `FAIR` | ≤ 8% | Worse than the best providers, not unreasonable |
| `POOR` | ≤ 20% | Expensive, and the user should be told so clearly |
| `UNUSABLE` | > 20% | Not a fee — value destruction. The tool recommends against it |
| `UNKNOWN` | — | Could not be scored, normally because no reference rate was available. Never presented as a recommendation |

**Thresholds are anchored to the incumbent market.** Established remittance
corridors run a total cost in the 3–8% band, so `GOOD` means "as good as
what already exists" rather than "good for a DEX."

A route scoring better than mid is reported as 0% loss rather than a negative
cost, because a negative "cost" reads as profit and invites misplaced
confidence in a thin market.

**`VerdictUnusable` means "recommend nothing"** — when every size across the
ladder grades UNUSABLE, the result carries `recommended: null`. Not the best
of a bad set. Nothing.

**Source:** `route/route.go` — `Verdict`, `verdictFor()`, `ThresholdGood`
(3), `ThresholdFair` (8), `ThresholdPoor` (20).
Checked 2026-08-25.

---

## Integrity

**Question:** Does this corridor have an independent market?

Describes the corridor's structural state — a different question from how
good its pricing is. Carried alongside the verdict, never folded into it,
because collapsing them discards the reason a corridor failed.

| Integrity | Meaning |
|:---|:---|
| `DIRECT` | At least one path reaches the destination without routing through another fiat-pegged token. An independent market exists. |
| `DERIVATIVE` | Every available path traverses another fiat-pegged token. The corridor inherits that token's liquidity and failure modes on top of its own. `depends_on` names what it depends on. |
| `NO-MARKET` | Horizon returned no path at any size. The absence of a price, not a bad one. |
| `UNKNOWN` | Structure not established — normally an unreachable upstream or a request that never landed. Nothing was learned about the corridor. |

`DERIVATIVE` examines **every path**, not the best one. A single path that
avoids fiat intermediaries proves an independent market exists, regardless of
what the other paths do.

`UNKNOWN` separates "nothing was learned" from "nothing exists". Both produce
identical zero-valued figures, so a caller that conflated them would publish
"0.00% floor loss" as a measurement of the corridor when it was a measurement
of the network.

`NO-MARKET` is a finding about the corridor. `UNKNOWN` is an absence of
information.

**`Priceable()`** returns true for `DIRECT` and `DERIVATIVE` — the states
where pricing is possible.

**Source:** `route/route.go` — `Integrity`, `classify()`.
`route/ladder.go` — `Failed()`, `PartiallyFailed()`.
Checked 2026-08-25.

---

## Reference agreement

**Question:** Do the two benchmark providers agree?

Two providers are queried per measurement. Rates are **never averaged** — a
blended mid names no provider, and every figure must be traceable to a source
a reader can check. One mid is chosen; the record says which.

| Agreement | Divergence | Scored against | Meaning |
|:---|:---|:---|:---|
| `AGREE` | ≤ 2% | The primary | Both providers agree within normal range |
| `DISAGREE` | 2–10% | The **more conservative** mid (the one producing the higher loss) | Genuine disagreement, scored conservatively |
| `STALE` | any, >48h apart | The fresher feed | The gap measures lag, not disagreement |
| `MALFUNCTION` | > 10% | **Nothing.** No verdict is issued | The two are not disagreeing about the rate — they are measuring different things |
| `SINGLE` | — | The one that answered | Only one provider answered; uncorroborated, and says so |

**Why 2% for agreement.** Two feeds quoting the same official rate should
agree to well inside one percent. Measured live on 2026-08-21,
exchangerate-api and currency-api quoted USD/NGN at 1348.0585 and 1350.2568
— 0.16% apart. Two percent is an order of magnitude above observed normal.

**Why 10% for malfunction.** Beyond 10% the feeds are not disagreeing about
the rate, they are measuring different things — a different pair, an official
rate against a parallel-market one, or a misplaced decimal. Wayfare cannot
adjudicate which is the benchmark, and a verdict would be an artefact of that
choice rather than a measurement.

**Conservative scoring on DISAGREE.** The larger mid produces the larger
loss. Choosing it ensures the project's bias against flattering a corridor
holds even when providers disagree.

**`Scorable()`** returns false for `MALFUNCTION` — a verdict derived from
a malfunctioning benchmark is not a measurement.

**Source:** `refrate/cross.go` — `Agreement`, `reconcile()`,
`DivergenceAgree` (2%), `DivergenceMalfunction` (10%), `StaleGap` (48h).
Checked 2026-08-25.

---

## Check result

**Question:** Did the check establish the fact, and what was it?

Every check produces one of three results. The third is the point.

| State | `Determined` | `Passed` | Meaning |
|:---|:---|:---|:---|
| **Determined and passed** | `true` | `true` | The check ran and the fact holds |
| **Determined and failed** | `true` | `false` | The check ran and the fact does not hold |
| **Not determined** | `false` | (meaningless) | The check could not establish either way — **not a failure** |

**Undetermined is not a failure.** An anchor that publishes no SEP-10
endpoint is a different fact from one whose endpoint is dead, and both differ
from one that works. `Determined` is a separate field from `Passed`, so
"unknown" cannot be expressed as a zero, a default, or a false.

Every undetermined result carries a **`Reason`** explaining what could not be
established and why. "Could not determine" with no explanation is an
assertion, not an observation.

`Failed()` is a method that returns `true` only for determined failures.
`!Passed` is also true for undetermined results — the distinction this
system exists to preserve.

**Checks qualify the headline. They never move it.** No check result, at any
severity, may change integrity or a verdict. Those are derived from
pathfinding and a reference rate; letting observations about third parties
rewrite them would make the headline unfalsifiable.

**Source:** `checks/checks.go` — `CheckResult`, `Determined`, `Passed`,
`Failed()`, `Undetermined()`.
`docs/checks.md` — the full contract.
Checked 2026-08-25.

---

## Freshness

**Question:** Is this measurement live or served from history?

Every API response carries a `live` boolean. The case where it is false is
unmistakable by design.

| State | `live` | `stale` | Meaning |
|:---|:---|:---|:---|
| **Live** | `true` | absent | Measured now against live upstreams |
| **Stale** | `false` | present | Served from the run store because a live fetch failed |

`live` is **always present and never omitted**, so a client that ignores the
field cannot mistake a stored reading for a fresh one by its absence.

When `live` is false, the `stale` block carries:
- `recorded_at` — when the measurement was taken
- `age_seconds` — how old it is
- `age_human` — human-readable age

**Nothing is ever fabricated to fill a gap.** When a live fetch fails and no
stored run exists, the request errors rather than returning a plausible
number. The `stale` block exists so the case where a stored run *does* exist
is unmistakable.

**Source:** `route/wire.go` — `CorridorJSON.Live`, `StaleJSON`.
`server/api.go` — `staleJSON()`.
Checked 2026-08-25.

---

## Severity

**Question:** How bad is a check failure?

Orders what a reader sees first. Carries no arithmetic weight and never feeds
back into a verdict.

| Severity | Meaning | Example |
|:---|:---|:---|
| `critical` | A failure that can cost a user their funds | Clawback enabled, authorization revocable |
| `warning` | A failure that makes a route less reliable | Dead declared endpoint |
| `notice` | A discrepancy worth knowing | Declared behaviour differs from observed |
| `info` | Context; not a problem | An issuer's auth_immutable flag is set |

`Worst()` returns the highest severity among **failed** checks only.
Undetermined results are excluded: not knowing something is not a finding
against the subject.

**Source:** `checks/checks.go` — `Severity`, `SeverityCritical`,
`SeverityWarning`, `SeverityNotice`, `SeverityInfo`.
`docs/checks.md` — severity table.
Checked 2026-08-25.

---

## Parallel status

**Question:** Was a street-market rate available?

The parallel/street-market rate is reported alongside the official mid and
**never blended into it**. The official rate and the parallel rate answer two
different questions: a user converting through a bank cares about the
official rate; a user converting on the street cares about the parallel one.

| Status | Meaning |
|:---|:---|
| `REPORTED` | A defensible parallel mid was obtained. `parallel_mid` and `parallel_gap_pct` are meaningful. |
| `UNABLE-TO-DETERMINE` | No parallel mid could be reported — no source configured, source failed, or the number cannot be defended. `parallel_reason` explains which. |

The gap (`parallel_gap_pct`) is a signed percentage of the official mid:
positive when the parallel rate quotes more units of quote per base than the
official rate — the usual direction when a currency's street value is weaker
than its official one.

**The official verdict and every loss figure are scored without reference to
the parallel rate.** It is a second reference dimension, not a replacement.

**Source:** `refrate/parallel.go` — `ParallelStatus`, `Parallel`,
`ParallelAgainst()`.
Checked 2026-08-25.

---

## Metric determination

**Question:** Was the metric measured, and what was it?

Metrics (spread, depth, price impact, concentration, deviation) follow the
same three-valued discipline as checks, applied to quantities rather than
booleans.

| State | `Determined` | `Value` | Meaning |
|:---|:---|:---|:---|
| **Determined** | `true` | meaningful | The metric was measured. `Value` and `Unit` carry the result. |
| **Not determined** | `false` | (omitted) | The metric could not be measured. `Reason` explains why. |

An unmeasurable metric carries `Determined: false`, never `Value: 0`. A
spread of nothing and a spread that could not be read are different facts,
and zero is a plausible-looking number for the second.

**Source:** `checks/metric.go` — `MetricResult`, `RunMetric()`.
`checks/checks.md` — the two-shape contract.
Checked 2026-08-25.

---

## How the vocabularies interact

The vocabularies are deliberately independent. A corridor can be:

- `DIRECT` + `GOOD` + `AGREE` — an independent market, a good price, and
  the benchmarks agree. The best case.
- `DIRECT` + `UNUSABLE` + `AGREE` — an independent market, but every route
  destroys value. The corridor is broken; the benchmark is fine.
- `DERIVATIVE` + `POOR` + `DISAGREE` — no independent market, an expensive
  price, and the benchmarks disagree (scored conservatively).
- `NO-MARKET` + `UNKNOWN` + `SINGLE` — no path exists, no verdict to grade,
  one benchmark answered.

**No vocabulary may override another.** Integrity and verdict are derived from
pathfinding and a reference rate. Check results qualify them without moving
them. Agreement determines whether verdicts may be derived at all. Freshness
labels the measurement's provenance. Each answers its own question.

---

## Related

- [README.md](../README.md) — shared contracts section, where these
  vocabularies are first mentioned
- [docs/checks.md](checks.md) — the full check contract, including the
  three-valued result and the composition rule
- [docs/snapshot-format.md](snapshot-format.md) — how recorded bytes make
  these states testable
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants, including the
  verdict thresholds and the recommendation rule
