# Regime classification

**Status: implemented in the `analysis` package, not published on the wire.**
The thresholds below are the ones the code applies; this document is where
they are written down and justified. Every claim was checked against the code
at time of writing (2026-10-17).

A regime label is a statement about a corridor's *average* behaviour over a
window of stored observations. It is computed by
`analysis.AnalyzeDecimal` (`analysis/analysis.go`) and classifies the mean
loss percentage into one of four states. It is deliberately **not** a verdict
and is **not** served by any endpoint today.

## The labels

| Regime | Condition on the mean loss | Read as |
|:---|:---|:---|
| `undetermined` | Fewer than 30 observations | Not enough history to classify |
| `normal` | mean loss < 30% | Operating within the expected range |
| `elevated` | 30% ≤ mean loss < 60% | Persistent degradation |
| `critical` | mean loss ≥ 60% | Value destruction, on average |

`RegimeUndetermined` is also returned when there are zero observations. It is
never folded into `normal`: "not enough evidence" and "fine" are different
answers, and the project's rule is that an unavailable quantity is unknown,
never a default.

## The thresholds and why those numbers

The defaults are `ElevatedBelow = 30` and `CriticalAbove = 60`
(`analysis.DefaultRegimeThresholds`), applied to the mean loss percentage in
decimal arithmetic.

They are anchored to the meaning of the verdict bands rather than to a fit.
The verdict grades **one rung** at a single size, with `UNUSABLE` above 20%
loss. The regime grades the **mean** across a window of sizes and runs, a
different statistic, so it deliberately does not reuse 3/8/20:

- **30% for `elevated`.** A mean at or above 30% means the corridor's *average*
  size is already well past the single-rung `UNUSABLE` boundary — this is not
  one unlucky large trade, it is the typical outcome. 30% leaves headroom above
  the 20% band so that a corridor whose average sits just inside `UNUSABLE`
  because of one large size is not labelled structurally degraded.
- **60% for `critical`.** At a mean of 60%, more value is lost than delivered
  on the average route. That is a different statement from "expensive".

The bands are **round-number conventions, documented here and chosen once**;
they are not derived from an empirical distribution and this document does not
claim otherwise. If they are ever changed it is a maintainer decision on the
analysis layer, not a contributor refactor.

**They are not verdict thresholds.** Changing them cannot change a published
verdict, an integrity state, a check result or a metric: the analysis layer
reads stored records and is downstream of all of them. The verdict thresholds
remain the 3/8/20 bands in [README.md](../README.md#verdict-thresholds--breaking-if-altered).

## Sample-size gates

Classification shares the mean/standard-deviation gate
`analysis.MinSampleSizeForMeanStdDev = 30` observations (roughly 7.5 days at
the six-hour measurement cadence). Below it, `AnalyzeDecimal` returns
`Undetermined = true`, a `Reason` naming the shortfall, and
`Regime = RegimeUndetermined`; `Mean`, `StdDev` and `Trend` are nil rather than
zero. Nothing is fabricated from insufficient data.

## Not on the wire, deliberately

No endpoint publishes a regime label today.

`GET /api/corridor/trend` reports reference-provider *divergence* statistics,
and its `divergence_stats` block deliberately omits a regime: the default
thresholds are calibrated to loss percentages, and publishing a label computed
against divergence percentages would read as a verdict the project never
issued. That reasoning is carried in `server/trend.go` and in
`analysis.DivergenceHistory`, which both say the regime field must be ignored
for that series.

The classification is a library primitive over stored records. Exposing it is
a separate decision, and would have to respect the layer rule: a label is only
as sound as the history beneath it.

## Verification

- `TestClassifyRegime` pins every boundary — just below, at, and above each
  threshold (`analysis/analysis_test.go`).
- `TestDefaultRegimeThresholds` pins the default values to 30 and 60, so a
  silent change to the documented numbers fails CI.
- `TestCustomThresholds` pins that a caller-supplied `RegimeThresholds`
  overrides the defaults.

## Related

- [market-structure.md](market-structure.md) — the market-structure vocabulary
- [glossary.md](glossary.md) — the state vocabularies a reader meets
- [run-store.md](run-store.md) — the stored history the analysis reads
- [spike-model-vs-deterministic.md](spike-model-vs-deterministic.md) — why a
  regime label is a description and not a prediction
