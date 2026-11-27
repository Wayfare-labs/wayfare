# Corridor Research Template

**Status:** [completed / in-progress / blocked]. **Finding:** [NO-MARKET / VERIFIED / UNVERIFIABLE / UNUSABLE].

> This document serves as a standardized template for conducting and publishing corridor research in Wayfare. It implements the standard set by initiative C1 (Corridor research) to ensure that answers across corridors are comparable and cheap to replicate.

All on-chain figures below were read live from Horizon (`https://horizon.stellar.org`) on **YYYY-MM-DD**. The reference mid was read from the project's existing rate provider the same day.

## Using the research tool

`cmd/corridor-research` pre-fills the evidence sections below automatically.
Run it once to get a skeleton with every live fact, source URL and timestamp
already in place:

```sh
go run ./cmd/corridor-research CODE ISSUER
```

For example:

```sh
go run ./cmd/corridor-research NGNC GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6
```

The tool fetches the issuer account from Horizon, resolves and verifies
`stellar.toml`, probes SEP-10, SEP-24 and SEP-38, and measures strict-send
paths at three sizes (1 / 100 / 1000 USDC) plus the direct order book.
Redirect its output to a new file, then fill in **Finding & Conclusion** and
**Recommendation** from the evidence it prints.

The tool never decides the finding — it prints evidence. The researcher writes
the conclusion.

---

## Why this matters

Explain the economic or remittance significance of the corridor, why measuring it matters, and what hypothesis is being tested.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), a corridor is only "verified" when the issuer account is read live from the issuer's own published `stellar.toml`. An asset code identifies nothing; the issuer account is the identity.

---

## Candidate issuers and identity verification

Enumerate candidates found via Horizon asset search (`/assets?asset_code=CODE`). For the primary candidate:

- **Issuer account:** `<Stellar Public Key>`
- **Declared home domain:** `example.com` (read from `home_domain` on Horizon)
- **Account auth flags:** Read via `issuer.auth-flags` or Horizon (`auth_required`, `auth_revocable`, `auth_immutable`, `auth_clawback_enabled`)
- **Authorized holders & amount outstanding:** Read from Horizon asset stats.

### SEP-1 (`stellar.toml`) check

Fetch `https://<home_domain>/.well-known/stellar.toml`.

- Does the TLS certificate match the host?
- Is the document readable (HTTP 200)?
- Does it contain a valid `[[CURRENCIES]]` entry matching the asset code and issuer public key, with a declared `status` and `anchor_asset`?

---

## Horizon path data (USDC → [Asset])

USDC issuer used as the source: Circle's mainnet account `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

State whether paths exist (DIRECT via orderbook/liquidity pools or DERIVATIVE via intermediate bridge assets like XLM). Report ladder results across trade sizes:

| Send (USDC) | Receive ([Asset]) | Effective rate ([Asset]/USDC) | Loss vs mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | ... | ... | ... | ... | ... |
| 1 | ... | ... | ... | ... | ... |
| 10 | ... | ... | ... | ... | ... |
| 100 | ... | ... | ... | ... | ... |
| 1000 | ... | ... | ... | ... | ... |
| 5000 | ... | ... | ... | ... | ... |

- **Reference mid:** `[Rate]` via `[Provider]`, as of `[Timestamp]`.
- **Structural floor / Spread:** Describe the zero-size loss or baseline friction.
- **Depth:** Describe where liquidity exhausts or paths disappear.

---

## SEP-38 (Quote Server) check

Check whether an `ANCHOR_QUOTE_SERVER` is declared in the `stellar.toml` and whether it responds successfully to info/quote requests.

---

## Finding & Conclusion

Summarize whether the corridor is **NO-MARKET**, **VERIFIED**, **UNVERIFIABLE**, or **UNUSABLE**.

Every measurement cited carries its source and the date it was checked. Negative or inconclusive findings are reported as valid results.

---

## Recommendation

State clearly what action the project should take (e.g., do not register, register in `asset/known.go`, etc.) and what would need to change for a re-evaluation.
