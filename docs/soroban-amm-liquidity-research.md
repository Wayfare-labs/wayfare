# Is fiat-token liquidity hiding in Soroban AMMs?

Research note for #681. Every figure Wayfare publishes is "the best route
Horizon's strict-send pathfinder finds", and that pathfinder sees classic order
books and classic AMM pools only ([liquidity-venues.md](liquidity-venues.md)).
Soroban-native AMMs (Soroswap, Aquarius, Phoenix) are invisible to it. If
material liquidity for a measured corridor's token lived there, the published
loss would overstate what a sender using a Soroban router would lose.

**Result in one line:** on 2026-11-23, only **NGNC** has any Soroban liquidity
at all: 31,415.88 NGNC across six pools, which is **3.43%** of the NGNC
liquidity on the ledger. **NGNT, GHSC and KESC have none, and cannot have any
today.** None of the three has a Stellar Asset Contract deployed, so no Soroban
contract can hold them. Published losses are not materially affected. The [matched-time comparison](soroban-aggregator-comparison-research.md)
(#682) shows that no Soroban router beats Horizon at any rung. Splitting a
trade across both venues improves NGNC delivery by at most 2.65% in the splits
sampled, and changes no grade. **Recommendation: document**, and re-run the
capture if Soroban NGNC reserves grow.

## Method

- **When:** 2026-11-23. First capture 12:54:46Z–12:55:42Z (Soroban RPC ledgers
  65630488–65630494, Horizon ledger 65630487). Second capture 12:56:49Z–12:57:37Z
  (ledgers 65630511–65630518), taken after the #682 quotes. **Both captures are
  identical, pool for pool and offer for offer.**
- **Assets:** the four corridor tokens with recorded history under `data/`:
  NGNC, GHSC, KESC (LINK.IO issuer) and NGNT (Cowrie issuer). The issuers are
  the ones `asset/known.go` records from each issuer's own `stellar.toml`.
- **Contract IDs:** each asset's Stellar Asset Contract ID is derived offline
  from (code, issuer) (`sac.py`). The derivation is checked against the two
  well-known IDs, native XLM `CAS3J7GY…OWMA` and Circle USDC `CCW67TSZ…MI75`.
  The derived IDs are recorded in `liquidity.json` under `contracts`. Whether
  each one is deployed is read with `getLedgerEntries` on its instance key and
  recorded under `sac_deployment`.
- **Pool enumeration, from on-chain state rather than protocol web APIs:**
  - **Soroswap:** the factory `CA4HEQTL…7AW2`, the address Soroswap publishes in
    `soroswap/core/public/mainnet.contracts.json`. The script reads
    `all_pairs_length`, then every pair address from factory storage, then every
    pair's instance storage (token 0, token 1, reserve 0, reserve 1). All
    **226 pairs** were read.
  - **Aquarius:** the router, read from the Soroswap aggregator's on-chain
    adapter storage. The script calls `get_tokens_sets_count` and
    `get_pools_for_tokens_range` over all **343 token sets (392 pools)**. For
    each pool holding a corridor token it calls `get_tokens`, `get_reserves`
    and `get_fee_fraction`.
  - **Phoenix:** the multihop contract comes from the aggregator's adapter
    storage, and the factory from the multihop's instance storage. The script
    calls `query_pools` (**13 pools**), then `query_pool_info` for each.
  - The aggregator's deployed wasm hash matched Soroswap's published one
    (`5e0bff5a…7d2c`), so these entry points are the live ones.
- **Classic liquidity (what Horizon *can* route through):**
  - every classic AMM pool holding the token (`/liquidity_pools?reserves=`);
  - every open offer *selling* the token (`/offers?selling=`).

  Both lists were paginated to the end; both came back complete.
- **Unit of comparison:** the corridor token itself. A reserve of NGNC and an
  offer of NGNC are the same unit, so no price is needed to compare them, and
  none is applied.
- **Read-only:** `simulateTransaction` and `getLedgerEntries` only. Nothing
  was signed or submitted.
- **Recorded:** `docs/research/soroban-liquidity/2026-11-23/liquidity.json` and
  `liquidity-after.json`, with `SHA256SUMS`. `run.sh` repeats the whole
  capture (Python 3 standard library only).

## Per-corridor reserves, 2026-11-23

All amounts are in the corridor token's own units.

| Token | SAC deployed | Soroban pools | Soroban reserve | Classic AMM reserve | Classic offers selling it | Soroban share |
|:--|:--|--:|--:|--:|--:|--:|
| NGNC | yes | 6 | 31,415.88 | 721,165.83 (27 pools) | 163,485.63 (43 offers) | **3.43%** |
| NGNT | **no** | 0 | 0 | 1,269,184.61 (33 pools) | 4,568,457.95 (151 offers) | **0%** |
| GHSC | **no** | 0 | 0 | 920.51 (1 pool, vs NGNC) | 0 | **0%** |
| KESC | **no** | 0 | 0 | 0 | 0 | — (no liquidity on either venue) |

The SAC column matters more than the zeros beside it. A Soroban AMM holds
tokens through the token's contract. With no contract deployed for NGNT, GHSC
or KESC, their zero is structural: it cannot be a pool the enumeration missed.
Anyone can deploy a classic asset's SAC permissionlessly, so this can change
without the issuer doing anything. Re-running the capture records it.

### The six Soroban NGNC pools

| Protocol | Pool | Pair | NGNC reserve | Other reserve | Implied NGNC per unit |
|:--|:--|:--|--:|--:|--:|
| Aquarius | `CBTGLCDJ…XUQK` | NGNC/USDC | 11,343.00 | 7.03 USDC | 1,614.2 per USDC |
| Soroswap | `CBLUZUK6…U4B` | NGNC/USDC | 8,336.95 | 5.20 USDC | 1,603.5 per USDC |
| Aquarius | `CA4EA5AR…T3RW` | XLM/NGNC | 6,591.77 | 31.94 XLM | 206.4 per XLM |
| Aquarius | `CBPDULTU…JE5` | AQUA/NGNC | 4,859.35 | 14,309.48 AQUA | 0.34 per AQUA |
| Soroswap | `CDDEKHF7…NQU7` | XLM/NGNC | 284.81 | 1.49 XLM | 191.4 per XLM |
| Soroswap | `CCSJOTOG…6VDI` | LIBRE/NGNC | 0.0003 | 0.00003 LIBRE | dust |

The implied rate is the reserve ratio, a pool's marginal price before fees.
It is shown only to make the pool sizes legible, and it is not a quote. The
quotes are in #682.

### Where it matters: the pairs a USDC sender actually routes through

| NGNC held against | Soroban | Classic (pools + offers) |
|:--|--:|--:|
| USDC | 19,679.96 | 34,818.68 |
| XLM | 6,876.57 | 345,066.29 |

This is the one place the Soroban figure is not small. Against USDC directly,
Soroban holds **36%** of the NGNC on the ledger. The pools behind it hold
**12.23 USDC** between them, so their depth is a few dollars. On this date the
Horizon route Wayfare publishes goes through XLM at every rung: USDC → XLM →
NGNC from 1 USDC up, and USDC → VELO → XLM → NGNC at 0.1. Against XLM, Soroban
holds 2% of the NGNC.

## Are published losses materially affected?

**No, on this date, for any measured corridor.**

- **NGNT, GHSC, KESC:** the token has no deployed contract, so no Soroban pool
  holds it and no Soroban route can end in it. The published figures are
  complete as far as Soroban is concerned.
- **NGNC:** the #682 matched-time capture quoted the Soroswap router and the
  Aquarius router at every rung. **The best Soroban quote is below Horizon's
  at every rung**: 2.97% less at 0.1 USDC, 93.81% less at 5,000 USDC.
  - **Soroban alone:** from 25 USDC up, Horizon delivers more NGNC than every
    Soroban pool *holds* combined (33,627.13 against 31,415.88). No Soroban-only
    route, split or not, could match it there.
  - **Splitting across both venues:** sending part of a trade through Soroban
    and the rest through Horizon helps. In the splits sampled, delivery
    improved by up to **2.65%**, and the best sampled split lowers the loss by
    **at most 1.70 percentage points** (36.05% → 34.35% at 100 USDC). No grade
    changes: 25 USDC stays GOOD, 50 stays POOR, 100 stays UNUSABLE. No router
    examined here performs that split. The Soroswap aggregator's on-chain
    adapters are Soroswap, Phoenix and Aquarius only.

So the charitable-benchmark worry, pointed the other way, does not hold on
this date. The Horizon figure is not a single router's limitation hiding a
cheaper Soroban path.

## What this does and does not show

- **It shows** the full set of pools in the three named protocols that hold a
  corridor token. Every protocol was enumerated from on-chain state, not from a
  listing that could omit pools.
- **It does not cover** other Soroban AMMs (for example Comet or a new
  protocol), or private or unlisted contracts. The three named in #681 were
  examined.
- **It is one date.** The two captures, about two minutes apart, agree exactly,
  but that says nothing about next month. NGNC's Soroban pools are small enough
  that one liquidity provider could change the picture.
- **Classic liquidity is counted generously.** The NGNC classic total includes
  pools against illiquid or joke tokens, and one 325,800.86 NGNC pool against
  GHSC, which a USDC sender cannot reach usefully. That is why the USDC and XLM
  pair table exists. The headline "3.43%" uses everything on both sides.
- **Reserves are not executable depth.** A constant-product pool's reserve can
  never be fully drained. The reserve figures bound what a pool could deliver
  and do not price it.

## Recommendation: document

- **Ignore** would be wrong for NGNC. Soroban does hold a third of the
  ledger's NGNC against USDC directly, and a cross-venue split measurably
  improves delivery, by up to 2.65% in the splits sampled.
- **Measure**, meaning a Soroban venue in the engine, is not justified:
  - it would change no published grade today;
  - it would mean pricing AMM math locally, which is maintainer-owned pricing
    arithmetic ([CONTRIBUTING.md](../CONTRIBUTING.md));
  - no existing router combines the two venues, so the figure would describe a
    route nobody can take.
- **Document** is what this note and the pointer in
  [liquidity-venues.md](liquidity-venues.md) do. The published figure is
  "best Horizon route". On 2026-11-23 that was also the best route available
  through any Soroban router, for every measured corridor.

**When to revisit:** re-run `docs/research/soroban-liquidity/run.sh` if any of
these change:
- NGNC's Soroban reserve against USDC or XLM grows past the classic figure;
- NGNT, GHSC or KESC gets a deployed SAC (`sac_deployment` in the capture);
- a router appears that splits across classic and Soroban liquidity.
