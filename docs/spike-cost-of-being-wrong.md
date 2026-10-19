# Spike: the cost of being wrong, per figure published

Issue [#224](https://github.com/Wayfare-labs/wayfare/issues/224), backlog
`#164`.

**Status: completed.** This is a risk register for figures the current
repository publishes. It ranks the consequence of an incorrect figure and the
hardening evidence that would reduce that risk. It does not estimate a
probability of error or a monetary loss: the repository contains no incident
log, consumer outcome data, or calibrated error model from which either could
be calculated (source: `docs/`, `data/`, `runstore/`; checked 2026-09-24).

No implementation was attempted as part of this spike. Future capabilities
remain future, and no verdict threshold, integrity semantic, check-composition
rule, or run-record layout is proposed for change (source: `route/route.go`,
`docs/checks.md`, `runstore/runstore.go`; checked 2026-09-24).

## Scope and method

The register covers the figures exposed by `GET /api/corridor`, its stored
history equivalent, the corridor trend output, and the documented measurement
tables. Those are the repository's published surfaces, not a claim about
figures that an external deployment may add (source: `docs/api.md`,
`server/api.go`, `docs/corridor-measurements.md`; checked 2026-09-24).

“Cost of being wrong” means the consequence if a reader accepts the figure and
it is materially wrong, stale, misattributed, or presented without a required
qualification. It is a prioritisation judgment, not a measured loss. The
ranking uses three questions:

1. Could the figure directly change a transfer or routing decision?
2. Could it cause a reader to trust a dependent figure that should be
   rejected or qualified?
3. Can the current repository independently check the figure, or is the
   evidence only live and manually joined?

Sources and check dates are included at the claim level below. Where the tree
has no evidence, the finding is explicitly negative or inconclusive.

## Register

| Priority | Published figure | If wrong, the damage is | Current protection and remaining exposure | Hardening priority | Source checked |
|:---|:---|:---|:---|:---|:---|
| **P0** | `loss_pct` on a priced rung | A reader can underestimate the value lost by a transfer and treat an uneconomic route as acceptable. This is the most direct financial-decision risk in the current output. | It is calculated from the Horizon effective rate and the selected reference mid; it is withheld when `scored` is false. The result still depends on upstream path data and benchmark correctness, and live figures are not bit-for-bit reproducible without a snapshot. | Highest: preserve raw inputs, benchmark identity, timestamp, and replay linkage for every published figure. | `route/route.go`, `docs/metrics.md`, `docs/verify-store.md`; checked 2026-09-24 |
| **P0** | `recommended` and `recommended_size` | A wrong non-null recommendation can direct a consumer toward a route the system should not endorse; a wrong `null` can hide the only acceptable size. | Recommendation is derived from acceptable verdicts and is absent when no size is acceptable. The code keeps checks from changing it, but no repository data quantifies how often a recommendation has been wrong in the market. | Highest: test recommendation/wire parity and require the same evidence used for the underlying rung before presenting it as actionable. | `route/route.go`, `route/ladder.go`, `docs/api.md`, `route/findings_test.go`; checked 2026-09-24 |
| **P0** | `floor_loss_pct`, `floor_size`, `worst_loss_pct`, `worst_size` | A wrong floor can hide a structural cost at the smallest tested amount; a wrong worst case can understate size-dependent slippage. Either can mislead a reader about the safe operating range. | The values are extrema over the discrete requested ladder, not a continuous market curve. They do not establish behavior outside the tested sizes. | Highest: retain the requested size set and make the sampled-domain limitation unavoidable wherever these figures are shown. | `route/ladder.go`, `route/wire.go`, `docs/metrics.md`; checked 2026-09-24 |
| **P1** | `verdict` / `scored` | A wrong grade converts a quantitative error into a simple action signal. A false `GOOD`, `FAIR`, or `POOR` is especially damaging because it suppresses the warning carried by `UNUSABLE`; a false `UNUSABLE` can unnecessarily reject a corridor. | `verdict` is deterministically derived from full-precision `loss_pct`; `UNKNOWN` is used when no reference is available, and unscored output is not graded. The thresholds are judgments, not measurements, and this spike does not propose changing them. | High: protect threshold reconciliation and the distinction between unknown and failure; separately validate the benchmark used for scoring. | `route/route.go`, `docs/metrics.md`, `route/route_test.go`; checked 2026-09-24 |
| **P1** | `reference_mid`, `reference_source`, `reference_pair` | A wrong benchmark contaminates every loss and verdict derived from it. The reader may believe a route is cheaper or dearer than the comparison supports. | Two providers are compared and the scoring source is retained; disagreement can make the result unscorable. Provider methodology and real-world “fair value” are not proven by the cross-check alone. | High: preserve provider observations and age, and keep benchmark movement distinct from corridor movement. | `refrate/`, `runstore/runstore.go`, `docs/metrics.md`, `docs/fair-value-ngn.md`; checked 2026-09-24 |
| **P1** | `reference_agreement`, divergence, and `reference_fetched_at` | A wrong or missing qualification can make a benchmark-dependent figure look more certain or current than it is. This amplifies the damage of a wrong loss or verdict rather than standing alone. | The wire carries agreement state, provider details, and fetch time when available. A stale response is labelled `live: false`, but live correctness at the instant of measurement remains a trust boundary unless bytes were recorded. | High: make benchmark status and age travel with every derived figure and keep stale/live paths wire-compatible. | `refrate/cross.go`, `server/api.go`, `docs/api.md`, `docs/verify-store.md`; checked 2026-09-24 |
| **P1** | `integrity`, `depends_on` | Calling a derivative corridor direct hides inherited liquidity and failure modes; calling a no-market corridor merely expensive invents a price where none was observed. The error changes what the loss figure means. | Integrity is explicitly separate from verdict, with `DIRECT`, `DERIVATIVE`, `NO-MARKET`, and `UNKNOWN`. The current semantics are tested and no composition rule allows checks to rewrite them. | High: retain the structural evidence and never collapse `NO-MARKET` or `UNKNOWN` into a price grade. | `route/route.go`, `docs/checks.md`, `route/integrity_snapshot_test.go`; checked 2026-09-24 |
| **P2** | `receive_amount`, `effective_rate`, and `path` | A wrong raw execution figure can misstate what the recipient receives or hide the route responsible for the result. It also makes independent recalculation of `loss_pct` impossible or misleading. | These are derived from Horizon pathfinding and carried per rung; money fields are decimal strings. A live upstream response is not a retained proof unless snapshot or run-record evidence exists. | Medium: retain verbatim upstream evidence for published research tables and connect each table to its snapshot or run record. | `route/route.go`, `route/wire.go`, `docs/metrics.md`, `docs/snapshot-format.md`; checked 2026-09-24 |
| **P2** | `finding` and warnings | Incorrect explanatory prose can cause a reader to overgeneralise a point measurement, mistake token delivery for fiat redemption, or miss why a route is not recommended. | The field is explanatory rather than an input to scoring. The repository has documented examples of the distinction, but prose-to-figure linkage is manual. | Medium: review prose against the exact measured sizes, benchmark, integrity, and redemption boundary. | `route/route.go`, `docs/corridor-measurements.md`, `docs/api.md`; checked 2026-09-24 |
| **P2** | `measured_at`, `recorded_at`, `live`, `stale`, and age | A correct old number presented as current can drive a decision on a market state that no longer exists. The damage is temporal misattribution rather than arithmetic error. | The API distinguishes live from stored output and exposes stale age; run records retain recording time and benchmark fetch time. The committed data contains only one record per corridor, so this checkout cannot quantify drift across a longer history. | Medium: make freshness visible at every consumer surface and retain enough history to test stale-window behavior. | `server/api.go`, `runstore/runstore.go`, `docs/api.md`, `data/*.ndjson`; checked 2026-09-24 |
| **P2** | Findings and metrics | A wrong counterparty fact or metric can change how a reader qualifies the headline, but the current contract says it must not change integrity or verdict. | Findings carry evidence and tri-state determination; metrics may be undetermined. The repository contains no demonstrated consumer-loss estimate for a wrong finding, and future health or notification behavior is not implemented by this spike. | Medium: preserve evidence and determination reasons; do not invent a single health number or threshold here. | `docs/checks.md`, `checks/`, `route/findings_test.go`; checked 2026-09-24 |

## What this register does not claim

The repository does not support a percentage such as “there is a 12% chance
that `loss_pct` is wrong,” nor does it support a dollar estimate of the harm
from such an error. No such number should be published from this spike. The
priority labels above are consequence rankings grounded in the output contract,
not empirical reliability scores (source: `docs/`, `data/`, `runstore/`; checked
2026-09-24).

The register also does not claim that a cross-check proves the reference mid is
true, that a hash chain proves a measurement was correct, or that a ladder
proves behavior outside its requested sizes. Those are explicit limitations in
the existing documentation and code (source: `docs/metrics.md`,
`docs/verify-store.md`, `route/ladder.go`; checked 2026-09-24).

No published figure in this register is currently backed by a complete,
automatic figure-to-snapshot-to-run-record link. The repository does have a
working replay precedent for selected research figures, but the general join
is manual. This is the main evidence gap identified by the spike, not a reason
to claim that all current figures are unverifiable (source:
`cmd/hop-analysis/`, `docs/spike-second-maintainer-verification.md`,
`docs/snapshot-workflow.md`; checked 2026-09-24).

## Finding

**Positive for prioritisation, inconclusive for numerical risk.** The figures
with the greatest plausible consequence are the loss and recommendation
outputs, followed by their benchmark, structural, and freshness qualifiers.
The repository can explain how those numbers are computed and can replay some
recorded figures, but it does not contain the observations needed to quantify
how often they are wrong or what consumers lose when they are wrong.

The practical hardening order is therefore: preserve and replay the inputs to
loss/recommendation, keep benchmark and integrity qualifications attached, and
make sampled size and freshness boundaries visible. This finding does not
change any production behavior or propose a new threshold, integrity meaning,
composition rule, or record field (source: `route/`, `refrate/`, `runstore/`,
`docs/spike-second-maintainer-verification.md`; checked 2026-09-24).

## Related

- [metrics.md](metrics.md) — definitions and limits of published measurements
- [api.md](api.md) — current HTTP wire surface
- [checks.md](checks.md) — evidence, determination, and composition rules
- [snapshot-format.md](snapshot-format.md) — byte-pinned upstream evidence
- [spike-second-maintainer-verification.md](spike-second-maintainer-verification.md)
  — current reproducibility boundary
- [corridor-measurements.md](corridor-measurements.md) — published measurement
  examples