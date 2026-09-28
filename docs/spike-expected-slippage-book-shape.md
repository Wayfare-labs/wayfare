# Spike: expected slippage from book shape alone

**Checked:** 2026-09-24

**Question:** Can Wayfare predict expected slippage from the shape of an
order book alone?

**Finding:** **Inconclusive from the recorded evidence, and negative for a
book-only claim.** The repository contains no paired historical observations
that relate an order-book shape to a later execution outcome. More
fundamentally, the order-book endpoint does not observe all of the liquidity
that the route engine can use. A model trained on book shape alone would
therefore omit a known input to the execution outcome. This is not evidence
that prediction is impossible in general; it is evidence that this repository
cannot establish feasibility from its current recordings.

## Scope and method

This review used only committed code, documentation, and snapshot fixtures.
No live measurement was taken and no implementation was attempted. Repository
state, source files, and fixture manifests were checked on 2026-09-24.

The target needs two distinct observations:

1. A contemporaneous description of the book shape.
2. A later, paired execution outcome that can serve as the prediction target.

The existing code calls the second type of observation `price-impact.size`:
the degradation in effective rate across probed trade sizes. It is an observed
pathfinding metric, not an expected or future slippage estimate. **Source:**
`checks/metric_price_impact.go`, checked 2026-09-24.

## Evidence inventory

| Evidence | What it contains | Limitation for this question |
| --- | --- | --- |
| `testdata/snapshots/usdc-ngnc-20260821T223040Z`, plus the GHSC and KESC snapshots | Twelve strict-send path sizes per corridor, with reference-provider traffic; recorded 2026-08-21 | No `/order_book` response is paired with any of these path runs. **Source:** the three manifests, checked 2026-09-24. |
| `checks/testdata/snapshots/xlm-ngnc-orderbook-*` | One `/order_book` response per fixture, including normal, deep, empty, and one-sided cases; recorded 2026-08-23 | These are isolated order-book metric fixtures. They have no paired execution curve or repeated outcome history. The send asset is XLM, not USDC. **Source:** the four manifests, checked 2026-09-24. |
| `checks/testdata/snapshots/usdc-ngnc-strictsend-curve-20260823T000000Z` | Six strict-send sizes from 0.1 through 5000; the fixture note records rates degrading from about 651 to 435 NGNC/USDC | This is a pathfinding-only curve fixture, not a simultaneous order-book capture. Its observed degradation cannot be used to validate book-shape prediction. **Source:** manifest and response bodies, checked 2026-09-24. |
| `runstore.Record` | Fields for rungs, loss, checks, and metrics, with hash-chained persistence | The schema can preserve metrics when they are supplied, but it does not create paired book/outcome observations by itself. **Source:** `runstore/runstore.go`, checked 2026-09-24. |

The production snapshots are therefore useful for replaying route behavior, but
they do not provide the labelled dataset this spike would need. The order-book
fixtures and the pathfinding curve are separate test inputs, not observations
from the same market state.

## Controlling limitation: different liquidity surfaces

Horizon `/order_book` returns offers only. Horizon
`/paths/strict-send` prices through order-book offers and AMM liquidity pools.
The repository explicitly labels these as different venues and forbids
combining their figures as though they described the same market. **Source:**
`docs/liquidity-venues.md`, checked 2026-09-24.

The repository records a concrete example of this gap: on 2026-08-04, the
USDC/NGNC order book was documented at a best bid of 333.33 NGNC per USDC with
2,184.54 units of depth, while pathfinding priced 100 USDC at 21,785.78 NGNC.
That measurement is cited in `docs/liquidity-venues.md`; the document was
checked on 2026-09-24. The difference is attributed there to AMM liquidity
that `/order_book` does not expose. This does not prove that every discrepancy
has that cause, but it is a known counterexample to treating book shape as a
complete execution input.

## What can be concluded

- **Observed slippage can be measured from pathfinding at selected sizes.**
  `PriceImpactMetric` already does this, including a curve entry point, but it
  reports observed degradation rather than a forecast. **Source:**
  `checks/metric_price_impact.go`, checked 2026-09-24.
- **Book shape can be described with existing metrics.** Spread, observed
  depth, concentration, and book-vs-reference deviation read `/order_book`.
  They are explicitly marked as `order-book` venue metrics. **Source:**
  `checks/metric_spread.go`, `checks/metric_depth.go`,
  `checks/metric_concentration.go`, `checks/metric_deviation.go`, checked
  2026-09-24.
- **Prediction is not established.** There is no committed paired dataset,
  no model, and no validation result connecting those book metrics to a future
  execution outcome. The analysis package is statistical history analysis,
  not predictive modelling, and requires at least 30 observations for mean and
  standard deviation or 60 for a trend. **Source:** `analysis/analysis.go`,
  checked 2026-09-24.

## Verdict and boundary

The answer to the spike is **no defensible conclusion from current snapshots**.
The stronger hypothesis that book shape alone is sufficient is **rejected as a
working premise**, because the repository documents a known execution venue
(AMM liquidity) that book shape does not contain.

This finding does not change verdict thresholds, integrity semantics, check
composition, or the run-record layout. A future feasibility study would first
need contemporaneous book snapshots and pathfinding outcomes at matching
corridors, sizes, and timestamps, followed by an out-of-sample comparison
against the deterministic pathfinding measurement. Designing that collection
or model is future work, not part of this issue.