# ADR 009: Why the corridor health score stays off the wire

**Status:** Accepted

## Context

`route/health_score.go` implements a corridor health score: five signals —
spread, depth, price impact, liquidity concentration and effective transfer cost
— blended into one decimal on a 0–100 scale, with the rule that the score is
undetermined whenever any input is undetermined.

It is implemented, tested (`route/health_score_test.go`), and unreachable. There
is no non-test caller of `HealthScore` or `HealthScoreWeighted` in this tree, and
`HealthScoreResult.Value` is tagged `json:"-"`, so the blended value cannot
reach a response even if something wired it up.

Two things changed while the question was open, and neither settles it:

- **The inputs became reachable.** Until 2026-10-22 `checks.Runner` could not run
  a metric, so every input except cost would have been undetermined and the
  undetermined rule would have made the score report nothing at all. That
  wiring has landed (#577), so the inputs are real for a live corridor.
- **The arguments against a single number did not weaken.** *A single number
  destroys the recommendation refusal* (`docs/spike-single-number-cost.md`): a
  corridor can have a refusal (no recommendation, no verdict) and a score, and a
  reader who sees only the score will treat a refusal as a bad grade rather than
  as the answer "this corridor does not price". The score also blends across
  venues, which `checks.Venue` and `docs/liquidity-venues.md` exist to keep
  separate.

Unreachable code in a maintainer-owned area invites a well-meaning PR that wires
it, which is the reason this had to be decided rather than left ambiguous. ADR
003's default in that situation is deletion: speculative structure is worse than
none. This file is not speculative structure, though — it is a fully written
composition with the undetermined rule encoded and tested, and it is the only
place those weights, ceilings and that rule are written down.

## Decision

**The corridor health score stays in the tree and stays unreachable.** It is not
wired into the API, the UI, the run store, or any response, and it is not
deleted.

Wiring it is a separate, deliberate change that must first:

1. Establish what a number alongside a refusal means. Either the score is absent
   whenever the corridor refuses a recommendation, or the refusal is carried in
   the same object so a reader cannot see one without the other. A score that
   can appear next to an empty recommendation is not acceptable.
2. State whether the score may be consumed by a machine. If it may, the venue
   rule applies: a score computed from order-book metrics must not be compared
   with one computed from pathfinding metrics.
3. Replace the provisional weights and ceilings with values justified against
   real measurements, and record them as maintainer-owned thresholds in
   `docs/maintainer-owned-areas.md`.
4. Bump the run-store version if the score is recorded, since `Record`'s hash
   covers every field.

Until all four are decided together, nothing calls it.

## Consequences

- The blended number remains off the wire, and `HealthScoreResult.Value`'s
  `json:"-"` tag is the enforcement: serialising a result cannot leak it.
- Contributors have a written reason not to wire it, and a list of the
  conditions that would change the decision. The package comment points here.
- The composition and its tests keep working as the specification of what a
  score would mean, without publishing one.
- Deleting the file remains available later. If the conditions above are never
  met, ADR 003's argument eventually wins and the file should go rather than
  carry provisional numbers indefinitely.

## Evidence

- `route/health_score.go` — the composition, the weights and the package comment
  that points at this ADR
- `route/health_score_test.go` — the tests, including the undetermined rule
- `docs/spike-single-number-cost.md` — the argument against a blended number
- [ADR 002](002-why-checks-never-move-the-headline.md) — why a derived number
  must not move the headline
- [ADR 003](003-why-layers-3-and-4-have-no-packages.md) — the no-speculative-
  structure default this decision departs from, deliberately
- `docs/liquidity-venues.md` — why two venues cannot be reconciled by arithmetic
