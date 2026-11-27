# Corridor research: USDC → ARS (Argentine peso)

**Status:** completed, 2026-11-22. **Finding:** **VERIFIED**, for Anclap's `ARS`. Its issuer is verified from its own `stellar.toml`, and Horizon prices a stable direct market up to ~1,000 USDC. The other credible peso token, Latamex's `ARST`, is issuer-verified but its USDC market is degenerate. A third `ARS` that names Anclap is **UNVERIFIABLE**.

> Research issue: [#674](https://github.com/Wayfare-labs/wayfare/issues/674). Method: [`docs/corridor-research-template.md`](corridor-research-template.md).

All on-chain figures below were read live from Horizon (`https://horizon.stellar.org`) on **2026-11-22** between 04:27Z and 04:29Z. The reference mid was read from the project's existing rate providers the same day: **1575.9572 USD/ARS** via exchangerate-api (as of 2026-11-22T00:02:32Z) and **1571.0598 USD/ARS** via currency-api (as of 2026-11-21T00:00:00Z), 0.31% apart.

---

## Why this matters

Argentina has a persistent gap between its official exchange rate and the rates people can actually get (the parallel and financial-market dollar rates). That is the same structure that makes the NGN corridor interesting ([`parallel-rate-research.md`](parallel-rate-research.md)). Peso stablecoins exist on Stellar, and the issue asks whether a machine-priceable USDC → ARS corridor exists before any claim is made about its quality.

The hypothesis tested here is narrower than "is the corridor cheap". It asks whether there is a peso token whose issuer can be verified from its own SEP-1 document and whose USDC market Horizon can price. A secondary question is how that price relates to the official mid that `refrate` publishes.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), the issuer account is the identity: an asset code identifies nothing.

---

## Candidate issuers and identity verification

Horizon asset searches (`/assets?asset_code=…`, 2026-11-22T04:27:33Z) for `ARS`, `ARST`, `ARSW`, `ARSC`, `ARSX`, `ARSD`, `NARS`, `WARS` and `ARSE` returned 42 issuers of `ARS`, 20 of `ARST`, and a handful of the rest. Ranked by authorized holders, three warrant full investigation:

| code | issuer | home domain (on-chain) | holders | issued | pools |
|:---|:---|:---|---:|---:|---:|
| `ARST` | `GCSAZVWXZKWS4XS223M5F54H2B6XPIIXZZGP7KEAIU6YSL5HDRGCI3DG` | `pubnet-sep.latamex.com` | 328,653 | 242,381,062 | 49 |
| `ARS` | `GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS` | `api.anclap.com` | 38,883 | 41,500,261 | 39 |
| `ARS` | `GCANCLAPGTMDMLLWHTRCIBZ6XBJOQVHNFCNOP6W3SOIWGGR4JFSOQLLF` | `anclap.io` | 313 | 4,999,944 | 0 |

Everything else is noise, recorded so it need not be re-checked. That covers synthetic "global reset", commodity or reward themes with tiny holder counts or absurd supplies, e.g. `ARS` from `salt.stellar-commodities.com` (13.4 trillion issued), `currenciesglobalreset.com` and `federalreserve.us.com`, `ARST` from `StellarHQ.io` and `xlmcoin.top`, `ARSD` from `afreum.com`, `ARSX` from `api.chronospay.io`, and `WARS` from `wars.getxlm.xyz`. None has a market worth probing.

### Candidate 1: `ARS`, Anclap (Grupo Anchor S.A.)

- **Issuer account:** `GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS`
- **Declared home domain:** `api.anclap.com`
- **Account auth flags:** none set (`auth_required`, `auth_revocable`, `auth_immutable`, `auth_clawback_enabled` all false). Thresholds 2/2/2, 4 signers.
- **Authorized holders & amount outstanding:** 38,883 holders; 41,500,261.0047 ARS.

