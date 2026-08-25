# Research corridor: USDC → MXN (Mexican Peso)

**Status: completed.** A real MXN market exists on Stellar, but it is
**unverifiable to this project's standard** (the issuer publishes no readable
SEP-1 document) and **structurally unusable** (a ~29% loss floor at zero size
and total on-chain depth worth roughly USD 62). This is **not** a NO-MARKET
finding — a market exists — but the corridor is **not fit to register or
measure** as things stand.

All on-chain figures below were read live from Horizon
(`https://horizon.stellar.org`) on **2026-08-25**. The reference mid was read
from the project's existing rate provider the same day.

---

## Why this matters

Mexico is the largest remittance-receiving country in Latin America, and the
USD→MXN corridor is one of the most-cited high-cost lanes in the world. If a
real MXN issuer exists on Stellar with usable Horizon path data, the corridor
is worth measuring. If it does not — or if the only market is too thin or too
poorly documented to price defensibly — the finding is worth recording so the
question is not re-opened without new facts.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is only
"verified" when the issuer account is read **live from the issuer's own
published `stellar.toml`**. An asset code identifies nothing; the issuer
account is the identity. That standard is the first thing this research
applies, and it is where the leading candidate fails.

---

## Candidate issuers

A Horizon asset search for `asset_code=MXN` returns many issuers. All but one
resolve to impersonation or spam domains — e.g. `bankofengland.com.co`,
`franklintempleton.co.com`, `fednow.us.org`, `iso20022.io`,
`ledgerscan.dtcc.markets`, `currenciesglobalreset.com`. None is a credible
Mexican-peso issuer and none is investigated further here.

The one genuine candidate is **Saldo.mx**, a Mexican fintech with a long
history in the Stellar ecosystem.

### Saldo.mx

- **Issuer account:** `GBUMQHWIQELILQEQ5YEEHUFR6SRLBNRKWHJ3JX7JBRFONG24FWUDG627`
- **Declared home domain:** `pagos.saldo.mx` (read from the account's
  `home_domain` field on Horizon)
- **Account auth flags:** `auth_required=false`, `auth_revocable=false`,
  `auth_immutable=false`, `auth_clawback_enabled=false`
- **Assets from this account, and their on-chain footprint:**

  | Asset | Authorized holders | Amount outstanding |
  |:---|---:|---:|
  | `MXN`  | 75 | 1,227,521.68 |
  | `MXNT` |  2 | 721.50 |

  `MXN` is the only asset with a non-trivial holder base; `MXNT` is negligible.

---

## SEP-1 (stellar.toml) — **FAILS**

The account advertises `home_domain = pagos.saldo.mx`, so per SEP-1 the
document must be served at
`https://pagos.saldo.mx/.well-known/stellar.toml`. It cannot be read:

- **Under normal TLS verification the request fails**: the server presents a
  certificate for `*.x9zkc6.usa-w1.cloudhub.io` (a MuleSoft CloudHub host),
  which does **not** cover `pagos.saldo.mx`. A conforming SEP-1 client rejects
  the connection.

  ```
  * SSL: no alternative certificate subject name matches target host name 'pagos.saldo.mx'
  * Server certificate: subject=CN=*.x9zkc6.usa-w1.cloudhub.io
  ```

- **Even ignoring the certificate** (`curl -k`), the path returns **HTTP 404**
  — there is no document there at all. `https://saldo.mx/.well-known/stellar.toml`
  also returns 404.

**Consequence.** The issuer publishes no readable SEP-1 document at its own
declared home domain. Under the project's verification standard, the issuer
identity cannot be confirmed from the issuer's own published source, and none
of the SEP-1 fields the corridor workflow depends on — `NETWORK_PASSPHRASE`,
`[[CURRENCIES]]` `status`, `anchor_asset`, or `ANCHOR_QUOTE_SERVER` — can be
read. This alone disqualifies the corridor from being registered as verified.

---

## Horizon path data (USDC → MXN) — **exists, but thin**

USDC issuer used as the source: Circle's mainnet account
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

A strict-send path from USDC to Saldo's `MXN` **does** exist. Every returned
path is a single hop through **XLM** (the native bridge asset), not through
another fiat token — so the integrity state is **DIRECT**, not DERIVATIVE, and
it is emphatically **not NO-MARKET**.

Ladder (`strict-send`, best `destination_amount` at each size, Horizon public,
2026-08-25):

| Send (USDC) | Receive (MXN) | Effective (MXN/USDC) |
|---:|---:|---:|
| 0.1  | 1.1995     | 11.995 |
| 1    | 11.9904    | 11.990 |
| 10   | 119.8829   | 11.988 |
| 100  | 1,049.34   | 10.493 |
| 150  | 1,051.92   |  7.013 (marginal) |
| 200  | 1,054.50   |  5.273 (marginal) |
| 300+ | NO-PATH    | — |

