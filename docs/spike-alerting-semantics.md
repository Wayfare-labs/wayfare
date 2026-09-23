# Spike: alerting semantics for corridor deterioration

Issue [\#222](https://github.com/Wayfare-labs/wayfare/issues/222), backlog `#162`.

**Status: completed, semantics only.** The repository contains every detector a
well-behaved alerting layer needs (state-transition detection, verdict bands,
determination gating, minimum-sample gates) but **no notification/delivery
channel of any kind** — no emails, webhooks, queues, or push (checked
2026-09-23 across `server/`, `monitor/`, `runstore/`, `checks/`). This
document therefore defines *what threshold crossing deserves a notification,
to whom, and — critically — what must never fire*, against the semantics the
code already encodes. Delivery of the notifications is future work and is
explicitly out of this issue's scope.

---

## Why this matters

A verdict system that produces GOOD/FAIR/POOR/UNUSABLE and integrity bands but
cannot say "this just changed" is a dashboard, not a monitor. The alerting
question is where the project's honesty rules reach their sharpest test: the
constraints that make measured statements safe — never predict, undetermined is
not failure, a benchmark move is not a corridor move — are exactly the things a
careless alert channel would violate first. This spike fixes the semantics so
that whoever later builds delivery does not have to invent the rules under
production pressure.

---

## The recording-and-comparison primitive that already exists

`runstore/transition.go` implements integrity-transition detection, merged via
the closed [\#24](https://github.com/Wayfare-labs/wayfare/issues/24) (PR #109,
merged 2026-08-25). Its design comments are the project's own alerting contract
in miniature (`runstore/transition.go:95-206`, checked 2026-09-23):

- **UNKNOWN is deliberately excluded.** "Alerting on DIRECT → UNKNOWN would turn
  every transient network failure into a false alarm… a monitor that cries wolf
  gets muted" (`transition.go:98-104`). This is the single most important
  semantic ever written for alerting here, and it is already in code.
- **Detection is idempotent**: re-running over the same history yields the same
  transitions, not duplicates (`transition.go:106-107`). An alert layer built on
  it is naturally de-duplicated: the same pairwise comparison cannot re-fire on
  unchanged history.
- **Two transition types**: an integrity-state change (e.g. DIRECT →
  DERIVATIVE) and a `depends_on` change within DERIVATIVE (`transition.go:52-60`).
- `DetectLatestTransition` compares exactly the two most recent runs — the
  efficient scheduled path (`transition.go:141-155`).

This is the *reference reader* for this spike: alerting semantics are expressed
as comparisons between consecutive recorded runs of the same corridor, nothing
more.

---

## What the code already grades, with sources

All checked 2026-09-23:

| Signal | Values / bands | Source |
|---|---|---|
| Verdict | GOOD ≤3%, FAIR ≤8%, POOR ≤20%, UNUSABLE >20% | `route/route.go:39-75`; band rationale: incumbent remittance corridors run 3–8% total cost (`route/route.go:65-70`) |
| Recommendation | `Recommended` is the best acceptable route, or `nil` when every route is UNUSABLE | `route/route.go:271-315`, `route/route.go:408-426` |
| Integrity | DIRECT / DERIVATIVE / NO-MARKET / UNKNOWN | `route/route.go:111-168` |
| Reference agreement | SINGLE / AGREE (≤2%) / DISAGREE / STALE (as-of gap >48h) / MALFUNCTION (>10%) | `refrate/cross.go:12-90` |
| Counterparty checks | tri-state `{determined, passed}`, severity `critical`/`warning`/`notice`/`info`, `worst_severity` roll-up | `docs/checks.md`, `server/api.go:686-747` |
| Sample gating | mean/std-dev needs ≥30 observations, trend needs ≥60 | `analysis/analysis.go:39-48`; 30 ≈ 12.5 days, 60 ≈ 25 days at 6 h cadence |
| Regime classification | Normal <30% mean loss, Elevated 30–60%, Critical ≥60% | `analysis/analysis.go:85-108` |
| Health score | 0–100 blend of five metrics; undetermined if any input is undetermined | `route/health_score.go:1-14,119-126` |

Two things every entry above has in common: each is a *recorded, determined
fact* or an explicit undetermined-with-reason, and none of them could fire
without a second recorded run to compare against. That is the whole basis for
the semantics below.

---

## Alert semantics

### Rule 1 — An alert is a comparison of two recorded runs. Never a single run.

Every alert below is `prevRun → currRun` on the same corridor, from the store
(`runstore.Recent(corridor, 2)` is the efficient read; `transition.go:141-155`).
A single run is a measurement, not an event; a change across two runs is the
event. This is non-negotiable because every value in the system is a
point-in-time measurement of an external market — a single reading changing band
is indistinguishable from noise until a second run confirms the first.

### Rule 2 — Undetermined is not an alert. UNKNOWN is not an alert.

The strongest precedent is `transition.go:98-104` (UNKNOWN excluded from
integrity transitions). The project-wide rule "undetermined is not a failure"
(`docs/checks.md`) extends here: three-valued results (`{determined, passed}`),
`scored: false` corridors, `integrity: UNKNOWN`, and `reference: STALE`/`SINGLE`
are **states to display, not events to alarm**. An alert whose condition is
"determined=false" converts measurement noise into pager traffic, and the code
base already names that failure mode and refuses it in the transition layer.
This must hold for every alert type below.

### Rule 3 — Benchmark movement is a benchmark alert, never a corridor alert.

\#220's neighboring finding and `refrate/cross.go`'s design put a second
provider in the store precisely so a corridor move can be told apart from a
benchmark move ("DivergenceStats summarises how far the corridor's two
reference providers have disagreed… a fact about the benchmark, not the
corridor" — `server/trend.go:49-56`). Alerting semantics preserve that split:
AGREE → MALFUNCTION is "the reference feed is broken", not "the corridor
deteriorated"; a corridor verdict changing *because the mid moved* is reported
as such (both mid and divergence are carried per run for exactly this,
`docs/run-store.md:98-105`).

### Rule 4 — Alert on state, and emit at most once per state change.

`transition.go`'s idempotence is the model. After an alert fires for
`prevRun→currRun`, the same event must not re-fire until the state changes
again — a corridor that stays UNUSABLE run after run is an ongoing condition
suited to a dashboard badge, not an escalating series of identical alerts. (An
escalation *timeout* — "still UNUSABLE after N days" — is a separate, future
capability and must be defined as one, not smuggled in as re-firing.)

### Rule 5 — Sample gates apply to statistical signals, not to single snapshots.

The verdict/integrity/check signals are single-run facts and need no histogram.
The regime classification and trend signals are *statistical* and must respect
`analysis`' minimums (≥30 for mean, ≥60 for trend) before they may be read as
an alert input (`analysis/analysis.go:39-48`). Alerting on a regime label
computed from 5 observations would be precisely the "looks precise, means
nothing" failure the minimums exist to block.

---

## What deserves a notification, and to whom

Two audiences: **a corridor consumer** (a wallet/PSP deciding whether to route
through a corridor) and **an operator/maintainer** (the deployment's owner).

### Consumer-facing alerts

| # | Event (prev → curr) | Completes when | Severity of impact | Why it needs a push, not a poll |
|---|---|---|---|---|
| C1 | Integrity structural change: DIRECT → DERIVATIVE, either side of NO-MARKET, or `depends_on` set changes | `runstore.DetectLatestTransition` returns non-nil with `TransitionType != UNKNOWN` | high | A corridor's *kind* changed; consumers choosing route objects built on the old shape are now wrong |
| C2 | Verdict band change at the floor rung: GOOD→FAIR→POOR→UNUSABLE (or reverse) | compare `FloorLossPct`/`FloorVerdict` across two consecutive runs | medium–high | The best available deal crossed a cost band; "still worse than X% but not quite UNUSABLE" is material to a routing decision |
| C3 | Recommendation presence flips: `Recommended` non-nil → nil (or vice versa) | compare `Recommended` across two runs | high | "there is a route worth taking" ↔ "there is none" is the single yes/no a consumer acts on (`route/route.go:408-426`) |

### Operator-facing alerts

| # | Event (prev → curr) | Completes when | Why this is an operator's, not a consumer's, alert |
|---|---|---|---|
| O1 | Reference agreement degraded: AGREE/DISAGREE → MALFUNCTION, or a STALE gap appears | compare `Reference.Agreement` / `DivergencePct` / `AsOf` across runs | It is a fact about the *feed*, and every consumer verdict derived in that window is suspect — the operator must fix the source or label output |
| O2 | Counterparty check flips determined-pass → determined-fail at `critical` severity | compare check `{determined,passed}` for the same check id across runs | A critical check failing is an operational fact about an issuer, not a routing fact; see `docs/checks.md` severities |
| O3 | A scheduled sweep produced no record (gap) | a previous record exists and the expected cadence elapsed with no successor (`monitor`, `measure.yml`) | Absence of measurement is the one state that is *not* read from a run — it is read from the schedule. It must never be conflated with NO-MARKET ("no path") |

Of these, **C1 is the only one with a detector already implemented**
(`runstore/transition.go`), and it currently has **no caller and no delivery**
— it was merged as capability in PR #109 with notification deferred. It is the
natural first alert. The rest are specifications; none of their alert code
exists in the tree today.

---

## What must never alert

1. **Single-run observations.** Any condition evaluated against one record
   (a lone UNUSABLE verdict, one MALFUNCTION) is a measurement, not a change.
2. **Undetermined / UNKNOWN on either side** of the compared runs — including
   `scored: false`, `integrity: UNKNOWN`, `determined: false` checks
   (`transition.go:169`, `docs/checks.md`).
3. **Verdict drift within a band.** 4% → 6% loss is both FAIR; no alert
   (band-boundary crossings are the unit, per C2).
4. **Predictive statements of any kind.** No "will reach UNUSABLE within N
   days", no trend extrapolation, no "statistically likely to cross". The
   `analysis` layer computes trends, but a trend's output is a *description of
   recorded history*, and using it as a forecast is a category error this
   project's "no ML, no predictive modelling" stance explicitly refuses
   (`analysis/analysis.go:24-31`).
5. **Re-fires while state persists** (Rule 4).
6. **Benchmark changes dressed as corridor changes** (Rule 3) — e.g. an AGREE →
   MALFUNCTION must never surface as "corridor worsening".

---

## What the future delivery layer would look like (design sketch, not built)

The receiver split maps to two channels: consumer-facing events (C1–C3) could
be delivered pollably by extending the *trend* endpoint's output with a
`last_transitions` block (client polls and diffs — one transition added since
last read → exactly one "new" event, preserving Rule 4), while operator-facing
events (O1–O3) would go to the deployment's own paging. Nothing in the repo
implements either channel today; both are **future** under this issue's
constraint "mark future capabilities as future".

---

## Verdict

**The detector infrastructure is 90% present; the delivery is 0% present.** The
semantics that make alerting safe are already written into the code — the
precedent of `transition.go` (no UNKNOWN, idempotent, compare-consecutive-runs)
and the `analysis` minimums tell a future alert layer exactly how to behave.
What this spike contributes is the alert *catalogue* (C1–C3, O1–O3), the
explicit never-list, and the audience split — and the finding that there is
currently **no notification channel in the tree**, so everything above beyond
`DetectLatestTransition` is spec awaiting an implementation that this spike
does not attempt.

## Related

- `runstore/transition.go` — the merged detector and its alerting-relevant
  design comments
- `route/route.go`, `refrate/cross.go`, `checks/`, `analysis/analysis.go`,
  `route/health_score.go` — the graded signals
- [checks.md](checks.md), [run-store.md](run-store.md) — tri-state and per-run
  recorded facts
- [\#24](https://github.com/Wayfare-labs/wayfare/issues/24) — the merged
  detection issue that established the "compare two runs" primitive
- [spike-realtime-vs-scheduled-economics.md](spike-realtime-vs-scheduled-economics.md)
  — why cadence does not change these semantics