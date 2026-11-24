# Research: MoneyGram Stellar cash-out observability

**Status: completed.** **Finding: observable capability metadata, but no
read-only corridor quote or fee was established.**

This check is for the public MoneyGram Stellar anchor only. It used no account,
wallet, KYC process, deposit or withdrawal.

## Published configuration

The MoneyGram Stellar SEP-1 file is published at:

- https://stellar.moneygram.com/.well-known/stellar.toml

It declares public-network USDC and these transfer endpoints:

```toml
TRANSFER_SERVER_SEP0024 =
  "https://stellar.moneygram.com/stellaradapterservice/sep24"
```

It also defines USDC for Circle's mainnet issuer:
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.
The TOML does not declare `ANCHOR_QUOTE_SERVER`, and it has no NGN, GHS or
KES currency entry.

## Read-only endpoint observations

Checked 2026-11-24:

| Endpoint | Observation | Account or KYC needed? |
|:---|:---|:---|
| `/sep24/info` | Returned USDC deposit and withdrawal limits and zero published fixed/percentage fee fields | No for capability metadata |
| `/sep24/fee?operation=withdraw&asset_code=USDC&amount=100` | HTTP 500 | A parameterized fee quote could not be obtained; no account or KYC was created |
| `/sep38/info` | Returned the MoneyGram Anchor UI instead of JSON SEP-38 `/info` | Not established; the SEP-38 quote server was not declared |

The `/sep24/info` JSON reported:

- deposit USDC enabled, 1–950 USDC, fixed fee `0`, percentage fee `0`
- withdrawal USDC enabled, 1–2500 USDC, fixed fee `0`, percentage fee `0`
- fee endpoint enabled

These fields describe fixed limits and anchor-declared fee settings. They are
not an NGN, GHS or KES quote and do not establish an observable cash-out rate.

The `/fee` endpoint's `HTTP 500` result means it did not provide a quote under
the read-only request tested. This repository did not create a wallet/account,
authenticate, submit identity information or initiate a transaction. It also
did not retry in ways that would imply account creation or KYC.

## NGN, GHS and KES

- **NGN:** not published in the anchor TOML or observed from an unauthenticated
  public endpoint.
- **GHS:** not published or observed.
- **KES:** not published or observed.

MoneyGram's Stellar USDC cash-out service can therefore not currently be used
as a measured DEX-vs-cash-out rail for these three corridors by this project's
read-only method.

## Conclusion

**NOT OBSERVABLE.**

The anchor exposes public SEP-24 capability metadata and USDC limits, but it
does not publish NGN, GHS or KES quote data through a declared SEP-38 server,
and the parameterized SEP-24 `/fee` probe did not yield a quote without an
account flow. It should not be recorded as a measured corridor rail.

A later recheck could change the finding if the anchor declares a working
`ANCHOR_QUOTE_SERVER`, publishes NGN/GHS/KES in SEP-38 `/info`, and provides
authenticated or unauthenticated `/price` or `/quote` data that can be
reproduced with the exact KYC and cash-out method disclosed. Until then, this
rail is unmeasured, not zero-cost.
