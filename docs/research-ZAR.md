# Research: does a usable ZAR (South African Rand) issuer exist on Stellar?

**Status: completed. 2026-08-27.** South Africa has a plausible Stellar
presence and one **real, self-declared Rand anchor exists** — TD Markets'
**ZARC**. But no usable corridor exists: ZARC is **NO-MARKET** on-chain (zero
Horizon path data to USDC at every size tested, no pools, no SEP-38), and the
only ZAR that *does* produce path data (a lobstr.co-homed token) is both
issuer-unverifiable from its own published document and priced ~10–200× off
its reference mid by a broken pool.

This mirrors the KESC/GHSC and BRL findings: a token existing on the ledger,
or even a company announcing a Rand partner, is not a corridor. The lesson
carries over unchanged — **no issuer found that can be measured means the
corridor is not added.**

---

## Why this matters

South Africa is one of the largest remittance-receiving markets in
sub-Saharan Africa, and the USD→ZAR lane is a real cross-border flow. Stellar
has visible South African presence — TD Markets chose Stellar to launch ZARC,
a Rand-pegged asset, in partnership with Shift Markets. But "plausible
presence" is not "a usable corridor with a real issuer and path data." This
research answers whether a machine-priceable USDC→ZAR corridor exists before
any claim is made about its quality.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is only
"verified" when the issuer account is read **live from the issuer's own
published `stellar.toml`**, and "a negative finding is a result." Both rules
drive everything below.

---

## Data sources

- **Ledger:** Horizon mainnet `https://horizon.stellar.org`, read live on
  **2026-08-27**.
- **Reference mid USD→ZAR:** **15.947906** via exchangerate-api and
  **15.970533** via currency-api, both as of 2026-08-27. (Current rand is ~
  **R15.95/USD**.)
- **XLM context price:** ~USD 0.185 (CoinGecko, 2026-08-27), so the fair
  USD→ZAR→XLM cross-implied rate is ~**2.95 ZAR per XLM**.

All on-chain prices are raw Horizon `/paths/strict-send` output — the same
`route.Engine` endpooint the product uses. Nothing is rounded in a direction
that improves the result.

---

## Candidate issuers

A Horizon asset search for `ZAR` returns **43 issuers** of a `ZAR` code and
**2 issuers** of the `ZARC` code (plus assorted `RSA`, `SONA`, `ZARCOIN`
codes). The `ZAR`-code issuers are almost entirely BRICS-themed synthetic
tokens with negligible holders and no pools. None of their home domains is a
credible South African institution (representative examples, all as read):

| home domain | holders | pools | status |
|:---|---:|---:|:---|
| `lobstr.co` (`GDYG7OEXT…`) | 596 | **7** | the only `ZAR` with path data (below) |
| `glp-stellar.com` (`GCTKY6IZ…`) | 809 | 0 | **no DNS** — domain does not resolve; ~103 **billion** ZAR issued (non-peg scale) |
| `ndb.finance` | 221 | 0 | empty `stellar.toml` (no content served); BRICS/"NDB" theme |
| `brics-eye.com`, `bricsqn-stellar.com`, `bricspay.live`, `bricsstellar.com`, `uae-og.com`, `currenciesglobalreset.com`, `audrev-stellar.com`, `uaebricsglobal.com` | 4–363 | 0 | BRICS/BRICS-reset synthetic themes; none publish a coherent SEP-1 for `ZAR` |

All but the first two are clearly not real Rand issuers. Two warrant full
investigation, and are taken below: the real **ZARC** (TD Markets) and the
only pool-having **lobstr.co `ZAR`**.

---

## Candidate 1 — ZARC, TD Markets (the real issuer): **NO-MARKET**

**What it is.** ZARC is a digital asset pegged 1:1 to the South African Rand,
issued by **TD Markets**, a South African company (Cape Town), launched on
Stellar in partnership with Shift Markets.

- **Issuer account:** `GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU`
- **home domain:** `tdmarkets.io` (confirmed on-chain)
- **Flags:** `auth_revocable` + `auth_clawback` — the institutional flag set
  none of the BRICS tokens carry
- **Supply:** ~1,047,230 ZARC (~USD 65,000 at mid), 14 authorized holders, **0
  liquidity pools**

**SEP-1 `stellar.toml` (tdmarkets.io) — coherent.** Read live
2026-08-27; HTTP 200:

```
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
ACCOUNTS=["GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU", …]

[[CURRENCIES]]
code="ZARC"
issuer="GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU"
is_asset_anchored="true"
anchor_asset_type="fiat"
anchor_asset="ZAR"
redemption_instructions="Send ZARC to TD Markets Exchange, process ZAR withdrawal"
attestation_of_reserve="https://www.tdmarkets.io/proof-of-reserves"
```

