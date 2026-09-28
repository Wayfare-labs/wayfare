# Spike: how a composite score would handle undetermined components

Issue [#210](https://github.com/Wayfare-labs/wayfare/issues/210), backlog
`#150` ([docs/backlog.md](backlog.md)).

**Status: completed. A mechanics finding, not a design change.** Backlog
`#150` says "the default-to-zero failure has an obvious new home here." This
spike works the mechanics through against the tree and finds something the
backlog entry does not anticipate: **the tree already contains a composite
score implementation, and it already solves the default-to-zero problem — by
abstaining.** Worked through honestly, a rule-abiding composite on this
corridor set is *undetermined in its realistic steady state*. The interesting
design residue is what to do about that, and the options are narrower than
they look. No code was written or changed; the existing implementation is
examined, not endorsed or wired.

Checked against the tree at `a89d985` on 2026-09-25.

---

## Why this matters

A composite score has to turn N component values into one number. The
project's central data rule — a layer can never be more certain than the
layer beneath it; an unmeasured component is unknown, not zero — makes the
naive implementation (treat missing as 0, blend) a fabrication machine: a
corridor whose spread could not be read is scored as if its spread were the
worst possible, silently. The question is what the correct handling is, and
what it costs.

## The default-to-zero failure, stated precisely

The tree names the failure at the point it was fixed:

- `checks/checks.go:290-294` — "An unmeasurable metric carries Determined
  false, never Value zero: a spread of nothing and a spread that could not
  be read are different facts, and zero is a plausible-looking number for
  the second." (checked 2026-09-25)
- `route/wire.go:35-42` — an undetermined cost component omits `amount` and
  `pct` from the wire entirely; `determined` is always present and a reason
  is required when undetermined. (checked 2026-09-25)
- `route/cost.go` — four of the five cost components are published
  undetermined with reasons (network fee, anchor fee, slippage, expected
  failure). Zeroes exist in the struct but are marked undetermined, and the
  wire omits them. (checked 2026-09-25)

The failure mode is not "wrong numbers" — it is *plausible-looking numbers
for facts that were never measured*, indistinguishable from measured ones by
a reader who does not dig.

## The composite that exists, and how it handles the problem

`route/health_score.go` (a library with **no production caller** as of
2026-09-25 — `HealthScore`/`HealthScoreWeighted` are referenced only from
tests) blends five inputs: spread, depth, price impact, concentration, and
effective transfer cost. Its handling:

1. **Any undetermined input makes the whole score undetermined.** Missing
   inputs are collected into a reason string; `Determined` is true only when
   zero inputs are missing (`HealthScoreWeighted`, checked 2026-09-25).
2. **Undetermined inputs still appear in the breakdown.** Each input carries
   `Determined` and, when false, a `Reason` — the missing component is
   visible, not swallowed. (checked 2026-09-25)
3. **Determined values are clamped to normalisation ceilings before
   blending**, so extreme inputs saturate rather than skew
   (`clamp`, `maxSpread` = 0.5, `maxCostLoss` = 50%, checked 2026-09-25).
4. **Weights are declared maintainer-owned**, "the same class of decision as
   the verdict bands" (`health_score.go` package comment, checked
   2026-09-25).

So on the specific question this spike was filed to answer: the composite
does not average over unknowns and does not default them to zero — it
refuses to produce a number. That is the only handling consistent with the
project's rules, and it is already implemented.

## The consequence nobody wrote down: abstention-gating composes multiplicatively

Here is the finding. Each component is itself gated:

- The four market metrics run against order books that do not exist for
  NO-MARKET corridors — nothing to spread, nothing to depth. Undetermined by
  construction (checked 2026-09-25: `checks/metric_*.go` measure
  `Subject.Venue` books; a NO-MARKET corridor has none).
