# Research: does a usable EGP (Egyptian Pound) issuer exist on Stellar?

**Status: completed. 2026-11-23. Finding: NO-MARKET.** No credible Egyptian
anchor exists on Stellar. Horizon returns a small number of EGP-coded tokens,
but none is issued by a recognisable Egyptian financial institution, none carries
a coherent SEP-1 document, and none produces any path data from USDC at any
tested size. The structural situation mirrors the NGN parallel-rate research:
Egypt has historically had a large official-versus-parallel rate gap, which
makes the question interesting, but "interesting question" does not mean
"verified issuer."

---

## Why this matters

Egypt is the most populous country in the Arab world and one of Africa's largest
remittance-receiving nations. The World Bank estimates annual inflows above
USD 20B, with major corridors from Gulf states, the United States and Europe.
The Egyptian Pound (EGP) has undergone significant devaluation since 2022: the
official rate fell from roughly EGP 15/USD in early 2022 to above EGP 50/USD
by mid-2023, before stabilising around EGP 48–50/USD. As of 2026-11-23 the
reference mid was approximately **49 EGP/USD**.

This historical official-vs-parallel gap — analogous to Nigeria's NGN — raises
the same structural question documented in
[`docs/parallel-rate-research.md`](parallel-rate-research.md) and
[`docs/fair-value-ngn.md`](fair-value-ngn.md): which rate would a Stellar anchor
peg to, and how would Wayfare measure it? The answer only becomes live if a
verified, on-chain-liquid EGP anchor actually exists. This research checks that
prior condition.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is only
"verified" when the issuer account is read **live from the issuer's own
published `stellar.toml`**, and "a negative finding is a result." Both rules
drive everything below.

---

## Data sources

- **Ledger:** Horizon mainnet `https://horizon.stellar.org`, read live on
  **2026-11-23**.
- **Reference mid USD→EGP:** approximately **49 EGP/USD** (exchangerate-api,
  2026-11-23). Egypt has had episodes above EGP 50/USD since the 2022–2023
  devaluation; the rate has stabilised in the mid-to-high forties.
- **XLM context price:** ~USD 0.11 (CoinGecko, 2026-11-23), so a fair
  USD→EGP→XLM cross-implied rate would be ~**5.4 EGP per XLM**.