The document is valid TOML, names the **same** account in `ACCOUNTS` and the
`[[CURRENCIES]]` issuer, declares `anchor_asset="ZAR"` with redemption and
proof-of-reserves pointers. This clears the project's identity bar — the
corridor's issuer is verified from its own published source. Two defects only
matter for *measurability*: there is **no `status` field**, and — the decisive
one — there is **no `ANCHOR_QUOTE_SERVER`** (no SEP-38).

**Horizon path data — NO-PATH at every size.** Strict-send from Circle's USDC
to ZARC(tdmarkets), all 12 ladder sizes 0.1–5000 USDC, returned **zero paths**:

| Send (USDC) | 0.1 – 5000 |
|---:|:---|
| USDC → ZARC | **NO-PATH** at every size tested |

ZARC has no liquidity pools and no orderbook route to USDC. There is **no
on-chain market**: not a bad price, but the absence of a price. This is the
same signature as KESC.

**SEP-38 — none.** No `ANCHOR_QUOTE_SERVER` in the `stellar.toml`, so there is
no programmatically priceable anchor leg. The sentiment in the issuance
announcement (an exchange off-ramp) is real, but it is not machine-priceable
through Horizon and therefore not measurable by Wayfare.

**Verdict: NO-MARKET.** The only real Rand corridor on Stellar has a verified
issuer and a coherent SEP-1 document, and **cannot be executed on-chain at
any size**. Nothing connects it to USDC.

---

## Candidate 2 — the lobstr.co `ZAR` (the only one with path data): **degenerate market**

The one `ZAR` with meaningful holders that *does* produce Horizon paths is
`GDYG7OEXT7GO2WOYJKRFMYK6PXQTPFRKO4JSNRRZWE4JM2V6QWQR2QZD`
(home domain `lobstr.co`, 596 holders, ~1,441,770 ZAR issued, **7 pools**).
It deserves investigation precisely because it is the only `ZAR` with path
data — this is what distinguishes it from KESC.

**Its path data routes USDC → native(XLM) → ZAR**, i.e. through the XLM/ZAR
liquidity pool. Horizon returns paths at every size. But the numbers are
not a market; they are a broken pool:

| Send (USDC) | route | best dest (ZAR) | effective (ZAR/USDC) | loss vs mid (15.948) |
|---:|---|---:|---:|---:|
| 0.1 | USDC → XLM → ZAR | 320.56 | 3205.6 | −20016% *("pays you 200× fair")* |
| 1 | USDC → XLM → ZAR | 2647.14 | 2647.1 | −16499% |
| 5 | USDC → XLM → ZAR | 7447.98 | 1489.6 | −9240% |
| 10 | USDC → XLM → ZAR | 9631.42 | 963.1 | −5939% |
| 25 | USDC → XLM → ZAR | 11686.95 | 467.5 | −2831% |
| 50 | USDC → XLM → ZAR | 15127.12 | 302.5 | −1797% |
| 100 | USDC → XLM → ZAR | 17385.87 | 173.9 | −990% |
| 250 | USDC → XLM → ZAR | 13403.49 | 53.6 | −236% |
| 500 | USDC → XLM → ZAR | 13513.82 | 27.0 | −69% |
| 1000 | USDC → XLM → ZAR | 13569.67 | 13.6 | +14.9% |
| 2500 | USDC → XLM → ZAR | 13603.39 | 5.4 | +65.9% |
| 5000 | USDC → XLM → ZAR | 13614.68 | 2.7 | +82.9% |

(Figures stable across repeated runs on 2026-08-27. Because the rate "beats"
the mid by up to 200×, the loss is negative at small sizes — that is an
arithmetic artifact, and it is itself the defect: no credible USD→ZAR market
pays R3.2k per dollar.)

**The pool is ~10–200× off fair value.** Fair cross-implied value is ~2.95
ZAR per XLM (XLM ≈ USD 0.185, ZAR/USD ≈ 15.95). The XLM↔ZAR order-book (read
live) quotes **best bid 56.4 ZAR/XLM, best ask 644.2 ZAR/XLM** — an inside
spread of more than **11×**, and a price that values a rand at between ~5%
and ~0.5% of its real value. The XLM/ZAR pool itself holds ~22 XLM against
~13,626 ZAR, i.e. it treats ~R13.6k as worth ~USD 4.

**Issuer identity is not verifiable from its own document.** The account's
`home_domain` is `lobstr.co`, but `lobstr.co/.well-known/stellar.toml` — a
large, well-formed SEP-1 document — contains **no `[[CURRENCIES]]` section at
all** and does not declare this asset. LOBSTR's document describes the LOBSTR
organization and its validators, not this `ZAR` token. Under
`adding-a-corridor.md`, issuer identity cannot be confirmed from the issuer's
own published source. This alone disqualifies the corridor.

