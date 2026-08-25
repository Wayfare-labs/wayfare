# Upstream failures: what each shape means

Wayfare measures by asking third parties — Horizon for pathfinding, two FX
feeds for the reference rate. Those parties fail in different ways, and the
project's rule that *nothing is ever synthesised to fill a gap* only works if
each failure shape has a defined contract. This page records them.

## A ladder where some sizes fail

`Engine.Ladder` prices a corridor at several sizes. Three outcomes are
possible, and they are distinct states, not shades of one:

| Outcome | Signal | Meaning |
|:---|:---|:---|
| Every size measured | `Failed() == false`, `PartiallyFailed() == false` | A complete measurement |
| Some sizes measured | `PartiallyFailed() == true`, `UnmeasuredSizes()` names them | A real measurement **of the sizes that answered**, qualified |
| No size measured | `Failed() == true` | Nothing was learned; every figure on the result is a zero-value artefact, not a number |

For the partial case the contract is:

- **Figures describe only measured sizes.** Floor, worst loss and the
  recommendation come from rungs that priced. An unmeasured size is unknown —
  never zero loss, never NO-MARKET.
- **Integrity reflects every rung that answered.** One dead size cannot erase
  a direct path found at another.
- **The Finding qualifies itself.** A partial ladder appends how many sizes
  could not be measured, so a reader never mistakes a partial curve for a
  complete one.

## Horizon throttling vs Horizon failing

A `429` from Horizon surfaces as [`dex.ErrRateLimited`](../dex/dex.go), with
the endpoint and whatever interval the `Retry-After` header requested. Any
other status stays a generic error carrying the code.

The distinction matters because the remedies differ: under a monitoring
schedule a 429 is routine and transient — wait for the interval and retry —
while a 500 means the upstream is unhealthy and no interval fixes it. A
corridor that was merely throttled must not read as one that is broken.

When Horizon sends no usable `Retry-After`, the reported interval is zero:
unknown, not guessed.

## Reference providers

The reference rate has its own contracts, documented at the definitions:

- **Agreement bands** (`refrate/cross.go`) — AGREE / DISAGREE / STALE /
  MALFUNCTION / SINGLE, what is scored against in each band, and why a
  benchmark the feeds cannot agree on is refused rather than adjudicated.
- **The cache age bound** (`refrate/cached.go`) — a cached rate is served
  only inside its TTL even when the provider is down; past the bound a failed
  refetch is an error, because presenting a stale figure as current is the
  one thing the bound exists to prevent. Rate limits unwrap to
  `refrate.ErrRateLimited` rather than flattening into a generic failure.

## Related

- [deployment.md](deployment.md) — serving when a whole measurement fails
- [snapshot-format.md](snapshot-format.md) — testing these paths offline
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