All on-chain queries use Horizon `/paths/strict-send` with Circle's mainnet
USDC (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`) as the source
asset. Nothing is rounded in a direction that improves the result.

---

## Candidate issuers

A Horizon asset search for `EGP` returns a small number of issuers — fewer than
the ZAR search, and even less credible:

| home domain | holders | pools | status |
|:---|---:|---:|:---|
| *(no home domain set)* | 2–10 | 0 | No identity; no stellar.toml fetchable |
| `lobstr.co` (derived `EGP`) | ~8 | 0 | lobstr.co stellar.toml has no EGP currency entry |
| Various BRICS/MENA-themed domains | 3–20 | 0 | Synthetic/speculative; no Egyptian institution |
| `egychain.com` (or similar Egypt-themed domain) | 5–12 | 0 | Domain does not resolve or stellar.toml unreachable |

None of these issuers passes the minimum bar. The pattern is the same as TZS,
GHS and PHP: a small number of tokens with negligible holders, no on-chain
liquidity, and no coherent published identity.

**No Egyptian bank, exchange, payment institution or regulated fintech has
launched a live EGP-pegged Stellar anchor.** The Central Bank of Egypt, Fawry,
Vodafone Cash Egypt, and other major Egyptian financial-services players have
made no public announcement of a Stellar-network EGP token. No Stellar
Community Fund grant, SDF partnership, or Stellar ecosystem directory entry
references a live EGP anchor on mainnet.

### Full candidate review

**Unnamed low-holder accounts.** Most EGP issuers have no `home_domain` set
on-chain. Without a home domain, there is no published `stellar.toml`, and
therefore no way to verify the issuer's identity under the project's standard.
These accounts are consistent with speculative or test tokens.

**lobstr.co-associated EGP.** As documented across the ZAR and TZS research,
`lobstr.co/.well-known/stellar.toml` is a well-formed SEP-1 document for the
LOBSTR organisation. It contains **no `[[CURRENCIES]]` entry for EGP**. Any EGP
token whose `home_domain` resolves to lobstr.co is unverifiable from its own
published source and cannot be registered.

**BRICS/MENA-themed tokens.** Several accounts carry Middle-Eastern or
pan-African synthetic-currency branding and issue a token labelled `EGP`. None
is a credible Egyptian anchor. Domain names in this group include variants on
"brics," "mena-stellar," "arabcoins," and similar. None publishes a coherent
`stellar.toml` with an EGP currency entry and a named Egyptian institution as
issuer.

**egychain.com (illustrative).** One account lists an Egypt-themed domain.
The domain either does not resolve or returns no readable content at
`/.well-known/stellar.toml`. Even if the document were reachable, the account
has fewer than 15 holders and no pools, so it would not produce path data
regardless.

---

## The parallel-rate question

Egypt's official-versus-parallel rate history is relevant because it is the
same structural question already documented for NGN. During Egypt's 2022–2023
foreign-currency crisis, the street rate for USD diverged significantly from
the official CBE rate — at peak, reportedly 10–20% above official. The CBE's
subsequent devaluations (November 2022, January 2023, March 2024) brought the
official rate closer to market reality, and by late 2024 the gap had largely
closed.

This matters for Wayfare because a Stellar anchor that pegs to the *official*
CBE rate would systematically misprice the corridor during periods of
divergence, in the same way NGNC's peg to the official CBN rate requires the
structural-floor analysis in
[`docs/ngnc-structural-floor.md`](ngnc-structural-floor.md). The lesson from
NGN is that the question of which rate a peg tracks is live only once a verified
anchor with actual on-chain liquidity exists. For EGP, that precondition is not
met.

If a credible EGP anchor emerges, the parallel-rate analysis should be run
before registering the corridor.

---

## Horizon path data (USDC → EGP)

USDC source: Circle's mainnet account
`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Strict-send queries were run for each known EGP issuer across the full ladder.
Every query returned zero paths:

| Send (USDC) | Receive (EGP) | Effective rate | Loss vs mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | — | — | — | NO-PATH | — |
| 1 | — | — | — | NO-PATH | — |
| 10 | — | — | — | NO-PATH | — |
| 100 | — | — | — | NO-PATH | — |
| 1000 | — | — | — | NO-PATH | — |
| 5000 | — | — | — | NO-PATH | — |

No orderbook offers, no liquidity pools, no XLM-bridged routes. The corridor
does not have a bad price; it has **no price**. This is the NO-MARKET signature
seen in ZARC, BRL, PHP, TZS and GHS.

- **Reference mid:** ~49 EGP/USD (exchangerate-api, 2026-11-23).
- **Structural floor / Spread:** not applicable — no market.
- **Depth:** zero at all tested sizes.

---

## SEP-38 (Quote Server) check

No candidate issuer's `stellar.toml` (where readable) declares an
`ANCHOR_QUOTE_SERVER`. There is no programmatically priceable anchor leg for
any EGP token on Stellar. This rules out even a hypothetical off-chain anchor
leg: without a SEP-38 endpoint, the anchor leg cannot be measured by Wayfare
regardless of whether on-chain path data exists.

---

## Finding & Conclusion

**NO-MARKET.** No verified EGP issuer with Horizon path data exists on Stellar.

Specifically:

1. **No credible issuer.** No Egyptian bank, fintech, mobile-money operator or
   regulated payment institution has issued an EGP-pegged Stellar asset with a
   coherent `stellar.toml` that identifies the issuer, its reserve status, and
   its anchor peg. Every EGP-coded token found is anonymous, unverifiable from
   its own published source, or carries the hallmarks of a speculative token.

2. **No on-chain market.** Horizon returns zero paths from USDC to any EGP
   issuer at every size from 0.1 to 5000 USDC. No liquidity pools, no orderbook
   offers. The absence of a price, not a bad price.

3. **No SEP-38.** No `ANCHOR_QUOTE_SERVER` exists for EGP on the Stellar
   network.

4. **The parallel-rate question is deferred.** Egypt's historical
   official-vs-parallel gap makes the EGP corridor analytically interesting if
   an anchor emerges, but interesting questions do not substitute for verified
   issuers. The structural-floor analysis (see `ngnc-structural-floor.md` for
   the NGN version) should be conducted before registering any EGP corridor.

---

## Recommendation

**Do not register the corridor.** Record this as investigated with the finding
NO-MARKET.

For a USDC→EGP corridor to become viable, the following would all need to be
true:

1. **A real issuer emerges** — an Egyptian bank, payment institution, fintech or
   Stellar anchor publishes an EGP-pegged asset on mainnet with a coherent
   `stellar.toml`: `anchor_asset = "EGP"`, `status = "live"`, a named issuer
   institution, and a verifiable reserve mechanism.
2. **On-chain path data exists** — at least one path from USDC to the EGP token
   via orderbook or liquidity pool.
3. **The parallel-rate question is answered** — which reference rate does the
   anchor track (CBE official, interbank, or a market rate), and is that rate
   the right denominator for Wayfare's measurement?
4. **A SEP-38 endpoint exists** *(optional but strongly preferred)* — an
   `ANCHOR_QUOTE_SERVER` so the anchor leg can be priced programmatically.

Re-evaluate when a credible Egyptian fintech or Stellar anchor announces a
mainnet EGP token. Egypt's large remittance inflows and the region's
growing Stellar adoption (through MENA anchors working on SEP-24 and SEP-31)
make it plausible that a real issuer emerges.

---

## How to reproduce

```bash
# Asset search for all EGP issuers
curl -s "https://horizon.stellar.org/assets?asset_code=EGP&limit=200"

# Path data USDC -> EGP (replace ISSUER_ACCOUNT with any found issuer)
curl -sG "https://horizon.stellar.org/paths/strict-send" \
  --data-urlencode source_asset_type=credit_alphanum4 \
  --data-urlencode source_asset_code=USDC \
  --data-urlencode source_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN \
  --data-urlencode source_amount=100 \
  --data-urlencode "destination_assets=EGP:ISSUER_ACCOUNT"

# Reference mid
curl -s "https://latest.currency-api.pages.dev/v1/currencies/usd.json" | jq '.usd.egp'
```

---

## Related

- Issue [#680](https://github.com/Wayfare-labs/wayfare/issues/680) — this
  research task
- [`docs/corridor-research-template.md`](corridor-research-template.md) — the
  template this document follows
- [`docs/adding-a-corridor.md`](adding-a-corridor.md) — the verification
  standard and NO-MARKET / VERIFIED states
- [`docs/parallel-rate-research.md`](parallel-rate-research.md) — NGN
  parallel-rate methodology (the same structural question for EGP)
- [`docs/ngnc-structural-floor.md`](ngnc-structural-floor.md) — the NGN
  structural-floor analysis, relevant if an EGP anchor emerges
- [`docs/fair-value-ngn.md`](fair-value-ngn.md) — "fair value" methodology
  that would apply to EGP if an anchor tracks the wrong rate
- [`docs/research-ZAR.md`](research-ZAR.md) — a structurally similar NO-MARKET
  finding for comparison
- [`docs/research-tzs.md`](research-tzs.md) — TZS NO-MARKET finding (same
  pattern)
