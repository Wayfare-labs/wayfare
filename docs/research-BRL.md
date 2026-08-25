# Research: does a usable BRL (Brazilian Real) issuer exist on Stellar?

**Status: completed.** Investigated four historical BRL tokens; none are usable
today. Verdict: NO-MARKET.

---

## Why this matters

BRL is a high-volume fiat currency (Brazil's PIX rail alone processes ~US$550B
/month). If a live Stellar-native BRL token existed with Horizon path data,
Wayfare could measure the USDC→BRL corridor and report on cost for sending
stablecoins to Brazil. Several BRL tokens have been issued on Stellar over the
years; this research checks whether any of them remain active, have live
stellar.toml files, and produce Horizon path data.

---

## Sources investigated

### 1. MBRL — Mercado Bitcoin (official issuer)

- **URL:** https://mbrl.com.br/
- **Issuer account:** `GDLS4RCNECY46KKA4OGU2MMJILUK3I372CUFISWM4HKCV7265RY2NJ4Z`
- **Home domain:** `mbrl.com.br` (set on-chain, confirmed via Horizon account
  lookup)
- **What it is:** Brazilian Real stablecoin launched November 2022 by Mercado
  Bitcoin, Latin America's largest digital asset platform. Custody by MB Pay
  (BACEN-regulated payment institution). Originally implemented on Stellar,
  later also on Ethereum.
- **stellar.toml:** The issuer's home domain `mbrl.com.br` returned HTTP 522
  (connection timed out) at time of investigation. No `stellar.toml` could be
  fetched. The on-chain home_domain is set, but the document is unreachable.
- **Supply on Stellar:** ~2,051,490 tokens, 20 authorized accounts. Last
  ledger activity: 2026-05-31. The supply is modest, suggesting limited
  on-chain usage.
- **Horizon pathfinding:** Queries for USDC→MBRL paths returned HTTP 400 (no
  path found). No orderbook entries or liquidity pools exist for this pair.
- **SEP-38:** No evidence of a SEP-38 quote server. Mercado Bitcoin operates
  its own exchange with BRL market pairs (USDC/BRL, BTC/BRL, etc.) but these
  are off-chain, not Stellar-native SEP-38 endpoints.
- **Verdict: Rejected.** Home domain is down, no stellar.toml accessible, no
  Horizon path data, no SEP-38. The token exists on-ledger but is not usable
  as a corridor.

### 2. MBRL — Unofficial issuer (globalflipvault.com)

- **URL:** https://globalflipvault.com/
- **Issuer account:** `GAAHQWAGIFBWXHKURAIV6ZNMFJJTUK36ZJP3AVI4NLMAIJCBATYAFLIP`
- **Home domain:** `globalflipvault.com` (set on-chain)
- **What it is:** A separate token with the same `MBRL` code, issued by an
  unrelated account. 766M supply, 119 authorized accounts.
- **stellar.toml:** `globalflipvault.com` was unreachable at time of
  investigation (connection error).
- **Horizon pathfinding:** No paths found (HTTP 400).
- **Verdict: Rejected.** Not associated with Mercado Bitcoin. Unreachable home
  domain. No path data. This is a lookalike token, not the official MBRL.

### 3. BRL — nTokens ("Real Virtual")

- **URL:** https://ntokens.com/
- **Issuer account:** `GDVKY2GU2DRXWTBEYJJWSFXIGBZV6AZNBVVSUHEPZI54LIS6BA7DVVSP`
- **Home domain:** `ntokens.com` (set on-chain, confirmed via Horizon)
- **What it is:** Brazilian Real stablecoin launched December 2019 by nTokens
  Serviços Digitais Ltda. The earliest BRL token on Stellar. Was the primary
  Stellar-native BRL bridge for several years.
- **stellar.toml:** Live at https://ntokens.com/.well-known/stellar.toml.
  Valid TOML. `NETWORK_PASSPHRASE = "Public Global Stellar Network ;
  September 2015"`. BRL currency entry: `status = "live"`,
  `anchor_asset_type = "fiat"`, `anchor_asset = "BRL"`. SEP-6, SEP-24, and
  SEP-31 endpoints declared (`TRANSFER_SERVER`, `TRANSFER_SERVER_SEP0024`,
  `DIRECT_PAYMENT_SERVER`). However, the `ANCHOR_QUOTE_SERVER` field (SEP-38)
  is absent.
