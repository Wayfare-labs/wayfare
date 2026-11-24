# Research: USDC → EUR via Circle's EURC

**Status:** completed, 2026-11-23. **Finding: VERIFIED** — with two recorded
caveats that the registration follow-up must carry (the issuer document omits
`NETWORK_PASSPHRASE` and `status`, and the on-chain price sits ~1.3% off the
EUR reference mid, so the forward leg *over*-delivers against it).

Circle's EURC issuer is identified both ways (the account names `circle.com`;
`circle.com`'s `stellar.toml` names the account and declares `anchor_asset="EUR"`,
fiat). Horizon prices USDC → EURC **directly** at every rung of the shared
ladder, 0.1 to 5000 USDC, through a USDC/EURC order book with 110 bids, 54 asks
and a 0.19% spread, plus a USDC/EURC liquidity pool. A 1-USDC round trip
(USDC → EURC → USDC) costs **0.13%**. This is the first corridor studied in
this series that is a real market rather than a token code.

This answers issue [#675](https://github.com/Wayfare-labs/wayfare/issues/675)
and follows [`docs/corridor-research-template.md`](corridor-research-template.md).

All on-chain figures below were read live from Horizon
(`https://horizon.stellar.org`) on **2026-11-23** between 12:46 and 12:49 UTC.
The reference mid was read from the project's two rate providers the same day.
Ladder figures come from the project's own `route.Engine` (the code path
`cmd/ladder` and `/api/corridor` run), checks from `checks.Runner`, and the
SEP-1 parse from `anchor.Resolver` — not from hand-computed paths.

---

## Why this matters

The six prior studies (INR, MXN, PHP, BRL, ZAR, parallel NGN — and since then
GHS and COP) all ended negative: a token code existed, an anchor did not. A
Circle-issued euro token next to Circle-issued USDC is the opposite case — the
same issuer family, the same attestation regime, a large holder base — and so
the cleanest corridor the network can offer.

That makes it a **control corridor**. If the method cannot produce a clean,
low-loss, size-stable measurement here, the problem is the method, not the
market. The hypothesis tested:

> Circle's EURC is identifiable from Circle's own SEP-1 document, and USDC →
> EURC prices directly on the DEX near the USD/EUR reference mid at every
> ladder size.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), "verified" means the
issuer account was read live from the issuer's own `stellar.toml`. The code
alone identifies nothing — and for EURC that is not hypothetical (see the
`mykobo.co` note below).

---

## Data sources

- **Ledger:** Horizon mainnet, read 2026-11-23 12:46–12:49 UTC.
- **Asset discovery:** `GET /assets?asset_code=EURC&limit=200` returned
  **84 issuer accounts**.
- **Issuer document:** `https://circle.com/.well-known/stellar.toml`, HTTP 200,
  1,429 bytes, fetched 2026-11-23T12:46:58Z. SHA-256 as hashed by
  `anchor.Resolver`:
  `f070510b81d12db211816bc5de5bb784804eca6d66ece153b6d382ec61084e49`.
- **Path data:** `route.Engine.Ladder` over `route.DefaultSizes` (Horizon
  `/paths/strict-send`), both directions.
- **Book / pool / trades:** Horizon `/order_book`, `/liquidity_pools`,
  `/trades`, `/trade_aggregations` for USDC/EURC.
- **Reference mid USD→EUR:** **0.911134** via exchangerate-api
  (`open.er-api.com`, as of 2026-11-23T00:02:32Z) and **0.90902884** via
  currency-api (`latest.currency-api.pages.dev`, dated 2026-11-23).

### Reference-mid cross-check — AGREE

```
|0.911134 − 0.90902884| / 0.90902884 = 0.2316%
```

Below `DivergenceAgree = 2` in [`refrate/cross.go`](../refrate/cross.go), so
the cross-check state is **`AgreementAgree`** and the scored mid is the
primary's, **0.911134 EUR/USD**. Every loss figure below uses that value.

---

## Candidate issuers and identity verification

Horizon lists 84 `EURC` issuers. By authorized holders the top two are:

