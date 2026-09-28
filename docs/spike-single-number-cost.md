# Spike: what a single number would destroy

Issue [#209](https://github.com/Wayfare-labs/wayfare/issues/209), backlog
`#149` ([docs/backlog.md](backlog.md)).

**Status: completed. The cost is written down; the score is not built and
should not be.** This spike takes the corridor health score as designed
(`route/health_score.go`, blended 0–100) and writes out, concretely, what
collapsing the repository's existing published dimensions into one number
would discard. The finding is a **negative one for the composite**: every
publishable question the score would answer is already answered by the wire
contract with more fidelity, and the collapse loses information that the
project's own founding finding says is the useful part. No code was written
or changed, and no score composition rule was proposed.

Checked against the tree at `a89d985` on 2026-09-25.

---

## Why this matters

The repository states the principle in the README: "Integrity is deliberately
carried alongside the verdict because collapsing them discards the reason —
and the reason is the useful part." The corridor health score
([#55](https://github.com/Wayfare-labs/wayfare/issues/55), backlog `#148`)
collapses further. A cost that stays abstract does not get weighed, so this
document makes it concrete: for each existing published dimension, what
exactly disappears inside a single number, on a real corridor shape from the
tree's own recorded measurements.

## The dimensions a score would collapse

What follows is the wire contract as it exists (`route/wire.go`,
`route/route.go`, checked 2026-09-25), and what a 0–100 blend does to each
dimension.

### 1. It destroys the reason a corridor failed

The founding measurement found three corridors failing in three different
ways: one prices continuously and prices badly (USDC→NGNC), one has no
independent market and inherits another token's failure modes (USDC→GHSC,
every path through NGNC), and one has no market at all (USDC→KESC)
([docs/corridor-measurements.md](corridor-measurements.md), measured
2026-08-08). The verdict+integrity pair distinguishes all three; a score
cannot.

Work the KESC case: NO-MARKET integrity, no quotes, verdict UNKNOWN. What is
the health score of "no market at any of the 12 ladder sizes"
(`dex.DefaultSizes`, 0.1 → 5000)? Zero — the worst value on the scale. Now
work the NGNC case at the 5000 rung: 97.68% loss, UNUSABLE. The normalised
cost component saturates its ceiling (`maxCostLoss` = 50%,
`route/health_score.go`) and also produces 0. Two corridors in categorically
different situations — *no price exists* versus *a price exists and destroys
value* — both render as 0. The integrity taxonomy exists precisely because
those are different facts; the score re-flattens them into one.

### 2. It destroys the scored/unscored distinction

When the two reference providers diverge beyond 10%, the engine publishes no
verdict at all — MALFUNCTION, `Scorable() == false`, no loss figure
(`refrate/cross.go`, `refrate/refrate.go:74`). The refusal is the safety
property: a number whose value depends on which feed was believed is "closer
to fabricated than measured" (`route/route.go`, `Engine.Quote`).

A score has no way to refuse. Blend normalised inputs and you get a number —
the arithmetic does not have an abstain branch. Either the score silently
absorbs an unscored corridor (publishing a number where the contract refuses
one), or the score itself must carry a determined flag — at which point the
wire shape is `score + determined + reason + per-input breakdown`, which is
the existing contract wearing a hat.

### 3. It destroys the undetermined distinction (the default-to-zero failure, again)

The tree's hardest-won rule: an unmeasured component carries no number, not a
zero (`route/cost.go` network-fee part; `route/wire.go` `CostPartJSON`
omits amount/pct when undetermined). The health score design already accepts
this for its inputs — any undetermined input makes the whole score
undetermined (`route/health_score.go`, `HealthScore` doc comment). Spelled
out: **under the project's own rules, a composite score is an all-or-nothing
proposition.** Five inputs, and any single missing one (spread unavailable,
cost decomposition absent) publishes no number at all. On the only corridors
measured so far — NGNC anchor without SEP-38, KESC with no market —
undetermined inputs are the common case, not the edge case. The realistic
steady-state of a rule-abiding score is: *undetermined, with a reason.*
Which is to say: the composite adds a second layer of gating on top of
components that are already gated, and publishes a number only in exactly
the situations where the existing per-component contract already published
everything the score summarises.

### 4. It destroys the ladder

The verdict is per-size across the 12-rung ladder, and the size dimension
carries the structural finding: the ~25% loss floor at 0.1 USDC means the
corridor is broken at the spread, not at depth — no trade size can be
acceptable (`dex/sizes.go` rationale; [docs/corridor-measurements.md](corridor-measurements.md)).
A corridor can be acceptable at 10 and destroyed at 5000; the floor rung and
the worst rung tell opposite stories and both are true. A per-corridor score
must pick a rung (best? worst? weighted?) and every choice is a silent
editorial claim. The ladder's shape is the diagnosis; one number is a
prognosis with the diagnosis redacted.

### 5. It destroys the benchmark provenance

Every verdict carries which mid produced it (`scored_against`), which
provider, the divergence, and the as-of — because a benchmark move is not a
corridor move. A score that blends loss-against-mid absorbs the benchmark
into the number: when the mid moves, the score moves, and the reader cannot
tell corridor from benchmark. The trend endpoint's `divergence_stats` exists
specifically to keep that split visible (`server/trend.go`; a fact about the
benchmark, not the corridor).

### 6. It destroys the recommendation refusal

`recommended: null` is a published statement — "these routes exist and you
should take none of them" (`route/route.go`). A score reintroduces the
ranking-by-number the project removed: a corridor scoring 41 looks better
than one scoring 37, which is a winner, which is the exact failure the
recommendation rule exists to prevent when both corridors are UNUSABLE at
every size.

### 7. It destroys falsifiability

Today every published figure is reproducible: recorded bytes → arithmetic →
same number. A composite adds weights and normalisation ceilings —
maintainer-owned judgement (`route/health_score.go` marks them maintainer
-adjusted) — between the bytes and the number. That is not fatal (the
weights can be published and pinned by test, as the verdict bands are), but
it moves every reader one step further from the measurement, and it makes
score drift across versions a new class of published-figure change. The
prior art agrees from the outside: Lighthouse's own documentation notes that
its weights change between versions and that a distribution of scores is
more honest than one number
([Lighthouse performance scoring](https://developer.chrome.com/docs/lighthouse/performance/performance-scoring),
checked 2026-09-25).

## What a single number would *not* destroy — the honest counterweight

Fairness requires the other side. There are things a score would genuinely
add:

- **One comparison axis for non-expert readers.** A 0–100 scale is easier to
  hold than verdict × integrity × scored × ladder.
- **A single trend line.** `analysis`' regime labels and trend statistics
  currently operate per-dimension; one number would make deterioration
  legible in one glance.
- **A sorting key for machines.** A wallet that must order N corridors needs
  some total order; the wire contract deliberately does not provide one.

The response to each, without padding: the first is a *rendering* problem
solvable at display time without collapsing the wire contract (the UI can
already show a summary chip); the second can be run over the loss dimension
alone, which `analysis` already does by regime; the third is real but is a
request for a *ranking*, and the project's answer to rankings on broken
corridors is already published — `recommended: null`. None of these requires
the score to replace anything; all of them can exist *beside* the contract,
which is exactly what the health score design claims for itself
("an additional signal alongside the verdict and integrity state; it does
not replace or modify either", `route/health_score.go`).

## Verdict

**What the single number destroys is the *reason* — the same thing collapsing
verdict into loss would destroy, one level further down.** Concretely, on the
tree's own corridors: KESC and a saturated NGNC become indistinguishable;
MALFUNCTION becomes a number; the undetermined gate makes the realistic
steady-state "no number, with a reason" anyway; and the ladder's diagnosis
(floor vs depth) disappears into whichever rung the composer picked. The
adoptable core — a coarse summary for glanceability — does not require the
collapse, and nothing in the tree blocks a consumer from building one
themselves from the published dimensions, which is the correct place for it:
in the consumer, where its weights are that consumer's responsibility, not
inside a contract whose figures must stay reproducible and reason-preserving.

Per the issue constraints, this is a written finding only. The score exists
in the tree as an unreferenced library (`route/health_score.go` — no
production caller as of 2026-09-25, checked); this spike takes no position
on wiring it and explicitly does not attempt it.

## Constraints check

- Claims checked against the tree at `a89d985`, 2026-09-25, not against
  README prose (notably: `Decompose` is now called from
  `route/ladder.go:443`, contradicting older backlog text — the spike uses
  the code as it is).
- The Lighthouse citation carries its date (2026-09-25).
- Future/unbuilt things are marked future: the health score has no
  production caller; no score composition rule is proposed here.
- The finding is negative-for-the-composite and is reported as such, with
  the honest counterweight section included rather than suppressed.
- No implementation attempted; no threshold, integrity, composition or
  run-record change proposed.

## Sources

| Source | Checked |
|:---|:---|
| Tree at `a89d985`: `route/health_score.go`, `route/route.go`, `route/wire.go`, `route/cost.go`, `route/ladder.go:443`, `refrate/cross.go`, `refrate/refrate.go:74`, `server/trend.go`, `dex/sizes.go` | 2026-09-25 |
| [docs/corridor-measurements.md](corridor-measurements.md) (2026-08-08 measurements) | 2026-09-25 |
| [docs/run-store.md](run-store.md) (record contents) | 2026-09-25 |
| [Lighthouse — Performance scoring](https://developer.chrome.com/docs/lighthouse/performance/performance-scoring) | 2026-09-25 |
| [docs/backlog.md:1013-1016](backlog.md) (backlog `#149` text) | 2026-09-25 |

## Related

- [spike-composite-undetermined.md](spike-composite-undetermined.md) — the
  undetermined-input mechanics (#210)
- [spike-confidence-expression.md](spike-confidence-expression.md) — prior
  art on expressing confidence (#206)
- [docs/checks.md](checks.md), [docs/adr/002-why-checks-never-move-the-headline.md](adr/002-why-checks-never-move-the-headline.md)
  — the composition rule this spike argues from
- [#55](https://github.com/Wayfare-labs/wayfare/issues/55) — the health
  score issue this spike informs without attempting
