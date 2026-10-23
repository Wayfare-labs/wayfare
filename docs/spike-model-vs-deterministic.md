# Spike: would a model add anything over the deterministic measurements?

**Issue:** [#207](https://github.com/Wayfare-labs/wayfare/issues/207), backlog
[#146](backlog.md), Initiative E2 — Predictive intelligence (V5).
**Checked:** 2026-09-29 against the tree at `c44c55b`.
**Status: completed, negative finding.** No implementation is attempted, and
none is proposed. This document answers a value question — *does a layer-3
model add anything a reader can use that the deterministic measurements do
not already provide?* — not a safety question; the safety question is already
worked in
[spike-wrong-prediction-failure-modes.md](spike-wrong-prediction-failure-modes.md)
(`#144` / [#205](https://github.com/Wayfare-labs/wayfare/issues/205)), and the
publishable-shape question in
[measurement-inference-boundary.md](measurement-inference-boundary.md)
(`#147` / [#208](https://github.com/Wayfare-labs/wayfare/issues/208)).

## The question, and why a negative answer is worth writing down

The entry's own note is the brief: *"The honest possible answer is no, and
establishing that would save the project an entire version."* So the work is to
test the proposal, not to look for reasons to accept it. The three candidate
contributions named for V5 are failure probability, expected slippage, and
anomaly flags (`docs/backlog.md`, Architecture snapshot, "What does not exist";
repeated in `measurement-inference-boundary.md` §4). Each is checked below
against what the tree computes **today**, not against what any document says it
computes — the backlog's own architecture snapshot is already stale about the
check set, which is the reason this constraint exists (see "A note on the stale
snapshot" below).

## What the deterministic measurements already publish

All of the following exist in the tree at `c44c55b`; the corridor rows reach
`GET /api/corridor` and the stored-history row reaches `GET /api/corridor/trend`
(`server/api.go:98,267`). Sources are in the table at the end.

| Reader question | Deterministic answer that already exists |
|:---|:---|
| What does the corridor cost right now, at size *q*? | The ladder prices twelve sizes from 0.1 to 5000 — `dex.DefaultSizes` — and each rung publishes `receive_amount`, `effective_rate`, `loss_pct`, `loss_amount` and a `verdict` (`route/wire.go`, `QuoteJSON`). |
| Is this size worth taking? | `verdictFor` grades full-precision loss against 3 % / 8 % / 20 % (`route/route.go:72-82,98-111`). |
| What is the floor, the worst case, the best acceptable size? | `floor_loss_pct`, `worst_loss_pct`, `recommended` / `recommended_size`, over exactly the sizes measured (`route/ladder.go`, `summarise`). |
| Does the corridor have an independent market at all? | `integrity` = DIRECT / DERIVATIVE / NO-MARKET, with `depends_on` naming the intermediate (`route/route.go`, package comment; `route/ladder.go`, `summarise`). |
| What is the shape of the cost-versus-size relationship? | The measured `curve` with `priced_count`, `observation_count` and `non_monotonic`; local slope as `marginal_cost` / `marginal_from` / `marginal_to` between adjacent priced rungs (`route/ladder.go`, `computeMarginalCosts`; `route/wire.go`, `RungJSON`). |
| Where did the money go? | Per-rung cost decomposition: `fx_loss` is determined; `network_fees`, `anchor_fee`, `slippage` and `expected_failure` are published **undetermined, with a reason and no number** (`route/cost.go`; wired at `route/ladder.go`, `summarise`). |
| Can I trust the benchmark the loss is scored against? | Both provider mids, divergence, fetch time, and `scored` — false when the two providers diverge past 10 % (`AgreementMalfunction`; `refrate/cross.go`; `refrate/refrate.go:74`). |
| How has this corridor behaved over stored runs? | The trend endpoint returns the stored run series plus divergence statistics from `analysis.DivergenceHistory` (`server/trend.go`). |

Two facts about this surface matter for the question:

1. **The metric layer is still unreachable.** `checks.Runner` has no `Metrics`
   field and `ForAsset` never calls `RunMetric`; `RunMetric` and `AddMetric`
   have no non-test caller at `c44c55b`. So `spread.bid-ask`, `depth.*`,
   `price-impact.size`, `concentration.liquidity` and
   `deviation.book-vs-reference` are implemented and tested but not published
   (see backlog `#49` / [#91](https://github.com/Wayfare-labs/wayfare/issues/91)).
   This weakens a model's case rather than strengthening it: the deterministic
   capability that *could* answer more is not yet switched on.
2. **The tree contains no predictive code of any kind.** A case-insensitive
   search of the Go tree for `predict`, `forecast`, `probability`, `regression`
   and `machine learning` finds only: the `analysis` package's own disclaimer
   ("Not ML or predictive modelling"), its linear least-squares **trend slope**
   (a description of observed values, not a forecast), and unrelated regression
   *tests*. `analysis` computes mean, sample standard deviation, a gated
   direction-of-change slope and a regime label, and its gates are `n ≥ 30` and
   `n ≥ 60` (`analysis/analysis.go:43,48`).

## Candidate 1 — failure probability

**What it would add:** a statement like *"this corridor has a 12 % chance of
failing to price at 500 USDC"*.

**What the deterministic layer already does about it.** For the present state,
the question a reader acts on — *can I price size q from here?* — is answered by
pricing size *q*. The ladder already issues that query across 0.1 → 5000 and
records a per-rung outcome that distinguishes a priced rung, a rung Horizon
answered "no path" (NO-MARKET, a finding), and a rung whose request never
landed (`Rung.Unmeasured`, `IntegrityUnknown`; `route/ladder.go`, package-level
comments on `Failed` and `Unmeasured`). There is no gap here for a probability
to fill; measuring is strictly better evidence than estimating whether the
measurement would succeed.

**What the predictive version would need, and does not have.**

- **No observational definition of "failure."** The store records a `priced`
  boolean per rung, but the tree goes out of its way to keep *no market*, *too
  large for the pool*, and *request never landed* distinct. A probability needs
  a single binary target; the data deliberately refuses to collapse those into
  one. That definitional question is open as backlog `#141`
  ([#339](https://github.com/Wayfare-labs/wayfare/issues/339)) and this spike
  does not resolve it.
- **No labelled history.** `data/*.ndjson` holds one record per corridor
  (recorded 2026-08-22, verified in this checkout); there is no stream of
  resolved prediction/outcome pairs to train or score against.
- **The code itself declines the estimate.** `CostExpectedFailure` is published
  `Determined: false` with the reason *"no failure history exists yet; runstore
  is collecting but has not accumulated enough observations"*
  (`route/cost.go:141-146`). So the deterministic layer's current answer to
  "expected failure cost" is *unknown, and here is why* — a model would be
  asked to fill exactly the gap the code says cannot be filled without history.

**Verdict.** A probability is not a better version of any present-state figure;
it is a different claim, about the future, that this repository cannot currently
label, train, or score. It adds nothing publishable now.

## Candidate 2 — expected slippage

**What it would add:** *"at 1000 USDC the expected additional loss beyond
measured depth is 1.8 %"*.

**What the deterministic layer already measures.**

- Observed degradation by size is the ladder itself: `loss_pct` at each of the
  twelve sizes, plus the measured `curve`.
- The local slope is published as `marginal_cost` between adjacent priced rungs
  — a deterministic quantity, not an estimate (`route/ladder.go`,
  `computeMarginalCosts`).
- `price-impact.size` (an observed pathfinding metric) exists in the tree,
  currently unreachable through `Runner`.

**Where the word "expected" changes the claim.** At a **measured** size,
"expected slippage" is redundant with the measurement. At an **unmeasured**
size, it is interpolation or extrapolation — which this project explicitly
forbids publishing:

- Backlog `#76` / [#166](https://github.com/Wayfare-labs/wayfare/issues/166):
  *"Never interpolate between measured rungs — the curve has holes where rungs
  did not price; a drawn line between two measured points is an inference and
  must be labelled as one."*
- The curve already preserves unpriced rungs as holes and asserts **no**
  monotonicity (`ExecutionRateCurve.NonMonotonic`; `route/ladder.go`,
  `buildCurve`).
- The design finding for `#147` restates it for exactly this case:
  *"Blending inference into the loss curve"* is rejected because inference
  between measured points is the interpolation the project forbids
  (`measurement-inference-boundary.md` §8).

So the only output a slippage model could add that the ladder does not already
give is a figure the project's own rules will not publish. Add nothing.

## Candidate 3 — anomaly flags

This is the one candidate that is **not** purely a forecast: *"loss at 100
deviates 2.3σ from the corridor's history"* is a statement about an observed
value relative to other observations, not a claim about the future. It deserves
the most careful treatment, and it is where the honest answer is not a flat no.

**What the deterministic layer already computes.**

- A direction-of-change summary: `TrendDirection` (improving / stable /
  worsening) and its magnitude, gated at `n ≥ 60` (`analysis/analysis.go`).
- A regime label over the mean: normal / elevated / critical
  (`analysis.DefaultRegimeThresholds`, mean loss < 30 %, 30–60 %, ≥ 60 %).
- Longitudinal divergence statistics for the benchmark itself, already
  published (`server/trend.go`, `DivergenceStatsJSON`).
- Integrity-state change tracking is a named V3 item and the chain already
  stores the states (backlog `#122` / [#193](https://github.com/Wayfare-labs/wayfare/issues/193)).

**The genuine, small gap.** There is no per-observation "this value is unusual
for this corridor" test and no defined baseline of normal. A statistical
distance from a baseline is something the deterministic layer does not compute.

**Why that gap does not argue for a model — yet.**

- The baseline itself is undefined and is an open research question, not a
  modelling question (backlog `#138` / [#336](https://github.com/Wayfare-labs/wayfare/issues/336),
  "what a corridor 'baseline of normal' would require").
- It is gated on history that does not exist: one record per corridor at
  `c44c55b`. With one observation there is no distribution to be unusual
  relative to.
- Once a baseline *is* defined, the test is arithmetic — a distance against a
  declared threshold — computed deterministically over stored records. The
  **threshold is a maintainer judgement** of the same class as the verdict bands
  (`route/route.go:65-70`), and it is the judgement, not the model, that carries
  the value. A model here would add a convenience, not a capability, and not on
  current data.

**Verdict.** A real but small and blocked gap. Calling it a reason to build a
V5 model would be padding a finding into a feature.

## The structural argument, which is the strongest part of the answer

The governing rule is *a layer can never be more certain than the layer beneath
it* (`docs/backlog.md`, and repeated in `README.md` and ADR 003). Applied to
this question:

- A model trained on a corpus of measured records **cannot exceed the corpus**.
  Where the question concerns the present state, the measurement *is* the
  ceiling, so the model is strictly below it. A model cannot improve a
  present-state figure; it can only restate it with less certainty, or make a
  claim about something not measured.
- Every published deterministic figure is independently recomputable from
  recorded upstream bytes (`snapshot/record.go`, `snapshot/replay.go`; the
  network-isolated CI job). A model output is not, unless training data, code
  and seed are all pinned — mechanisms that do not exist (failure mode F8 in
  `spike-wrong-prediction-failure-modes.md`). So even a *correct* model figure
  would carry **less** reader trust than a measured one, not more.
- The reader's actionable decision — *should I move value through this corridor
  now, and at what size?* — is already answered by the deterministic layer. A
  model's marginal value would live at the edge where that layer is silent: the
  future and the unmeasured. That edge is exactly where the evidence and the
  publication rules are absent.

There is one place a model could plausibly help without touching the wire: the
internal choice of *where to spend the next measurement*. That is maintainer
tooling, not a published capability, and it points the opposite way from a V5
feature — it would reduce measurement, while the project's discipline is to
measure the whole ladder and let the holes show.

## Finding

**No — a model would add nothing publishable over the deterministic
measurements on the evidence this repository has, and the deterministic
measurements already answer the reader's question.** Stated precisely, this is
what the three candidates resolve to:

- **Failure probability** — no observational definition of failure, no labelled
  history, and the code already declares expected failure cost undetermined for
  want of that history. It is a different claim about the future, not a better
  answer about the present.
- **Expected slippage** — redundant at measured sizes and forbidden
  (interpolation) at unmeasured ones. Its only distinct output is one the
  project will not publish.
- **Anomaly flags** — a genuine but small gap, gated on a baseline definition
  and on history that does not exist; once ungated, it is a deterministic test
  whose value is the threshold, not the model.

The value of this result is the one the entry anticipated: it is the held-out
negative that other E2 spikes defer to
(`spike-wrong-prediction-failure-modes.md`, design implication 6), and it means
V5 is not justified by "a model must add something." On this evidence it does
not, and the deterministic layers are the product.

### What this finding does not claim

- It is **not** a proof that prediction is impossible or useless in general.
  On a much longer and denser dataset, with an observational definition of
  failure and a predeclared method, a calibrated forecast could be a legitimate
  *additional* claim. That is future work, and it is blocked on V3/V4
  accumulating history first.
- It is **inconclusive** on one question the repository cannot yet ask: whether
  a model adds value *for an unmeasured corridor or size*, because there is no
  data on either side of that comparison. The finding is negative for what is
  publishable **now**, not for every conceivable dataset.
- It does not rank or replace the safety findings in
  `spike-wrong-prediction-failure-modes.md`; a wrong prediction remains harmful
  even where a model would be useful.

### Constraint check

- Every claim above was checked against the tree at `c44c55b` on 2026-09-29,
  including the reachability of the metric layer (`RunMetric` / `AddMetric`
  have no non-test caller) and the claim that no predictive code exists (a
  case-insensitive search of the Go tree). Line numbers are given where the
  source is a specific definition.
- A negative result is reported as negative, and the one inconclusive question
  is labelled inconclusive rather than spun positive.
- Future work is marked future; nothing is described as built that is not.
- No implementation was attempted. Nothing here changes verdict thresholds,
  integrity semantics, the check-composition rule, or the run-record layout —
  flagging that boundary explicitly per the issue text.

### A note on the stale snapshot

The backlog's architecture snapshot says `Runner.ForAsset()` runs **three**
checks. At `c44c55b` it runs **seven** (`checks/runner.go`, `Default`). The
snapshot is a document describing a past tree, not the tree; this spike used
the code. The stale-snapshot observation is recorded here only because the
issue's first constraint asks that claims be checked against the code as it is.

## Sources

| Source | Checked |
|:---|:---|
| Tree at `c44c55b`: `route/route.go` (verdicts, thresholds, integrity), `route/ladder.go` (`summarise`, `computeMarginalCosts`, `buildCurve`, `Unmeasured`/`Failed`/`PartiallyFailed`), `route/wire.go` (`CorridorJSON`, `QuoteJSON`, `RungJSON`, `CostBlockJSON`), `route/cost.go` (`Decompose`, `CostExpectedFailure`), `dex/sizes.go` (`DefaultSizes`) | 2026-09-29 |
| Tree at `c44c55b`: `checks/runner.go` (no `Metrics` field), `checks/metric.go` (`RunMetric`), `checks/checks.go` (`Findings.AddMetric`), `checks/metric_*.go` (metric IDs and venues) | 2026-09-29 |
| Tree at `c44c55b`: `analysis/analysis.go` (`MinSampleSizeForMeanStdDev` = 30, `MinSampleSizeForTrend` = 60; trend is least-squares over observation index), `analysis/divergence.go`, `server/trend.go` (`DivergenceHistory` at line 295) | 2026-09-29 |
| Tree at `c44c55b`: `refrate/cross.go` (MALFUNCTION), `refrate/refrate.go:74` (`Scorable`) | 2026-09-29 |
| `data/USDC-NGNC.ndjson`, `data/USDC-GHSC.ndjson`, `data/USDC-KESC.ndjson` — one record each, recorded 2026-08-22 | 2026-09-29 |
| `docs/backlog.md` — Architecture snapshot ("What does not exist", the V1–V6 table), E2 preamble, entry `#146` ("would a model add anything…"), entries `#76`, `#122`, `#138`, `#141` | 2026-09-29 |
| [ADR 003](adr/003-why-layers-3-and-4-have-no-packages.md) — why layers 3 and 4 have no packages | 2026-09-29 |
| [spike-wrong-prediction-failure-modes.md](spike-wrong-prediction-failure-modes.md) (`#144` / `#205`) — F1–F8, design implication 6 (the held-out negative) | 2026-09-29 |
| [measurement-inference-boundary.md](measurement-inference-boundary.md) (`#147` / `#208`) — §4 candidate list, §8 rejected approaches | 2026-09-29 |
| [spike-90-day-history.md](spike-90-day-history.md) (`#135` / `#333`) — what the projected sample can and cannot support | 2026-09-29 |
| [spike-cost-of-being-wrong.md](spike-cost-of-being-wrong.md) (`#164` / `#224`) — the publication risk register | 2026-09-29 |
| [non-goals.md](non-goals.md) §8 "Not yet: prediction and attestation — blocked on evidence, not appetite"; [about.md](about.md) ("It does not predict"); `route/cost_test.go:22` (expected failure cost is a layer-3 quantity) | 2026-09-29 |

## Related

- [#207](https://github.com/Wayfare-labs/wayfare/issues/207) — this spike
- [spike-wrong-prediction-failure-modes.md](spike-wrong-prediction-failure-modes.md) — what a wrong
  prediction costs a reader [#205](https://github.com/Wayfare-labs/wayfare/issues/205); the safety half of this value question
- [measurement-inference-boundary.md](measurement-inference-boundary.md) — how inference would have to be
  marked if it were ever published [#208](https://github.com/Wayfare-labs/wayfare/issues/208)
- [#339](https://github.com/Wayfare-labs/wayfare/issues/339) — what a route failure actually is,
  observationally: the definitional prerequisite this spike found missing
- [#340](https://github.com/Wayfare-labs/wayfare/issues/340) — what would make a prediction publishable
  under the project's rules (a question this negative result makes less urgent, not more)
- [#91](https://github.com/Wayfare-labs/wayfare/issues/91) — the metric-reachability gap: the
  deterministic layer has more measurement available than it currently publishes
- [non-goals.md](non-goals.md) — where the project has already drawn this line
