# Research: does a usable USDC → UGX (Ugandan Shilling) corridor exist on Stellar?

**Status: completed.** Investigated every UGX-coded issuer visible on the public network. **Finding: NO-MARKET.** No verified Stellar-native Ugandan shilling (UGX) issuer exists — no valid SEP-1 `stellar.toml` declaring a live redeemable anchor asset, no SEP-38 quote server, and no honest Horizon path liquidity.

> Written to the standard set by initiative C1 (Corridor research) and [`docs/corridor-research-template.md`](corridor-research-template.md).
> Answers issue [#677](https://github.com/Wayfare-labs/wayfare/issues/677). All lookups performed live against Horizon (`https://horizon.stellar.org`) and issuers' endpoints on **2026-11-22**.

---

## Why this matters

Uganda is one of East Africa's core economies, receiving over $1.2B annually in formal remittances from the diaspora, with significant additional regional trade flows from Kenya, South Sudan, and Rwanda. Domestic retail transactions are overwhelmingly dominated by mobile money (MTN Mobile Money and Airtel Money).

Following research into Kenya's KESC (which was determined to be NO-MARKET with an unbacked order book), the question in issue [#677](https://github.com/Wayfare-labs/wayfare/issues/677) was whether East African remittance corridors beyond Kenya — specifically Uganda (UGX) — have functioning on-chain anchors.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), an asset code identifies nothing: the issuer account is the identity. A corridor is verified only when the issuer publishes a valid SEP-1 `stellar.toml` declaring the asset on mainnet.

---

## Candidate issuers and identity verification

Horizon asset search (`/assets?asset_code=UGX`) returned **25 distinct issuing accounts**.

Primary candidates ranked by trustlines and on-chain visibility:

| Code | Issuer account | `home_domain` | Status / SEP-1 Check (2026-11-22) |
|:---|:---|:---|:---|
| UGX | `GANDGDF7ZHF7RVXMI53PUSCMVWEFANGTZ4RLEH2DGPDFI5BGWYOLAXRR` | `sendwise.org` | **No `stellar.toml`** — HTTP 404 / connection failure on `/.well-known/stellar.toml` |
| UGX | `GBANKIPYLOQ22HODF7RSQXB6Y3X46USJPSEFTOISSNQZPU6EKCRKACYU` | `starforex.co` | Domain unreachable; speculative forex token with no redemption |
| UGX | `GBANKU6LWIFVS7WR4ZX3ZF7W56UZ5ZBNWETPKDUJIVEBIZALLM5SPQA2` | `galtbank.com` | Speculative meme token; no fiat anchor facilities |
| UGX | `GADSZSZVMKIPCEVMP6SAHCPIUZ53GLRJPWY2ZLDD75I7RX6FWRBAGOVZ` | `fednow.us.org` | **Fraudulent / spoof cluster** — generic multi-currency spam issuer |
| UGX | `GATAHISWYJ4CYX5KQW2DXL2MEN7VS2NTUZCLKCVNR2GHPQXGWMDVFHZE` | `fiat.stellarpredict.com` | Prediction token issuer; no fiat backing |
| UGX | Multiple (12+ accounts) | *(none set)* | Anonymous issuing accounts without domain registration |

None of the candidates represent a verifiable fiat anchor or financial institution in Uganda.

---

## Horizon path data (USDC → UGX)

Source USDC: Circle's mainnet issuing account `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Probing `/paths/strict-receive` from USDC to UGX:
- For legitimate anchors: **0 paths exist**.
- For speculative / spoof tokens: sparse synthetic paths exist through low-liquidity XLM order books:
  - Reference mid USD/UGX (2026-11-22 via open.er-api.com): **4,048.22 UGX / USD**.
  - Quoted rate on speculative books: trades at erratic pennies with > 98% loss against the official Bank of Uganda interbank mid.
  - No two-sided book depth or liquidity pool exists.

---

## SEP-38 (Quote Server) check

No candidate issuer publishes an `ANCHOR_QUOTE_SERVER` in their `.well-known/stellar.toml`. Programmatic quoting is non-existent.

---

## Finding & Conclusion

**Finding: NO-MARKET.**
Similar to KESC in Kenya, there is currently no operational, verified fiat anchor for the Ugandan shilling (UGX) on the Stellar public network.

---

## Recommendation

Do not register a UGX corridor in `asset/known.go`.
Re-evaluation criteria: A regulated Ugandan financial institution or mobile money aggregator (such as MTN MoMo Uganda, Airtel Money Uganda, or Chipper Cash) publishes a conforming SEP-1 `stellar.toml` declaring a 1:1 backed UGX asset with SEP-38 or live DEX market making.