- The cost input comes from `route.Decompose`, whose `TotalLossPct` is only
  meaningful when the rung priced (`route/ladder.go:443` populates it from
  the priced rung's quote; an unpriced rung carries an empty decomposition).
- The reference must be scorable for loss to exist at all (MALFUNCTION → no
  verdict, `refrate/refrate.go:74`).

So for a composite to publish a number, **every one of these must hold
simultaneously**: the corridor priced, the reference agreed, the book
existed, and every metric answered. On the tree's three measured corridors:

| Corridor | Integrity | Reference | Priced? | Composite outcome under the rules |
|:---|:---|:---|:---|:---|
| USDC→NGNC | DIRECT | Scored | Yes (with a 25%+ structural floor) | Determined — *if* the book metrics all answer |
| USDC→GHSC | DERIVATIVE | Scored | Yes (through NGNC) | Determined only with metrics against NGNC's book — which measures the dependency, not the corridor |
| USDC→KESC | NO-MARKET | — | No | **Undetermined, always** |

One corridor can occasionally be fully determined; one is determined only by
measuring the wrong market; one is never determined. **The realistic
steady-state of a rule-abiding composite score on this corridor set is
"undetermined, with a reason."** The score's own tests anticipate this:
`TestHealthScoreSpreadUndetermined`, `TestHealthScoreCostUndetermined`,
`TestHealthScoreAllUndetermined` (`route/health_score_test.go`, checked
2026-09-25).

This is a valid, honest result — and it is also a design smell. A component
that is undetermined most of the time on the corridors it exists to score is
spending wire complexity on a number that usually isn't there.

## The options from here, with their costs

Everything below is future; none of it is proposed for implementation by this
issue.

1. **Stay with all-or-nothing (current implementation).** Cost: the
   steady-state above. Benefit: the number, when present, is fully
   evidenced. Consistent with the project's rules; the honest answer if the
   answer must be one number.
2. **Partial credit with declared coverage.** Blend only the determined
   inputs, rescale their weights, and publish `coverage: 3/5` beside the
   value. This is what CoinGecko's per-pair Trust Score does with a None
   state ([methodology](https://www.coingecko.com/en/methodology), checked
   2026-09-25). Cost: the number's meaning changes with coverage (a 70
   computed from 5 inputs is not comparable to a 70 computed from 2), and
   the coverage dimension itself becomes a new thing readers must notice —
   a quieter version of the same opacity the score was meant to fix. It
   also quietly reintroduces a default: an excluded undetermined input
   behaves *as if* it were average, which is zero-information pretending to
   be mid-information.
3. **Publish the breakdown only.** Drop the blended value; the per-input
   `Determined/Reason` list is the composite. Cost: not a single number.
   Benefit: nothing is destroyed (see
   [spike-single-number-cost.md](spike-single-number-cost.md)) — and the
   breakdown is already the part of the design that carries the reasons.
4. **Fix the inputs first.** The gating that makes composites abstain lives
   upstream: metrics are unreachable (#91), the run record stores no
   metrics, and NO-MARKET corridors cannot have book metrics by definition.
   Until those change, any composite — however handled — inherits
   abstention as its normal state. This ordering is why #55 (the score
   issue) is correctly `blocked`.

The spike's position, as a finding and not a decision: **option 1 is correct
under the project's rules and option 2 is the default-to-zero failure
returning through a side door** — "undetermined input excluded and weights
rescaled" is a plausible-looking number for a fact that was never measured.
Option 3 is the only one that adds no new opacity. Option 4 is the
prerequisite for any of them being worth doing.

## Verdict

**The default-to-zero failure's "obvious new home" turns out to be already
occupied and already defended: the existing composite refuses to score when
any input is undetermined.** The finding worth carrying forward is the
multiplicative consequence — all-or-nothing gating means a composite on
today's corridors is undetermined most of the time, especially on exactly
the corridors (no-market, derivative, unscored) where a health signal is most
wanted — and that the tempting escape hatches (partial blending, coverage
rescaling) each reintroduce the fabrication the gate exists to prevent.

No implementation is attempted. Nothing here touches verdict thresholds,
integrity semantics, the check composition rule, or the run-record layout —
flagging that boundary explicitly per the issue text.

## Constraints check

- Assertions checked against the tree at `a89d985`, 2026-09-25, including
  the reachability status of `route/health_score.go` (no production caller)
  and `Decompose`'s current caller (`route/ladder.go:443`).
- The CoinGecko citation carries its date (2026-09-25).
- Future options are marked future; the current implementation is described
  as it exists, without endorsement and without a wiring proposal.
- The partially negative finding (the composite is honest but mostly
  abstains, and the corridors where it is most wanted are the ones where it
  can never score) is reported as a finding, not spun into a feature.
- No implementation attempted.

## Sources

| Source | Checked |
|:---|:---|
| Tree at `a89d985`: `route/health_score.go`, `route/health_score_test.go`, `route/wire.go:35-42`, `route/cost.go`, `route/ladder.go:443`, `checks/checks.go:290-294`, `checks/metric_*.go`, `refrate/refrate.go:74`, `dex/sizes.go` | 2026-09-25 |
| [docs/backlog.md:1018-1021](backlog.md) (backlog `#150` text) | 2026-09-25 |
| [docs/run-store.md](run-store.md) (record stores no metrics) | 2026-09-25 |
| [CoinGecko — Methodology](https://www.coingecko.com/en/methodology) | 2026-09-25 |
| [docs/metrics.md](metrics.md) (metric methodology, undetermined rules) | 2026-09-25 |

## Related

- [spike-single-number-cost.md](spike-single-number-cost.md) — what the
  collapse itself costs (#209)
- [spike-confidence-expression.md](spike-confidence-expression.md) — prior
  art including CoinGecko's None state (#206)
- [#55](https://github.com/Wayfare-labs/wayfare/issues/55) — the blocked
  score-composition issue this informs
- [#91](https://github.com/Wayfare-labs/wayfare/issues/91) — the
  metrics-reachability gap that gates every option above