**Depth is near zero.** Total ZAR across all 7 pools ≈ **28,147 ZAR ≈
~USD 1,765** at mid. This is on the order of one pool emptying: the rate
collapses monotonically from ~R3,200/USD at dust to ~R2.7/USD at 5000 USDC,
i.e. it falls *through* the mid and keeps going.

**SEP-38 — none.** No `ANCHOR_QUOTE_SERVER` in `lobstr.co/stellar.toml`;
there is nothing to discover.

**Verdict: unverifiable + degenerate; not NO-MARKET but not measurable —
not fit to register.** The KESC distinction keeps its meaning:
KESC has no price because no market exists; this `ZAR` has a price the ledger
publishes that is irreconcilable with any Rand reference.

---

## Other codes checked

- `ZARC` mintsoroban.org issuer (`GACWH5TUEA…`, ~9M ZARC, 8 holders,
  `home_domain=mintsoroban.org`): a Soroban minting/demo asset, no path data,
  no credible SEP-1.
- `RSA` / `SONA` / `ZARCOIN` / `ZARS`: trivial holder counts, no pools, no
  usable path data. Noise.

---

## Finding

**One real, self-declared South African Rand issuer exists on Stellar — TD
Markets' ZARC — and it is verified from its own coherent `stellar.toml`.**
That clears the "does a real issuer exist" layer-1 question: **yes.** But
**no usable corridor exists**, because:

1. **ZARC is NO-MARKET.** Horizon returns zero paths from USDC to ZARC at
   every size from 0.1 to 5000 USDC. No pools, no orders, no SEP-38. Even the
   real issuer cannot be measured on-chain.
2. **The only ZAR with path data is unusable and unverifiable.** Its issuer
   is not declared in the document its own `home_domain` serves, and its
   market prices a rand at 0.5–5% of real value with an 11× inside spread —
   ~USD 1,765 of total depth.

There is no size at which sending USDC for ZAR on Stellar is either
executable (ZARC) or price-defensible (lobstr.co `ZAR`).

### Recommendation

**Do not register the corridor.** Record this as investigated, with the
specificity that separates the two negatives:

- **ZARC (TD Markets)** is the *real* Rand anchor, but **NO-MARKET** on-chain.
- The lobstr.co `ZAR` is **not** NO-MARKET (a market exists) but is both
  **issuer-unverifiable** and **degenerate** (priced 10–200× off mid).

Re-evaluate the corridor when **either** of the following changes:

1. **TD Markets adds on-chain liquidity** — pools and/or order-book depth
   so that `find_payment_paths` returns USDC→ZARC routes, ideally with an
   `ANCHOR_QUOTE_SERVER` (SEP-38) for the anchor leg. Until then ZARC is a
   Real token with no executable market.
2. **A Rand issuer publishes a SEP-1 document for a `ZAR`/`ZARC` asset it
   actually declares**, with liquidity whose on-chain price is within
   defensible range of the interbank mid.

---

## How to reproduce

```bash
# ZARC issuer account, flags and home_domain
curl -s https://horizon.stellar.org/accounts/GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU

# ZARC footprint (holders, issued, pools)
curl -s "https://horizon.stellar.org/assets?asset_code=ZARC&asset_issuer=GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU"

# SEP-1 for the real issuer (coherent)
curl -s https://tdmarkets.io/.well-known/stellar.toml

# Path data USDC -> ZARC (NO-PATH at every size)
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=100 \
  --data-urlencode "destination_assets=ZARC:GAJKJQSVUUPKDWV6SS3COVVREPR5YGJEM2MHKG6AWPNCTX4IUFBPT6MU"

# The only ZAR with path data (broken pool) — repeat over the size ladder
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=1 \
  --data-urlencode "destination_assets=ZAR:GDYG7OEXT7GO2WOYJKRFMYK6PXQTPFRKO4JSNRRZWE4JM2V6QWQR2QZD"

# XLM/ZAR inside spread (best bid 56.4, best ask 644.2 ZAR/XLM)
curl -sG "https://horizon.stellar.org/order_book" \
  --data-urlencode selling_asset_type=native \
  --data-urlencode "buying_asset_type=credit_alphanum4" \
  --data-urlencode "buying_asset_code=ZAR" \
  --data-urlencode "buying_asset_issuer=GDYG7OEXT7GO2WOYJKRFMYK6PXQTPFRKO4JSNRRZWE4JM2V6QWQR2QZD" \
  --data-urlencode selling_amount=100
```

---

## Related

- Issue [#58](https://github.com/Wayfare-labs/wayfare/issues/58) — this
  research task
- Issue [#33](https://github.com/Wayfare-labs/wayfare/issues/33) — the
  reference standard for detail
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and the NO-MARKET / integrity states
- [`docs/research-BRL.md`](research-BRL.md) and
  [`docs/corridor-measurements.md`](corridor-measurements.md) — the KESC /
  sister-corridor format this finding follows