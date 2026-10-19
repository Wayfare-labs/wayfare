# Spike: what a corridor attestation would actually assert

Issue [#211](https://github.com/Wayfare-labs/wayfare/issues/211), backlog
[#151](https://github.com/Wayfare-labs/wayfare/blob/main/docs/backlog.md).

**Status: completed, design-only. Finding: INCONCLUSIVE — the repository provides
a rich set of deterministically measured, per-run facts but no mechanism to bind
them into a single signed claim over a time window. Every candidate assertion
either (a) reduces to a single run (which the project already treats as
insufficient for alerting/trust), (b) requires statistical aggregation the
current store cannot support, or (c) introduces a publisher-trust model the
project explicitly refuses.** No implementation is attempted, and nothing below
is described as built.

The README and `CONTRIBUTING.md` state that layers 3 and 4 have no packages
and no stubs deliberately; this spike honours that constraint. Every claim
carries its source and the date it was checked.

---

## Why this matters

"Signing 'this corridor is bad'" is the natural language version of a V6
(Verifiable Intelligence) capability. The project's four-layer epistemic model
places V6 at Layer 4: *Verifiable output*, which "asks readers to trust the
publisher instead" of independently reproducing every figure from recorded bytes
(`docs/backlog.md:1022-1032`, checked 2026-09-24). Before any signature scheme
or key custody question can be answered, the *claim itself* must be defined:
what is asserted, over what window, and what would falsify it. This spike
answers that question against the code as it exists.

---

## What the repository currently publishes, with sources

All sources checked against the tree at commit `93f5cda` and the live
deployment on 2026-08-24 (backlog verification date), and re-checked against
the current checkout on 2026-09-24.

| Fact family | What is emitted today | Source |
|---|---|---|
| **Verdict per rung** | GOOD ≤3%, FAIR ≤8%, POOR ≤20%, UNUSABLE >20% loss vs reference mid | `route/route.go:39-75`, `route/route.go:408-426` |
| **Integrity state** | DIRECT / DERIVATIVE / NO-MARKET / UNKNOWN | `route/route.go:111-168` |
| **Reference cross-check** | SINGLE / AGREE (≤2%) / DISAGREE / STALE (>48h as-of gap) / MALFUNCTION (>10% divergence) | `refrate/cross.go:12-90` |
| **Scored flag** | `scored: false` when reference agreement is DISAGREE, STALE, or MALFUNCTION — no verdict issued | `route/route.go:271-315`, `route/wire.go:103-113` |
| **Counterparty checks** | Tri-state `{determined, passed}` with severity `critical`/`warning`/`notice`/`info`, `worst_severity` roll-up | `checks/checks.go`, `checks/wire.go`, `server/api.go:686-747` |
| **Floor / worst loss** | `FloorLossPct`, `FloorSize`, `WorstLossPct`, `WorstSize` per run | `route/route.go:230-260`, `route/wire.go:146-149` |
| **Recommendation** | `Recommended` (best acceptable rung) or `nil` when all UNUSABLE | `route/route.go:408-426` |
| **Execution curve** | `P_exec(q)` points, `PricedCount`, `ObservationCount`, `NonMonotonic` flag | `route/route.go:650-720`, `route/wire.go:63-75` |
| **Cost decomposition** | *Merged but unreachable* — `checks/metric.go` family exists, `route/cost.go` exists, `Runner` has no `Metrics` field | `docs/backlog.md:73-92` |
| **Run-store record** | Hash-chained NDJSON, one record per sweep, carrying all of the above plus `Checks` and `Metrics` arrays | `runstore/runstore.go:106-165`, `docs/run-store.md` |

Two invariants the project already enforces (checked 2026-09-24):

1. **Checks qualify the headline and never move it.** `route.WithFindings` is
   the only composition point and it branches on nothing
   (`route/wire.go:373-398`, `route/findings_test.go:TestFindingsDoNotMoveTheHeadline`).
2. **Undetermined is not failure.** A metric or check with `determined: false`
   carries its reason and no value; zero is never substituted
   (`checks/wire.go:39-41`, `route/health_score.go:121-125`).

---

## What an attestation *could* assert (candidate claims)

Each candidate below is evaluated against the current code. **None is currently
expressible as a single signed object the repository produces.**

### Candidate A — Single-run summary

> "At 2026-09-24T12:00:00Z, USDC→NGNC measured UNUSABLE at every size, integrity
> DIRECT, reference AGREE, all checks passed."

This is exactly what `CorridorJSON` already emits (`route/wire.go:90-174`). It
is a **measurement**, not an attestation. The project's own alerting semantics
(`docs/spike-alerting-semantics.md:80-87`, checked 2026-09-24) state
explicitly: "a single run is a measurement, not an event; a change across two
runs is the event". Signing a single run adds publisher trust without adding
information — the reader could already verify the measurement by re-running the
code against the same recorded bytes (if a snapshot exists) or by measuring
live.

**Verdict:** reducible to existing output; adds no semantic value.

### Candidate B — State-transition attestation

> "USDC→NGNC transitioned from DIRECT to DERIVATIVE between run 47 and run 48."

`runstore/transition.go` already detects this (`DetectLatestTransition`,
`TransitionType`), merged via #24 / PR #109 (2026-08-25). The detector is
idempotent, compares exactly two consecutive runs, and excludes UNKNOWN
(`transition.go:98-104`). This is the strongest primitive the repository has
for "something changed that matters".

**Gap:** the detector has no caller and no delivery channel. An attestation
would need to (a) define the window (two consecutive runs), (b) bind the two
run hashes, (c) assert the transition type. This is *implementable* on current
data but does not exist.

### Candidate C — Sustained-condition attestation

> "USDC→NGNC has been UNUSABLE at the floor rung for the last 30 consecutive
> runs (7.5 days at 6h cadence)."

The run store has the data (`runstore.Recent(corridor, 30)`), but no code
computes this. `analysis/analysis.go` computes mean/std-dev/trend/regime but
requires ≥30 observations for mean and ≥60 for trend
(`analysis/analysis.go:39-48`). A sustained-condition claim is a statistical
statement over a window, and the project's rule is: "a trend's output is a
*description of recorded history*, and using it as a forecast is a category
error" (`docs/spike-alerting-semantics.md:172-175`).

**Gap:** the store has the records; the analysis layer has the minimums; no
code emits "N consecutive runs in state X". This would be a new computation
over existing data.

### Candidate D — Counterparty-health attestation

> "The issuer of NGNC (GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6)
> has auth_required set, auth_revocable not set, and SEP-10 endpoint responding,
> as of 2026-09-24T12:00:00Z."

Checks already emit this per run (`checks/issuer_auth_flags.go`,
`checks/sep10_endpoint.go`, `checks/toml_anchor_asset.go`). The `FindingsJSON`
carries `worst_severity` across failed checks (`checks/wire.go:70-72`). But:
checks are *per run*, and a single check failure at `critical` severity is an
operator alert (O2 in `docs/spike-alerting-semantics.md:150`), not a corridor
attestation.

**Gap:** binding a *set* of check results over a *window* (not one run) into a
claim about the counterparty. The repository has no such aggregation.

### Candidate E — Composite health-score attestation

> "USDC→NGNC health score is 12/100 (undetermined: spread, depth, price impact,
> concentration unavailable; cost loss determined at 25.02%)."

`route/health_score.go` defines the composition (five metrics, equal weights,
undetermined if any input missing). But: **the five metrics are unreachable**
(`docs/backlog.md:49-53`, checked 2026-09-24 — `checks.Runner` has no `Metrics`
field, `Runner.Default()` returns three checks and no metrics). The health
score is *spec only*; it cannot be computed today.

**Verdict:** blocked on V2 metric plumbing (#49). Not a claim the repo can
currently make.

### Candidate F — Benchmark-integrity attestation

> "The USD/NGN reference mid used for scoring was corroborated by two providers
> within 0.5% divergence, fetched at 2026-09-24T00:02:31Z, as-of 2026-09-24."

`refrate/cross.go` produces `ReferenceAgreement`, `DivergencePct`, both mids,
both `AsOf`, and `FetchedAt` (Version 3 record, `runstore/runstore.go:81-91`).
This is already recorded per run. An attestation would bind this to a corridor
claim: "the verdicts above were scored against *this* benchmark, which *did not*
malfunction".

**Gap:** the data exists per run; no multi-run claim exists.

---

## What would falsify each candidate

| Candidate | Falsification condition |
|---|---|
| A (single run) | A live re-measurement at the same sizes produces different verdicts (expected — markets move). Not falsifiable as a *claim* because it asserts only "this is what we saw then". |
| B (transition) | The two run hashes don't chain, or `DetectLatestTransition` on the same two runs returns a different type. Falsifiable *if* the two runs are pinned. |
| C (sustained) | Any run in the claimed window shows a different state. Falsifiable by exhibiting the counterexample run. |
| D (counterparty) | A check in the window shows `determined: true, passed: false` at `critical` severity. Falsifiable by exhibiting the check result. |
| E (health score) | Any input metric's recorded value differs from what the score claims. Falsifiable *if* metrics are recorded (they are not today). |
| F (benchmark) | The run's `Reference` fields show MALFUNCTION or STALE. Falsifiable by exhibiting the run record. |

**Key observation:** only candidates B, C, D, F are falsifiable by *recorded
history*. Candidate A is a snapshot — it asserts nothing about the future and
nothing about the past beyond "this measurement happened". The project's
existing reproducibility tooling (`wayfared -verify-store`, snapshot replay)
already covers A.

---

## What the repository does NOT support (and would need to, for any attestation)

| Missing piece | Why it matters for attestation |
|---|---|
| **No multi-run aggregation** | No code computes "N of last M runs in state X", "consecutive UNUSABLE count", "check failure rate over window". |
| **No window definition** | An attestation must name its window (e.g. "last 30 runs", "since 2026-09-01", "runs 100–129"). The store has `Seq` and `RecordedAt`; no query layer understands windows. |
| **No binding format** | A signed attestation needs a canonical serialization. `CorridorJSON` is for live/stale *measurements*; `runstore.Record` is for *storage*. Neither is an attestation envelope. |
| **No publisher identity** | The project holds no keys. `docs/backlog.md:1049-1052` (issue #216): "A project that holds no keys today would begin holding one; that is a change in kind, not degree." |
| **No consumer of attestations** | `docs/backlog.md:1034-1037` (issue #213): "If no contract would read it, the trade is not worth making. Interview-based, with named candidates." Unresolved. |
| **No on-chain anchor** | `docs/backlog.md:1039-1042` (issue #214): "Publishing the chain head costs nothing and asserts nothing beyond 'this history existed at this time'." Unresearched. |
| **No key custody model** | `docs/backlog.md:1049-1052` — would require a decision the project has deliberately deferred. |

---

## The trust trade-off, written out

Per `docs/backlog.md:1029-1032` (issue #212): "Today every figure is
independently reproducible from recorded bytes; an oracle asks readers to trust
the publisher instead."

An attestation *is* that trade. The question is: what claim is valuable enough
to justify the trade?

| Claim type | Reproducible without trust? | Adds publisher trust? | Value if trusted? |
|---|---|---|---|
| Single-run measurement | Yes (re-measure or replay) | No — adds only "I saw this" | Low — reader can self-verify |
| State transition | Yes (verify two runs + detector) | Only if detector is trusted | Medium — "it changed" is actionable |
| Sustained condition | Yes (verify all runs in window) | Only if aggregation is trusted | High — "it's been bad for a week" |
| Counterparty health | Yes (verify checks in window) | Only if aggregation is trusted | High — issuer risk is not self-verifiable from one run |
| Health score | No (metrics not recorded) | Yes — entirely trust-based | Unknown — metric plumbing not live |
| Benchmark integrity | Yes (verify run's Reference) | No — data already in run | Medium — "the benchmark was honest" |

**The project's current data supports only single-run verification.** Every
multi-run claim (B, C, D, F) requires a *new aggregation* that does not exist
in the tree. The health score (E) requires metric plumbing that is merged but
unreachable.

---

## What a minimal viable attestation would require

If the project were to build *one* attestation type on current data, the only
candidate with (a) existing detector, (b) falsifiability by recorded history,
(c) actionable semantics, is **Candidate B: state-transition attestation**.

Minimal requirements:

1. **Canonical envelope** — a JSON object carrying: `corridor`, `prev_run_hash`,
   `curr_run_hash`, `transition_type`, `prev_integrity`, `curr_integrity`,
   `detected_at` (timestamp of detection), `detector_version` (to pin the
   `transition.go` logic).
2. **Signature** — over the canonical envelope. Requires key custody decision
   (issue #216).
3. **Verification path** — a reader must be able to: fetch the two run records,
   verify their hashes and chain linkage, re-run `DetectLatestTransition` on
   them, confirm the result matches the attestation. This is *already possible*
   with `wayfared -verify-store` + the two run records + the detector code.

Everything else (C, D, E, F) requires either:
- A new aggregation computation over the run store (C, D, F), or
- Metric plumbing to be completed (E), or
- Both.

---

## Negative findings (reported as such)

1. **No attestation claim is currently expressible as a single object the
   repository produces.** The closest is `CorridorJSON` (a measurement) and
   `runstore.Record` (a stored measurement with chain linkage). Neither is an
   attestation.
2. **The only multi-run detector that exists (`transition.go`) has no caller,
   no delivery, and no envelope.** It was merged as capability with notification
   deferred.
3. **All multi-run claims require aggregation code that does not exist.** The
   run store has the data; the analysis layer has minimums; no code emits
   "N consecutive runs in state X" or "check failure rate over window W".
4. **The health score is spec-only.** Its five input metrics are unreachable
   (blocked on #49). An attestation of health score would be entirely
   trust-based today.
5. **Publisher trust model is undeclared.** No keys, no custody, no consumer
   interviews. The trade "trust us instead of verifying" has no agreed
   counterparty.
6. **Signing a single run adds no semantic value.** The measurement is already
   reproducible (via snapshot replay or live re-measurement) and the chain
   already proves it wasn't edited afterwards. A signature would only assert
   "we the publishers saw this", which the run store already proves by
   construction.

---

## What would have to be true first (per the gate in `docs/backlog.md:933-936`)

Before any attestation capability is meaningful, the following must exist:

| Prerequisite | Backlog issue | Status |
|---|---|---|
| Metric plumbing (metrics reachable in Runner) | #49 | Ready, unstarted |
| Checks and metrics recorded in runstore | #62 | Ready, unstarted |
| Statistical reader over runstore history | #113 | Ready, unstarted |
| Transition detector has a caller | #24 (merged) / #111 | Detector merged, no caller |
| Publisher trust trade-off documented | #212 | This spike's sibling |
| Consumer of attestations identified | #213 | Interview-based, not started |
| Key custody model decided | #216 | Deferred |
| On-chain anchoring feasibility | #214 | Research spike |

**The critical path:** #49 → #62 → #113 enables the data foundation. Without
metrics in the store, Candidate E is impossible. Without #113, Candidates C, D,
F have no reader. #212, #213, #216 are the trust-model questions that must be
answered before any signature is meaningful.

---

## Verdict

**No corridor attestation can be defined today that (a) asserts something not
already in the wire output, (b) is falsifiable by recorded history, and (c)
does not require a publisher-trust model the project has not adopted.**

The repository provides:
- Per-run measurements with full provenance (`CorridorJSON`)
- Tamper-evident history of those measurements (`runstore.Record` chain)
- A state-transition detector with no caller (`runstore.DetectLatestTransition`)
- A health-score composition with no live inputs (`route/HealthScore`)

What it does not provide, and what an attestation would need:
- A canonical *claim envelope* distinct from a measurement
- A defined *window* and *aggregation* over history
- A *publisher identity* and *key custody* model
- A *consumer* who would act on the attestation differently than on the
  measurement

**Recommendation:** do not design an attestation format. The prerequisite work
is #49 (metric plumbing), #62 (record metrics), #113 (statistical reader),
#112 (chain head publication), #212 (trust trade-off), #213 (consumer
identification), #216 (key custody). Each is a separate issue with a different
review bar. This spike's contribution is the catalogue above and the finding
that the current tree contains measurements and a chain, but no multi-run
claims and no publisher trust model.

---

## Sources

| Source | Checked |
|---|---|
| `route/route.go:39-75` (verdict bands), `:111-168` (integrity), `:230-260` (floor/worst), `:271-315` (recommendation), `:408-426` (recommendation logic), `:538` (classify), `:591-594` (quoteDEX), `:650-720` (execution curve) | 2026-09-24 |
| `route/wire.go:90-174` (`CorridorJSON`), `:373-398` (`WithFindings`), `:63-75` (`ExecutionRateCurveJSON`) | 2026-09-24 |
| `route/health_score.go:1-14,119-126` (health score spec, undetermined rule) | 2026-09-24 |
| `refrate/cross.go:12-90` (reference agreement states) | 2026-09-24 |
| `checks/checks.go`, `checks/wire.go` (tri-state, `FindingsJSON`, `MetricJSON`) | 2026-09-24 |
| `checks/issuer_auth_flags.go`, `checks/sep10_endpoint.go`, `checks/toml_anchor_asset.go` (counterparty checks) | 2026-09-24 |
| `runstore/runstore.go:106-165` (`Record`), `:81-91` (Reference with FetchedAt), `:168` (CorridorKey) | 2026-09-24 |
| `runstore/transition.go:52-60,98-104,141-155` (transition detector, UNKNOWN exclusion, idempotence) | 2026-09-24 |
| `analysis/analysis.go:24-31,39-48,85-108` (no-prediction stance, sample minimums, regime classification) | 2026-09-24 |
| `docs/backlog.md:73-92` (unreachable metrics), `:1022-1032` (E4 initiative), `:1029-1032` (publisher trust), `:1034-1037` (consumer), `:1039-1042` (on-chain anchor), `:1049-1052` (key custody) | 2026-09-24 |
| `docs/spike-alerting-semantics.md:80-87,172-175` (single-run vs event, no forecast rule) | 2026-09-24 |
| `docs/run-store.md` (chain mechanics, Version 3 migration) | 2026-09-24 |
| `docs/corridor-measurements.md` (sister-corridor sweep, three failure modes) | 2026-09-24 |

---

## What this spike did not do

- It did not define an attestation envelope or signature scheme.
- It did not call any external consumer to validate demand.
- It did not change the integrity taxonomy, verdict thresholds, check
  composition, or run-record layout — those are flagged above as separate work.
- It did not attempt any implementation.

---

## Related

- `runstore/transition.go` — the merged state-transition detector
- `route/health_score.go` — the unreachable health score composition
- `checks/wire.go` — tri-state check/metric wire format
- [spike-alerting-semantics.md](spike-alerting-semantics.md) — alerting on transitions, not single runs
- [spike-second-maintainer-verification.md](spike-second-maintainer-verification.md) — what is reproducible without trust
- [spike-the-publisher-trust-tradeoff.md](spike-the-publisher-trust-tradeoff.md) — #212, the sibling spike
- [docs/backlog.md](backlog.md#151) — initiative E4, this issue's context