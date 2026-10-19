# Liquidity venues: book-based metrics vs route figures

**Status: v1, implemented.** This document is the single reconciliation
statement for the two liquidity sources Wayfare reads from — Horizon's
`/order_book` endpoint and Horizon's `/paths/strict-send` endpoint — and the
rule for comparing figures derived from each. It exists because the two
describe different markets, and shipping metrics from both side by side
without saying so invites a wrong inference.

The rule is short: **do not reconcile figures across venues by arithmetic.**
Every metric carries a `venue` field on the wire — `order-book` or
`pathfinding` — so a consumer can enforce that mechanically.

---

## Why the two disagree

Horizon exposes two different views of the on-chain liquidity behind a pair:

- **`/order_book`** returns offers only. Automated market makers do not appear.
- **`/paths/strict-send`** returns paths priced through the same engine that
  will execute the payment — order-book offers **and** AMM liquidity pools.

The gap is not theoretical. Measured on 2026-08-04 (recorded in
[dex/dex.go](../dex/dex.go)'s package comment), the USDC/NGNC order book
showed a best bid of 333.33 NGNC per USDC with 2,184.54 units of depth, while
Horizon's own pathfinder priced 100 USDC at 21,785.78 NGNC over the same
market at the same moment. The larger figure cannot be reconstructed from the
book alone under either reading of the amount field. The engine settled the
missing depth against an AMM the order-book endpoint does not observe.

The engine's pricing has always used pathfinding for exactly this reason. The
change here is not to what is measured but to how each measurement labels the
market it looked at.

---

## The venue tag

Every corridor metric's descriptor now declares one of:

```
venue: order-book      # Horizon /order_book, offers only, no AMM
venue: pathfinding     # Horizon /paths/strict-send, offers plus AMM pools
```

The field is required for corridor metrics (`checks.Descriptor.ValidateAsMetric`
enforces it) and is copied verbatim into `MetricJSON` on the wire. Anchor and
asset metrics carry no venue: liquidity is a corridor property, and a venue
on a non-corridor metric would invent one.

The mapping today:

| Metric                        | Venue         | Endpoint                       |
| ----------------------------- | ------------- | ------------------------------ |
| `spread.bid-ask`              | `order-book`  | `/order_book`                  |
| `depth.observed`              | `order-book`  | `/order_book`                  |
| `depth.executable`            | `pathfinding` | `/paths/strict-send` (n sizes) |
| `concentration.liquidity`     | `order-book`  | `/order_book`                  |
| `price-impact.size`           | `pathfinding` | `/paths/strict-send` (2 sizes) |
| `deviation.book-vs-reference` | `order-book`  | `/order_book`                  |

`depth.observed-executable` is the composite `Metric.Describe()` returned by
`DepthMetric`. It declares `order-book` because `DepthMetric.Run` delegates
to `RunObserved`; the executable half is exposed through `RunExecutable` and
carries `pathfinding` on its own descriptor. The two never share a
`MetricResult`.

---

## The comparison rule

When a corridor is rendered with metrics from both venues alongside a route
figure, a reader must treat them as follows:

1. **Do not subtract, divide, or otherwise combine an `order-book` figure
   with a `pathfinding` figure or a route figure.** A wide book spread and a
   good route price are not a contradiction; the second may have priced
   through an AMM the first cannot see. A tight book and a poor route price
   are not a contradiction either; the book size the trade needed may not
   have been where the book was tight.
2. **Do compare figures within the same venue.** Two `order-book` figures
   describe the same market. Two `pathfinding` figures describe the same
   market.
3. **Route pricing (`route.Quote`, `route.Ladder`) is `pathfinding` by
   construction.** The `Loss%` on a rung is derived from the `pathfinding`
   receive amount against the reference mid, and it may be compared with
   `price-impact.size` and `depth.executable` but not with the `order-book`
   metrics.
4. **A `venue` a consumer does not recognise is a stop condition, not a
   default.** The list of legal values is
   [checks/checks.go](../checks/checks.go). A new venue means a new market
   surface, and treating it as one of the two known ones would recreate the
   silent-reconciliation failure this field exists to prevent.

---

## An AMM-inclusive measurement, and what it would cost

The acceptance criteria for issue #104 ask whether an AMM-inclusive depth or
spread measurement is possible from available Horizon endpoints, and what it
would need. The finding, as of Horizon on 2026-08-24:

- **`/liquidity_pools`** and **`/liquidity_pools/{id}`** expose each pool's
  two reserves and share count. This is the raw ingredient for pool-price
  math: a constant-product pool prices as `reserveOut / reserveIn` at the
  margin, degrading toward `(reserveOut - dy) / (reserveIn + dx)` as `dx`
  grows.
- **`/paths/strict-send`** already prices through pools in aggregate — the
  path record's `path` field lists the intermediate assets but not the venues
  crossed, so a caller can see *that* an AMM contributed to a fill but not
  *which pool* or *how much*.

An AMM-inclusive depth or spread metric would therefore need one of:

- **Pool enumeration + local reserve math.** Fetch every pool involving the
  pair (or the substituted underlying pair, for DERIVATIVE corridors), read
  reserves, compute an implied best bid/ask and a depth curve locally. Cost:
  N `/liquidity_pools` calls per corridor per sweep, where N is the number of
  pools; then a per-size local computation. This is a re-implementation of
  the pricing engine at the AMM layer, which is exactly the pricing
  arithmetic contributor work stays out of
  ([CONTRIBUTING.md](../CONTRIBUTING.md), "Maintainer-owned areas").
- **A two-sided path probe.** Ask `/paths/strict-send` for a tiny probe both
  ways (buy and sell). Their implied rates approximate an AMM-inclusive
  spread — but the estimate is a synthesis of two pathfinding calls, not a
  direct spread reading, so the resulting figure is `pathfinding` venue, not
  a book venue.

Neither is a small change. The correct scope for this document is to record
that neither is implemented today, and that a book metric labelled
`order-book` is book-only by design.

---

## What this document does not do

It does not change any pricing arithmetic; it does not add a new metric; it
does not decide which venue any future metric should observe. It writes down
the venue every existing metric already has and states the reconciliation
rule the engine has been silently relying on.
