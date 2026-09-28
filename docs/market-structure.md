# Market structure

**Status: vocabulary for the current implementation. Checked: 2026-09-25.**

In Wayfare, *market structure* means the observable arrangement and
accessibility of liquidity for one send/receive corridor. It describes what
the available market data can establish. It is not a verdict about whether a
trade is good, and it is not a claim about facts the data source does not
expose.

The unit of analysis is a corridor: a source asset and a destination asset.
The current implementation obtains on-chain data through a configured Horizon
endpoint. It does not maintain a registry of venues, identify every liquidity
provider, or compare independent venues.

## Vocabulary

### Venue

A *venue* is a separately identifiable source or mechanism of liquidity. In
this repository, the configured Horizon endpoint is the data source, not a
venue inventory. Horizon's strict-send pathfinding can return a route through
the Stellar DEX, and Stellar settlement can use order-book offers and AMM
liquidity together. The current code uses that pathfinding result for pricing,
but does not attribute a result to an individual offer book, AMM, account, or
other venue.

### Direct market and derivative corridor

A *direct market* is the requested asset pair's own order book. A
`DERIVATIVE` corridor has no direct pair and is reached through an underlying
fiat-pegged asset; a `NO-MARKET` corridor has no path at all. These are
structural states, not verdicts. The book metrics stop before a fetch when the
structure makes a direct book unavailable. A caller may explicitly provide an
underlying pair for a derivative corridor, and the evidence must name that
substitution.

### Observed depth

*Observed depth* is the number of bid and ask levels returned by Horizon's
`/order_book` endpoint for the pair being measured. It says how many levels the
endpoint reports, not how much can be executed and not whether the offers are
fresh. The implementation reports the sides separately and also reports their
sum.

### Executable depth

*Executable depth* is what Horizon strict-send pathfinding can return at the
configured probe sizes. The current metric records the largest destination
amount found among those probes. A missing path or a failed probe is reported
as undetermined; it is not converted to zero.

### Concentration

*Concentration* here means the distribution of the reported order-book levels
between price levels, measured by the metric named
`concentration.liquidity`. In the current implementation, the value is the
equal-share HHI over the combined bid and ask level count, which is
`1 / (bid levels + ask levels)`. It is therefore a structural level-count
signal, not an amount-weighted liquidity share.

It does not measure concentration by account: the order-book response does not
expose the offering account. It also does not identify concentration by AMM,
venue, issuer, or route share. No threshold turns this metric into a check.

### Depth distribution

*Depth distribution* would describe how liquidity is spread across price
levels, trade sizes, sides, paths, or other explicitly chosen buckets. That
broader measurement is **future work**. Today, Wayfare has observed level
counts, one HHI value derived from those counts, and executable probes at
specified sizes; it does not publish a depth-distribution curve or histogram.

### Fragmentation

*Fragmentation* would describe liquidity split across independently identified
venues, accounts, paths, or time periods. Wayfare does not currently measure
that quantity. A list of paths from Horizon is not, by itself, a fragmentation
measurement: the implementation does not attribute liquidity or volume shares
to those paths and does not infer independent venues from them.

## What can be said today

The current metrics can establish a limited, dated observation about a
corridor:

| Question | Current answer | Source checked |
|:---|:---|:---|
| Is there a direct order book, and how many levels does it report? | Sometimes. Empty, one-sided, unavailable, derivative, and no-market cases can remain undetermined or structural. | [`DepthMetric.RunObserved`](../checks/metric_depth.go), Horizon `/order_book`; checked 2026-09-25 |
| How wide is the direct book's top-of-book spread? | The bid/ask spread can be measured when both sides exist. | [`SpreadMetric`](../checks/metric_spread.go); checked 2026-09-25 |
| How does a route respond to selected trade sizes? | Price impact and executable depth can be probed through strict-send pathfinding. | [`PriceImpactMetric`](../checks/metric_price_impact.go), [`DepthMetric.RunExecutable`](../checks/metric_depth.go); checked 2026-09-25 |
| Is liquidity concentrated by account, venue, or path share? | No conclusion. Those dimensions are not exposed or calculated by the current code. | [`ConcentrationMetric`](../checks/metric_concentration.go), [`dex.Client`](../dex/dex.go); checked 2026-09-25 |

These observations are measurements, not judgements. The repository has no
threshold for spread, depth, concentration, or price impact. An undetermined
or inconclusive result is valid: it records that the current source and
measurement did not establish the requested fact.

## Future capabilities

The following are deliberately not described as implemented:

- fragmentation across venues, accounts, paths, or time;
- a depth-distribution curve or amount-weighted concentration;
- venue attribution, venue comparison, or liquidity-share accounting;
- historical market-structure change and regime labels.

Adding any of these requires defining its observation source, timestamp,
identity rules, and missing-data behavior before it can support a verdict.
That work must not silently change existing verdict thresholds, integrity
semantics, check composition, or run-record layout.
