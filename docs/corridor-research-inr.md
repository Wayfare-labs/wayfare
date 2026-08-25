# Research: does a usable USDC → INR (Indian Rupee) corridor exist on Stellar?

**Status: completed.** Investigated every INR-coded issuer visible on the public
network. **Finding: NO-MARKET.** No verified Stellar-native INR issuer exists —
no valid SEP-1 `stellar.toml` declaring a live INR asset, no SEP-38 quote
server, and no honestly-priced Horizon path liquidity. Every INR token on the
public network traces to a "BRICS" / "currency reset" / "revaluation" /
staking-reward cluster, or reuses an unrelated domain it does not control.

> Written to the standard set by [#33](https://github.com/Wayfare-labs/wayfare/issues/33).
> This corridor was requested in
> [#62](https://github.com/Wayfare-labs/wayfare/issues/62). No code changes —
> this is a Layer-1 observable-fact write-up.

---

## Why this matters

India is the world's largest remittance-receiving country, and USDC → INR is
the single largest fiat corridor by volume globally. If a real INR issuer
existed on Stellar with honest path data, this corridor would be extremely
high-value. Precisely because the market is so large, a NO-MARKET finding is
itself notable: it says the on-chain rails do not yet exist for the corridor
that would matter most.

The bar for "exists" here is the same one `docs/adding-a-corridor.md` sets for
every corridor: **the issuer account is the identity, the asset code is only a
label on it.** An asset called `INR` proves nothing. What counts is an issuer
that publishes a valid SEP-1 `stellar.toml` — on the public network passphrase
— declaring the INR asset it issues, ideally with an `ANCHOR_QUOTE_SERVER`
(SEP-38) so its own rails can be priced. None of the candidates clear that bar.

---

## Method

All lookups were performed live against Horizon and the issuers' own published
documents on **2026-08-25**. Nothing below is copied from a block explorer,
wallet asset list, or blog post.

1. **Enumerate candidates** — `GET https://horizon.stellar.org/assets?asset_code=INR`
   (and the variants `inr`, `INRC`, `INRT`, `INRx`).
2. **Resolve identity** — for each serious candidate,
   `GET https://horizon.stellar.org/accounts/<issuer>` to read its
   `home_domain` and auth flags.
3. **SEP-1 check** — fetch `https://<home_domain>/.well-known/stellar.toml`
   and check `NETWORK_PASSPHRASE`, the `[[CURRENCIES]]` INR entry, and
   `ANCHOR_QUOTE_SERVER`.
4. **Horizon path data** — `GET /paths/strict-receive` from USDC
   (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`, Circle) to each
   INR issuer, plus order-book and trade history on the DEX.
5. **SEP-38 check** — look for a quote server on any candidate.

---

## What the network shows

`assets?asset_code=INR` returns a **full page of 50 distinct issuing accounts**
(the response is capped at 50, so there are at least that many). The code is
common and cheap; none of that implies a real anchor. Ranked by trustlines, the
serious-looking candidates and what they actually publish:

| Issuer (abbrev.) | Trustlines | `home_domain` | SEP-1 result |
|:---|---:|:---|:---|
| `GB3OE4…AUT2A` | 6,890 | `swisscustody.org` | **No `stellar.toml`** — domain serves a Hostinger website-builder HTML page |
| `GBNV3VPJ…DUAE` | 329 | `uae-og.com` | Domain does not resolve / no HTTPS |
| `GA3S5IFY…E4QS` | 289 | `bricspay.live` | Domain does not resolve / no HTTPS |
| `GANLK2VP…SNEW` | 285 | *(none set)* | Anonymous — no identity to verify |
| `GB5C4E47…2XLM` | 209 | `bricsstellar.com` | Domain does not resolve / no HTTPS |
| `GAHZ5Y4F…OVDN` | 185 | `ndb.finance` | Domain does not resolve / no HTTPS |
| `GBMBHUIN…ZSXU` | 178 | `currenciesglobalreset.com` | Domain does not resolve / no HTTPS |
| `GAIF52QZ…GAUD` | 72 | `audrev-stellar.com` | Serves a `stellar.toml`, but a **fraudulent** one (see below) |
| `GB77G5VY…VDIN` | 67 | `inmetals.in` | Domain does not resolve / no HTTPS |
| `GBUYUAI7…KOFU` | 45 | `funtracker.site` | Reward/"staking" site — no anchor toml |
| `GBKOQVSN…PNMR` | 36 | `qmsn.xyz` | Throwaway domain — no anchor toml |
| `GBHBNH44…L54J` | 30 | `visy-staking.com` | "Staking" site — no anchor toml |
| `GB32RVOX…RTIJ` | 22 | `x.token.io` | `token.io` is an unrelated UK fintech; returns HTTP 403, no toml |
| `GC6QLTHZ…OQ7` | 87 | `uaebricsglobal.com` | "BRICS global" cluster — no anchor toml |

The pattern is uniform: the INR supply on Stellar is dominated by
"BRICS payment" / "currency global reset" / "revaluation" / staking-reward
projects — the same genre of asset that names a national currency to imply a
peg it does not hold.

### The one issuer that does publish a toml — and why it fails anyway

`audrev-stellar.com` (issuer `GAIF52QZ…GAUD`) is the only candidate that serves
a parseable `stellar.toml`. It is disqualifying on its face: a **single
account** declares itself the issuer of `XLM`, `XRP`, `BTC`, `ETH`, `USDT`,
`USDC`, `RLUSD` **and** `INR` simultaneously. Its INR entry reads:

```toml
[[CURRENCIES]]
code="INR"
issuer="GAIF52QZUPYCADXF7I7RNPMED7DT2B5JGPR7DEHCC5TPDPUJTMLGGAUD"
name="AUD Indian Rupee"
desc="AUD-issued digital asset intended to maintain a 1:1 reference value with the Indian Rupee (INR)."
```

"Intended to maintain a 1:1 reference value" is an aspiration, not a backing.
An organisation that issues Bitcoin, Ether and USDC from one Stellar key is not
an INR anchor. There is **no `ANCHOR_QUOTE_SERVER`** in the document, so even on
its own terms it publishes no SEP-38 quote server.

---

## Horizon path data — paths exist, but they are phantom

Path data is not absent; it is worse than absent, because it looks like a market
until you read the price. `strict-receive` for **100 INR** from USDC returns:

- **`GB3OE4…AUT2A`** (swisscustody, the 6,890-trustline leader):
  source amount **0.0000001 USDC** for 100 INR. 100 INR is worth roughly
  1.2 USD; a quote of one ten-millionth of a cent means the token is
  effectively worthless on the DEX. The order book carries a handful of stale
  asks and no bids; the most recent INR/XLM trade was **2026-07-02**.
- **`GAIF52QZ…GAUD`** (audrev): source amount **≈19.4 USDC** for 100 INR —
  roughly **16× above** the real ~1.2 USD value. Off-peg in the other
  direction.
- **`GBNV3VPJ…DUAE`** (uae-og): **no path found.**

A corridor priced at 0.0000001 USDC on one side and 19.4 USDC on the other is
not a corridor. There is no coherent USDC → INR price on the network, only
disconnected pools of a mislabelled token.

---

## SEP-38 endpoint check

**No INR issuer on the public network publishes an `ANCHOR_QUOTE_SERVER`.** The
one issuer with a served `stellar.toml` (audrev) omits it; every other candidate
serves no `stellar.toml` at all. There is nothing to query for a SEP-38 quote,
so the anchor leg cannot be priced programmatically for any INR issuer.

---

## Conclusion

**NO-MARKET.** As of 2026-08-25 there is no verified Stellar-native INR issuer.
Concretely, no INR issuer satisfies even the first gate in
`docs/adding-a-corridor.md`:

- **SEP-1:** no issuer publishes a valid `stellar.toml`, on the public network
  passphrase, that honestly declares a live INR asset it backs. The largest by
  trustlines serves no toml at all; the only one that serves a toml is a
  self-issued "revaluation" fraud.
- **SEP-38:** no issuer publishes an `ANCHOR_QUOTE_SERVER`.
- **Horizon path data:** the paths that exist are nonsensically priced
  (0.0000001 USDC or ~19.4 USDC for 100 INR), i.e. phantom liquidity, not a
  usable peg.

This is a genuine finding, not a gap in the search: the corridor that matters
most by global remittance volume has no honest on-chain representation on
Stellar today. **This corridor must not be registered.**

### What would change the finding

A real INR corridor would exist the day an issuer:

1. Publishes a valid SEP-1 `stellar.toml` on `NETWORK_PASSPHRASE =
   "Public Global Stellar Network ; September 2015"`, declaring an INR asset it
   actually backs (`anchor_asset = "INR"`, ISO-4217), from an account whose
   `home_domain` matches the serving domain.
2. Publishes an `ANCHOR_QUOTE_SERVER` (SEP-38) so its off-chain rail can be
   priced.
3. Has Horizon path liquidity that prices USDC → INR near the real ~83 INR/USD
   rate rather than at 7 or 8 orders of magnitude away.

Until then, USDC → INR is a NO-MARKET corridor and is recorded here as one.

---

## Related

- Issue [#62](https://github.com/Wayfare-labs/wayfare/issues/62) — this research
  task.
- [#33](https://github.com/Wayfare-labs/wayfare/issues/33) — the reference
  standard for detail.
- `docs/adding-a-corridor.md` — the verification bar a real corridor must clear.
- `docs/parallel-rate-research.md` — companion NO-SOURCE research write-up,
  same evidence standard.
