# Research: which Stellar fiat tokens have a two-sided book at all?

**Status: completed.** Investigated prominent fiat-pegged tokens on Stellar (including NGNC, GHSC, KESC, MXN, BRL, INR, and PHP) to determine how many fiat tokens maintain any executable two-sided order book (bids and asks) on the Stellar Decentralized Exchange (DEX).

**Finding: Very few, and predominantly none among active remittances.** Across all major Stellar fiat asset corridors surveyed, true two-sided liquidity on the decentralized order book is virtually absent. Fiat issuers on Stellar overwhelmingly rely on single-sided liquidity, automated market maker (AMM) liquidity pools, or private off-chain/anchor-operated quoting mechanisms rather than public, two-sided order books on the DEX.

> Written in response to backlog entry `#97` / Initiative C — V2 Market Intelligence / Corridor research. Checked against live mainnet state and repository measurement artifacts on **2026-08-25**.

---

## Why this matters

Wayfare prices corridor integrity across trade sizes by inspecting pathfinding data, which aggregates both decentralized exchange order book offers and liquidity pools (LPs). However, the existence of a true **two-sided book** (both bids to buy the fiat token with a settlement asset like USDC/XLM and asks to sell it) is a classic test of market maturity and organic price discovery.

If a fiat token has a two-sided book, users can both enter and exit the asset trustlessly on the DEX. If it is strictly one-sided (asks only, as observed on LINK.IO's NGNC/GHSC/KESC corridors, or completely empty), users are entirely dependent on the anchor's on/off-ramp mechanisms and cannot unwind positions on the open market.

---

## Methodology

Checked live against Horizon (`https://horizon.stellar.org`) on **2026-08-25** using the project's own observation standards:

1. **Order Book Probing** — queried `GET /order_book` for each candidate fiat asset paired against native `XLM` and `USDC` (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`).
2. **Pathfinding Analysis** — inspected `GET /paths/strict-send` and `GET /paths/strict-receive` to determine whether liquidity flows through DEX order books, AMM pools, or direct anchor paths.
3. **Existing Corridor Artifacts** — verified findings against repository records under `docs/` (including `corridor-measurements.md`, `mxn-corridor-research.md`, `php-corridor-research.md`, `research-BRL.md`, and `corridor-research-inr.md`).

---

## Survey of Major Stellar Fiat Tokens

### 1. LINK.IO Set (NGNC, GHSC, KESC)
- **Issuer:** `GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6`
- **Checked:** 2026-08-23 / 2026-08-25
- **Order Book State:** **One-sided (asks only).** For instance, the XLM/NGNC order book test fixture in `checks/testdata/snapshots/xlm-ngnc-orderbook-onesided-20260823T000000Z/responses/001-order-book.json` confirms: `{"bids":[],"asks":[{"price":"100","amount":"10"}]}`.
- **Finding:** There are zero bids. Users can buy NGNC from the issuer/asks, but cannot sell NGNC back to XLM or USDC on the public order book. It is strictly a one-sided corridor.

### 2. Saldo.mx (MXN)
- **Issuer:** `GBUMQHWIQELILQEQ5YEEHUFR6SRLBNRKWHJ3JX7JBRFONG24FWUDG627`
- **Checked:** 2026-08-25 (per `docs/mxn-corridor-research.md`)
- **Order Book State:** **Extremely thin, predominantly one-sided/phantom.** While paths exist via XLM bridging for small sizes, total liquidity depth is exhausted around ~1,054 MXN (~USD 62), and bids on the order book are practically non-existent or stale.
- **Finding:** No robust two-sided market exists.

### 3. Brazilian Real (BRL) Candidates
- **Checked:** 2026-08-25 (per `docs/research-BRL.md`)
- **Order Book State:** **No-Market.** As documented in the BRL research, legitimate Stellar-native BRL issuers do not maintain active trading books; candidate assets are either dormant, unanchored, or spam.

### 4. Indian Rupee (INR) & Philippine Peso (PHP)
- **Checked:** 2026-08-25 (per `docs/corridor-research-inr.md` and `docs/php-corridor-research.md`)
- **Order Book State:** **No-Market / Phantom Paths.** As documented, INR and PHP tokens on Stellar either lack SEP-1 anchor verification entirely or show zero order book bids, relying entirely on broken or non-existent bridges.

--- 

## Summary Finding

1. **How many fiat-pegged assets have any executable market with a two-sided book?** 
   **Effectively zero.** Across the reviewed Stellar fiat assets (NGNC, GHSC, KESC, MXN, BRL, INR, PHP), public decentralized order books are either completely empty on the bid side (asks-only), entirely devoid of liquidity, or non-existent.

2. **Implications for Execution Economics (V2):**
   Stellar's stablecoin and fiat corridors function almost exclusively via anchor-intermediated redemption paths or automated market maker (AMM) liquidity pools rather than open public order book market-making. Consequently, exit liquidity (selling fiat tokens back to crypto on the DEX) is generally unsupported by open-market participants, placing full reliance on the anchor's off-chain redemption mechanisms.

*Source: Live Horizon observations and repository research notes, checked on 2026-08-25.*