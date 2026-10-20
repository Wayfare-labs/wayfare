# Research: is there an independent GHS token on Stellar?

**Status: completed. 2026-10-20. Finding: UNUSABLE (no independent GHS
corridor).** Independent Ghanaian-cedi tokens do exist, but none is both a
verified cedi anchor and measurable: the only independent `GHSC` has no market
and an unreachable issuer document, while the only independent tokens with
path data (Afreum's `SGHS` and `AGHS`) are derivative and priced far below the
reference mid.

This answers issue [#757](https://github.com/Wayfare-labs/wayfare/issues/757):
the current GHSC is `pending` and every GHSC path runs through NGNC
(`DERIVATIVE`). The open question was whether any GHS corridor escapes that
dependency. **On the evidence below, no usable one does.**

---

## Why this matters

Ghana is a large remittance market and the cedi is the reference currency for
the GHSC corridor. Today the only registered GHS corridor is GHSC from the
same issuer as NGNC (`GASBV…`), and every path to it routes through NGNC. That
means GHSC's cost and failure modes are bounded by NGNC's: it cannot be an
independent check on the naira corridor, and if the shared issuer fails both
corridors fail together.

A GHS token from a different issuer could remove that shared dependency. This
research looks for one, verifies its issuer from the issuer's own published
`stellar.toml`, and probes the market — because a token existing on the ledger
is not a corridor, and a different issuer is not automatically an independent
market.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is verified
only when the issuer account is read live from the issuer's own published
`stellar.toml`; a negative finding is a valid result.

---

## Data sources

- **Ledger:** Horizon mainnet `https://horizon.stellar.org`, read live on
  **2026-10-20** (≈08:45 UTC).
- **Asset discovery:** Horizon `/assets?asset_code=GHS` (15 issuers) and
  `/assets?asset_code=GHSC` (2 issuers); StellarExpert search for `GHS`
  returned `AGHS` and `SGHS` as well.
- **Reference mid USD→GHS:** **11.695062** via exchangerate-api
  (`open.er-api.com`, updated 2026-10-20 00:02 UTC) and **11.68024189** via
  currency-api (as of 2026-10-19). Loss figures below use **11.695062**.
- **Path data:** raw Horizon `/paths/strict-send` from Circle's USDC
  (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`); the same
  endpoint `route.Engine` uses. Nothing is rounded toward a better result.

---

## Independent `GHSC` candidate — Leaf Global

A second `GHSC` exists from an issuer unrelated to the NGNC/GHSC issuer:

- **Issuer account:** `GALC6W3IIYMJ3Q5BL7TECTEW5JAC7QIB4FB2QWCAV2IKFBE4MYWEITEQ`
- **Home domain:** `leafglobal.tech` (read from Horizon)
- **Flags:** `auth_required`, `auth_revocable`
- **Footprint (2026-10-20):** 1,758 authorized holders, 717.4608 GHSC
  authorized, 0 liquidity pools, 0 recorded trades (StellarExpert)
- **Created:** 2022-04-06 (StellarExpert)

### SEP-1 check — **fails**

`https://leafglobal.tech/.well-known/stellar.toml` could not be fetched on
2026-10-20: the hostname **does not resolve** (`curl: (6) Could not resolve
host`). No SEP-1 document is available, so the issuer's identity and its
declared peg cannot be verified from its own published source. The on-chain
code `GHSC` and the 1,758 trustlines are not a substitute: an asset code
identifies nothing, and anyone can issue a token called GHSC.

### Market probe — **NO-MARKET**

Strict-send from 0.1 to 5,000 USDC returned **zero paths at every ladder
size**. There are no pools and no trades. This is the same signature as KESC:
not a bad price, but the absence of a price.

**Result: UNVERIFIABLE identity, NO-MARKET.** Not registrable.

---

## Afreum stable GHS (`SGHS`) — independent and measurable, but unusable

Afreum issues a token that explicitly claims a 1:1 cedi peg, from an issuer
unrelated to `GASBV…`:

- **Issuer account:** `GDQYXPQ2KEPH6TTVSZL6O3ZQQ4JB3BPTUE4NSVLZNLIAUJTGR5OWVJCR`
- **Home domain:** `afreum.net`
- **Flags:** `auth_revocable`, `auth_clawback_enabled`
- **Footprint (2026-10-20):** 20 authorized holders, 5.6204514 SGHS authorized,
  1 liquidity pool, 7 recorded trades (StellarExpert)

### SEP-1 check — **partially coherent**

`https://afreum.net/.well-known/stellar.toml` returned HTTP 200 on
2026-10-20 and contains a `[[CURRENCIES]]` entry whose `code` and `issuer`
match the on-chain asset, with `status="live"`:

```toml
[[CURRENCIES]]
code="SGHS"
issuer="GDQYXPQ2KEPH6TTVSZL6O3ZQQ4JB3BPTUE4NSVLZNLIAUJTGR5OWVJCR"
name="AFREUM Stable GHS"
desc="AFREUM Stable GHS (SGHS) is a utility token that maintains a 1:1 exchange rate with the Ghanian Cedi (GHS)."
status="live"
```

But the entry declares **no `anchor_asset_type` and no `anchor_asset`**. The
peg exists only in prose, not in the structured fields the verification
standard requires, and the document declares no `ANCHOR_QUOTE_SERVER`
(no SEP-38). Identity is therefore verifiable; the claimed fiat anchor is not.

### Market probe — **DERIVATIVE and UNUSABLE**

Paths exist at every size, but every one is derivative: `yXLM → AFR` or
`XLM → AFR`, never a direct USDC/SGHS market. Prices are far below the
reference mid at every rung:

| Send (USDC) | Receive (SGHS) | Effective rate (GHS/USDC) | Loss vs mid | Path |
|---:|---:|---:|---:|:---|
| 0.1 | 0.8792698 | 8.792698 | 24.82% | yXLM → AFR |
| 1 | 3.8293232 | 3.829323 | 67.26% | yXLM → AFR |
| 5 | 5.4566844 | 1.091337 | 90.67% | yXLM → AFR |
| 10 | 5.7628148 | 0.576281 | 95.07% | yXLM → AFR |
| 25 | 5.9633453 | 0.238534 | 97.96% | XLM → AFR |
| 50 | 6.0333722 | 0.120667 | 98.97% | XLM → AFR |
| 100 | 6.0691308 | 0.060691 | 99.48% | XLM → AFR |
| 250 | 6.0907902 | 0.024363 | 99.79% | XLM → AFR |
| 500 | 6.0980436 | 0.012196 | 99.90% | XLM → AFR |
| 1000 | 6.1016764 | 0.006102 | 99.95% | XLM → AFR |
| 2500 | 6.1038593 | 0.002442 | 99.98% | XLM → AFR |
| 5000 | 6.1045874 | 0.001221 | 99.99% | XLM → AFR |

The output barely moves after 1 USDC: the pool is effectively exhausted, and
the token trades at a small fraction of the cedi it claims to track. This is
not a conservative benchmark; it is a different price scale.

**Result: independently issued, structurally derivative, UNUSABLE at every
size.** Not registrable.

---

## Afreum GHS (`AGHS`) — a flexible-price token, not a cedi peg

Afreum also issues `AGHS`, whose own SEP-1 entry (read live 2026-10-20, HTTP
200) describes it as a **"flexible price alternative"**, not a 1:1 peg:

- **Issuer account:** `GDNUVPUOMWOF2ML5FA5E4HQDX7EHV3VCJTLLTO563PUMZKMHJUJIJSYI`
- **Home domain:** `afreum.com`; `status="live"`; no `anchor_asset`
- **Footprint (2026-10-20):** 261 authorized holders, 28,183,946.55 AGHS
  authorized, 4 liquidity pools, 54,472 recorded trades (StellarExpert)

Paths exist but are derivative (`yXLM → AFR`, `XLM`, or `EURC → XLM`) and the
effective rate is 27.9–99.8% below the reference mid:

| Send (USDC) | Effective rate (GHS/USDC) | Loss vs mid |
|---:|---:|---:|
| 0.1 | 8.434929 | 27.88% |
| 1 | 7.867263 | 32.73% |
| 10 | 4.702502 | 59.79% |
| 100 | 3.345199 | 71.40% |
| 1000 | 3.344972 | 71.40% |
| 5000 | 0.020988 | 99.82% |

A flexible-price token is a deliberate product choice, but it is not a cedi
anchor: its price is not required to track GHS, and the measured prices do
not.

**Result: independent, not pegged, UNUSABLE as a GHS corridor.** Not
registrable.

---

## The other `GHS` issuers

Horizon returned 15 issuers of a `GHS` code. Every one was read live on
**2026-10-20**; the table records the issuer's home domain, whether a SEP-1
document was actually served, its authorized footprint, pools, and the
0.1-USDC path result.

| Issuer (short) | Home domain | SEP-1 (2026-10-20) | Holders | Authorized balance | Pools | 0.1 USDC path |
|:---|:---|:---|---:|---:|---:|:---|
| `GADSZS…` | `fednow.us.org` | 200, TOML | 2 | 100,000,000 | 0 | 6.3021633 via XLM |
| `GANDGD…` | `sendwise.org` | unreachable (HTTPS; HTTP 404) | 121 | 2,043,300,714,519.99 | 0 | 2,095,043.48 via XLM |
| `GATAHI…` | `fiat.stellarpredict.com` | 200, HTML not TOML | 2 | 1,110,000,000 | 0 | 28,500.93 via XLM |
| `GBANKI…` | `starforex.co` | DNS does not resolve | 2 | 1,000,000 | 0 | NO-PATH |
| `GBANKU…` | `galtbank.com` | unreachable (HTTPS; HTTP 404) | 1 | 1,000,000 | 0 | NO-PATH |
| `GBBWRC…` | `bankofengland.com.co` | 200, HTML not TOML | 1 | 90,000,000 | 0 | 6.3003155 via EURC → XLM |
| `GBDLC6…` | — | no home domain; `auth_required` | 13 | 2,767,011,611,056.43 | 0 | NO-PATH |
| `GBIPZB…` | `iso20022.io` | 200, TOML | 2 | 80,000,000 | 0 | 510.8363963 via EURC → XLM |
| `GBZQNU…` | `fixedidr.com` | DNS does not resolve | 1 | 0 | 0 | NO-PATH |
| `GCC4YL…` | `flutterwave.com` | HTTP 404 | 14 | 66,704.57 | 0 | NO-PATH |
| `GCKEIE…` | `qprinting-app.com` | 200, TOML (commodities; no GHS entry) | 5 | 100,001,000,000 | 0 | NO-PATH |
| `GCMSHQ…` | `federalreserve.us.com` | 200, TOML | 10 | 999,999,990 | 0 | 567.2423 via XLM |
| `GCSTTN…` | — | no home domain | 1 | 900,000,000,000 | 0 | NO-PATH |
| `GDIB5U…` | — | no home domain; `auth_revocable` | 1 | 900 | 0 | NO-PATH |
| `GDJEBN…` | `federalreserve.co.com` | DNS does not resolve | 4 | 920,000,000,000 | 0 | 5,675,975 via XLM → yXLM |

The pattern is the one seen in the ZAR research: domains impersonating central
banks or standards bodies (`fednow.us.org`, `federalreserve.us.com`,
`federalreserve.co.com`, `iso20022.io`, `bankofengland.com.co`) issue tokens
with supplies many orders of magnitude larger than the cedi in circulation,
and the only "market" is a derivative XLM hop that prices the token anywhere
from about half the reference mid to nearly two million times it. They are not
cedi anchors.

One candidate is worth calling out specifically: **Flutterwave's `GHS`**
(`GCC4YL…`, `flutterwave.com`) is the only issuer in this list with a credible
Ghanaian-payments name, but its `stellar.toml` returns **HTTP 404**, it has no
pools, and there is **NO-PATH** at every size. A known company name without a
published SEP-1 and without a market is not a verified corridor.

The remaining `GHS` paths in the table are all derivative and wildly
mis-scaled; none is a GHS/USDC market in any meaningful sense.

---

## SEP-38 check

No candidate declared an `ANCHOR_QUOTE_SERVER`:

- Leaf Global's document is unreachable.
- Afreum's `afreum.com` and `afreum.net` documents declare no
  `ANCHOR_QUOTE_SERVER`.
- Flutterwave publishes no document at all (HTTP 404).

There is no quoted anchor leg to measure.

---

## Finding & conclusion

**Independent GHS tokens exist; an independent GHS corridor does not.**

1. **Leaf Global's independent `GHSC` is NO-MARKET and unverifiable.** It has
   1,758 holders and no liquidity, and its home domain does not resolve, so it
   cannot clear the identity bar. No path exists at any ladder size.
2. **Afreum's `SGHS` is independently issued and has paths, but is UNUSABLE.**
   The claim is a 1:1 cedi peg, but the SEP-1 entry declares no structured
   `anchor_asset`, there is no direct market, and the derivative path prices
   SGHS 25–99.99% below mid, exhausting after 1 USDC.
3. **Afreum's `AGHS` is explicitly a flexible-price token, not a peg.** Its
   market is also derivative and 28–99.8% below mid.
4. **The shared-issuer GHSC remains the only GHSC with an executable market,
   and every path still routes through NGNC.** Escaping the shared issuer means
   leaving the only liquid GHSC; staying with it means keeping the NGNC
   dependency.

The existing GHSC corridor is not made redundant by any of these candidates,
and none can serve as an independent check on the cedi.

### Recommendation

**Do not register a new GHS corridor from this research.** Keep the existing
shared-issuer GHSC and its `DERIVATIVE` label; it is an honest description of
the only measurable GHS market. Record this as a negative finding.

Re-evaluate if any of the following happens:

1. **Leaf Global restores `leafglobal.tech`** and publishes a SEP-1 document
   that declares `GHSC`, its issuer, and a fiat anchor — then check whether
   its 1,758 holders ever produce path data.
2. **Afreum publishes structured `anchor_asset`/`anchor_asset_type` metadata
   for `SGHS` and adds a direct (non-AFR) market** whose price is within a
   defensible band of the cedi mid. Today the token is 25–99.99% off and
   derivative.
3. **Flutterwave publishes a SEP-1 document for its `GHS`** and a market
   exists. Today there is neither.

Until then, the answer to "does any GHS corridor escape the NGNC dependency?"
is **not in a form the project can measure or recommend.**

---

## How to reproduce

```bash
# All GHS issuers and their on-chain footprints
curl -s "https://horizon.stellar.org/assets?asset_code=GHS&limit=50" | jq
curl -s "https://horizon.stellar.org/assets?asset_code=GHSC&limit=50" | jq

# The independent GHSC issuer (home_domain is leafglobal.tech)
curl -s https://horizon.stellar.org/accounts/GALC6W3IIYMJ3Q5BL7TECTEW5JAC7QIB4FB2QWCAV2IKFBE4MYWEITEQ | jq

# Its document does not resolve (2026-10-20)
curl -sS https://leafglobal.tech/.well-known/stellar.toml

# SGHS and AGHS SEP-1 entries
curl -s https://afreum.net/.well-known/stellar.toml
curl -s https://afreum.com/.well-known/stellar.toml

# Path probe; repeat for each ladder size and candidate
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=100 \
  --data-urlencode "destination_assets=SGHS:GDQYXPQ2KEPH6TTVSZL6O3ZQQ4JB3BPTUE4NSVLZNLIAUJTGR5OWVJCR"
```

---

## Related

- Issue [#757](https://github.com/Wayfare-labs/wayfare/issues/757) — this
  research task
- Issue [#464](https://github.com/Wayfare-labs/wayfare/issues/464) — the
  reference-divergence fields the trend endpoint publishes
- [`docs/corridor-research-template.md`](corridor-research-template.md) — the
  template this document follows
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and the NO-MARKET / integrity states
- [`docs/research-ZAR.md`](research-ZAR.md) — the sister finding on independent
  Rand tokens
- [`docs/corridor-measurements.md`](corridor-measurements.md) — the measured
  GHSC corridor this research compares against