| Issuer | Home domain | Holders | Authorized | Pools |
|:---|:---|---:|---:|---:|
| `GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2` | `circle.com` | 55,413 | 4,000,708.27 | 118 |
| `GAQRF3UGHBT6JYQZ7YSUYCIYWAF4T2SAA5237Q5LIQYJOHHFAWDXZ7NM` | `mykobo.co` | 12,812 | 122,037.77 | 38 |

The next issuer has 629 holders. The remaining 82 are dominated by the same
impersonation pattern the COP, GHS and ZAR studies found (`circle-assets.com`,
`blackrock.co.com`, `coinbase.com.co`, `JPmorganchase.co.com`,
`kraken.com.bz`, `stellar.dtcc.network`, …), many with ~10⁹–10¹² units
"outstanding" to one to three holders. None was pursued: the issue asks about
Circle's EURC, and Circle's document names exactly one EURC issuer.

**A code collision worth recording.** `mykobo.co` is a separate EUR anchor
that also issues an asset coded `EURC`. Its document is well formed
(`NETWORK_PASSPHRASE` = public mainnet, `status="live"`, `anchor_asset="EUR"`,
fiat), and its issuer's `home_domain` is `mykobo.co`. It is not Circle's
EURC. Any registration must key on Circle's issuer account, never on the code.

### Primary candidate: Circle

- **Issuer account:** `GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2`
- **Declared home domain:** `circle.com` (Horizon `home_domain`, 2026-11-23)
- **Account flags:** `auth_required=false`, `auth_revocable=true`,
  `auth_immutable=false`, `auth_clawback_enabled=false` — the issuer can
  freeze a holder's balance. `checks.IssuerAuthFlags` reports this as a
  **critical FAIL** ("the issuer can freeze your balance"), the same posture as
  Circle's USDC issuer.
- **Signers / thresholds:** 5 signers, low/med/high thresholds all 2. Account
  last modified 2026-11-18T14:25:32Z.
- **Footprint:** 55,413 authorized trustlines (+1 authorized to maintain
  liabilities, 0 unauthorized); 4,000,708.27 EURC authorized; 118 liquidity
  pools; a Stellar Asset Contract is deployed
  (`CDTKPWPLOURQA2SGTKTUQOWRCBZEORB4BWBOMJ3D3ZTQQSGE5F6JBQLV`).

### SEP-1 (`stellar.toml`) check

`https://circle.com/.well-known/stellar.toml`, 2026-11-23:

- **TLS:** the certificate matches the host — `CN=circle.com`, SAN includes
  `circle.com` and `www.circle.com`, issued by DigiCert EV RSA CA G2, valid
  2026-10-06 → 2027-04-22. HTTP 200 (served as `binary/octet-stream`).
- **Parses cleanly:** `anchor.Resolver` parsed it without the salvage path
  (`Malformed=false`).
- **`ACCOUNTS`** lists four accounts, including the EURC issuer above and
  Circle's USDC issuer `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.
- **`[[CURRENCIES]]`** has an EURC entry:

  ```toml
  code="EURC"
  issuer="GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2"
  is_asset_anchored=true
  anchor_asset_type="fiat"
  anchor_asset="EUR"
  attestation_of_reserve="https://www.circle.com/en/transparency"
  desc="EURC is a fully collateralized euro stablecoin."
  ```

- **Round trip:** `checks.HomeDomainRoundTrip` **PASS** — "the account
  home_domain and stellar.toml both identify EURC issued by
  GDHU6WRG…NPP2".

**Two deviations from the checklist in `adding-a-corridor.md`,** recorded
rather than worked around:

1. **No `NETWORK_PASSPHRASE`.** The document does not declare one, so
   `anchor.Profile.Mainnet` is `false`. The checklist says it "must be" the
   public passphrase, to rule out a testnet token. Here the identity is not in
   doubt for a different reason: the issuer account exists on mainnet, names
   `circle.com` as its home domain, and the round trip closes. The deviation is
   real and is Circle's to fix; it does not make the token a testnet one.
2. **No `status`.** The EURC entry declares none. SEP-1 makes `status`
   optional and `anchor.Currency.Live()` treats an absent status as live, so
   the resolver lists EURC among the live currencies — but Circle has not
   *declared* it live. Register it with the status recorded as absent, not as
   `live`.

