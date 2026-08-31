# Metrics methodology

**Status: v1, current as of 2026-08-27.** One page per metric: definition, unit,
data source, what it cannot determine, and what undetermined means for it.

Every claim about the code is checked against the code at time of writing. Future
capabilities are marked as future. A negative or inconclusive finding is a valid
result and is reported as one.

---

## Verdict metrics

### loss_pct — Loss against mid

**Definition.** How far the achieved (effective) rate falls below the reference
mid-market rate, as a percentage:

```
loss_pct = (mid - effective_rate) / mid × 100
```

Clamped to zero: a route better than mid is reported as 0% loss rather than
negative, since a negative "cost" reads as profit and invites misplaced
confidence in a thin market.

**Unit.** Percent (decimal string on the wire, full precision).

**Data source.** `route/route.go`, `Quote.score()`. The effective rate comes
from Horizon pathfinding (send amount / receive amount). The reference mid comes
from a cross-checked pair of independent rate providers (see `refrate`).

**What it cannot determine.** Whether the loss reflects a fair comparison — the
reference mid itself may be stale, divergent between providers, or unavailable
entirely. When the cross-check is unscorable, no loss figure is published (see
`scored`).

**Undetermined.** When `scored` is false, `loss_pct` is not published at all.
The corridor is reported with no verdict and no loss figure.

**Source checked:** `route/route.go` lines 241–253, `route/wire.go` line 179.

---

### verdict — Route grade

**Definition.** A qualitative grade derived from `loss_pct` against fixed
thresholds:

| Verdict | Loss | Meaning |
|:---|:---|:---|
| `GOOD` | ≤ 3% | Comparable to a competitive remittance service |
| `FAIR` | ≤ 8% | Worse than the best providers, not unreasonable |
| `POOR` | ≤ 20% | Expensive, and the user is told so plainly |
| `UNUSABLE` | > 20% | Not a fee — value destruction |

**Unit.** Enum string: `GOOD`, `FAIR`, `POOR`, `UNUSABLE`, `UNKNOWN`.

**Data source.** `route/route.go`, `verdictFor()`. Grades the full-precision
`loss_pct` value — the same value published on the wire, so the number and the
grade always reconcile.

**What it cannot determine.** Whether the grade is appropriate for the
corridor's risk profile. A thin market with a 2% loss may be less reliable than
a deep market with a 5% loss; verdict captures cost, not certainty.

**Undetermined.** `UNKNOWN` when no reference rate was available. Never presented
as a recommendation.

**Source checked:** `route/route.go` lines 97–108, `route/route_test.go`
`TestLossPctReconcilesWithVerdict`.

---

### floor_loss_pct — Best-case loss

**Definition.** The lowest `loss_pct` across all priced sizes in the ladder,
typically at the smallest size where price impact is negligible.

**Unit.** Percent (decimal string, 2dp on the wire).

**Data source.** `route/ladder.go`, computed as the minimum `LossPct` across
all priced rungs.

**What it cannot determine.** Whether the floor is representative — it is
measured at a specific point in time and may change with market conditions.

**Undetermined.** When no size produces a priced quote (NO-MARKET corridor).

**Source checked:** `route/ladder.go`, `route/wire.go` line 194.

---

### worst_loss_pct — Worst-case loss

**Definition.** The highest `loss_pct` across all priced sizes in the ladder,
typically at the largest size where price impact is greatest.

**Unit.** Percent (decimal string, 2dp on the wire).

**Data source.** `route/ladder.go`, computed as the maximum `LossPct` across
all priced rungs.

**What it cannot determine.** Whether the worst case would be even worse at
sizes beyond the ladder — the ladder samples discrete sizes, not the full curve.

**Undetermined.** When no size produces a priced quote (NO-MARKET corridor).

**Source checked:** `route/ladder.go`, `route/wire.go` line 196.

---

## Reference rate metrics

### reference_mid — Independent mid-market rate

**Definition.** The mid-market exchange rate for the corridor's fiat pair
(e.g. USD/NGN), sourced from an independent provider and cross-checked against
a second provider.

**Unit.** Decimal string (units of quote currency per unit of base currency).

**Data source.** `refrate` package. Two providers are queried; their mids are
compared for divergence.

**What it cannot determine.** Whether the mid is the "true" rate — different
providers may use different data sources, time windows, or methodologies.

**Undetermined.** When neither provider answers, or the cross-check is
unscorable (divergence exceeds tolerance).

**Source checked:** `refrate/provider.go`, `refrate/crosscheck.go`.

---

### reference_agreement — Cross-check result

**Definition.** Whether the two reference providers agree on the mid-market rate.

| Value | Meaning |
|:---|:---|
| `AGREE` | Both providers' mids are within tolerance |
| `DISAGREE` | Mids diverge beyond tolerance |
| `STALE` | One provider's data is too old |
| `MALFUNCTION` | One or both providers returned errors |
| `SINGLE` | Only one provider is configured |

**Unit.** Enum string.

**Data source.** `refrate/crosscheck.go`.

**What it cannot determine.** Which provider is "right" when they disagree —
the divergence is a measurement of disagreement, not an explanation.

**Undetermined.** N/A — this is always determined when a reference rate is
fetched.