**SEP-1 check.** `https://api.anclap.com/.well-known/stellar.toml` returned HTTP 200 with a valid certificate (curl `ssl_verify_result=0`), 1,773 bytes. It names the account in `ACCOUNTS` and declares:

```toml
NETWORK_PASSPHRASE = "Public Global Stellar Network ; September 2015"

[[CURRENCIES]]
code="ARS"
issuer="GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS"
name="Peso Digital"
anchor_asset_type="fiat"
anchor_asset="ARS"
is_asset_anchored=true
status="live"
redemption_instructions="Enhanced KYC verification through anclap is required to redeem this asset."

[DOCUMENTATION]
ORG_NAME = "Grupo Anchor S.A."
ORG_DBA = "Anclap"
ORG_URL = "https://anclap.com"
```

`https://anclap.com/.well-known/stellar.toml` (the `ORG_URL` domain) serves the same `ACCOUNTS` list and the same `ARS` entry. Identity is **verified**: the on-chain `home_domain` serves a document that declares this exact issuer for this code, live, pegged to ARS.

### Candidate 2: `ARST`, Latamex (Relay Tech Services Ltd)

- **Issuer account:** `GCSAZVWXZKWS4XS223M5F54H2B6XPIIXZZGP7KEAIU6YSL5HDRGCI3DG`
- **Declared home domain:** `pubnet-sep.latamex.com`
- **Account auth flags:** none set. Thresholds 2/4/4, 6 signers.
- **Authorized holders & amount outstanding:** 328,653 holders; 242,381,062.3251 ARST.

**SEP-1 check.** `https://pubnet-sep.latamex.com/.well-known/stellar.toml` returned HTTP 200 with a valid certificate, 2,472 bytes. It is served with `content-type: application/x-www-form-urlencoded`, which is cosmetic and doesn't affect parsing. It names the account in `ACCOUNTS` and declares:

```toml
[DOCUMENTATION]
ORG_NAME="Relay Tech Services Ltd"
ORG_URL="https://anchors.relay.vision"

[[CURRENCIES]]
code="ARST"
issuer="GCSAZVWXZKWS4XS223M5F54H2B6XPIIXZZGP7KEAIU6YSL5HDRGCI3DG"
anchor_asset_type="fiat"
anchor_asset="ARS"
is_anchored_asset=true
status="live"
attestation_of_reserve="https://anchors.relay.vision/audits/ARST-Audit-No-1.pdf"
```

Identity is **verified**. One defect in the document is noted for the record: the entry spells the SEP-1 key `is_anchored_asset` where the spec's key is `is_asset_anchored`. Its own `USDC` entry in the same file spells it correctly. This doesn't change identity, because `issuer`, `anchor_asset_type`, `anchor_asset` and `status` are all present. It is the sister token of `BRLT`, already noted in [`research-BRL.md`](research-BRL.md).

### Candidate 3: `ARS`, `GCANCLAP…` (anclap.io)

- **Issuer account:** `GCANCLAPGTMDMLLWHTRCIBZ6XBJOQVHNFCNOP6W3SOIWGGR4JFSOQLLF`, a vanity address spelling "ANCLAP"
- **Declared home domain:** `anclap.io`; thresholds 0/0/0, one signer
- **Holders / pools:** 313 / 0

**SEP-1 check.** `anclap.io` does **not resolve** (`curl: (6) Could not resolve host`, 2026-11-22T04:27:43Z). Anclap's real documents (`api.anclap.com`, `anclap.com`) do not list this account in `ACCOUNTS`. **UNVERIFIABLE**: an account whose name imitates an issuer is exactly what "asset code alone never identifies an asset" guards against. Not probed further; it has no pools.

---

## Horizon path data (USDC → ARS / ARST)