Both deviations apply equally to Circle's USDC entry in the same document.

**Verdict on identity: VERIFIED**, read live from the issuer's own document on
2026-11-23.

---

## Horizon path data (USDC → EURC)

USDC issuer used as the source: Circle's mainnet account
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`. Reference mid
**0.911134 EUR/USD** (exchangerate-api, agreeing with currency-api).

Integrity: **DIRECT** at every size. The engine's own grade is in the
"Verdict" column; the "signed" column is the same figure *before* the
engine's clamp (see below), so a negative number means the trade delivered
more EUR than the mid.

| Send (USDC) | Receive (EURC) | Effective rate (EURC/USDC) | Loss vs mid (engine) | Signed vs mid | Verdict | Best path |
|---:|---:|---:|---:|---:|:---|:---|
| 0.1 | 0.0924143 | 0.9241430 | 0.00% | −1.43% | GOOD | USDC → XLM → EURC |
| 1 | 0.9231470 | 0.9231470 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 5 | 4.6157354 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 10 | 9.2314708 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 25 | 23.0786770 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 50 | 46.1573541 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 100 | 92.3147082 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 250 | 230.7867705 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 500 | 461.5735411 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 1000 | 923.1470822 | 0.9231471 | 0.00% | −1.32% | GOOD | USDC → EURC |
| 2500 | 2307.6565466 | 0.9230626 | 0.00% | −1.31% | GOOD | USDC → EURC |
| 5000 | 4603.5840066 | 0.9207168 | 0.00% | −1.05% | GOOD | USDC → EURC |

- **Structural floor / spread:** the rate is flat to seven decimal places
  from 1 to 1000 USDC; the order-book spread is **0.19%** (best bid 0.9231488,
  best ask 0.9243600 EURC/USDC). Size first moves the price at 2500 USDC
  (−0.01%) and by 5000 USDC the rate has slipped **0.26%** from the 1-USDC
  rate.
- **Depth:** no exhaustion inside the ladder. Every rung prices, through a
  single direct path from 1 USDC up.

### The clamp, and why the verdict column is not the finding

[`route/route.go`](../route/route.go) `Quote.score` clamps a negative loss to
zero (≈L284–294). Every forward rung delivers ~1.3% *more* EURC than the USD/EUR
mid predicts, so the engine reports **0.00% loss, GOOD** at every size. That
grade is accurate as far as it goes — the sender loses nothing against the
mid — but it hides the fact that matters: **the Stellar market prices EURC
about 1.3% below the EUR reference**, not at it.

### The reverse leg (EURC → USDC) shows the same offset from the other side

Reference mid **1.097535 USD/EUR** (exchangerate-api), integrity DIRECT at
every size, best path `EURC → USDC` throughout:

| Send (EURC) | Receive (USDC) | Effective rate (USDC/EURC) | Loss vs mid | Verdict |
|---:|---:|---:|---:|:---|
| 0.1 | 0.1082285 | 1.0822850 | 1.39% | GOOD |
| 1 | 1.0818643 | 1.0818643 | 1.43% | GOOD |
| 100 | 108.1802739 | 1.0818027 | 1.43% | GOOD |
| 1000 | 1078.8730827 | 1.0788731 | 1.70% | GOOD |
| 5000 | 5390.0730890 | 1.0780146 | 1.78% | GOOD |

Forward over-delivers by ~1.3%; reverse under-delivers by ~1.4%. A spread
would cost in both directions; an offset gains one way and loses the other.
This is an offset. The round trip makes it concrete:

```
1 USDC → 0.9231470 EURC → 0.9231470 × 1.0818643 = 0.99872 USDC
round-trip cost = 0.128%
```

The executable market is tight; it is simply centred ~1.3% away from the
reference mid.

### The offset is persistent, not a stale reference

The two reference providers agree within 0.23% today, so the gap is not one
bad feed. To rule out a one-day artefact, the daily average USDC/EURC trade
price (Horizon `/trade_aggregations`, 1-day resolution) was compared with
currency-api's dated USD→EUR mid for the same day:

| Day (UTC) | Trades | Volume (USDC) | DEX avg (EURC/USDC) | currency-api mid | DEX above mid |
|:---|---:|---:|---:|---:|---:|
| 2026-11-09 | 6,331 | 122,938 | 0.9031130 | 0.89604638 | +0.79% |
| 2026-11-12 | 6,016 | 115,398 | 0.9125894 | 0.90275295 | +1.09% |
| 2026-11-15 | 7,335 | 78,458 | 0.9209833 | 0.90727164 | +1.51% |
| 2026-11-18 | 5,957 | 108,859 | 0.9203219 | 0.91080569 | +1.04% |
| 2026-11-21 | 6,112 | 113,091 | 0.9187725 | 0.91174164 | +0.77% |
| 2026-11-22 | 3,411 | 50,066 | 0.9202233 | 0.91158016 | +0.95% |

On every sampled day USDC bought **0.8–1.5% more EURC** on Stellar than USD
bought EUR at the reference mid. A daily average against a daily mid is a
coarse comparison — intraday EUR/USD moves are of the same order — but the
sign never flips. Why EURC trades at a discount on this venue (DEX inventory
imbalance, redemption friction outside Stellar, or the reference mid's own
timing) is outside what this study can establish; that it does is measured.

---

## Direct book and liquidity pool probe

**Order book**, `selling=USDC buying=EURC`, 2026-11-23T12:48 UTC:

- **110 bids, 54 asks.** Best bid **0.9231488**, best ask **0.9243600**
  EURC/USDC — spread **0.19%**, book mid 0.92346 EURC/USDC (implied
  1.08288 USD/EUR against the reference 1.09754).
- One bid carries a zero price and was excluded from the spread.

**Liquidity pool:** exactly one USDC/EURC constant-product pool:

- **Pool:** `328306d8d623aa358415f29fca051afbbe8f0c591c28bbcb78e80907deffb2a7`
- **Fee:** 30 bp; **trustlines:** 29
- **Reserves (2026-11-23T12:48:06Z):** 2,176.7411781 EURC / 2,362.7829849 USDC
  — implied 0.92126 EURC/USDC, within 0.25% of the book mid.

Small next to the book; the ladder's 1-to-5000-USDC rungs all route
`USDC → EURC` and the flat rate shows they are filled from the book.

**Trades:** the last 200 USDC/EURC trades span 11:53:21 → 12:48:06 UTC
(≈55 minutes), 5,557.59 USDC in volume, from both the order book and the pool.
The most recent order-book fills, at 12:47:56Z, executed at 0.9231479
EURC/USDC — the price the ladder quoted at the same moment.

This is a live, two-sided, continuously traded market.

---

## SEP-38 (Quote Server) check

`circle.com`'s document declares **no `ANCHOR_QUOTE_SERVER`**, and no
`WEB_AUTH_ENDPOINT`, `TRANSFER_SERVER`, `TRANSFER_SERVER_SEP0024`,
`DIRECT_PAYMENT_SERVER` or `KYC_SERVER`. `anchor.Resolver` reports
`Priceable=false`: "quotes: NONE. No ANCHOR_QUOTE_SERVER in stellar.toml, so
this anchor publishes no machine-readable rate." Redemption is off-chain,
"through a Circle account at https://circle.com".

So Wayfare can measure only the on-chain leg, and the SEP-10/SEP-6/SEP-24/
SEP-31/SEP-38 checks will report *undetermined (not declared)* for this
corridor. That is the finding, not a gap to fill.

---

## What the project's checks say today

`checks.Runner.ForAsset` against the unregistered EURC asset, 2026-11-23:

| Check | Result |
|:---|:---|
| `toml.home-domain-roundtrip` | **PASS** — home_domain and stellar.toml agree |
| `issuer.auth-flags` | **FAIL (critical)** — `auth_revocable=true` |
| `issuer.auth-immutable` | FAIL (info) — flags are mutable |
| TOML-reading checks (`toml.anchor-asset-iso4217`, `sep10`, `sep6`, `sep12`, `sep24`, `sep31`, `sep38`, `issuer.drift`) | undetermined — "no stellar.toml has been resolved for this asset" |

The undetermined TOML checks are an artefact of the asset not being
registered: `checks.Runner` only resolves a home domain from the registry
(`asset.HomeDomain`), by design, so an unregistered asset has no profile. The
document itself resolves (above). Once registered with
`HomeDomain: "circle.com"`, `toml.anchor-asset-iso4217` should pass
(`anchor_asset="EUR"`), and the SEP checks should report *not declared*.

---

## Finding & conclusion

**VERIFIED.** USDC → EUR via Circle's EURC is a real, directly priced,
size-stable corridor.

1. **Identity is verified both ways.** The issuer's `home_domain` is
   `circle.com`; `circle.com`'s TLS-valid `stellar.toml` lists the issuer under
   `ACCOUNTS` and in a `[[CURRENCIES]]` entry with `anchor_asset="EUR"`,
   `anchor_asset_type="fiat"`, `is_asset_anchored=true`.
2. **The market is direct and deep enough for the whole ladder.** DIRECT at
   all twelve rungs; the rate is flat from 1 to 1000 USDC and slips only 0.26%
   by 5000 USDC. 110 bids, 54 asks, a 0.19% spread, and a trade roughly every
   16 seconds.
3. **The corridor is cheap.** A 1-USDC round trip costs 0.13%.
4. **The price is offset from the reference mid by ~1.3%**, persistently
   (+0.8% to +1.5% on every day sampled). Forward, this is over-delivery that
   the engine clamps to 0.00%/GOOD; reverse, it is a 1.4–1.8% loss.
5. **Caveats to carry into registration:** no `NETWORK_PASSPHRASE` and no EURC
   `status` in Circle's document; `auth_revocable=true` on the issuer; no
   SEP-38 quote server.

As a control corridor it does its job: the method produces a clean, stable
measurement when a real market exists. It also exposes a reporting gap the
negative studies never reached: a corridor that *beats* the reference mid
shows as "0.00% loss" and hides the size of the offset.

---

## Recommendation

1. **Register the corridor** in [`asset/known.go`](../asset/known.go) — a
   follow-up issue, per this issue's acceptance criteria. Proposed entry:

   ```go
   // CircleEURCIssuer is Circle's mainnet EURC issuing account. Verified
   // 2026-11-23 from circle.com's stellar.toml (docs/eurc-corridor-research.md).
   const CircleEURCIssuer = "GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2"

   {
       Code:             "EURC",
       Issuer:           CircleEURCIssuer,
       Peg:              "EUR",
       // ValidateEntry requires a non-empty Status, and the document declares
       // none. Record the absence explicitly — never "live" — and let the
       // follow-up pick the label (e.g. "undeclared").
       Status:           "undeclared",
       VerificationDate: "2026-11-23",
       SourceURL:        "https://circle.com/.well-known/stellar.toml",
       HomeDomain:       "circle.com",
   },
   ```

   and record a replay snapshot with
   `go run ./cmd/ladder -to EURC -record testdata/snapshots` from a clean tree.
   The snapshot belongs with the registration rather than this document:
   `cmd/ladder -record` only accepts registered corridors, and its clean-tree
   rule exists so `git_revision` names the code that produced the bytes.

2. **The registration should state the offset, not hide it.** Until the
   engine reports signed loss (or an "above mid" state) alongside the clamped
   figure, the corridor will read as "0.00% loss, GOOD" at every size. That
   is a separate engine question; it should not block registration, but the
   corridor's comment in `asset/known.go` should name it.

3. **Side finding — Circle's USDC issuer is now verified too.**
   `asset/known.go` still says USDC is "NOT YET VERIFIED against circle.com's
   stellar.toml" and registers it with `Status: "unverified"`. The same document, read 2026-11-23, lists
   `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN` under `ACCOUNTS`
   and as the issuer of `USDC` with `anchor_asset="USD"`. Updating that entry
   is a small follow-up, out of scope here.

Re-evaluate if Circle's document stops listing this issuer, the issuer's
`home_domain` changes, the USDC/EURC book thins to the point that the ladder
stops pricing DIRECT, or the offset from the reference mid grows past the 2%
`DivergenceAgree` scale.

---

## How to reproduce

```bash
H=https://horizon.stellar.org
EURC_ISSUER=GDHU6WRG4IEQXM5NZ4BMPKOXHW76MZM4Y2IEMFDVXBSDP6SJY4ITNPP2
USDC_ISSUER=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN

