# Research: does a usable TZS (Tanzanian Shilling) issuer exist on Stellar?

**Status: completed. 2026-11-23. Finding: NO-MARKET.** No credible Tanzanian
anchor exists on Stellar. A Horizon asset search for `TZS` returns a handful of
tokens, but none is issued by a recognisable Tanzanian financial institution,
none carries a coherent SEP-1 document, and none produces any Horizon path data
from USDC at any tested size.

This follows the same pattern as the BRL, PHP, and GHS findings: a token code
appearing on the ledger is not a corridor. No issuer found that can be verified
means the corridor is not added.

---

## Why this matters

Tanzania is one of East Africa's larger economies and a meaningful remittance
destination: the World Bank places annual inflows at roughly USD 500M–600M,
with corridors from the Gulf, Europe and North America. The Tanzanian Shilling
(TZS) is the domestic currency; the reference mid on 2026-11-23 was
approximately **2660 TZS/USD**.

Wayfare already measures the USDC→NGNC and USDC→KESC African-fiat corridors and
has investigated UGX and KES. TZS is the same regional question: is there a
Stellar-native on-ramp to the shilling that could be measured and reported?

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is only
"verified" when the issuer account is read **live from the issuer's own
published `stellar.toml`**, and "a negative finding is a result." Both rules
drive everything below.

---

## Data sources

- **Ledger:** Horizon mainnet `https://horizon.stellar.org`, read live on
  **2026-11-23**.
- **Reference mid USD→TZS:** approximately **2660 TZS/USD** (currency-api,
  `usd.tzs`, 2026-11-23).
- **XLM context price:** ~USD 0.11 (CoinGecko, 2026-11-23), so a fair
  USD→TZS→XLM cross-implied rate would be ~**293 TZS per XLM**.

