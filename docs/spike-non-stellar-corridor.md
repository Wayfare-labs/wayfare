# Spike: could Wayfare measure a non-Stellar corridor

Issue [#218](https://github.com/Wayfare-labs/wayfare/issues/218), backlog
[#158](https://github.com/Wayfare-labs/wayfare/blob/main/docs/backlog.md).

**Status: completed, design-only. Finding: PARTLY — the measurement core is
chain-agnostic and would port; the pricing source, asset identity and integrity
classification are Stellar-shaped and would have to be replaced, not reused.**
No implementation is attempted, and nothing below is described as built.

The README "avoids claiming Stellar exclusivity"; this document establishes
what would actually have to change, checked against the tree at `81da004` on
2026-09-24.

---

## Method

For each package on the measurement path, the question asked was narrow: **does
this package depend on Stellar, or on something more general?** A package is
only reusable if its inputs are expressible without a Stellar concept. The
answer is a three-way split below: reusable, replaceable, and shaped by
Stellar.

---

## Reusable as-is

### `refrate` — the independent reference mid

`refrate.Rate` is `{Base, Quote ISO-4217, Mid decimal, AsOf, Source,
FetchedAt}` (`refrate/refrate.go:27-42`). There is no Stellar concept in it.
The two live providers (`refrate/exchangerate.go`, `refrate/currencyapi.go`)
are ordinary HTTP currency APIs. The cross-check, the caching, and the
malfunction band (`refrate/cross.go`) are all pair-level logic.

A non-Stellar corridor needs the same thing: an independent mid for the fiat
pair to score against. **This layer would port unchanged.**

### The library's stance

The properties the README and `CONTRIBUTING.md` treat as the product — never
recommend when nothing clears the threshold, never invent a rate, decimal-only
money, report a negative finding as one — are properties of the *reporting*
layer, not of Stellar. They are the part worth carrying to another chain.

---

## Replaceable behind the same interface

### `dex` — the pricing source

`dex.Client.StrictSendPaths` is a Horizon `/paths/strict-send` call
(`dex/dex.go:177-189`), and `route.Engine.quoteDEX` prices each rung from its
result (`route/route.go:591-594`). The slippage probe is a second pair of
`BestPath` calls (`dex/dex.go:296-309`).

Nothing in `route`'s *scoring* depends on where the paths came from — a quote
has a receive amount, a rate, and a description. So a non-Stellar chain would
need a **second pricing adapter** satisfying the same shape, not a rewrite of
scoring. Candidates that already publish a machine-readable quote exist, for
example:

| Chain | Quote surface | Status |
|:---|:---|:---|
| Solana | Jupiter Quote API (`/swap/v1/quote`, Metis routing engine) | live; [docs](https://dev.jup.ag/docs/swap/v1/get-quote), checked 2026-09-24 |
| EVM | 0x Swap API; Uniswap-family quoters | live; [0x](https://0x.org/post/announcing-0x-api-v1), checked 2026-09-24 |

**The hard part is not the call, it is the hop disclosure.** Stellar's
pathfinder returns the full path, which is what makes `DIRECT` vs `DERIVATIVE`
decidable (`route/route.go:538` `classify`). Aggregator quote APIs generally
return a best price and an opaque route. A DERIVATIVE corridor on an EVM
router may be **undetectable from the quote alone**, which would silently
downgrade the integrity taxonomy — the thing the project most refuses to do.
Replacing the pricing adapter therefore requires either a route-disclosing
endpoint or an explicit `UNKNOWN`-style integrity for chains that do not
disclose hops. The latter is a taxonomy change and, per the issue boilerplate,
**must be flagged as a different issue with a different review bar.**

### `snapshot` / `runstore` — identity and storage

`runstore.CorridorKey(send, receive)` (`runstore/runstore.go:168`) yields a key
like `USDC-NGNC`. Asset code alone does not identify an asset — the project
states this itself and resolves issuers per SEP-1
(`CONTRIBUTING.md`, "Verify against live sources"). Two chains would collide on
`USDC-…` immediately, so keys would need a chain qualifier. That is a
**run-record layout change** and is likewise out of scope here.

---

## Shaped by Stellar — no drop-in equivalent

### `asset` — identity

`asset.Asset` carries a CAIP-2 Stellar identifier
(`stellar:CODE:ISSUER`, `asset/asset.go:83-92`), an issuer account, and a
`KindStellar` in the classifier (`asset/class.go`). An EVM ERC-20 contract
address or a Solana mint is a different identity model (no issuer account, no
`stellar.toml`). A chain-agnostic asset type would have to be introduced before
anything else.

### `anchor` — issuer declaration

`anchor` reads SEP-1 `stellar.toml` and SEP-38 `ANCHOR_QUOTE_SERVER`
(`sep38/`). These are Stellar ecosystem standards. There is no chain-agnostic
equivalent; a non-Stellar corridor would have to establish the issuer's own
declared status some other way, or report it as not established — which the
project's rules prefer to estimating.

### The `status`/`live` corroboration

The strongest existing result is that the ledger and the issuer's own SEP-1
document agree independently (see `docs/corridor-measurements.md`, the
sister-corridor sweep). That two-independent-sources property is Stellar-
specific, because SEP-1 is what supplies the second source. Losing it would
lose a property the project currently relies on.

---

## Conclusion, and what it costs

**Measurable in principle. Not a port.** The realistic shape is a second
implementation of the *pricing source* and the *asset identity*, sharing
`refrate`, the ladder shape, the scoring, the verdict thresholds, and the
reporting stance.

Three things would have to be settled *before* any code, and each is a separate
issue with a different review bar:

1. **Hop disclosure.** What integrity value a chain that does not disclose
   route hops gets, and whether that is acceptable or disqualifying.
2. **Identity scheme.** A chain-qualified asset identifier, which changes the
   run-record key format.
3. **The second source for issuer status.** What replaces SEP-1, or a statement
   that no second source exists on that chain.

Recommendation: do not start. The value of a non-Stellar corridor is real only
if a consumer exists, and that is unresolved — see
[`spike-api-consumers.md`](spike-api-consumers.md) (#217), which returns
inconclusive pending #326.

## What this spike did not do

- It did not call any non-Stellar quote API.
- It did not measure a non-Stellar corridor.
- It did not change the integrity taxonomy, thresholds, or the run-record
  layout — those are flagged above as separate work.

## Sources

| Source | Checked |
|:---|:---|
| `asset/asset.go:83-92`, `asset/class.go` (CAIP-2 Stellar ids, `KindStellar`) | 2026-09-24 |
| `dex/dex.go:169-189` (`StrictSendPaths` → `/paths/strict-send`) | 2026-09-24 |
| `route/route.go:538` (`classify`), `:591-594` (`quoteDEX`) | 2026-09-24 |
| `route/route.go:649-658`, `dex/dex.go:296-309` (slippage probe) | 2026-09-24 |
| `refrate/refrate.go:27-42` (ISO-4217 `Rate`, no Stellar concept) | 2026-09-24 |
| `runstore/runstore.go:168` (`CorridorKey`) | 2026-09-24 |
| [Jupiter Quote API](https://dev.jup.ag/docs/swap/v1/get-quote) | 2026-09-24 |
| [0x Swap API](https://0x.org/post/announcing-0x-api-v1) | 2026-09-24 |
| [SEP-1 / SEP-38](https://github.com/stellar/stellar-protocol) | 2026-09-24 |