USDC issuer used as the source: Circle's mainnet account `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Both ladders were run through the project's own `route.Engine` (the same strict-send pathfinding, scoring and integrity classification `cmd/ladder` uses). A throwaway harness was needed because neither token is registered, and `cmd/ladder -to` only accepts registered destinations. The ladder is `route.DefaultSizes`. "Loss" is the engine's figure. A route that pays **more** than the official mid reports `0.00%`, so for this corridor the effective-rate column is the one to read.

### `ARS` (Anclap): DIRECT, stable to ~1,000 USDC

Run 2026-11-22T04:28:13Z, scored against exchangerate-api 1575.9572:

| Send (USDC) | Receive (ARS) | Effective rate (ARS/USDC) | vs official mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | 178.07 | 1780.72 | +12.99% | GOOD (0.00% loss) | USDC → XLM → ARS |
| 1 | 1767.60 | 1767.60 | +12.16% | GOOD (0.00%) | USDC → XLM → ARS |
| 5 | 8732.21 | 1746.44 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 10 | 17464.31 | 1746.43 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 25 | 43660.60 | 1746.42 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 50 | 87321.10 | 1746.42 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 100 | 174642.09 | 1746.42 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 250 | 436605.07 | 1746.42 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 500 | 873210.03 | 1746.42 | +10.82% | GOOD (0.00%) | USDC → ARS |
| 1000 | 1745886.27 | 1745.89 | +10.78% | GOOD (0.00%) | USDC → ARS |
| 2500 | 2246395.87 | 898.56 | −42.98% | UNUSABLE (42.98%) | USDC → ARS |
| 5000 | 2256812.53 | 451.36 | −71.36% | UNUSABLE (71.36%) | USDC → ARS |

Against currency-api (1571.0598, run 04:28:15Z) the receive amounts are byte-identical. The losses at 2500 and 5000 are 42.81% and 71.27%. Integrity is **DIRECT** at every size.

- **Reference mid:** 1575.9572 USD/ARS via exchangerate-api, as of 2026-11-22T00:02:32Z (currency-api 1571.0598, as of 2026-11-21).
- **Structural floor / spread:** none in the usual sense. From 5 to 1,000 USDC the corridor pays a flat **~1,746 ARS per USDC, 10.8% above the official mid**. A rate above an official benchmark, flat across two orders of magnitude, is the on-chain trace of Argentina's official/parallel gap: the book is priced near the rate people actually transact at, not the official one. This is an inference from the shape. Wayfare has no parallel-rate reference for ARS, so the size of the gap is **not** measured here against an independent parallel source.
- **Direct book:** `order_book` selling `ARS` / buying `USDC` (04:28:27Z): best ask **0.0005722 USDC/ARS (1,747.64 ARS per USDC)**, best bid 0.0005609 (1,782.85). Asks total 2,294,835 ARS across 9 levels, but all except ~54k sit at 0.0005722–0.0005733 before the next level at 0.24. That is about **1,280 USDC** of usable depth, which is why the ladder exhausts between 1,000 and 2,500.
- **Pools:** 39 pools hold 521,326 ARS in total; the largest are ARS/XLM (215,906 ARS) and ARS/ZARZ (199,627 ARS).
- **Depth:** the book clears ~1,000 USDC at the flat rate, and the rate collapses past it.

### `ARST` (Latamex): DIRECT, but a degenerate market

Run 2026-11-22T04:28:09Z, scored against exchangerate-api 1575.9572:

