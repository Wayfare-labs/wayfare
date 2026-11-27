# Research: is there a usable USDC → COP corridor on Stellar?

**Status: completed. 2026-11-23. Finding: UNUSABLE (no verified COP
anchor).** Forty-seven issuer accounts publish a `COP` asset on Stellar
mainnet. None of them is both a verified Colombian-peso anchor and priced
sensibly: the only issuer whose document declares a public-network passphrase
*and* a fiat COP entry prices the token at up to ~609 billion COP per USDC,
and every other priced candidate trades 98.9%–99.8% **below** the reference
mid. The corridor exists as a token code, not as a market.

This answers issue [#679](https://github.com/Wayfare-labs/wayfare/issues/679)
and follows
[`docs/corridor-research-template.md`](corridor-research-template.md).

All on-chain figures below were read live from Horizon
(`https://horizon.stellar.org`) on **2026-11-23**. The reference mid was read
from the project's rate providers the same day.

No snapshot was taken and no corridor was registered: under the acceptance
criteria, a corridor is snapshotted only if it qualifies.

---

## Why this matters

Colombia is one of the largest remittance recipients in Latin America, and
the peso is the destination currency for a large share of US-origin transfers.
A USDC → COP corridor would let the project measure that flow against the
official mid instead of a provider-quoted retail price.

The hypothesis being tested is narrow: **does a `COP`-coded Stellar asset
exist whose issuer publishes a verifiable SEP-1 document declaring a Colombian
peso anchor, and which has a market priced near the reference mid?**

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is
"verified" only when the issuer account is read live from the issuer's own
published `stellar.toml`. An asset code identifies nothing — anyone can issue
a token called `COP`, and on this ledger many have.

---

## Data sources

- **Ledger:** Horizon mainnet `https://horizon.stellar.org`, read live on
  **2026-11-23** (≈11:20–12:25 UTC).
- **Asset discovery:** `GET /assets?asset_code=COP&limit=200&order=desc`,
  which returned **47 issuer accounts**. Code variants (`COPC`, `COPM`,
  `COPT`, `COPX`, `COLP`, `COL`, `COPS`, `COPB`, `COPR`, `COP U`, …) were
  also probed the same day and returned **0 issuers each**; `COP` is the only
  code in use.
- **Issuer documents:** `https://<home_domain>/.well-known/stellar.toml` for
  all 47 issuers, fetched 2026-11-23.
- **Path data:** raw Horizon `/paths/strict-send` from Circle's USDC
  (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`) — the same
  endpoint `route.Engine` uses. The sweep used `destination_assets=COP:<issuer>`
  because Horizon rejects `destination_asset_type` together with
  `destination_assets` (HTTP 400: *"The request requires either a list of
  destination assets or a destination account. Both fields cannot be
  present."*).
- **Reference mid USD→COP:** **3437.41712** via exchangerate-api
  (`open.er-api.com`, updated `Mon, 23 Nov 2026 00:02:32 +0000`) and
  **3508.0906075** via currency-api (`latest.currency-api.pages.dev`, dated
  2026-11-23).

### Reference-mid cross-check — DISAGREE

The two providers differ by **2.06%**:

```
|3508.0906075 − 3437.41712| / 3508.0906075 = 2.0146%
```

That exceeds `DivergenceAgree = 2` (percent) and sits well below
`DivergenceMalfunction = 10` in [`refrate/cross.go`](../refrate/cross.go), so
the cross-check state is **`AgreementDisagree`**. Under the conservative-mid
rule in that file, the scored mid is the **higher** of the two,
**3508.0906075 COP/USDC**. Every loss figure below uses that value, so the
reported losses are the *smallest* defensible ones.

---

## Candidate issuers and identity verification

Horizon returned 47 issuers of `COP`. All 47 were read live: account flags,
authorized footprint, liquidity pools, SEP-1 document, and a 1-USDC
strict-send path probe. The table records the issuer (short), the declared
home domain, the SEP-1 result, authorized holders, authorized balance, pool
count, and the best 1-USDC path output.

| Issuer | Home domain | SEP-1 (2026-11-23) | Holders | Authorized | Pools | 1 USDC → COP |
|:---|:---|:---|---:|---:|---:|:---|
| `GCRFP7…` | `trump347.com` | unreachable (DNS) | 785 | 900.8B | 0 | 10.9344 (EURC→XLM) |
| `GAHFKA…` | `luxbank.co` | 200, public passphrase | 755 | 8.90T | 7 | 66,178,978,193.0649 (yUSDC→yXLM) |
| `GCANCL…` | `anclap.io` | unreachable (DNS) | 371 | 5.0M | 0 | 38.8777 (EURC→XLM) |
| `GDFFB7…` | `thegfb.org` | unreachable (DNS) | 97 | 77.3M | 0 | NO-PATH |
| `GBIO5N…` | — | no home domain | 83 | 0 | 0 | NO-PATH |
| `GC6HPU…` | `api2.anclap.com` | unreachable (timeout) | 45 | 4,129.87 | 0 | NO-PATH |
| `GDV23D…` | `anclap.io` | unreachable (DNS) | 22 | 1.0M | 0 | 8.4545 (EURC→XLM) |
| `GC6C2O…` | — | no home domain | 20 | 46,233.00 | 0 | NO-PATH |
| `GDA4OK…` | `nasdaqstellar.pages.dev` | 200, public passphrase | 11 | 49.9B | 1 | 77,773,364.0000 (EURC→XLM) |
| `GCKEIE…` | `qprinting-app.com` | unreachable (HTTP 404) | 11 | 1.00T | 0 | NO-PATH |
| `GBANKI…` | `nasdaq.company` | 200, no passphrase | 11 | 1,000.0M | 0 | 7,006.6094 (EURC→XLM) |
| `GASPXZ…` | `www.agapanto.com.co` | 200, no passphrase | 10 | 200.0M | 0 | NO-PATH |
| `GCMSHQ…` | `federalreserve.us.com` | 200, no passphrase | 9 | 1,000.0M | 0 | 7,777.3364 (EURC→XLM) |
| `GDMJE6…` | `bitcoins.uk` | unreachable (HTTP 403) | 8 | 500,000.00 | 0 | NO-PATH |
| `GATU3C…` | `kamalaagenda.com` | unreachable (DNS) | 6 | 900.0B | 0 | NO-PATH |
| `GAEDLN…` | `anclax.com` | unreachable (DNS) | 6 | 0.80 | 0 | NO-PATH |
| `GBQAQ2…` | `spglobal.com.co` | 200, no passphrase | 4 | 69.0M | 0 | 4,601.9742 (EURC→XLM) |
| `GBLLDE…` | `blackrock.us.org` | unreachable (DNS) | 4 | 920.0B | 0 | 77,758,284.0000 (yUSDC→yXLM) |
| `GAYNVS…` | — | no home domain | 4 | 37.8M | 0 | NO-PATH |
| `GB3RYS…` | `nasdaq.network` | 200, no passphrase | 3 | 40.0M | 0 | 8,641.4530 (EURC→XLM) |
| `GATBPO…` | `sp500.xmoney.trading` | 200, no passphrase | 3 | 840.0B | 0 | 58,856,574.0880 (EURC→XLM) |
| `GAT5P7…` | `stocks.li` | unreachable (conn closed) | 3 | 50.0B | 0 | 77,773,077.0000 (EURC→XLM) |
| `GAOGGT…` | `cop-stellar.org` | unreachable (DNS) | 3 | 55.0M | 0 | 116.0792 (EURC→XLM) |
| `GDLV7Z…` | `anclap.global-stellar.org` | unreachable (DNS) | 2 | 10.0B | 0 | 26,818.3024 (EURC→XLM) |
| `GC4OCR…` | `ledgerscan.dtcc.markets` | 200, no passphrase | 2 | 740.0B | 0 | 58,856,574.0880 (EURC→XLM) |
| `GBDFKE…` | `stellarvatican.com` | unreachable (DNS) | 2 | 900.0B | 0 | NO-PATH |
| `GBBWRC…` | `bankofengland.com.co` | 200, no passphrase | 2 | 90.0M | 0 | 86.4145 (EURC→XLM) |
| `GBANKI…` | `starforex.co` | unreachable (DNS) | 2 | 1.0M | 0 | NO-PATH |
| `GATAHI…` | `fiat.stellarpredict.com` | 200, no passphrase | 2 | 1.1B | 0 | 390,819.4824 (EURC→XLM) |
| `GASJFS…` | `nasdaq.co.com` | 200, no passphrase | 2 | 1.0B | 0 | 299,127.2192 (EURC→XLM) |
| `GA7UGT…` | `stocks.vanguard.co.com` | 200, no passphrase | 2 | 288,888.00 | 0 | 86.4145 (EURC→XLM) |
| `GCSTTN…` | — | no home domain | 1 | 900.0B | 0 | NO-PATH |
| `GCOPSD…` | `kzlabs.co` | unreachable (HTTP 404) | 1 | 2.91 | 0 | NO-PATH |
| `GCERAL…` | `luxbank.co` | 200, public passphrase | 1 | 0 | 0 | NO-PATH |
| `GCBV2T…` | `canton.dtcc.markets` | 200, no passphrase | 1 | 740.0B | 0 | 58,856,574.0880 (EURC→XLM) |
| `GBXKZ5…` | `stocks.blackrock.co.com` | 200, no passphrase | 1 | 40.0M | 0 | 8,641.4530 (EURC→XLM) |
| `GBIPZB…` | `iso20022.io` | 200, no passphrase | 1 | 80.0M | 0 | 7,006.5835 (EURC→XLM) |
| `GBERT4…` | `xlmmoney.pages.dev` | 200, public passphrase | 1 | 900.0B | 0 | 77,773,077.0000 (EURC→XLM) |
| `GBANO7…` | `nyse.foundation` | unreachable (DNS) | 1 | 1.0B | 0 | 7,006.5835 (EURC→XLM) |
| `GBANKU…` | `galtbank.com` | unreachable (conn refused) | 1 | 1.0M | 0 | NO-PATH |
| `GB3DGZ…` | `allamericans.io` | unreachable (DNS) | 1 | 100.0B | 0 | NO-PATH |
| `GB33FH…` | `kamalaagenda.com` | unreachable (DNS) | 1 | 900.0B | 0 | NO-PATH |
| `GAXRXI…` | `quantumbc-rewards.com` | 200, public passphrase | 1 | 100.0M | 0 | NO-PATH |
| `GAVXEB…` | `symbol.lol` | unreachable (HTTP 404) | 1 | 100.0B | 0 | NO-PATH |
| `GAOPCF…` | — | no home domain | 1 | 0 | 0 | NO-PATH |
| `GADSZS…` | `fednow.us.org` | 200, no passphrase | 1 | 100.0M | 0 | 86.3947 (EURC→XLM) |
| `GAA4JD…` | `lobstr.co` | 200, public passphrase | 1 | 0 | 0 | NO-PATH |

The 1-USDC column is from the 47-issuer sweep; the ladder tables below are a
separate run minutes later, so exact outputs differ slightly between probes.
"public passphrase" means the document contains a `NETWORK_PASSPHRASE` equal
to the mainnet passphrase *and* a `[[CURRENCIES]]` entry for `COP`.

### SEP-1 check — the decisive filter

Only **four** documents both serve over HTTPS and declare the mainnet
`NETWORK_PASSPHRASE`: `luxbank.co`, `nasdaqstellar.pages.dev`,
`xlmmoney.pages.dev`, and `quantumbc-rewards.com`. A fifth,
`lobstr.co`, serves a valid mainnet document but has **no `COP` entry** at
all — it is not a candidate. TLS host matching was confirmed for
`luxbank.co`, `www.agapanto.com.co`, and `anclap.com`.

Of the four, none declares a credible fiat COP anchor:

- **`luxbank.co`** (`GAHFKA…`) — HTTP 200, mainnet passphrase, `COP` entry
  with `status="live"`, `anchor_asset_type="fiat"`, but **no `anchor_asset`**
  and no `is_asset_anchored`. The entry's name is *"Currency Operation
  Protocols"*, its description *"Experimental synthetic proprietary Crypto
  currency."*, and its organization *"LUX ECONOMETRIC THINK-TANK"*. Its
  `TRANSFER_SERVER` points at `api.anchorusd.com`, an unrelated anchor. A
  fiat *type* with no declared fiat *asset*, described as an experimental
  synthetic token, is not a peso anchor.
- **`nasdaqstellar.pages.dev`** (`GDA4OK…`) — HTTP 200, mainnet passphrase,
  `COP` entry with `status="live"`, but `anchor_asset="NASDAQ Stock"` and
  `is_asset_anchored="true"`. The document anchors the peso code to a US
  equity index.
- **`xlmmoney.pages.dev`** (`GBERT4…`) — HTTP 200, mainnet passphrase, `COP`
  entry with **no `status`** and no `anchor_asset`.
- **`quantumbc-rewards.com`** (`GAXRXI…`) — HTTP 200, mainnet passphrase,
  `COP` entry with **no `status`** and no `anchor_asset`.

The remaining 200-status documents (`nasdaq.company`, `federalreserve.us.com`,
`spglobal.com.co`, `bankofengland.com.co`, `iso20022.io`, `fednow.us.org`,
`dtcc.markets`, `stellarpredict.com`, `vanguard.co.com`, `nasdaq.co.com`,
`nasdaq.network`, `sp500.xmoney.trading`, `www.agapanto.com.co`, …) **omit
`NETWORK_PASSPHRASE` entirely**, so they are not valid mainnet SEP-1
documents by the project's standard. Several of them do declare
`anchor_asset="COP"` with `anchor_asset_type="fiat"` — `federalreserve.us.com`,
`bankofengland.com.co`, `iso20022.io`, `fednow.us.org` — but they pair a
well-formed fiat claim with domains impersonating a central bank, a standards
body, or a market-data vendor. On this ledger that pattern is the signature of
a fabricated asset, not an anchor: the same domains issue `GHS`, `ZAR`, and
equity tokens in the sister studies.

**`anclap.com`** is the only issuer in the set with a plausible Colombian
payments identity. Its TOML resolves over HTTPS with a mainnet passphrase, but
declares only **PEN and ARS** currencies — **no `COP` entry** — even though
371 authorized `COP` holders and 5.0M COP sit on its issuer account.

Identity verification therefore **fails for every candidate**: no issuer
publishes a valid public-network SEP-1 document declaring a real COP fiat
anchor.

---

## Horizon path data (USDC → COP)

USDC issuer used as the source: Circle's mainnet account
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Every state-verified candidate was probed at 0.1 / 1 / 10 / 100 / 1000 / 5000
USDC. There are **no direct USDC/COP order-book markets** for any candidate:
all executable paths are **DERIVATIVE**, routed through XLM, yXLM, yUSDC, or
EURC. Reference mid: **3508.0906075 COP/USDC** (2026-11-23, conservative
cross-rule value).

### `luxbank.co` — paths exist, on a different price scale

| Send (USDC) | Receive (COP) | Effective rate (COP/USDC) | Loss vs mid | Best path |
|---:|---:|---:|---:|:---|
| 0.1 | 60,970,755,190.3498 | 609,707,551,903.4983 | −17,380,039,930.89% | yUSDC → yXLM |
| 1 | 66,176,614,854.0037 | 66,176,614,854.0037 | −1,886,399,718.54% | XLM → yXLM |
| 10 | 166,246,917,177.7860 | 16,624,691,717.7786 | −473,895,633.54% | XLM |
| 100 | 66,804,227,411.8235 | 668,042,274.1182 | −19,042,802.50% | XLM → yXLM |
| 1000 | 66,809,990,300.9522 | 66,809,990.3010 | −1,904,354.52% | XLM → yXLM |
| 5000 | 66,810,502,606.6832 | 13,362,100.5213 | −380,793.83% | XLM → yXLM |

Negative "loss" here means over-delivery. The best rung delivers **~3,809×**
the reference mid at 5000 USDC and **~609 billion×** at 0.1 USDC. This is not
a spread; it is a token with no relationship to the peso. The non-monotonic
output (66.2B at 1 USDC, 166.2B at 10, 66.8B at 100) reflects path selection
across several mispriced pools, not depth.

### `anclap` main issuer — paths exist, ~98.9% below mid

| Send (USDC) | Receive (COP) | Effective rate (COP/USDC) | Loss vs mid | Best path |
|---:|---:|---:|---:|:---|
| 0.1 | 3.8725 | 38.7252 | 98.90% | GZC → XLM |
| 1 | 38.7203 | 38.7203 | 98.90% | yUSDC → XLM |
| 10 | 387.0199 | 38.7020 | 98.90% | yXLM → XLM |
| 100 | 3,837.2046 | 38.3720 | 98.91% | XLM |
| 1000 | 38,332.4121 | 38.3324 | 98.91% | EURC → XLM |
| 5000 | 191,615.0997 | 38.3230 | 98.91% | XLM |

The effective rate is flat across four orders of magnitude — a stable price
at roughly **1.1% of the peso's value**, i.e. the token is priced as if the
peso were worth one cent. A flat, deeply wrong price is worse than a
thin-but-correct one: it looks measurable and is not.

### `anclap` alt issuer (`GDV23D…`) — ~99.76% below mid

| Send (USDC) | Receive (COP) | Effective rate (COP/USDC) | Loss vs mid | Best path |
|---:|---:|---:|---:|:---|
| 0.1 | 0.8421 | 8.4209 | 99.76% | yUSDC → XLM |
| 1 | 8.4183 | 8.4183 | 99.76% | yUSDC → XLM |
| 10 | 84.1430 | 8.4143 | 99.76% | yXLM → XLM |
| 100 | 841.3127 | 8.4131 | 99.76% | XLM |
| 1000 | 8,412.6799 | 8.4127 | 99.76% | EURC → XLM |
| 5000 | 42,056.5344 | 8.4113 | 99.76% | XLM |

### `trump347.com` — ~99.69% below mid, then NO-PATH

| Send (USDC) | Receive (COP) | Effective rate (COP/USDC) | Loss vs mid | Best path |
|---:|---:|---:|---:|:---|
| 0.1 | 1.0894 | 10.8935 | 99.69% | yUSDC → XLM |
| 1 | 10.8901 | 10.8901 | 99.69% | yUSDC → XLM |
| 10 | 108.8499 | 10.8850 | 99.69% | yXLM → XLM |
| 100 | 1,088.3289 | 10.8833 | 99.69% | EURC → XLM |
| 1000 | 10,882.8304 | 10.8828 | 99.69% | EURC → XLM |
| 5000 | NO-PATH | — | — | — |

### NO-PATH candidates

`anclap` api2 (`GC6HPU…`), `thegfb.org` (`GDFFB7…`), `agapanto.com.co`
(`GASPXZ…`), and `starforex.co` (`GBANKIP…`) returned **zero paths at every
ladder size**. For these there is no price to grade: the absence of a price,
not a bad one.

- **`anclap` api2** has 45 authorized holders and 4,129.87 COP outstanding.
  Its home domain `api2.anclap.com` times out, so identity is unverifiable.
  Its own order book holds **1 bid and 1 ask** — technically a market, too
  thin to accept a 0.1-USDC strict-send.
- **`agapanto.com.co`** serves a 200 TOML with **no `NETWORK_PASSPHRASE`**,
  no `status`, and no `anchor_asset`; the entry describes tokens that are
  explicitly **non-redeemable** and accepted only for goods and services.
  That is a voucher, not a peso anchor.
- **`thegfb.org`** and **`starforex.co`** do not resolve in DNS and have no
  pools.

### Depth and structural floor

There is no structural floor to report for the corridor because no candidate
has a USDC/COP book in the relevant direction: all other paths are derivative
hops through XLM/yXLM/EURC. Where paths exist, depth is irrelevant — the
price is wrong at 0.1 USDC, before any size effect. Where the price is
plausible in magnitude (none here), depth would matter; it never gets that
far.

---

## Direct book and liquidity pool probe

One genuine COP/USDC liquidity pool exists on mainnet, belonging to
`luxbank.co`:

- **Pool:** `c465e843caca5b7ed751e7540f25d5a76025ecc46c0356d92d81b9cf57c1a77e`
- **Fee:** 30 bp; **type:** constant product; **trustlines:** 1
- **Total shares:** 1,581.1388300
- **Reserves (2026-11-23):** **5,573,432,388.0617716 COP** /
  **0.0008055 USDC**
- **Last modified:** `2026-11-22T23:14:11Z`
- **Trades:** 200 records returned by `/liquidity_pools/<id>/trades`

The reserves are the finding: 5.57 **billion** COP against 0.0008 USDC, an
implied pool price of 6,919,220,841,789.91 COP/USDC — about **1.97 billion
times** the reference mid. A strict-send of 1 USDC along the direct path
returns **5,568,933,114.6176146 COP** (≈1,587,454× mid), which is simply the
pool's COP reserve minus fees: the trade saturates the pool immediately.

The most recent recorded trade, at `2026-11-22T23:14:11Z`, is representative:

```
base:    0.0000100 USDC
counter: 69,851,817.6102776 COP
price:   6,985,181,761,027.76 COP/USDC  (≈1.99 billion × mid)
```

Trades exist, so this is not a dead pool — it is an actively mispriced one.
Volume at these prices is not a COP market in any economic sense.

No COP/USDC order book in the relevant direction (selling COP, buying USDC)
held more than a single offer (`anclap` api2: 1 bid, 1 ask); the rest were
empty.

---

## SEP-38 (Quote Server) check

No candidate declared an `ANCHOR_QUOTE_SERVER` on 2026-11-23. The four
mainnet-passphrase documents (`luxbank.co`, `nasdaqstellar.pages.dev`,
`xlmmoney.pages.dev`, `quantumbc-rewards.com`) and `lobstr.co` were all
checked: **zero** occurrences of `ANCHOR_QUOTE_SERVER`. There is no quoted
anchor leg to measure, so SEP-38 cannot be probed for this corridor.

---

## Finding & conclusion

**A `COP` token exists on Stellar in abundance; a USDC → COP corridor does
not.** The finding is **UNUSABLE**.

1. **Identity fails for all 47 issuers.** Only four documents declare both a
   mainnet `NETWORK_PASSPHRASE` and a `COP` currency entry, and none of those
   four declares a credible fiat COP anchor: `luxbank.co` calls its token an
   "experimental synthetic proprietary Crypto currency" with no
   `anchor_asset`; `nasdaqstellar.pages.dev` anchors COP to "NASDAQ Stock";
   `xlmmoney.pages.dev` and `quantumbc-rewards.com` declare no `status` and
   no `anchor_asset`. `anclap.com`, the only issuer with a Colombian
   payments identity, publishes no COP entry at all.
2. **The only issuer with a direct USDC/COP pool prices COP ~1.99 billion
   times too high.** `luxbank.co`'s pool holds 5.57B COP against 0.0008
   USDC; a 1-USDC direct send returns 5.57B COP and exhausts it.
3. **Every other priced candidate is 98.90%–99.76% below mid** — `anclap`
   main (38.3–38.7 COP/USDC), `anclap` alt (8.41), `trump347.com` (10.88).
   These are not spreads or fees; they are a different price scale, stable
   across four orders of magnitude of trade size.
4. **Four candidates have NO-PATH at every size**, including the two
   `anclap`-adjacent accounts whose documents do not resolve and the
   `agapanto.com.co` voucher token that is explicitly non-redeemable.
5. **No SEP-38 quote server is declared**, so there is no quoted alternative
   to the on-chain paths.

The negative result is consistent with the sister studies on GHS, ZAR, BRL,
INR, and PHP: a code and a trustline are not a corridor, and on this ledger
the `COP` code is dominated by impersonation domains and synthetic tokens.

### Recommendation

**Do not register a COP corridor and do not add COP to `asset/known.go`.**
Recording a corridor that the engine could not price honestly would be worse
than recording none: `route.Engine` clamps negative loss to zero
([`route/route.go`](../route/route.go), ≈L281–296), so `luxbank.co`'s
over-delivery would grade as **GOOD at 0% loss** if registered naively.

Re-evaluate only if **all** of the following hold:

1. A candidate publishes a mainnet SEP-1 document with a `[[CURRENCIES]]`
   entry for the exact issuer key, `status="live"`,
   `anchor_asset="COP"`, and `anchor_asset_type="fiat"`, on a domain that is
   plausibly the issuer's own — and, ideally, an `ANCHOR_QUOTE_SERVER` whose
   quotes are within a defensible band of the reference mid.
2. That asset has a USDC/COP path at 1 USDC priced within a few percent of
   the reference mid, with depth across the ladder rather than a single
   saturated trade.
3. The `COP` reference cross-check resolves to `AgreementAgree`; today the
   providers disagree by 2.06%, which is an independent reason to treat any
   COP measurement cautiously.

Until then, the answer to "can the project measure USDC → COP?" is **not
from anything currently issued on Stellar.**

---

## How to reproduce

```bash
# All COP issuers on mainnet (47 as of 2026-11-23)
curl -s "https://horizon.stellar.org/assets?asset_code=COP&limit=200&order=desc" | jq

# Code variants: all return 0 records
for c in COPC COPM COPT COPX COLP COL COPS COPB COPR COPU; do
  echo -n "$c "
  curl -s "https://horizon.stellar.org/assets?asset_code=$c&limit=10" \
    | jq '.records | length'
done

# SEP-1 for the four mainnet-passphrase candidates
for d in luxbank.co nasdaqstellar.pages.dev xlmmoney.pages.dev \
         quantumbc-rewards.com anclap.com; do
  echo "== $d"
  curl -sS "https://$d/.well-known/stellar.toml" \
    | grep -E 'NETWORK_PASSPHRASE|ANCHOR_QUOTE_SERVER|^\[\[CURRENCIES\]\]|code=|status=|anchor_asset'
done

# Path probe, one size at a time; destination_assets, not asset_type/code/issuer
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=1 \
  --data-urlencode "destination_assets=COP:GAHFKAGQ2XDJ22TNNXRSRUJ4JW3RZL4YZZ6HXRSTBPOR5PSNMOI7V5TQ"

# The one direct USDC/COP pool, its reserves, and its trades
curl -s "https://horizon.stellar.org/liquidity_pools/c465e843caca5b7ed751e7540f25d5a76025ecc46c0356d92d81b9cf57c1a77e" | jq
curl -s "https://horizon.stellar.org/liquidity_pools/c465e843caca5b7ed751e7540f25d5a76025ecc46c0356d92d81b9cf57c1a77e/trades?limit=5&order=desc" | jq '.records[0] | {base_amount, counter_amount, price, ledger_close_time}'

# Reference mid (both providers; note the 2.06% disagreement)
curl -s https://open.er-api.com/v6/latest/USD | jq '.rates.COP, .time_last_update_utc'
curl -s https://latest.currency-api.pages.dev/v1/currencies/usd.json | jq '.cop, .date'
```

---

## Related

- Issue [#679](https://github.com/Wayfare-labs/wayfare/issues/679) — this
  research task
- [`docs/corridor-research-template.md`](corridor-research-template.md) — the
  template this document follows
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and the NO-MARKET / integrity states
- [`docs/research-GHS.md`](research-GHS.md) — the sister finding on
  independent cedi tokens
- [`docs/research-ZAR.md`](research-ZAR.md) — the sister finding on
  independent Rand tokens
- [`docs/research-BRL.md`](research-BRL.md),
  [`docs/php-corridor-research.md`](php-corridor-research.md),
  [`docs/corridor-research-inr.md`](corridor-research-inr.md) — prior
  negative corridor studies
- [`docs/reference-providers.md`](reference-providers.md) — provider roles
  and the cross-check rule
- [`refrate/cross.go`](../refrate/cross.go) — `DivergenceAgree`,
  `DivergenceMalfunction`, and the conservative-mid rule
