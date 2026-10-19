# Cost decomposition

**Status: implemented, on the wire since 2026-08-25** (commit `d762693`,
"route: publish per-rung cost decomposition on the wire"). What the `cost` block
on a priced rung contains, which components are determined today, and —
explicitly — which are not and why. Every claim was checked against the code at
time of writing (2026-10-17).

The single `loss_pct` a rung publishes answers *how much* a route loses against
the reference mid. It does not answer *where the money went*. The cost
decomposition is that second answer: one block per priced rung, breaking the
effective transfer cost into named components.

Production call site: `route/ladder.go` (`summarise`, at the priced-rung loop)
calls `route.Decompose`. Wire conversion: `route.ToCostBlockJSON`. The block is
attached to `RungJSON.Cost` and is omitted entirely for an unpriced rung or an
unavailable decomposition.

## Wire shape

```json
"cost": {
  "parts": [
    { "component": "fx_loss",        "amount": "5.82", "pct": "4.31", "determined": true },
    { "component": "network_fees",   "determined": false, "reason": "network fee not measured; …" },
    { "component": "anchor_fee",     "determined": false, "reason": "anchor fee not available; …" },
    { "component": "slippage",       "determined": false, "reason": "single quote available; …" },
    { "component": "expected_failure","determined": false, "reason": "no failure history exists yet; …" }
  ],
  "total_loss_pct": "4.31"
}
```

Two properties carry the project's discipline:

- **Amounts and percentages are decimal strings**, never JSON numbers, like
  every other money value on the wire (`CostPartJSON`, `route/wire.go`).
- **An undetermined component carries no number at all.** `amount` and `pct`
  are omitted, and `reason` is present. Zero is not used for "not measured":
  an unmeasured component has no amount, and emitting `0` would be the
  default-to-zero failure in a new place.

## Components

| Component | Determined today? | Meaning |
|:---|:---|:---|
| `fx_loss` | **Yes** | The measured loss against the reference mid. |
| `network_fees` | No | Stellar base-fee charges for the path's operations. |
| `anchor_fee` | No | The anchor's own conversion charge, obtainable via SEP-38. |
| `slippage` | No | Cost that appears when a size is compared with another. |
| `expected_failure` | No | The probabilistic cost of a route that fails after being chosen. |

### `fx_loss` — determined

The component's `amount` and `pct` are the rung's `loss_amount` and `loss_pct`
(`route/cost.go`, `Decompose`). The reference mid is the one the rung was
scored against.

**It currently carries the whole measured loss, not only a currency-conversion
component**, because it is the only component that is determined. The name
describes the residual it fills; when the other components become determinable
they will take their share out of it. A reader should treat `fx_loss` as "the
measured loss, unattributed" rather than as proof that currency movement
accounts for all of it.

### `network_fees` — undetermined

Reason: *network fee not measured; determining it requires the path's operation
count and the current Stellar base fee.*

A Stellar path payment charges a base fee per operation, and a multi-hop path is
more operations than a direct one. `Decompose` sees a single `Quote`, not the
path's operation count, and the currently-effective base fee is not part of the
measurement. Unavailable is unknown, never zero, and small is not zero.

### `anchor_fee` — undetermined

Reason (DEX route): *anchor fee not available; the anchor does not publish an
ANCHOR_QUOTE_SERVER, so its fee cannot be obtained programmatically.*

`sep38` can price an anchor's fee when the anchor publishes an
`ANCHOR_QUOTE_SERVER`. On the corridors measured today the route is DEX-only,
and an anchor that publishes no quote server has no machine-readable rate — the
absence is a fact about the anchor, not a zero fee.

Reason (when a quote is `kind: anchor-sep38`): *anchor fee available via SEP-38
but no corridor with a published quote server has been priced yet.* No live
SEP-38 quote has ever been produced by a production caller (see the ecosystem
map and issue #180); the `sep38.Quote.FeeInBuyAsset` field is the input that
would fill this component.

### `slippage` — undetermined

Reason: *single quote available; slippage requires a comparison across sizes.*

`Decompose` takes one quote. Slippage is the cost that only appears when one
size is compared with another, so it cannot be seen from a single rung. It
belongs at the ladder level (backlog entry #79). At present, adjacent-rung
execution behaviour is reported separately as `marginal_cost` on the rung
(`route/ladder.go`), which is a measurement of the change, not a decomposed
component.

### `expected_failure` — undetermined

Reason: *no failure history exists yet; runstore is collecting but has not
accumulated enough observations.*

The probability that a route fails after it was chosen is a layer-3 quantity.
The repository records no labelled failure history, so the component stays
explicitly unknown. `TestDecomposeExpectedFailureCostUndetermined` exists so a
later change cannot quietly default it to zero.

## The total

`total_loss_pct` is the rung's loss percentage. Because `fx_loss` is the only
determined component, the determined pct currently equals the total by
construction. `route.ReconcileComponents` exists to assert that determined
components plus the explicitly-undetermined remainder reconcile with the
published total within a stated tolerance, so the block cannot silently drift
from the headline; its test lives with the other cost tests (`route/cost_test.go`).

## What it does not do

- It does not compare across sizes, so it does not report slippage.
- It does not know a path's operation count, so it does not report network fees.
- It does not find an anchor fee without a published SEP-38 quote server.
- It does not estimate failure cost.
- It does not change any verdict or integrity state. Like every check and
  metric, it qualifies the headline and never moves it.

## Related

- [api.md](api.md#get-apicorridor) — the corridor response, including the rung
  `cost` block
- [metrics.md](metrics.md) — the methodology pages for the measured metrics
- [liquidity-venues.md](liquidity-venues.md) — why two venues cannot be
  reconciled by arithmetic
- [market-structure.md](market-structure.md) — the market-structure vocabulary
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the unknown-never-zero invariant this
  document applies