| Send (USDC) | Receive (ARST) | Effective rate (ARST/USDC) | vs official mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | 290.77 | 2907.74 | +84.51% | GOOD (0.00% loss) | USDC → XLM → ARST |
| 1 | 2880.00 | 2880.00 | +82.75% | GOOD (0.00%) | USDC → XLM → ARST |
| 5 | 13814.11 | 2762.82 | +75.31% | GOOD (0.00%) | USDC → XLM → ARST |
| 10 | 26291.12 | 2629.11 | +66.83% | GOOD (0.00%) | USDC → XLM → ARST |
| 25 | 57394.76 | 2295.79 | +45.68% | GOOD (0.00%) | USDC → XLM → ARST |
| 50 | 94765.44 | 1895.31 | +20.26% | GOOD (0.00%) | USDC → XLM → ARST |
| 100 | 142545.38 | 1425.45 | −9.55% | POOR (9.55%) | USDC → XLM → ARST |
| 250 | 197793.56 | 791.17 | −49.80% | UNUSABLE (49.80%) | USDC → XLM → ARST |
| 500 | 228878.12 | 457.76 | −70.95% | UNUSABLE (70.95%) | USDC → XLM → ARST |
| 1000 | 349177.25 | 349.18 | −77.84% | UNUSABLE (77.84%) | USDC → ARS → ARST |
| 2500 | 449279.17 | 179.71 | −88.60% | UNUSABLE (88.60%) | USDC → ARS → ARST |
| 5000 | 451362.51 | 90.27 | −94.27% | UNUSABLE (94.27%) | USDC → ARS → ARST |

Against currency-api (04:28:11Z) the figures differ only in the last digits (e.g. 100 USDC: 9.27% loss). Integrity is **DIRECT** at every size. At 1,000 USDC and above the best route runs **through Anclap's `ARS`**, which is unregistered and so counts as an independent hop under the documented false-negative in `asset/known.go`.

- **Shape:** the rate falls monotonically from 2,908 at dust **through** the official mid near 75 USDC, and keeps falling. No rate is stable across any two adjacent rungs. This is a pool emptying, not a market with a price.
- **Direct book:** `order_book` selling `ARST` / buying `USDC` (04:28:27Z): best ask **0.0020000 USDC/ARST (500 ARST per USDC)**, best bid 0.0002580 (3,875.97). That is a **7.75× inside spread**, with only 11.46 USDC on the bid side. The direct book is not a market, and Horizon routes around it through XLM.
- **Pools:** 49 pools hold 735,440 ARST in total. The largest, ARST/XLM, holds 271,625 ARST against 733.76 XLM; the next, ARST/ZARZ, holds 262,547 ARST.
- **Holder count vs market:** 328,653 trustlines and 242M ARST issued, but under ~100 USDC of price-stable liquidity against USDC. Holder count is not depth.

---

## SEP-38 (Quote Server) check

Neither document declares `ANCHOR_QUOTE_SERVER`. Anclap publishes SEP-6 (`TRANSFER_SERVER`), SEP-24 (`TRANSFER_SERVER_SEP0024`), SEP-10 (`WEB_AUTH_ENDPOINT`) and SEP-12 (`KYC_SERVER`); `DIRECT_PAYMENT_SERVER` (SEP-31) is commented out. Latamex publishes SEP-6/SEP-24/SEP-10 at `transfer-server.zetl.network`. So neither anchor's off-ramp leg is machine-priceable. Per the project's invariant, it is omitted, not estimated. The on-chain leg is all Wayfare can measure.

---

## Finding & Conclusion

**VERIFIED: Anclap `ARS` (`GCYE7C77…DARS`).** The issuer is verified from its own `stellar.toml` at its on-chain `home_domain` (status `live`, `anchor_asset="ARS"`). Horizon prices a DIRECT USDC → ARS market at a flat ~1,746 ARS/USDC from 5 to 1,000 USDC, and the market exhausts between 1,000 and 2,500 USDC. Both reference providers agree within 0.31%.

The corridor's headline is unusual: **it pays ~10.8% more pesos than the official rate**, so the engine scores it `GOOD` with `0.00%` loss at every size up to 1,000. That zero is an artifact of benchmarking a dual-rate currency against its official mid, and it would be published as such if the corridor were registered unchanged. It is the ARS analogue of the open NGN parallel-rate question.

**Issuer-verified but degenerate: Latamex `ARST` (`GCSAZVWX…I3DG`).** The identity is sound, but there is no price-stable USDC market. The rate falls 97% across the ladder and the direct book has a 7.75× spread. Above 1,000 USDC its best route depends on Anclap's `ARS`. It is not fit to register as an ARS corridor on its own evidence.