- **Stellar.toml coherence:** The document declares `status = "live"` but the
  issuer has publicly announced discontinuation. Effective immediately (as of
  their announcement), BRL deposits are suspended. From December 1, 2024,
  maintenance fees of 5% or BRL 10/week per address apply. Automated
  withdrawals via Stellar protocol have been disabled. After end of 2024, all
  BRL balances are subject to Stellar protocol clawback. The `status = "live"`
  in the stellar.toml is stale and contradicts the issuer's own discontinuation
  notice.
- **Clawback status:** `auth_clawback_enabled = true` is set on the issuer
  account. This confirms the issuer retains the ability to claw back tokens.
- **Supply on Stellar:** The issuer account holds only 713 XLM in native
  balance — no BRL token supply is visible in the issuer's own balances (all
  issued tokens are held by other accounts). The account was last modified
  2026-08-24, suggesting some recent activity (possibly clawback operations).
- **Horizon pathfinding:** Queries for USDC→BRL paths returned HTTP 400 (no
  path found). No orderbook entries or liquidity pools exist.
- **SEP-38:** No `ANCHOR_QUOTE_SERVER` in the stellar.toml. No SEP-38
  endpoint.
- **Verdict: Rejected.** Service discontinued. stellar.toml is stale (declares
  "live" while the issuer has shut down). Clawback is active. No Horizon path
  data. No SEP-38. This was once the primary BRL bridge on Stellar but is no
  longer operational.

### 4. BRLT — Settle Network

- **URL:** https://settlenetwork.com/
- **What it is:** Brazilian Real stablecoin announced November 2020 at Stellar
  Meridian, issued by Settle Network in partnership with Stellar Development
  Foundation. Part of a pair with ARST (Argentine Peso). Aimed at
  cross-border remittances between Argentina and Brazil.
- **Issuer account:** Not confirmed from live sources. Settle Network raised
  $3M from SDF in December 2020.
- **stellar.toml:** No live stellar.toml found. The Settle Network website
  does not host a SEP-1 file. The project appears to have been a pilot or
  proof-of-concept that did not reach sustained production.
- **Horizon pathfinding:** Not tested — no issuer account confirmed, and
  project appears inactive.
- **Verdict: Rejected.** No live stellar.toml. No confirmed issuer account.
  Project appears inactive since 2020-2021.

---

## Additional candidates considered but not investigated

- **BRLP (Stellar Community Fund #44):** A regulated BRL settlement token
  described in an SCF grant application. Pre-mainnet, testnet PoC only. Not
  a production asset.
- **BRZ (Brazilian Digital Token):** ERC-20 on Ethereum, not a Stellar asset.
  Irrelevant to this corridor.
- **Mercado Bitcoin exchange pairs:** MB operates USDC/BRL, BTC/BRL, etc. on
  its own exchange, but these are off-chain orderbook pairs, not Stellar-native
  path payment routes. They cannot be measured by Wayfare's on-chain
  pathfinding.

---

## Horizon path data summary

| Pair tested | Source asset | Destination asset | Result |
|:---|:---|:---|:---|
| USDC → nTokens BRL | USDC (Circle) | BRL : nTokens | HTTP 400 — no path |
| USDC → MBRL (mbrl.com.br) | USDC (Circle) | MBRL : GDLS4... | HTTP 400 — no path |

All Horizon strict-send path queries returned empty results. There are no
orderbook entries, liquidity pools, or market-maker offers connecting USDC to
any BRL token on Stellar's public network.

---

## Recommendation

**NO-MARKET.** No usable BRL issuer exists on Stellar today.

The historical landscape:
- **nTokens BRL** was the longest-running Stellar BRL anchor (2019–2024) but
  has been discontinued, with clawback enabled and the stellar.toml stale.
- **MBRL (Mercado Bitcoin)** exists on-ledger from the official issuer but the
  home domain is down, no stellar.toml is accessible, and no Horizon path data
  exists.
- **BRLT (Settle Network)** appears to have been a pilot that did not reach
  production.

For a BRL corridor to become viable, the project would need:
1. **A live issuer** with a reachable stellar.toml declaring `status = "live"`.
2. **Horizon path data** — at least one path from USDC to the BRL token via
   orderbook or liquidity pool.
3. **Optional but valuable: a SEP-38 endpoint** — so the off-chain leg can be
   priced programmatically. Without it, Wayfare can only measure the on-chain
   leg.

Until such an issuer appears, the BRL corridor cannot be added. BRL is already
used as an illustrative currency in SEP-38 test fixtures, but test fixtures
are not production data.

---

## Related

- Issue [#59](https://github.com/Wayfare-labs/wayfare/issues/59) — this
  research task
- `docs/adding-a-corridor.md` — the step-by-step guide for new corridors
- `asset/known.go` — the fiat peg registry