All on-chain queries use Horizon `/paths/strict-send` with Circle's mainnet
USDC (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`) as the source
asset — the same `route.Engine` endpoint the product uses. Nothing is rounded in
a direction that improves the result.

---

## Candidate issuers

A Horizon asset search for `TZS` returns a small number of issuers. None meets
even the minimum bar of a believable Tanzanian institution:

| home domain | holders | pools | status |
|:---|---:|---:|:---|
| *(no home domain set)* | 2–8 | 0 | Unnamed accounts; no domain, no identity |
| `lobstr.co` (derived `TZS`) | ~12 | 0 | lobstr.co stellar.toml has no TZS currency entry; unverifiable |
| Various BRICS-themed domains | 3–15 | 0 | BRICS/synthetic themes; no connection to Tanzania |

The pattern is familiar from ZAR and PHP: a small number of issued tokens, most
with negligible holders, no pools, and no coherent SEP-1 document that claims a
Tanzanian Shilling peg from a credible issuer.

**No Tanzanian bank, mobile-money operator, payment institution or Stellar
anchor has launched a TZS-pegged token on Stellar mainnet** that this research
could locate. The Central Bank of Tanzania has made no public announcement of a
Stellar partnership. No Stellar Community Fund grant, SDF announcement or
partnership press release references a TZS anchor.

### Full candidate review

**Unnamed low-holder accounts.** The handful of TZS issuers with 2–8 holders
and no home domain set fail the first check: without a `home_domain` on the
account there is no published `stellar.toml` to fetch, and therefore no way to
verify the issuer's identity under `adding-a-corridor.md`. These are consistent
with test accounts or speculative tokens never brought to market.

**lobstr.co-associated TZS.** One account lists `lobstr.co` as its home domain.
As documented in the ZAR research, `lobstr.co/.well-known/stellar.toml` is a
large, well-formed SEP-1 document that describes the LOBSTR organisation and its
validators — it contains **no `[[CURRENCIES]]` entry for TZS**. The token
therefore cannot be verified from its own published source, disqualifying it
under the project's standard.

**BRICS/synthetic tokens.** Several accounts carry domains with BRICS or
synthetic-currency themes and claim a `TZS` code. None of these domains serves
a coherent `stellar.toml` with a TZS currency entry. None has a meaningful
holder count. None is plausibly a Tanzanian institution.

---

## Horizon path data (USDC → TZS)

USDC source: Circle's mainnet account
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Strict-send queries were run for each known TZS issuer across the full ladder.
Every query returned zero paths:

| Send (USDC) | Receive (TZS) | Effective rate | Loss vs mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | — | — | — | NO-PATH | — |
| 1 | — | — | — | NO-PATH | — |
| 10 | — | — | — | NO-PATH | — |
| 100 | — | — | — | NO-PATH | — |
| 1000 | — | — | — | NO-PATH | — |
| 5000 | — | — | — | NO-PATH | — |

No orderbook offers, no liquidity pools, and no indirect (XLM-bridged) paths
exist for any TZS issuer on Horizon. The corridor does not merely have a poor
price; it has **no price**. This is the KESC/ZARC signature: NO-MARKET.

- **Reference mid:** ~2660 TZS/USD (currency-api, 2026-11-23).
- **Structural floor / Spread:** not applicable — no market.
- **Depth:** zero at all tested sizes.

---

## SEP-38 (Quote Server) check

No candidate issuer's `stellar.toml` (where one was readable at all) declared
an `ANCHOR_QUOTE_SERVER`. There is no programmatically priceable anchor leg for
any TZS token on Stellar. This rules out even a hypothetical off-chain anchor
leg: without a SEP-38 endpoint, the anchor leg cannot be measured by Wayfare
regardless of whether on-chain path data exists.

---

## Finding & Conclusion

**NO-MARKET.** No verified TZS issuer with Horizon path data exists on Stellar.

Specifically:

1. **No credible issuer.** No Tanzanian financial institution, mobile-money
   operator or regulated payment entity has published a TZS-pegged asset on
   Stellar mainnet with a coherent `stellar.toml` identifying the issuer, its
   reserve status, and its anchor peg. Every TZS-coded token found is either
   anonymous, unverifiable from its own published source, or carries the
   hallmarks of a speculative or test token.

2. **No on-chain market.** Horizon returns zero paths from USDC to any TZS
   issuer at every size from 0.1 to 5000 USDC. There are no liquidity pools and
   no orderbook offers. This is not a bad price; it is the absence of any price.

3. **No SEP-38.** No `ANCHOR_QUOTE_SERVER` exists for TZS on the Stellar
   network. The off-chain anchor leg cannot be priced programmatically.

The finding contrasts with ZAR, where a real, SEP-1-verified issuer (TD Markets'
ZARC) exists but has no on-chain market. For TZS, the situation is simpler: no
real issuer exists at all.

---

## Recommendation

**Do not register the corridor.** Record this as investigated with the finding
NO-MARKET.

For a TZS corridor to become viable, the following would all need to be true:

1. **A real issuer emerges** — a Tanzanian bank, mobile-money provider (e.g.,
   M-Pesa Tanzania, Airtel Money Tanzania), fintech or Stellar anchor publishes
   a TZS-pegged asset on mainnet with a coherent `stellar.toml` that identifies
   the issuer, its reserve, and its anchor peg as `anchor_asset = "TZS"` with
   `status = "live"`.
2. **On-chain path data exists** — at least one path from USDC to the TZS token
   via orderbook or liquidity pool, so that Horizon's `/paths/strict-send`
   returns a result.
3. **A SEP-38 endpoint exists** *(optional but strongly preferred)* — an
   `ANCHOR_QUOTE_SERVER` in the `stellar.toml` so the anchor leg can be priced
   programmatically.

Re-evaluate when any credible Tanzanian anchor announces a Stellar mainnet
launch.

---

## How to reproduce

```bash
# Asset search for all TZS issuers
curl -s "https://horizon.stellar.org/assets?asset_code=TZS&limit=200"

# Path data USDC -> TZS (replace ISSUER_ACCOUNT with any found issuer)
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=100 \
  --data-urlencode "destination_assets=TZS:ISSUER_ACCOUNT"

# Reference mid
curl -s "https://latest.currency-api.pages.dev/v1/currencies/usd.json" | jq '.usd.tzs'
```

---

## Related

- Issue [#678](https://github.com/Wayfare-labs/wayfare/issues/678) — this
  research task
- [`docs/corridor-research-template.md`](corridor-research-template.md) — the
  template this document follows
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and NO-MARKET / VERIFIED states
- [`docs/research-ZAR.md`](research-ZAR.md) — a structurally similar finding
  with a real issuer but no on-chain market
- [`docs/research-BRL.md`](research-BRL.md) — the BRL NO-MARKET finding, same
  pattern
- [`docs/research-GHS.md`](research-GHS.md) — the GHS UNUSABLE finding for
  comparison
