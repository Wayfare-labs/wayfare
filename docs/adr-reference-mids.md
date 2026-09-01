# ADR: reference mids are never averaged

- **Status:** Accepted
- **Date:** 2026-08-27
- **Decision:** Wayfare never averages reference-provider mids. It selects a named provider according to the agreement rules and carries the other provider separately.

## Context

Wayfare scores an achieved route against an independent reference mid. The reference is part of the measurement's provenance: a reader must be able to identify the source and reproduce or check the benchmark used for the verdict.

Two providers are queried where the configured cross-check permits it. They can return different mids because of aggregation, timing, coverage, or a broken feed. A simple average would produce a number neither provider published. It would also make a stored or API result impossible to trace to one source.

This is not a theoretical concern. The code carries `mid`, `source`, `secondary_mid`, `secondary_source`, `divergence_pct`, and `scored_against` through `refrate.Rate`, the route wire shape, and the run store. Those fields exist specifically to preserve which observation produced the scored result.

## Decision

Do not calculate `(primary + secondary) / 2` or otherwise blend provider values.

Instead:

1. **AGREE:** score against the primary provider and retain the secondary mid and source.
2. **DISAGREE:** score against the more conservative mid—the larger mid for a loss measured as the achieved rate below the benchmark—and name that provider in `scored_against`.
3. **STALE:** when the providers describe materially different timestamps, score against the fresher provider and retain the older observation as secondary.
4. **SINGLE:** when only one provider answers, use it and explicitly identify the result as uncorroborated.
5. **MALFUNCTION:** when the divergence exceeds the malfunction threshold, issue no verdict. The system cannot defend either feed as the benchmark.

The result carries both observations even when they agree, because a future reader must be able to distinguish a moving corridor from a moving benchmark.

## Consequences

### Benefits

- Every scored figure names a checkable provider.
- Stored history preserves enough context to explain benchmark changes.
- Conservative scoring avoids flattering a corridor during ordinary provider disagreement.
- Extreme disagreement is represented as uncertainty rather than a fabricated precision.

### Costs

- The API and run record carry more provenance fields.
- Consumers must understand agreement state and must not assume `mid` is an average.
- Some measurements produce no verdict when the providers cannot be reconciled.

Those costs are intentional. A less detailed response would be shorter but would make the published loss figures less auditable.

## What this decision does not mean

This ADR does not add a third provider, define a parallel-market benchmark, or change verdict thresholds. It also does not claim that the selected reference is ground truth. It records which defensible benchmark the system used and refuses to hide disagreement behind a blended number.

## Implementation evidence

As checked on 2026-08-27:

- `refrate/cross.go` implements reconciliation and explicitly documents why it does not average.
- `route/wire.go` publishes the selected and secondary reference fields.
- `runstore/convert.go` and `runstore/runstore.go` preserve the reference provenance in stored runs.
- `server/trend.go` returns the reference used by each historical run.

## Related

- [HTTP API reference](api.md)
- [Run store](run-store.md)
- [Parallel-rate research](parallel-rate-research.md)
- [Checks contract](checks.md)