Reference mid **USD→MXN = 16.9469** (project rate provider, 2026-08-25).

**Structural floor (spread, not depth).** At 0.1 USDC — where price impact is
negligible — the effective rate is 11.995 MXN/USDC, a loss of
**(16.9469 − 11.995) / 16.9469 ≈ 29.2%** against the mid. A structural floor
above 20% means **no size can be acceptable**: the zero-size limit is already
unacceptable.

**Depth.** The market is exhausted almost immediately. By 200 USDC the total
extractable amount has flattened at ~1,054 MXN (each extra USDC returns almost
nothing), and by 300 USDC **there is no path at all**. The entire liquid depth
of this corridor is therefore ~1,054 MXN — roughly **USD 62 at the mid**. At
100 USDC the realised loss is already ~38%.

---

## SEP-38 (quote server) — **none**

SEP-38 quote servers are discovered from `ANCHOR_QUOTE_SERVER` in the
`stellar.toml`. Because no `stellar.toml` is served (above), there is nothing to
discover, and a direct probe of `https://pagos.saldo.mx/sep38/info` returns
**HTTP 404**. There is no programmatically priceable anchor leg; only the
on-chain leg exists, and its absence of a SEP-38 endpoint is itself part of the
finding.

---

## Finding

**A real USDC → MXN market exists on Stellar** (Saldo.mx issuer, 75 holders,
~1.23M MXN outstanding, a DIRECT single-hop path through XLM). It is **not**
NO-MARKET.

**But the corridor is not fit to register or measure**, for two independent
reasons, either of which is sufficient:

1. **Unverifiable to standard.** The issuer publishes no readable SEP-1
   document at its declared home domain (`pagos.saldo.mx` serves the wrong TLS
   certificate and 404s the `.well-known/stellar.toml` path). The project's
   hard rule is that a corridor is verified only when read live from the
   issuer's own published document; that cannot be done here.

2. **Structurally unusable.** Even setting verification aside, the on-chain
   market has a ~29% loss floor at zero size and a total liquid depth of
   ~1,054 MXN (~USD 62). No transfer size clears the project's acceptability
   bar, and there is no SEP-38 anchor leg to price the off-chain side.

---

## Recommendation

**Do not register the corridor.** Record it as investigated, not as
NO-MARKET (the distinction matters: a market exists, it is simply
undocumented and too thin to be useful).

Re-evaluate if **both** of the following change:

1. **Saldo.mx serves a valid SEP-1 `stellar.toml`** at `pagos.saldo.mx` — with
   a certificate matching the host, `NETWORK_PASSPHRASE` confirmed as public
   mainnet, and the `MXN` currency declared with a `status`. Until then the
   issuer identity is unverifiable from its own source.
2. **The on-chain market deepens** — enough that the structural loss floor
   falls below the acceptability bar at a usable transfer size, and ideally an
   `ANCHOR_QUOTE_SERVER` (SEP-38) exists to price the anchor leg.

---

## How to reproduce

```bash
# Issuer account, flags and home_domain
curl -s https://horizon.stellar.org/accounts/GBUMQHWIQELILQEQ5YEEHUFR6SRLBNRKWHJ3JX7JBRFONG24FWUDG627

# Asset footprint (holders, amount)
curl -s "https://horizon.stellar.org/assets?asset_code=MXN&asset_issuer=GBUMQHWIQELILQEQ5YEEHUFR6SRLBNRKWHJ3JX7JBRFONG24FWUDG627"

# SEP-1 resolution (fails: cert mismatch, then 404)
curl -sv https://pagos.saldo.mx/.well-known/stellar.toml

# Path data, strict-send 100 USDC -> MXN(saldo)
curl -s "https://horizon.stellar.org/paths/strict-send?source_asset_type=credit_alphanum4&source_asset_code=USDC&source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN&source_amount=100&destination_assets=MXN%3AGBUMQHWIQELILQEQ5YEEHUFR6SRLBNRKWHJ3JX7JBRFONG24FWUDG627"
```

---

## Related

- Issue [#61](https://github.com/Wayfare-labs/wayfare/issues/61) — this
  research task
- Issue [#33](https://github.com/Wayfare-labs/wayfare/issues/33) — the
  reference standard for detail
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and the DIRECT / DERIVATIVE / NO-MARKET integrity states
- [`docs/parallel-rate-research.md`](parallel-rate-research.md) — the format
  this write-up follows
