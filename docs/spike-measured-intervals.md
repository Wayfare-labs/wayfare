# Spike: representing a measured figure with an interval

**Issue:** [#338](https://github.com/Wayfare-labs/wayfare/issues/338),
backlog [#140](backlog.md). **Checked:** 2026-10-26.
**Status:** Research finding only. No implementation, calculation, threshold,
or run-record change is proposed.

**Scope note:** This spike is limited to the meaning and possible wire shape of
a source-supported interval around one measurement. It does not design a
confidence interval for a historical statistic, a forecast range, or a new
scoring rule.

## Finding

**The current wire contract carries no source-supported interval for a
published corridor figure.** It publishes point values for priced rungs and
names the reference observation used to score them. It also preserves a
second reference provider's point and their disagreement. Neither the path
quote nor the reference fields carry lower and upper bounds or a coverage
meaning. This establishes what the application currently exposes; it does
not claim that an upstream has no additional undocumented capability.

Consequently, the repository cannot currently publish a measured interval
without inventing one. A standard deviation across historical observations is
not an interval around today's measurement; the gap between reference
providers is not a confidence band; and the midpoint between providers is
explicitly not the selected benchmark. This is a negative finding about
available evidence, not a claim that a source-backed interval could never be
represented.

## What the current contract says

`route.QuoteJSON` carries `receive_amount`, `effective_rate`, and `loss_pct` as
decimal strings. `CorridorJSON` carries the selected `reference_mid` and its
source, agreement state, optional secondary mid/source, divergence, the
`scored` gate, and the rungs. There are no lower/upper or interval fields in
these wire types. `ToCorridorJSON` serializes the values calculated by the
measurement and stamps `measured_at`; it does not construct a range.

Reference-provider disagreement is already represented as a different
concept. `refrate` retains both provider observations, chooses a named
benchmark under its reconciliation rules, and returns `MALFUNCTION` without
scoring when the disagreement is too large. The third-provider diagnostic, if
present, attributes disagreement and does not replace or blend the selected
mid. These fields describe which benchmark observations were seen and how
they related; they do not assert that the selected figure lies within a
probabilistic or guaranteed interval.

There is historical analysis, but it does not supply a per-measurement
interval. `analysis.AnalyzeDecimal` computes a mean and sample standard
deviation after 30 observations and a slope after 60. The trend endpoint's
`divergence_stats` applies those summaries to recorded reference-provider
divergence observations; when insufficient, the numeric summaries are omitted
and a reason is carried. A standard deviation describes observed spread under
that analysis; the contract does not call it a confidence interval, give it a
coverage probability, or attach it to an individual rung.

The persisted `runstore.Record` likewise has point-valued string fields for
floor/worst loss, reference observations, and rung values, but no interval
representation. Its fields and order are part of the hash preimage: changing
the record layout requires a schema-version/migration decision, not a quiet
extension as part of this spike.

**Sources checked 2026-10-26:** `route/wire.go` (`QuoteJSON`,
`CorridorJSON`, `ToCorridorJSON`); `refrate/cross.go` (`Agreement`,
`Diagnostic`, reconciliation); `docs/adr-reference-mids.md` (decision not to
average reference mids); `analysis/analysis.go` (`AnalyzeDecimal` and sample
gates); `server/trend.go` (`DivergenceStatsJSON`); `runstore/runstore.go`
(`Record` and hash-preimage rule).

## Three different meanings of “interval”

The word is not enough to define a contract. These meanings must not be
substituted for one another:

1. **Source-reported measurement bounds.** A producer explicitly reports a
   lower and upper bound for the observation, with a defined unit and
   interpretation. This could qualify a measured value if a future upstream
   actually supplies those bounds.
2. **Observed disagreement or historical spread.** Multiple point
   observations differ. Provider divergence and historical standard
   deviation are examples already in the repository. They describe those
   observations; they do not establish guaranteed bounds for one new reading.
3. **An inferential interval.** A statistical procedure estimates a range for
   a parameter or future outcome, usually with a stated confidence/credible
   level and assumptions. That is an inference, not a measurement interval,
   and requires a defensible method and validation before publication.

The existing [measurement/inference boundary](measurement-inference-boundary.md)
rejects adding a confidence interval to a measured loss when no such
uncertainty model exists. This finding is consistent with that boundary: a
source-reported interval would be new measurement evidence, while a calculated
statistical interval would be a separate inferred quantity. Neither is
available for current corridor figures.

## Future wire-design constraint (not an implemented contract)

If an upstream later provides a meaningful interval, represent its epistemic
state explicitly rather than overloading a point field or using zero as a
placeholder. A future contract should distinguish at least:

- a point measurement;
- a measured interval with decimal-string lower and upper bounds; and
- an undetermined figure with a reason and no numeric bounds.

For an interval, the wire description would need to preserve the source,
unit, and the source's definition of the bounds. It must not add a confidence
percentage unless the source actually supplies that meaning. It must not
derive a center by averaging bounds or providers. If a future interval is
statistical rather than source-reported, it needs a separately named
inferential kind and enough method/coverage context that a consumer cannot
mistake it for a measurement.

This is a design constraint, not a proposed finalized schema. The present
sources cannot populate the interval state, so selecting exact field names,
compatibility policy, or calculation would be premature. If the interval
must persist in the tamper-evident history, that run-record change is outside
this issue and needs its own migration/review decision.

## Conclusion

The current output supports **measured point** or **undetermined/unavailable**;
the wire contract does not support **measured with interval** for corridor
loss or execution rate. Do not synthesize bounds from provider disagreement,
historical standard deviation, or a point estimate. Revisit the wire
representation only when a source and interpretation for actual measurement
bounds are identified. No calculation, UI change, threshold change, or
record-layout change is justified by the current evidence.

## Related

- [Measurement/inference boundary](measurement-inference-boundary.md) —
  distinguishes observed measurements from future statistical inference
- [Confidence-expression survey](spike-confidence-expression.md) — prior-art
  survey; it does not establish intervals for Wayfare measurements
- [Reference mids ADR](adr-reference-mids.md) — provider disagreement is not
  averaged into a synthetic benchmark
- [Run store](run-store.md) — hash-pinned persisted record format
