# Ladder sizes

Wayfare prices every corridor across a range of trade sizes, not a single
amount. The default set — called the **ladder** — is defined in
`route.DefaultSizes`:

| Rung | Size (USDC) | Purpose |
|-----:|------------:|:--------|
| 1 | 0.1 | **Structural floor probe.** Price impact at this size is negligible, so whatever loss remains is the corridor's spread rather than its depth. This is the rung that revealed the founding finding: NGNC loses ~25% even at dust size. |
| 2 | 1 | Small retail transfer. |
| 3 | 5 | Small retail transfer. |
| 4 | 10 | Typical micro-remittance. |
| 5 | 25 | Typical micro-remittance. |
| 6 | 50 | Mid-range remittance. |
| 7 | 100 | Mid-range remittance. |
| 8 | 250 | Larger remittance. |
| 9 | 500 | Larger remittance. |
| 10 | 1000 | Near the corridor's liquidity ceiling on thin markets. |
| 11 | 2500 | **Exhaustion probe.** Exposes how much the curve degrades past realistic sizes. |
| 12 | 5000 | **Exhaustion probe.** The top rung exists to show which of two causes produced a bad number: spread or depth. |

---

## Why a ladder and not a single size

A corridor that looks acceptable at $10 may be value-destroying at $100, and
vice versa. Pricing at one size hides the shape of the curve — whether loss
is flat (spread-dominated) or steep (depth-dominated), whether it is
monotonic, whether there is a local minimum. The ladder makes the curve
visible.

---

## Why these specific sizes

The ladder spans four orders of magnitude (0.1 → 5000) deliberately.

**The bottom rung (0.1) exists to isolate the structural floor.** At 0.1
units, price impact is negligible — the trade is small enough that it routes
directly with no bridge hop. Whatever loss remains is the corridor's spread,
not its depth. This is the corridor's cost with liquidity effects removed, and
it determines whether any trade size can be acceptable. On the NGNC corridor
this floor was measured at ~25%, already exceeding the Unusable threshold —
meaning no trade size can be acceptable because the zero-size limit is already
unacceptable.

**The top rung (5000) exists to expose exhaustion.** At 5000 USDC the NGNC
corridor's marginal return drops to 1.90 NGN per dollar (against a mid of
1364), showing the pool is effectively empty. Without this rung, a reader
seeing 25% loss at dust might assume "just use a smaller amount" without
knowing how much worse it gets.

**The middle rungs cover realistic remittance sizes.** The 1 → 250 range
covers the typical stablecoin-to-fiat remittance. Each rung is a Horizon round
trip, so the count is bounded — twelve rungs is enough to show the curve shape
without being expensive to measure.

**The progression is not uniform.** The rungs are denser at small sizes
(where the curve changes fastest on thin corridors) and sparser at large
sizes (where the curve flattens into exhaustion). This is a deliberate
choice: more resolution where the signal is richest.

---

## Custom sizes

Callers can override the ladder via the `sizes` query parameter on the API or
the `Sizes` field on `LadderRequest`. When `Sizes` is empty, `DefaultSizes`
is used. Custom sizes are sorted ascending before pricing, regardless of the
order provided.

---

## What the ladder measures

For each rung, the engine:

1. Prices the corridor through Horizon pathfinding.
2. Computes the effective rate and loss against the reference mid.
3. Grades the loss against the verdict thresholds (Good ≤3%, Fair ≤8%,
   Poor ≤20%, Unusable >20%).

The ladder result then summarises the curve: the floor (loss at the smallest
priced rung), the worst loss, and the recommended quote (if any size reached
Poor or better).

---

## Related

- [docs/corridor-measurements.md](corridor-measurements.md) — published
  figures from a full ladder run
- [docs/snapshot-format.md](snapshot-format.md) — how upstream responses are
  recorded and replayed
- `route/ladder.go` — the implementation