**Source checked:** `refrate/crosscheck.go`.

---

### reference_divergence_pct — Provider disagreement

**Definition.** The signed percentage gap between the two reference mids:

```
divergence = (mid_a - mid_b) / mid_a × 100
```

**Unit.** Percent (decimal string, 4dp on the wire).

**Data source.** `refrate/crosscheck.go`.

**What it cannot determine.** Why the providers disagree — it is a measurement
of disagreement, not an explanation.

**Undetermined.** When fewer than two providers are configured, or when the
cross-check could not complete.

**Source checked:** `refrate/crosscheck.go`.

---

### parallel_mid — Street-market rate

**Definition.** A parallel/street-market mid-market rate, reported as a second
dimension alongside the official rate and never blended into it.

**Unit.** Decimal string (same units as `reference_mid`).

**Data source.** An optional `refrate.Provider` configured as `Parallel` on the
engine.

**What it cannot determine.** Whether the parallel rate is more or less accurate
than the official rate — it is reported as a separate dimension, not a
correction.

**Undetermined.** `UNABLE-TO-DETERMINE` with a reason when a parallel source was
configured but could not be defended. Absent entirely when no parallel source is
configured.

**Source checked:** `refrate/parallel.go`.

---

## Order book metrics

### spread.bid-ask — Bid/ask spread

**Definition.** The cheapest possible signal of whether a market is real,
measured as `(ask - bid) / mid` on the direct order book.

**Unit.** Percent (decimal).

**Data source.** Horizon `/order_book` endpoint for the corridor's direct pair.

**What it cannot determine.** Whether the spread reflects executable depth or
only the top of book. Horizon's order_book endpoint does not expose AMM
liquidity, so the spread measures the book alone.

**Undetermined.** When either side of the book is empty, or the corridor has no
direct pair by construction (NO-MARKET, or DERIVATIVE without an Underlying).

**Source checked:** `checks/metric_spread.go`.

---

### depth.observed-executable — Order book depth

**Definition.** Two measurements of depth:
- **Observed:** the number of bid and ask levels on the direct order book.
- **Executable:** the maximum destination amount reachable via pathfinding
  across multiple sizes.

**Unit.** Count (observed) and amount (executable).

**Data source.** Horizon `/order_book` for observed; Horizon pathfinding for
executable.

**What it cannot determine.** Whether those levels represent executable
liquidity or stale offers. Horizon's order_book does not distinguish live from
passive offers.

**Undetermined.** When the corridor has no direct pair by construction, or when
pathfinding returns no result at any probed size.

**Source checked:** `checks/metric_depth.go`.

---

### concentration.liquidity — Liquidity concentration

**Definition.** How concentrated the order book is across price levels, measured
as the Herfindahl-Hirschman Index (HHI).

**Unit.** Ratio (0 to 1, where 1 is fully concentrated in one level).

**Data source.** Horizon `/order_book` endpoint.

**What it cannot determine.** Account-level concentration — Horizon's
/order_book does not expose the offering account, so whether a single party
controls the book is unknown.

**Undetermined.** When the corridor has no direct pair by construction, or
the order book is empty.

**Source checked:** `checks/metric_concentration.go`.

---

### price-impact.size — Price impact by trade size

**Definition.** How much the effective rate degrades between a small probe
(default 10 send units) and a full-size trade, as a percentage.

**Unit.** Percent (decimal).

**Data source.** Horizon pathfinding at two sizes: probe and full.

**What it cannot determine.** The full curve shape — this reports the single
degradation figure between probe and full size, not the intermediate points.

**Undetermined.** When the corridor has NO-MARKET integrity, or pathfinding
fails at either size.

**Source checked:** `checks/metric_price_impact.go`.

---

### deviation.book-vs-reference — Book mid vs reference mid

**Definition.** The signed percentage gap between the mid implied by the
corridor's direct order book and the independent reference mid:

```
deviation = (book_mid - reference_mid) / reference_mid × 100
```

**Unit.** Percent (decimal, signed).

**Data source.** Horizon `/order_book` for the book mid; `refrate` for the
reference mid.

**What it cannot determine.** Whether a deviation reflects manipulation, a stale
reference, or a genuinely different local price. This is a measurement of
disagreement, not an explanation — and it says nothing about what a route
through this corridor actually costs; see `price-impact.size` and
`depth.observed-executable` for that half.

**Undetermined.** When either side of the book is empty, the corridor has no
direct pair, no reference rate was supplied, or the reference cross-check came
back unscorable.

**Source checked:** `checks/metric_deviation.go`.

---

## What is not yet measured

The following capabilities are described in the backlog but are not yet
implemented. They are documented here to prevent confusion between what the
repository supports today and what is planned.

- **SEP-38 live pricing** (backlog #106, issue #180) — a live round-trip through
  an anchor's SEP-38 quote server. Blocked on a corridor where an anchor
  publishes `ANCHOR_QUOTE_SERVER`.

- **Account-level concentration** — whether a single party controls the order
  book. Horizon's `/order_book` does not expose the offering account, so this
  cannot be measured from the current data source.

- **Historical trend metrics** — how loss, spread, or depth change over time.
  The run store records per-run data, but trend analysis is not yet computed.
