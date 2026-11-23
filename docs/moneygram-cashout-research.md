# MoneyGram Stellar USDC cash-out observability

**Status:** completed.
**Finding:** Observable as a read-only SEP-24 integration in limited form,
but not observable as an NGN, GHS or KES corridor rail.

No account was created, no KYC was performed, and no transaction was
initiated.

## Identity and published endpoints

MoneyGram's mainnet `stellar.toml` is published at
`https://stellar.moneygram.com/.well-known/stellar.toml`. It declares:

- `TRANSFER_SERVER_SEP0024=https://stellar.moneygram.com/stellaradapterservice/sep24`
- `WEB_AUTH_ENDPOINT=https://stellar.moneygram.com/stellaradapterservice/auth`
- Only USDC issued by Circle's mainnet issuer

The document does **not** declare `ANCHOR_QUOTE_SERVER`, so SEP-38 quote
pricing is not published as a machine-readable service by this anchor.

## Read-only probes

Performed on 2026-11-22:

| Probe | Result |
|:---|:---|
| `GET .../sep24/info` | HTTP 200; USDC deposit and withdraw capability, static limits and fee fields |
| `GET .../sep24/fee` | HTTP 500, `{"code":500,"message":"No static resource sep24/fee."}` |
| Parameterized `/fee` variants | The endpoint remains HTTP 500 |
| `GET .../sep38/info` | HTTP 200 but returned the MoneyGram Anchor UI HTML, not a SEP-38 API |

The `/info` response reports:

- withdraw enabled for USDC: 1–2500 USDC
- fee fixed 0
- fee percent 0
- deposit enabled for USDC: 1–950 USDC
- account creation and claimable balances feature flags

Those values are static capability fields, not evidence of a live cash-out
rate or fee for NGN, GHS or KES.

## Corridors

The observed SEP-24 asset table contains only USDC, not NGNC, GHSC or KESC,
and it contains no NGN, GHS or KES receive or destination asset declaration.
The `/fee` resource is absent for this implementation rather than working
as a public quote endpoint.

Therefore Wayfare cannot currently observe, without account context:

- an executable USDC → NGN, GHS or KES cash-out quote
- a live FX margin
- a live fee for a comparable send amount
- availability or speed for a named destination country or corridor

If the implementation requires interactive account creation or authentication
for a real quote, it is outside this finding's allowed scope.

## Evidence classification

| Question | Answer |
|:---|:---|
| Public TOML | Yes |
| Public SEP-24 `/info` | Yes, USDC only |
| Public SEP-24 `/fee` | No usable endpoint |
| Public SEP-38 quote server | No published `ANCHOR_QUOTE_SERVER` |
| Read-only NGN/GHS/KES quote | Not observable |
| Account or KYC performed | No |

## Sources

- [MoneyGram Stellar TOML](https://stellar.moneygram.com/.well-known/stellar.toml)
- [SEP-24 protocol](https://stellar.org/protocol/sep-24)
- [SEP-38 protocol](https://stellar.org/protocol/sep-0038)
- [MoneyGram Ramps](https://xramps.moneygram.com/)