**UNVERIFIABLE: `GCANCLAP…QLLF`.** Its home domain does not resolve, and the real Anclap documents do not list it.

Every figure above carries its source and timestamp. Nothing is rounded toward a better result.

---

## Recommendation

1. **Register Anclap `ARS` as a corridor (follow-up issue)** in `asset/known.go`: issuer `GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS`, peg `ARS`, status `live`, source `https://api.anclap.com/.well-known/stellar.toml`, home domain `api.anclap.com`, verified 2026-11-22. Record a `-record` snapshot once registered. The follow-up should decide, before the corridor is published, how to present a corridor that beats its official benchmark. Either a parallel ARS reference is investigated (as [`parallel-rate-research.md`](parallel-rate-research.md) did for NGN), or the finding text states the premise over the official mid instead of showing `0.00%` loss as though it were a costless corridor.
2. **Do not register `ARST`.** Re-evaluate if its USDC or XLM liquidity becomes price-stable across at least two adjacent ladder rungs near the reference mid.
3. **Do not register `GCANCLAP…`.** Re-evaluate only if a resolvable home domain publishes a SEP-1 document declaring it.

---

## How to reproduce

```bash
# Candidates
curl -s "https://horizon.stellar.org/assets?asset_code=ARS&limit=200"
curl -s "https://horizon.stellar.org/assets?asset_code=ARST&limit=200"

# Issuer accounts (home_domain, flags)
curl -s https://horizon.stellar.org/accounts/GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS
curl -s https://horizon.stellar.org/accounts/GCSAZVWXZKWS4XS223M5F54H2B6XPIIXZZGP7KEAIU6YSL5HDRGCI3DG
curl -s https://horizon.stellar.org/accounts/GCANCLAPGTMDMLLWHTRCIBZ6XBJOQVHNFCNOP6W3SOIWGGR4JFSOQLLF

# SEP-1 documents
curl -s https://api.anclap.com/.well-known/stellar.toml
curl -s https://anclap.com/.well-known/stellar.toml
curl -s https://pubnet-sep.latamex.com/.well-known/stellar.toml
curl -s https://anclap.io/.well-known/stellar.toml    # does not resolve

# Path data (repeat over 0.1 … 5000)
curl -sG https://horizon.stellar.org/paths/strict-send \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=100 \
  --data-urlencode "destination_assets=ARS:GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS"

# Direct books (asks are priced in USDC per token)
curl -sG https://horizon.stellar.org/order_book \
  --data-urlencode selling_asset_type=credit_alphanum4 --data-urlencode selling_asset_code=ARS \
  --data-urlencode selling_asset_issuer=GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS \
  --data-urlencode buying_asset_type=credit_alphanum4 --data-urlencode buying_asset_code=USDC \
  --data-urlencode buying_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode limit=200

# Pools
curl -s "https://horizon.stellar.org/liquidity_pools?reserves=ARS:GCYE7C77EB5AWAA25R5XMWNI2EDOKTTFTTPZKM2SR5DI4B4WFD52DARS&limit=200"
```

The full ladders were produced with a few lines calling `route.Engine.Ladder` with `asset.Stellar(code, issuer)`, `route.DefaultSizes` and the reference pair `USD/ARS`. After registration, `go run ./cmd/ladder -to ARS` reproduces them directly.

---

## Related

- Issue [#674](https://github.com/Wayfare-labs/wayfare/issues/674): this research task
- [`docs/corridor-research-template.md`](corridor-research-template.md): the method
- [`docs/research-BRL.md`](research-BRL.md): BRLT, ARST's sister token
- [`docs/parallel-rate-research.md`](parallel-rate-research.md): the official/parallel gap question for NGN
- [`docs/adding-a-corridor.md`](adding-a-corridor.md): the verification standard
