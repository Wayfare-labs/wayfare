# Why Stellar-native?

Issue [#253](https://github.com/Wayfare-labs/wayfare/issues/253), backlog
`#193` ([docs/backlog.md](backlog.md)).

Every claim below was checked against the code and documents of this
repository at commit `c45bbbc` on 2026-09-27. Where a claim depends on a
measurement, the measurement's own date and source are carried. Nothing here
describes a capability the repository does not have, and future roadmap items
are explicitly marked as future.

---

## The short answer

Wayfare is **Stellar-native because its measurement pipeline is built directly
on six concrete protocol primitives and ecosystem standards exposed by
Stellar**:

1. **Protocol-level asset identifiers** (`asset/`)
2. **Native strict-send pathfinding with transparent hop disclosure** (`dex/`, `route/`)
3. **Atomic dual-venue execution across order books and AMM pools** (`dex/`, `docs/liquidity-venues.md`)
4. **Anchor discovery and two-source cryptographic identity via SEP-1** (`anchor/`, `checks/`)
5. **Standardized off-chain Request-for-Quote (RFQ) pricing via SEP-38** (`sep38/`, `checks/`)
6. **Decoupled execution and inspection without smart-contract ABI wrappers**

Wayfare does **not** claim Stellar is the only network capable of moving value,
nor that Stellar has solved foreign exchange. The core measurement stance —
decimal-only money, scoring against independent reference mids, refusing to
recommend value-destroying routes, and reporting negative findings as facts —
is chain-agnostic ([docs/spike-non-stellar-corridor.md](spike-non-stellar-corridor.md)).
What is Stellar-native is the **data-gathering and verification layer**: Stellar
exposes the exact on-chain liquidity structures and standardized counterparty
metadata needed to evaluate corridor integrity without private broker agreements
or bespoke smart-contract integrations.

---

## 1. On-chain assets: protocol primitives vs smart contract bytecode

On Ethereum, Solana, or other general-purpose smart-contract chains, an "asset"
is an entry in the storage of an arbitrary contract address (an ERC-20 contract
or an SPL Token mint). Interacting with them requires compiling ABIs, handling
varying decimal precisions (0 to 18 decimals), and inspecting contract state
subject to custom execution logic, transfer fees, or blacklists.

In Stellar:

- **Assets are first-class ledger primitives.** An asset is represented
  natively on the ledger as either native lumens (`XLM`) or an alphanumeric
  code issued by a specific cryptographic account (`Code:IssuerAccount`,
  represented in code by `asset.Asset`, `asset/asset.go:27-31`).
- **Precision is uniform and fixed.** Every Stellar asset prices to at most 7
  decimal places (`dex/dex.go:167`). An amount with more than 7 decimal places
  is rejected as upstream corruption rather than silently rounded, ensuring no
  fabricated liquidity is manufactured (`dex/dex.go:224-231`).
- **Identity requires both Code and Issuer.** Anyone on Stellar can issue an
  asset called `USDC` or `NGN`. In Wayfare, `asset.Equal` and `asset.Identifiable`
  strictly enforce that asset codes alone do not constitute identity
  (`asset/asset.go:63-78, 110-115`). The verified asset registry
  (`asset/known.go`) keys every supported asset to its exact issuer key (e.g.,
  Circle for USDC, Link for NGNC, Bebop/Stablr for GHSC, Kotani for KESC).

### What the repository does not claim
Native asset primitives do not guarantee solvency, reserves, or backing. An
on-chain token is a credit obligation of its issuer. Wayfare's asset layer
models this boundary explicitly: unverified or unlisted assets encountered in
pathfinding are tracked as coverage gaps (`asset/known.go`, `route/route.go:520-525`),
not certified as safe.

---

## 2. Pathfinding: protocol engine with transparent hop disclosure

When pricing a remittance corridor, the question is: *if I send exactly 100
USDC, what is the best achievable amount of local currency delivered on-chain?*

Stellar Core and Horizon provide native endpoints for this question:
`/paths/strict-send` (`dex/dex.go:177-191`).

Crucially, Horizon's strict-send pathfinder provides **full hop disclosure**:
every returned record includes the exact sequence of intermediate assets
traversed (`dex/dex.go:239-251`). This hop transparency is the foundation of
Wayfare's corridor integrity model (`route/route.go:527-570`):

- **`DIRECT`**: At least one path executes directly between the send asset and
  the destination asset without routing through intermediate fiat assets.
- **`DERIVATIVE`**: Every path routes through an intermediate fiat currency
  (e.g., routing USDC → NGNC → GHSC). The corridor is therefore not an
  independent market, and `depends_on` names the intermediate asset whose
  counterparty risk the corridor inherits.
- **`NO-MARKET`**: Pathfinding returns zero paths across all tested trade
  sizes; no liquidity connects the two assets.

### What the repository does not claim
- Wayfare does **not** claim pathfinding is exclusive to Stellar. Decentralized
  exchange aggregators exist on EVM chains (e.g., 0x, 1inch) and Solana (e.g.,
  Jupiter).
- However, as evaluated in [docs/spike-non-stellar-corridor.md](spike-non-stellar-corridor.md),
  commercial aggregator APIs frequently return opaque or split routes that do
  not disclose intermediate hop dependencies in a uniform machine-readable format.
  Without full hop disclosure, the distinction between a `DIRECT` market and a
  `DERIVATIVE` market cannot be proven from the quote alone.
- Wayfare does **not** claim `/paths/strict-send` finds the global theoretical
  optimum across all off-chain venues. Horizon's pathfinder is heuristic and
  depth-limited (up to 3 hops). Wayfare checks what the network's own payment
  engine actually delivers.

---

## 3. Order books vs AMM pools: dual-venue reality

Stellar combines two distinct on-chain execution mechanisms at the protocol level:
1. The **SDEX (Stellar Decentralized Exchange)**: a protocol-native Central
   Limit Order Book (CLOB).
2. **AMM Liquidity Pools** (introduced in Protocol 18 / CAP-38): constant-product
   automated market makers built directly into the consensus layer.

When a path payment executes on Stellar, the settlement engine draws liquidity
from **both order books and AMM pools simultaneously**.

This dual-venue structure led to a fundamental architectural decision in
Wayfare:

- **Order book walks produce incorrect prices on Stellar.**
  Measured live on mainnet on 2026-08-04 (`dex/dex.go:9-14`): the USDC/NGNC order
  book (`/order_book`) showed a best bid of 333.33 NGNC per USDC with 2,184.54
  units of depth. Asked to price 100 USDC over the same market at the same
  moment, Horizon's `/paths/strict-send` returned 21,785.78 NGNC. That executed
  amount could not be reconstructed from order book offers because the engine
  settled depth against an AMM pool that `/order_book` does not observe.
- **Pathfinding is the pricing source; the order book is a diagnostic.**
  Wayfare prices executable rungs through `/paths/strict-send` (`venue: pathfinding`).
  It queries `/order_book` solely for diagnostic market-health metrics (`venue: order-book`,
  such as `spread.bid-ask` and `depth.observed`), as reconciled in
  [docs/liquidity-venues.md](liquidity-venues.md) and
  [docs/market-structure.md](market-structure.md).

### The negative finding
Native order books and AMMs do not imply liquidity. Across African fiat
corridors measured by Wayfare on 2026-08-08 ([docs/corridor-measurements.md](corridor-measurements.md)),
order books were observed to be either extremely thin or entirely absent:
- **USDC → NGNC**: Live, but delivering 25.02% to 97.68% loss against the USD/NGN
  reference mid across the size ladder (`UNUSABLE` at every size).
- **USDC → GHSC**: Every path ran through NGNC (`DERIVATIVE`), with losses of
  74.14% to 99.47%.
- **USDC → KESC**: Zero routes returned at any size (`NO-MARKET`).

Reporting these structural failures honestly is the reason Wayfare exists as a
monitor rather than an optimistic router.

---

## 4. Anchors & SEP-1: two-source cryptographic corroboration

On Stellar, an **anchor** is an entity that bridges off-chain banking rails to
on-chain credit tokens. Stellar Ecosystem Proposal 1 (SEP-1) defines
`stellar.toml`, a standardized file hosted at `https://<home_domain>/.well-known/stellar.toml`.

Wayfare reads SEP-1 (`anchor/anchor.go:1-70`) to establish a two-source
independent verification of counterparty identity:

1. **On-chain declaration**: The issuing account on the Stellar ledger sets its
   `home_domain` field (`checks/toml_home_domain_roundtrip.go`).
2. **Web declaration**: The server at that `home_domain` publishes a
   `stellar.toml` file containing `[[CURRENCIES]]` that lists the issuing
   account ID and asset code (`checks/toml_anchor_asset.go`).
3. **Issuer flag immutability**: Wayfare checks whether the issuing account
   maintains `AUTH_REQUIRED`, `AUTH_REVOCABLE`, or `AUTH_CLAWBACK_ENABLED`
   flags (`checks/issuer_auth_flags.go`), exposing whether funds can be frozen
   or clawed back by the anchor.

This provides verifiable counterparty identity without a centralized registry or
third-party certificate authority.

### What the repository does not claim
SEP-1 does not prove an anchor is solvent or that its fiat reserves are
audited. The checks in `checks/` evaluate structural compliance (e.g., does the
domain resolve? Is the asset declared? Does the signing key match?). Where
anchors fail these checks — such as missing currency declarations or DNS
misconfigurations — Wayfare records the finding without modifying the on-chain
measurement figures.

---

## 5. SEP-38: standardized off-chain Request-for-Quote (RFQ)

SEP-38 defines the Anchor RFQ API (`ANCHOR_QUOTE_SERVER`). It allows software
to query indicative prices (`GET /prices`) and firm quotes (`POST /quote`)
directly from an anchor before executing a deposit or withdrawal.

Wayfare implements a full SEP-38 client (`sep38/sep38.go:1-60`), including
rigorous handling of the fee-denomination trap:
- SEP-38 quotes may denominate fees in either the sell asset or the buy asset.
- As documented in `sep38/sep38.go:8-36`, naively adding fees across currencies
  produces dimensional errors. Wayfare uses a unified derivation
  (`gross_in_buy_asset = sell_amount / price`) that is mathematically correct
  regardless of which asset the anchor selects for fees.

### The empirical negative finding
While the SEP-38 client is built and tested, **live SEP-38 pricing across African
fiat corridors is currently unserviceable**.

A comprehensive empirical survey completed and documented in
[docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md) inspected all
four anchors issuing African-fiat tokens (NGN, GHS, KES, ZAR):
- Three anchors were reachable, one was unreachable.
- Of the reachable anchors, only one declared `ANCHOR_QUOTE_SERVER`, and its
  quote server did not list its African-fiat token among supported assets.
- `ngnc.online` published `WEB_AUTH_ENDPOINT` and `TRANSFER_SERVER_SEP0024`, but
  no `ANCHOR_QUOTE_SERVER` (`anchor/anchor.go:14-17`).

Wayfare reports this negative finding plainly: the repository supports the
standard in code, but marks live execution as blocked pending anchor adoption.
Synthesizing an anchor quote where none exists is strictly forbidden.

---

## 6. What Wayfare refuses to claim (the boundary)

To avoid misleading readers, the project maintains strict boundaries regarding
its relationship with Stellar:

| Dimension | What Wayfare relies on | What Wayfare explicitly refuses to claim |
|:---|:---|:---|
| **Exclusivity** | Native `Code:Issuer` assets, `/paths/strict-send`, SEP-1, SEP-38. | Does not claim other chains cannot support corridors or that Stellar is uniquely suited for all payments. |
| **Liquidity** | Inspects on-chain CLOB order books and AMMs together. | Does not claim Stellar order books are deep or liquid (measured African fiat corridors are thin or empty). |
| **Routing** | Evaluates Horizon's best path for fixed inputs. | Does not claim pathfinding guarantees a good rate or finds off-chain private market liquidity. |
| **Anchors** | Verifies SEP-1 declarations and issuer flags. | Does not claim anchors are solvent, fully reserved, or immune to counterparty failure. |
| **SEP-38** | Standard client and checks are implemented. | Does not claim SEP-38 is widely supported; survey confirmed zero active African fiat quote servers. |
| **Roadmap** | Real-time scheduled monitoring and historical analysis. | Does not claim future features (ML, predictive routing, multi-chain) are implemented today. |

---

## Sources and verification

Every code path and observation cited above was checked against the repository:

| Subject | Source in repository | Checked at |
|:---|:---|:---|
| Asset primitives & identity | `asset/asset.go:27-31, 85-94, 110-115` | `c45bbbc`, 2026-09-27 |
| Verified corridor assets | `asset/known.go:37-60` | `c45bbbc`, 2026-09-27 |
| Precision limit (7 decimals) | `dex/dex.go:167, 224-231` | `c45bbbc`, 2026-09-27 |
| Strict-send pathfinding | `dex/dex.go:177-191` (`/paths/strict-send`) | `c45bbbc`, 2026-09-27 |
| Hop disclosure & decoding | `dex/dex.go:239-251` | `c45bbbc`, 2026-09-27 |
| Structural integrity classifier | `route/route.go:527-570` (`DIRECT`, `DERIVATIVE`, `NO-MARKET`) | `c45bbbc`, 2026-09-27 |
| Order book vs AMM finding | `dex/dex.go:9-14`, [docs/liquidity-venues.md](liquidity-venues.md) | Measured 2026-08-04 |
| Corridor ladder measurements | [docs/corridor-measurements.md](corridor-measurements.md) | Measured 2026-08-08 |
| SEP-1 TOML discovery | `anchor/anchor.go:1-70`, `checks/toml_anchor_asset.go` | `c45bbbc`, 2026-09-27 |
| Two-way domain round-trip | `checks/toml_home_domain_roundtrip.go:20-60` | `c45bbbc`, 2026-09-27 |
| SEP-38 RFQ client & math | `sep38/sep38.go:8-36, 60-120` | `c45bbbc`, 2026-09-27 |
| SEP-38 African fiat survey | [docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md) | Completed survey |
| Non-Stellar spike & boundaries | [docs/spike-non-stellar-corridor.md](spike-non-stellar-corridor.md) | Issue #218 |
