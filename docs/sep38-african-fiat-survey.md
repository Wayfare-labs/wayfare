# Research: do any Stellar anchors publish SEP-38 for African fiat?

**Status: completed.** Surveyed four anchors issuing African-fiat-pegged
Stellar tokens: three were reachable, one was not. Of the three reachable
anchors, one declares `ANCHOR_QUOTE_SERVER`, but its own quote server does not
list the African-fiat asset it issues among the assets it advertises.
**Verdict: no positive result among the three reachable anchors; the fourth
(ClickPesa) remains inconclusive, not negative — its `stellar.toml` could not
be reached at all.** GitHub issue
[#180](https://github.com/Wayfare-labs/wayfare/issues/180) — "a live SEP-38
round-trip has never been performed", tracked as backlog entry `#106` — is not
shown reachable by anything this survey found.

> **A numbering note, checked directly against the tracker rather than
> assumed:** the issue that commissioned this survey (GitHub
> [#182](https://github.com/Wayfare-labs/wayfare/issues/182)) says in its own
> body, quoting `docs/backlog.md`, "Determines whether #106 is reachable at
> all." `docs/backlog.md` numbers backlog entries independently of the
> GitHub issues filed for them, and GitHub issue **#106** is a different,
> closed issue ("Recorded snapshot fixtures for every metric") — not the live
> SEP-38 round-trip question. The backlog entry actually meant is `#106` in
> the backlog's own numbering, filed as GitHub issue **#180**. This document
> uses the GitHub issue numbers throughout and flags the mismatch once, here,
> rather than silently repeating a reference that would point a reader at the
> wrong issue.

---

## Why this matters

`docs/backlog.md` lists backlog entry `#106` — performing a live SEP-38
round-trip, GitHub issue #180 — as `blocked`, on the condition "a corridor
with SEP-38". This project's own case study is three African-fiat tokens
(NGNC, GHSC, KESC) from one issuer that publishes no `ANCHOR_QUOTE_SERVER` at
all (documented in `anchor/anchor.go`'s package comment, checked 2026-08-04).
Before spending effort on #180, or on extending Wayfare to a new
African-fiat corridor, it is worth knowing whether *any* anchor serving this
currency set would even make #180 reachable. This issue is that check, done
once, in the open, with sources and dates, so the answer doesn't need
re-deriving from scratch the next time it comes up.

Per the issue's own constraints: this is a bounded survey, a negative or
inconclusive result is an acceptable and complete answer, and no code changes
are in scope.

---

## Scope and method

"African fiat" here means the four currencies this project's own code already
names as fiat pegs for African-issued Stellar tokens: NGN (Nigerian naira),
GHS (Ghanaian cedi), KES (Kenyan shilling), and ZAR (South African rand) — see
`asset/known.go`'s registry comments.

The anchors checked are:

1. Every anchor currently in `asset/known.go`'s verified registry
   (`asset.Registry()` / `asset.LookupEntry`, keyed by code and issuer
   together) that issues a token pegged to one of those four currencies. This
   is a principled, reproducible boundary rather than an arbitrary one: these
   are exactly the anchors this project could plausibly build a corridor
   against today. (`asset.HomeDomain` is a separate, narrower lookup — issuer
   account to publishing domain, keyed by issuer alone — used later in this
   survey to find each anchor's `stellar.toml`; it is not itself what defines
   registry membership.)
2. One anchor named by public sources (the Stellar Community Fund and an
   anchor's own blog post) as issuing a KES-pegged token, to check whether a
   fourth candidate exists outside the current registry. This is **not** a
   claim to have found every African-fiat Stellar anchor in existence — a
   full census of the ecosystem is a different, much larger project than a
   bounded survey, and this document does not attempt one.

For each, the anchor's own `stellar.toml` was fetched live and checked for
`ANCHOR_QUOTE_SERVER`, per SEP-1 / SEP-38. Where one was declared, its
`/info` endpoint (SEP-38 §"GET /info") was also fetched live, because a
declared quote server is a claim, not a working rate — the same distinction
`checks/sep10_endpoint.go` already draws for SEP-10, and issue
[#183](https://github.com/Wayfare-labs/wayfare/issues/183) generalizes.

**A stated limit of this method:** `/info` only reports which assets an
anchor's quote server *advertises*; it is not itself a price or a quote.
Confirming a rate can actually be obtained would mean going further — a
`GET /price` or `POST /quote` call, per SEP-38 — which this survey did not do.
Every finding below that reads "declares SEP-38" or "lists as quotable" refers
to what `/info` reports, not to a fetched rate; where a currency does not even
appear in `/info`, no further probe could have produced one regardless, so the
gap does not change this survey's answer for the currencies it is actually
about.

All fetches were made 2026-08-30.

---

## Anchors checked

### 1. ngnc.online (LINK.IO) — NGNC, GHSC, KESC (NGN, GHS, KES)

- **Source:** https://ngnc.online/.well-known/stellar.toml, fetched
  2026-08-30.
- **Fields present:** `NETWORK_PASSPHRASE`, `ACCOUNTS`, `SIGNING_KEY`,
  `WEB_AUTH_ENDPOINT` (`https://anchor.ngnc.online/auth`),
  `TRANSFER_SERVER_SEP0024` (`https://anchor.ngnc.online/sep24`),
  `DOCUMENTATION`, `PRINCIPALS`, three `[[CURRENCIES]]` entries.
- **`ANCHOR_QUOTE_SERVER`: absent.** Also absent: `TRANSFER_SERVER`,
  `DIRECT_PAYMENT_SERVER`, `KYC_SERVER`.
- This corroborates the existing finding already recorded in this repository
  (`anchor/anchor.go`'s package doc comment, checked 2026-08-04): the finding
  has not changed in the intervening three weeks.
- **Verdict: no SEP-38.** Covers three of the four currencies (NGN, GHS, KES)
  in this survey.

### 2. cowrie.exchange — NGNT (NGN)

- **Source:** https://cowrie.exchange/.well-known/stellar.toml, fetched
  2026-08-30.
- **Fields present:** `NETWORK_PASSPHRASE`, `FEDERATION_SERVER`,
  `AUTH_SERVER`, `KYC_SERVER`, `TRANSFER_SERVER`, `WEB_AUTH_ENDPOINT`,
  `DIRECT_PAYMENT_SERVER`, `VERSION`, `SIGNING_KEY`, `ACCOUNTS`,
  `DOCUMENTATION`, `PRINCIPALS`, `VALIDATORS`, `CURRENCIES`. In SEP terms:
  SEP-12 (KYC), SEP-6 (programmatic transfer), SEP-10 (web auth) and SEP-31
  (cross-border payment) are all declared.
- **`ANCHOR_QUOTE_SERVER`: absent.** `TRANSFER_SERVER_SEP0024` (SEP-24) is
  also absent.
- **Verdict: no SEP-38**, despite declaring SEP-12, SEP-6, SEP-10 and SEP-31.
  (ClickPesa's fields are unknown, since its `stellar.toml` could not be
  reached; this is a statement about cowrie.exchange's own declarations, not
  a ranking across all four anchors.)

### 3. zeam.money — ZARZ (ZAR); also issues USDZ (USD, out of scope here)

- **Source:** https://zeam.money/.well-known/stellar.toml, fetched
  2026-08-30.
- **Fields present:** `WEB_AUTH_ENDPOINT`, `ANCHOR_QUOTE_SERVER`
  (`https://anchor.zeam.money/sep38`), `DIRECT_PAYMENT_SERVER`,
  `FEDERATION_SERVER`, `SIGNING_KEY`, `TRANSFER_SERVER_SEP0024`,
  `PRINCIPALS`, `KYC_SERVER`, `ACCOUNTS`, `NETWORK_PASSPHRASE`, `VERSION`,
  `CURRENCIES` (eight entries, including `ZARZ` with `anchor_asset="ZAR"`),
  `VALIDATORS`, `DOCUMENTATION`.
- **`ANCHOR_QUOTE_SERVER` is present** — the only anchor in this survey that
  declares one.
- **But:** its `/info` response
  (`https://anchor.zeam.money/sep38/info`, fetched 2026-08-30) is reproduced
  below in full — this is the complete `assets` array, not an excerpt:

  ```json
  {"assets":[{"asset":"stellar:USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},{"asset":"stellar:BRL:GDVKY2GU2DRXWTBEYJJWSFXIGBZV6AZNBVVSUHEPZI54LIS6BA7DVVSP"},{"asset":"iso4217:BRL","sell_delivery_methods":[{"name":"cash","description":"Deposit cash BRL at one of our agent locations."},{"name":"ACH","description":"Send BRL directly to the Anchor's bank account."},{"name":"PIX","description":"Send BRL directly to the Anchor's bank account."}],"buy_delivery_methods":[{"name":"cash","description":"Pick up cash BRL at one of our payout locations."},{"name":"ACH","description":"Have BRL sent directly to your bank account."},{"name":"PIX","description":"Have BRL sent directly to the account of your choice."}],"country_codes":["BR"]}]}
  ```

  Three assets advertised: USDC, a Stellar BRL token, and `iso4217:BRL` with
  cash/ACH/PIX delivery methods for Brazil. **Neither `ZARZ` nor
  `iso4217:ZAR` — zeam's own currency and the African fiat it claims to
  track — appears anywhere in this list.** The `asset.USDCIssuer` value in
  this repository's own registry matches the USDC issuer quoted here exactly,
  which is at least some evidence the endpoint is genuine SEP-38 machinery
  and not a placeholder — it is just not advertising the anchor's own
  African-fiat token as one it can quote. (This survey checked `/info` only —
  see the stated method limit above — so it cannot say whether the three
  assets that *are* listed would actually yield a price via `/price` or
  `/quote`; the point here is narrower: ZARZ is not even offered.)
- This repository does not know why ZARZ is absent from `/info`. Possible
  explanations — an unconfigured or leftover demo deployment, or
  infrastructure shared with an unrelated Brazilian anchor — are speculation
  this document declines to make; only the observed `/info` response is
  reported.
- **Verdict: SEP-38 is declared, but `/info` does not list the African-fiat
  asset.** This is the most important finding of this survey: a check that
  only asks "is `ANCHOR_QUOTE_SERVER` present?" — which is exactly what
  `anchor.Profile.Priceable` currently means in this codebase — would
  misreport zeam.money as able to price ZARZ. `/info`, fetched live, shows
  otherwise. `Priceable` is accurate to what it currently claims to measure
  (a declared quote server exists) and this is not a defect in it; it is a
  gap between "declares SEP-38" and "advertises this specific asset" that no
  code in this repository currently checks, because no anchor in the
  project's existing case study declares SEP-38 at all. Any future work
  wiring an anchor's `Priceable` flag into a corridor decision should account
  for this — the field cannot currently promise the asset in question is
  actually offered, only that a quote server for *something* is declared.

### 4. ClickPesa — KES, TZS, RWF (not in this repository's registry)

- **Named by:** the Stellar Community Fund's public project page for
  "Clickpesa Sender Portal", and a ClickPesa-authored 2021 Medium post
  ("A non-technical guide to integrate with Stellar and ClickPesa's assets"),
  which names `connect.clickpesa.com` as the domain serving its
  `stellar.toml`.
- **Checked 2026-08-30:** `https://connect.clickpesa.com/.well-known/stellar.toml`
  did not resolve (DNS lookup failure — `getaddrinfo ENOTFOUND
  connect.clickpesa.com`). `https://clickpesa.com/.well-known/stellar.toml`
  returned HTTP 404.
- **Verdict: inconclusive**, not negative. This survey could not reach any
  `stellar.toml` for ClickPesa as of the check date, so nothing about its
  SEP-38 status — or whether it is still operating as a Stellar anchor at
  all — could be determined. A domain that does not resolve is a different
  fact from one that resolves and declares no `ANCHOR_QUOTE_SERVER`, and this
  document does not collapse the two.

---

## What this means for #180 (backlog `#106`)

Of the three anchors this survey actually reached, none offers a working
SEP-38 quote for the African-fiat currency it issues, as observed on
2026-08-30: two (ngnc.online, cowrie.exchange) declare no quote server at
all, and the one that does (zeam.money, for ZAR) does not list ZARZ among the
assets its quote server advertises. The fourth candidate (ClickPesa, for KES)
could not be reached to check at all, and its status is **inconclusive, not
negative** — it does not add to the negative count above.

**Nothing this survey found demonstrates #180 is reachable.** That is not the
same claim as "every African-fiat anchor lacks SEP-38" — this was a bounded
survey of four anchors, one of which could not be checked. Performing a live
SEP-38 round-trip on an African-fiat corridor would require either
ClickPesa turning out, on a future check, to declare a working quote server;
a currently-unsurveyed anchor this document did not look at; or one of the
three reachable anchors changing its published configuration. Of those,
zeam.money may be worth re-checking periodically, since its
`ANCHOR_QUOTE_SERVER` infrastructure is at least present and answering — but
this document does not know why ZARZ is missing from its `/info` response,
so it cannot say how much work reaching a positive result there would
actually take; that is a hypothesis for a future check to test, not a
finding this survey established.

This survey does not recommend building toward a ZAR corridor on the strength
of zeam's `/info` response; #179 (reporting SEP-38 availability as a corridor
fact, backlog `#105`) and #180 both need an asset a quote server actually
advertises, and `ZARZ` is not currently one.

---

## Related

- Issue [#182](https://github.com/Wayfare-labs/wayfare/issues/182) — this
  research task
- Issue [#180](https://github.com/Wayfare-labs/wayfare/issues/180) — blocked
  on this survey's question (backlog entry `#106`; see the numbering note
  above)
- Issue [#179](https://github.com/Wayfare-labs/wayfare/issues/179) — reporting
  SEP-38 availability as a corridor fact (backlog `#105`); the same "declared
  but not quotable" gap applies
- Issue [#183](https://github.com/Wayfare-labs/wayfare/issues/183) — the
  declared-vs-working distinction this survey applied to `ANCHOR_QUOTE_SERVER`
  is the same discipline that issue asks anchor checks to hold generally
- `anchor/anchor.go` — `Profile.Priceable` and the package doc comment's
  2026-08-04 ngnc.online finding, which this survey corroborates on
  2026-08-30
- `asset/known.go` — the verified-issuer registry this survey's anchor list
  was drawn from
