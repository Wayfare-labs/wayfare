# Research: does a usable USDC → XOF / XAF (CFA Franc) corridor exist on Stellar?

**Status: completed.** Investigated every XOF and XAF-coded issuer visible on the public network. **Finding: NO-MARKET.** No verified Stellar-native CFA franc (XOF or XAF) issuer exists — no valid SEP-1 `stellar.toml` declaring a live redeemable anchor asset, no SEP-38 quote server, and no honestly-priced Horizon path liquidity.

> Written to the standard set by initiative C1 (Corridor research) and [`docs/corridor-research-template.md`](corridor-research-template.md).
> Answers issue [#676](https://github.com/Wayfare-labs/wayfare/issues/676). All lookups performed live against Horizon (`https://horizon.stellar.org`) and issuers' endpoints on **2026-11-22**.

---

## Why this matters

The CFA franc consists of two regional currencies:
- **XOF** (West African CFA franc), issued by the Central Bank of West African States (BCEAO) for 8 nations in the West African Economic and Monetary Union (UEMOA): Benin, Burkina Faso, Côte d'Ivoire, Guinea-Bissau, Mali, Niger, Senegal, and Togo.
- **XAF** (Central African CFA franc), issued by the Bank of Central African States (BEAC) for 6 nations in the Economic and Monetary Community of Central Africa (CEMAC): Cameroon, Central African Republic, Chad, Republic of the Congo, Equatorial Guinea, and Gabon.

Both currencies share a statutory fixed peg to the Euro (`1 EUR = 655.957 CFA francs`), which makes reference pricing mathematically deterministic given EUR/USD exchange rates. Francophone West and Central Africa is one of the highest-volume remittance destinations in the developing world (over $5B annually in formal remittances, with informal flows estimated to be significantly higher), yet remittance fees through traditional money-transfer operators (MTOs) frequently exceed 6–9%.

If an honest Stellar-native CFA franc anchor existed with liquid USDC paths, this corridor would be uniquely suited for automated monitoring and cross-border settlement.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), an asset code identifies nothing: the issuer account is the identity. A corridor is verified only when the issuer publishes a valid SEP-1 `stellar.toml` declaring the asset on mainnet.

---

## Candidate issuers and identity verification

Horizon asset search (`/assets?asset_code=XOF` and `/assets?asset_code=XAF`) returned:
- **15 distinct issuing accounts for XOF**
- **44 distinct issuing accounts for XAF**

Primary candidates ranked by trustlines and on-chain visibility:

| Code | Issuer account | `home_domain` | Status / SEP-1 Check (2026-11-22) |
|:---|:---|:---|:---|
| XOF | `GBAYRKQPMIUQBI2R7QZLNI7LBYQP3GW7LTYUHNEC4O4C62XCMDRV7USV` | `afripay.finance` | **No `stellar.toml`** — domain returns HTTP 404 on `/.well-known/stellar.toml` |
| XOF | `GADSZSZVMKIPCEVMP6SAHCPIUZ53GLRJPWY2ZLDD75I7RX6FWRBAGOVZ` | `fednow.us.org` | **Fraudulent / spoof cluster** — impersonates US FedNow; serves generic toml with no anchor redemption or reserve backing |
| XOF | `GBBWRCJSZRDRWAOHATTRELQY4HIW32B65ZZQ5NBZQRNVJZHHZ6Y6STHP` | `bankofengland.com.co` | **Fraudulent / spoof cluster** — impersonates Bank of England; no anchor facilities |
| XOF | `GAP5LETOV6YIE62YAM56STDANPRDO7ZFDBGSNHJQIYGGKSMOZAHOOS2S` | `eurt.exchange` | Serves toml, but has no `[[CURRENCIES]]` entry for XOF |
| XOF | `GATAHISWYJ4CYX5KQW2DXL2MEN7VS2NTUZCLKCVNR2GHPQXGWMDVFHZE` | `fiat.stellarpredict.com` | Prediction token issuer; no fiat backing or redemption rails |
| XAF | `GAAUPWPAMBTVA2JEYJTAAPZ5ZMHPZGEXBK6S6BDVOHHP2ACBZTDUKGXE` | `inter-stellar.exchange` | Defunct DEX gateway; `.well-known/stellar.toml` unmaintained with dead deposit/withdraw endpoints |
| XAF | `GARPL3TRNQ4LIWEP4OFRLS7E24W5WGGBAHYYZAVPLEWSMI3VGORBT63A` | `intrestellar.exchange` | Typo/clone domain; unreachable |
| XAF | `GA3GAJQGYTORKIXUYNNI3HM6DUDOCOYWOPEWBMK7KVQ5B4L3RNZGZOTT` | `ppowsa.litemint.store` | NFT store account; no fiat currency anchor |
| XAF | `GAFDI44OQCCYFP6BPRS72PCYO3WB2EBCZGGEXJBV7MIOZ5JYT5FYAJNP` | `stocks.stellarpredict.com` | Synthetic/prediction token; not a redeemable anchor |
| XOF/XAF | Multiple (20+ accounts) | *(none set)* | Anonymous accounts with zero identity verification |

None of the candidates operate a legitimate, verifiable fiat gateway for XOF or XAF.

---

## Horizon path data (USDC → XOF / XAF)

Source USDC: Circle's mainnet issuing account `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Probing `/paths/strict-receive` across candidate issuers:
- For legitimate anchors: **0 paths exist**.
- For speculative / spoof tokens (`fednow.us.org`, `stellarpredict.com`): sparse multi-hop paths exist via illiquid XLM orderbook offers, exhibiting extreme price distortion:
  - Reference mid USD/XOF (2026-11-22 via open.er-api.com): **597.74 XOF / USD** (EUR peg `1 EUR = 655.957 XOF`).
  - Quoted rate on speculative books: < 0.1 XOF / USDC (> 99.9% loss vs reference mid).
  - Depth: entirely vanishes above $5.

---

## SEP-38 (Quote Server) check

No candidate issuer declares an `ANCHOR_QUOTE_SERVER` in their `.well-known/stellar.toml` for XOF or XAF. No programmatic quoting exists.

---

## Finding & Conclusion

**Finding: NO-MARKET.**
Despite the substantial economic need and remittance flows across the 14 CFA franc nations, no verified fiat anchor issues XOF or XAF on Stellar. Tokens bearing these codes are either unanchored speculative experiments or impersonation artifacts.

---

## Recommendation

Do not register an XOF or XAF corridor in `asset/known.go`.
Re-evaluation criteria: A regulated financial institution or licensed mobile money provider in UEMOA or CEMAC (e.g. Wave, Orange Money, MTN MoMo, or a commercial bank) publishes a valid SEP-1 `stellar.toml` declaring an XOF or XAF token with live 1:1 on-chain reserve backing and SEP-38 / SEP-24 support.