# All EURC issuers (84 on 2026-11-23)
curl -s "$H/assets?asset_code=EURC&limit=200" | jq '.records | length'

# Issuer home domain and flags
curl -s "$H/accounts/$EURC_ISSUER" | jq '{home_domain, flags}'

# Circle's document: the EURC entry, and the absent passphrase / quote server
curl -s https://circle.com/.well-known/stellar.toml
curl -s https://circle.com/.well-known/stellar.toml \
  | grep -cE 'NETWORK_PASSPHRASE|ANCHOR_QUOTE_SERVER'   # 0

# Strict-send at 1 USDC
curl -sG "$H/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=$USDC_ISSUER \
  --data-urlencode source_amount=1 \
  --data-urlencode "destination_assets=EURC:$EURC_ISSUER" | jq '.records[0]'

# Book, pool, trades
curl -sG "$H/order_book" \
  --data-urlencode selling_asset_type=credit_alphanum4 \
  --data-urlencode selling_asset_code=USDC --data-urlencode selling_asset_issuer=$USDC_ISSUER \
  --data-urlencode buying_asset_type=credit_alphanum4 \
  --data-urlencode buying_asset_code=EURC --data-urlencode buying_asset_issuer=$EURC_ISSUER \
  | jq '{bids: (.bids|length), asks: (.asks|length), best_bid: .bids[0].price, best_ask: .asks[0].price}'
curl -s "$H/liquidity_pools?reserves=USDC:$USDC_ISSUER,EURC:$EURC_ISSUER" | jq '.["_embedded"].records[] | {id, reserves}'

# Daily averages vs the dated reference
curl -sG "$H/trade_aggregations" \
  --data-urlencode base_asset_type=credit_alphanum4 \
  --data-urlencode base_asset_code=USDC --data-urlencode base_asset_issuer=$USDC_ISSUER \
  --data-urlencode counter_asset_type=credit_alphanum4 \
  --data-urlencode counter_asset_code=EURC --data-urlencode counter_asset_issuer=$EURC_ISSUER \
  --data-urlencode resolution=86400000 --data-urlencode order=desc --data-urlencode limit=14 \
  | jq '.["_embedded"].records[] | {timestamp, trade_count, avg}'
curl -s https://2026-11-22.currency-api.pages.dev/v1/currencies/usd.json | jq '.usd.eur'

# Reference mid, both providers
curl -s https://open.er-api.com/v6/latest/USD | jq '.rates.EUR, .time_last_update_utc'
curl -s https://latest.currency-api.pages.dev/v1/currencies/usd.json | jq '.usd.eur, .date'
```

The ladder tables were produced by `route.Engine.Ladder` with
`route.DefaultSizes` and `refrate.ExchangeRateAPI`, the wiring `cmd/ladder`
builds; once EURC is registered, `go run ./cmd/ladder -to EURC` reproduces
them directly.

---

## Related

- Issue [#675](https://github.com/Wayfare-labs/wayfare/issues/675) — this
  research task
- [`docs/corridor-research-template.md`](corridor-research-template.md) — the
  template this document follows
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and the registration steps the follow-up will take
- [`docs/cop-corridor-research.md`](cop-corridor-research.md),
  [`docs/research-GHS.md`](research-GHS.md),
  [`docs/research-ZAR.md`](research-ZAR.md),
  [`docs/research-BRL.md`](research-BRL.md),
  [`docs/php-corridor-research.md`](php-corridor-research.md),
  [`docs/mxn-corridor-research.md`](mxn-corridor-research.md),
  [`docs/corridor-research-inr.md`](corridor-research-inr.md) — the prior,
  negative corridor studies this one is the control for
- [`refrate/cross.go`](../refrate/cross.go) — the reference cross-check rule
- [`route/route.go`](../route/route.go) — `Quote.score` and the negative-loss
  clamp
