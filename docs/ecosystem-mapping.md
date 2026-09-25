# Spike: mapping the Stellar ecosystem projects Wayfare could inform

Issue [#326](https://github.com/Wayfare-labs/wayfare/issues/326), backlog
`#273` ([docs/backlog.md](backlog.md)).

**Status: completed. The map is short, and several angles closed negative or
inconclusive — those are reported as such rather than padded.** Every Wayfare
capability named below was checked against the tree at `295085b` on
**2026-09-25**; every external claim carries its own source and the date it
was checked. No code was written, and none is proposed by this issue.

> **A numbering note, checked directly against the tracker rather than
> assumed.** Issue #326's body says the map is "the input **#157** needs to be
> worth answering." That reference is to **backlog entry `#157`** — "Spike:
> who would consume this API, and what shape do they need"
> ([docs/backlog.md](backlog.md), checked 2026-09-25) — which was filed as
> GitHub issue [#217](https://github.com/Wayfare-labs/wayfare/issues/217).
> GitHub issue **#157** is a different issue entirely: "Depth at the dust size
> is the structural-floor probe", a V2 pricing issue, closed on 2026-08-31
> (checked 2026-09-25 via the GitHub API). This is the same
> backlog-versus-GitHub numbering split documented in
> [docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md).
> Issue #217 was closed on 2026-09-24 with an inconclusive "who" finding
> ([docs/spike-api-consumers.md](spike-api-consumers.md)), which is exactly
> the gap this map supplies input to. The mismatch does not change this
> document's task; it is flagged once, here, so a reader chasing "issue #157"
> does not land on a dust-size probe.

---

## What the map is and is not

This document names real, existing Stellar-ecosystem projects and ties each to
Wayfare capabilities **verified in code on 2026-09-25** — not to roadmap
prose. It is a desk-research map from public sources. What it is not, stated
plainly: it is not the answer to backlog `#157`'s actual question. Only asking
a person at one of these organisations settles what they would really need;
nothing in this map substitutes for that outreach
([docs/spike-api-consumers.md](spike-api-consumers.md) reached the same
conclusion on 2026-09-24). Each entry below is a hypothesis with sources, not
a demand signal.

### What Wayfare can answer today (the capabilities this map ties to)

Checked against the tree at `295085b` on 2026-09-25. Each row names the
package or endpoint a consumer would actually bind to.

| Capability | Where it lives | Status on 2026-09-25 |
|:---|:---|:---|
| Ladder measurement of a stablecoin → fiat-token corridor, twelve sizes 0.1 → 5000, priced by Horizon strict-send pathfinding | `dex/dex.go`, `route/route.go`; `GET /api/corridor` ([docs/api.md](api.md)) | **Live.** Every published quote is `kind: "dex"` today (`route/route.go:186` is the only value reached; `docs/spike-adversarial-review.md:261` agrees) |
| Verdicts GOOD/FAIR/POOR/UNUSABLE per size, and the recommendation rule (nothing recommended when nothing is POOR or better) | README "Verdict thresholds" and "The recommendation rule"; `route/route.go` | **Live**, wire contract: `recommended` present and `null` when nothing is worth taking |
| Integrity states DIRECT / DERIVATIVE (with `depends_on`) / NO-MARKET / UNKNOWN | README "Integrity states"; `route/route.go` | **Live** |
| Cross-checked reference mid, two providers, never averaged; >10% divergence publishes no verdict | `refrate/cross.go`; README "Reference agreement" | **Live** |
| SEP-1 discovery — an anchor's published `stellar.toml`, `Priceable`, and the advertised-SEP inventory via `Profile.SEPs()` | `anchor/anchor.go` | **Live** (capability exists in the package; see the API column below for what reaches the wire) |
| Counterparty checks: 7 in the default set — `AnchorAssetISO4217`, `SEP10EndpointResponds`, `SEP24InfoListsAsset`, `SEP38QuoteServerPublished`, `IssuerAuthFlags`, `IssuerFlagImmutability`, `HomeDomainRoundTrip` | `checks/runner.go` `Default()` (checked 2026-09-25); tri-state contract in [docs/checks.md](checks.md) | **Live.** They appear as the `findings` block on `/api/corridor`; they qualify the headline and never move it |
| Historical trend for a corridor, oldest-first, from the hash-chained store | `GET /api/corridor/trend` ([docs/api.md](api.md)) | **Live**, reads stored runs only |
| Browser consumers without a proxy | README "HTTP API": `Access-Control-Allow-Origin: *`, keyless, read-only | **Live** |
| Money as decimal strings on the wire | ADR [006](adr/006-why-money-crosses-the-wire-as-decimal-strings.md) | **Live** |
| A live SEP-38 round-trip performed by the project, and an anchor quote published on the wire | `sep38/` client; `route.KindAnchorSEP38` | **Future.** The client and the fee-denomination identity are implemented and unit-tested, including a recorded fixture from testanchor.stellar.org (`sep38/live_roundtrip_test.go`, checked 2026-09-25), but **nothing in production calls the package**: `route/cost.go:83-87` says an anchor fee "can be wired here" when a SEP-38 corridor is wired in, and a code search of `route/` and `server/` on 2026-09-25 finds no `sep38.` call outside tests. No published quote has ever carried `kind: "anchor-sep38"`. Describing anchor quotes as a Wayfare output today would be wrong; this map marks that need **future** wherever it appears |
| Market-quality metrics (spread, depth, price impact, concentration) on the wire | `checks/metric_*.go` | **Future** — implemented in the tree but unreachable: `checks.Runner` has no way to run a Metric (README v2 section, checked 2026-09-25; [#91](https://github.com/Wayfare-labs/wayfare/issues/91)) |

Two discrepancies found while verifying the above are recorded in the closing
section rather than silently relied on.

---

## Wallets

### Freighter (Stellar Development Foundation)

**What it does today.** Freighter is the SDF's non-custodial Stellar wallet,
available as a browser extension and a mobile app; users hold their own keys
and transact on Stellar, including swaps inside the wallet
([freighter.app](https://freighter.app/) and
[docs.freighter.app](https://docs.freighter.app/), both checked 2026-09-25).

**What it would need from Wayfare.** A per-corridor reading it can render
before or alongside a swap/send: which of the corridors its user is about to
cross is fit to offer at the size being sent, scored against an independent
mid rather than the route's own optimism. Concretely, verified capabilities:
`GET /api/corridor` for the loss ladder and verdicts at the user's own size
(`sizes=` accepts arbitrary rungs, [docs/api.md](api.md)); the
`recommended: null` contract as an honest "don't route through this" signal;
`integrity: DERIVATIVE` so a wallet can tell a user their naira leg is really
another token's market; and the CORS/decimal-string wire properties that make
a browser extension a natural client (README "HTTP API", checked 2026-09-25).
A swap-interface integration would additionally want anchor-vs-DEX comparison
and depth metrics — **future** (see the capability table).

**Met today?** The single-size, verdict-plus-integrity reading is fully met by
what ships on 2026-09-25. The swap-comparison reading is future. Whether
Freighter would ever surface a third-party corridor-integrity figure in its
send flow is **unknown** — no public statement exists either way; this entry
is a hypothesis with a matching API shape, not a demand signal.

### LOBSTR

**Inconclusive — reported as such, deliberately.** LOBSTR is a widely used
Stellar wallet ([lobstr.co](https://lobstr.co/), checked 2026-09-25 for
existence and general purpose only). I could not determine from public
sources whether its send flow exposes any pre-send corridor quality
information, or what data it would need to do so, without speculating about a
product I have not used at integration depth. Rather than invent a "need"
from a marketing page, this entry records: real project, plausible wallet
candidate for backlog `#157`-style outreach, **no verifiable mapping
established**.

---

## PSPs (payment service providers)

### Kotani Pay (Kenya / Africa-wide)

**What it does today.** Kotani Pay sells last-mile payout and off-ramp
services: businesses pay out in stablecoins (USDC, USDT, cUSD, USDGLO are
named on the page) and withdraw to local payment channels across Africa,
listing use cases from gig-work payouts to remittances
([kotanipay.com/apis](https://kotanipay.com/apis), checked 2026-09-25). The
page names fiat currencies KES, GHC and XAF among what it connects to.

**What it would need from Wayfare.** A way to decide, per corridor and size,
whether the on-chain leg its customers will traverse is worth routing through
at all: `GET /api/corridor`'s ladder summary (`floor_loss_pct` /
`worst_loss_pct`) plus `integrity` to catch derivative corridors whose
liquidity is borrowed from another token. If Kotani settles into NGN, GHS or
KES via Stellar fiat tokens, `GET /api/corridor/trend` adds the "is this
getting worse?" dimension ([docs/api.md](api.md), checked 2026-09-25). All of
that is live. A per-leg all-in cost figure would additionally want the cost
decomposition and depth metrics — **future** (see the capability table).

**Met today?** The route-or-don't-route reading is met today. **The
structural caveat, found while checking and reported plainly:** Kotani's
public material describes the business without naming Stellar as its
settlement rail, and
`https://kotanipay.com/.well-known/stellar.toml` returns HTTP 404 (checked
2026-09-25). It therefore appears in this map as a PSP whose *corridors
overlap* Wayfare's (NGN/GHS/KES off-ramps), not as a verified Stellar
integration; whether it actually moves value over Stellar could not be
determined from its own site. Flagged as the first question outreach should
ask.

### Eversend (Uganda / Africa-wide)

**What it does today.** Eversend operates a USDC off-ramp API: USDC or USDT
in, local-currency payouts out via mobile money (M-Pesa, MTN MoMo) and bank
rails, with the conversion rate "returned on the quote" and custody via
Fireblocks. Its "Networks in" list includes **Stellar** explicitly, alongside
Tron, Ethereum, Solana, Base, Polygon and BNB Chain
([eversend.co/platform/apis/usdc-off-ramp](https://eversend.co/platform/apis/usdc-off-ramp),
checked 2026-09-25).

**What it would need from Wayfare.** A benchmark for the rate its own quote
locks: `reference_mid` with `reference_source` and `reference_pair` is a
published, independently checkable mid for exactly the pairs Eversend quotes
(README "Reference agreement", checked 2026-09-25), and the two-provider
divergence fields bound how much that benchmark itself can be trusted.
`GET /api/corridor/trend` supplies rate history for the same corridor —
relevant when their quote-hold window is short. All live capabilities. Their
own per-quote rate is their business; Wayfare would inform, not replace it.

**Met today?** Yes for the benchmark-and-trend reading. **Caveat:** Eversend
has no Stellar issuer presence — `https://eversend.co/.well-known/stellar.toml`
returns HTTP 404 (checked 2026-09-25) — so Wayfare can measure the corridor
their quotes traverse (e.g. USDC→NGN via NGNC or NGNT), not Eversend's
own product. What Eversend would think of an independent, public benchmark
against its quoted rates is **unknown**; both the informative and the
awkward readings of that are honest possibilities, and only outreach settles
it.

### HoneyCoin (Kenya / Africa-wide)

**What it does today.** HoneyCoin provides stablecoin payments
infrastructure — on-ramps, off-ramps, collections, payouts and FX across 21
African markets through a single API, moving USDC and USDT via mobile money,
bank transfer and virtual accounts
([honeycoin.app/stablecoin-payments-africa](https://honeycoin.app/stablecoin-payments-africa),
checked 2026-09-25). The page's own framing is the corridor-loss problem
Wayfare measures: "lose another 3-7% in FX spread before the money lands".

**What it would need from Wayfare.** Independent evidence for exactly that
spread claim, per corridor: the loss ladder and verdicts from
`GET /api/corridor` measured on mainnet against a published reference mid.
All live capabilities on 2026-09-25.

**Met today?** The measurement need is met. **Caveat, same shape as
Kotani's:** no Stellar issuer endpoints were found for HoneyCoin
(`https://honeycoin.app/.well-known/stellar.toml` returns HTTP 404, checked
2026-09-25) and its public material does not name its chain integrations on
the page checked. Included as a corridor-overlapping PSP, not a verified
Stellar integration.

**Negative finding on the PSP category, reported as such:** no PSP was found
that publishes its own SEP-1 `stellar.toml` or operates as a Stellar anchor
in the sense Wayfare's `checks`/`anchor` packages would observe. Every PSP
entry above consumes corridor *measurements*; none could be an `anchor/`
discovery subject itself on the evidence available. A PSP that also runs an
anchor (with a `stellar.toml`) may exist in the ecosystem — none was found in
this bounded survey, and that absence is a finding, not a gap in the list to
be filled next pass.

---

## Anchors

These four are the anchors with African-fiat tokens — the corridor set
Wayfare's own case study measures — and all four carry in-repo findings
re-verified from primary sources on 2026-09-25. They are the concrete
subjects any Wayfare-informs-anchors story has to start from.

### LINK.IO / ngnc.online (NGNC, GHSC, KESC — Nigeria, Ghana, Kenya)

**What it does today.** Issues the naira token NGNC (status `live`) plus
GHSC and KESC (both `pending`), all from one issuing account, and publishes
`WEB_AUTH_ENDPOINT` and `TRANSFER_SERVER_SEP0024`
([https://ngnc.online/.well-known/stellar.toml](https://ngnc.online/.well-known/stellar.toml),
fetched 2026-09-25; same picture as the in-repo verification at
`asset/known.go`, checked 2026-09-25).

**What it would need from Wayfare.** An independent, reproducible reading of
its own corridor: `/api/corridor?from=USDC&to=NGNC` returns the loss ladder,
verdicts, and the cross-checked USD/NGN reference with both providers named —
a published figure about the market an anchor's token routes through that the
anchor did not produce itself. The `findings` block adds the issuer-flag
picture (`issuer.auth-flags`, `issuer.auth-immutable`) and the declared-SEP
inventory read from their own toml. All live capabilities (checked
2026-09-25).

**Met today?** Yes — and this is the one case where the interest is not
hypothetical: the corridor Wayfare measures IS this anchor's market, and the
measurement found its structural floor (25% at dust size; README "What the
measurements found", checked 2026-09-25). Whether LINK.IO wants that
published is a different question this map does not answer.

### Cowrie Integrated Systems / cowrie.exchange (NGNT — Nigeria)

**What it does today.** Issues NGNT, the most widely-held naira token on
mainnet, status `live`, and declares KYC_SERVER, TRANSFER_SERVER (SEP-6),
WEB_AUTH_ENDPOINT and DIRECT_PAYMENT_SERVER (SEP-31) — but **no**
`ANCHOR_QUOTE_SERVER` ([https://cowrie.exchange/.well-known/stellar.toml](https://cowrie.exchange/.well-known/stellar.toml),
fetched 2026-09-25; matches `asset/known.go`'s verification of 2026-08-26,
checked 2026-09-25).

**What it would need from Wayfare.** As an issuer, the same independent
corridor reading as LINK.IO (live capabilities). As a *programmatic* anchor
there is a sharper one: Wayfare's `anchor.Profile.Priceable` and the
`SEP38QuoteServerPublished` check exist precisely to report that their toml
publishes no machine-readable rate — the fact that keeps NGNT's anchor leg
unpriced. That report is live today; an anchor RFQ comparison would be
**future**.

**Met today?** Yes for the discovery/report reading.

### Zeam Mint / zeam.money (ZARZ — South Africa; USDZ)

**What it does today.** Issues ZARZ (status `live`, pegged 1:1 to ZAR, from
an FSCA/SARB-regulated issuer) and USDZ, and is the one African-fiat anchor
in this set that **declares `ANCHOR_QUOTE_SERVER`**
([https://zeam.money/.well-known/stellar.toml](https://zeam.money/.well-known/stellar.toml),
fetched 2026-09-25). However, its quote server's `/info` still advertises
only USDC, a BRL token and `iso4217:BRL` — **neither ZARZ nor
`iso4217:ZAR` appears**
([https://anchor.zeam.money/sep38/info](https://anchor.zeam.money/sep38/info),
fetched 2026-09-25). This reproduces the finding of
[docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md) (2026-08-30)
with a fresh check.

**What it would need from Wayfare.** Exactly what the survey doc predicted:
a consumer that treats "declares SEP-38" and "quotes this asset" as different
facts. Today, `anchor.Profile.Priceable` reports the declared server (live),
and `SEP38QuoteServerPublished` reports the same as a tri-state check (live).
Checking whether the *specific asset* is advertised requires fetching the
anchor's `/info` — `sep38/` has the client machinery to do that (implemented
and tested), but nothing in production calls it, so that check is not
something Wayfare runs for any corridor on 2026-09-25 (**future** as a
published finding; the survey's #183 is the tracked gap). If ZARZ ever
appears in their `/info`, the full SEP-38 quote comparison (anchor RFQ vs DEX)
is likewise **future** — see the capability table.

**Met today?** Partially, and this entry is the map's clearest illustration
of the declared-vs-working boundary: Wayfare's live checks would not
misreport Zeam as priceable-for-ZARZ (they only assert the declared server),
but they would not catch the gap either — the gap is visible in this document
because a human fetched `/info` on 2026-09-25.

### ClickPesa (KES, TZS, RWF — Kenya and region)

**Inconclusive — unchanged from the August survey.** Named as an
African-fiat anchor by the Stellar Community Fund's project page and its own
2021 integration post (sources cited in
[docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md), checked
2026-08-30). Re-checked on **2026-09-25**:
`https://connect.clickpesa.com/.well-known/stellar.toml` does not resolve
(HTTP 000 — DNS failure) and `https://clickpesa.com/.well-known/stellar.toml`
returns HTTP 404. Whether ClickPesa still operates as a Stellar anchor at all
**could not be determined**. It stays in the map as an outreach candidate and
a standing unknown, not as a negative: an unreachable domain is a different
fact from a declared-absent quote server, and this document does not collapse
the two.

### testanchor.stellar.org (SDF reference implementation) — included for completeness, not as an ecosystem candidate

The SDF's test anchor serves a working SEP-38 API
([https://testanchor.stellar.org/sep38/info](https://testanchor.stellar.org/sep38/info)
answered with an asset list including `stellar:SRT` and `iso4217:USD`, fetched
2026-09-25) and supplied the recorded fixture behind the one verified live
round-trip in `sep38/live_roundtrip_test.go` (checked 2026-09-25). It is a
test service, not a project Wayfare could inform — listed here because it is
the proof, cited in-repo, that the **future** anchor-quote capability has at
least one endpoint it demonstrably works against.

**Negative finding on the anchor category, reported as such:** among the
African-fiat anchors actually reachable on 2026-09-25, **zero publish a SEP-38
quote server that advertises the African-fiat asset it issues** (ngnc.online:
no server; cowrie: no server; zeam: server present, ZARZ absent). The
consequence for this map is structural: the anchor-uses-Wayfare's-RFQ-story
angle has **no live subject in the corridor set Wayfare actually measures**
until one of these tomls changes or an unsurveyed anchor appears. The angle
is real (the `sep38/` client and `route.KindAnchorSEP38` are built and
tested), but on today's evidence it is **future for every named anchor**.

---

## Who is conspicuously missing, and why

- **Mobile-first African consumer wallets (the e.g. Fonbnk/Accrue class).**
  The 2026-07 Spark+Kotani analysis of mobile-money/stablecoin bridges names
  API layers in this class (third-party analysis; not checked against their
  own docs in this spike). They would be natural wallet entries, but no entry
  was written: without checking a specific product's own documentation for
  its Stellar exposure and send flow, any "need" assigned to it would be the
  speculation this issue's constraints forbid. Listed as outreach targets,
  not mapped entries.
- **Non-African corridor anchors** (e.g. Montelibero for EURMTL, verified in
  `asset/known.go`, checked 2026-09-25) are measured by Wayfare today and
  could be informed exactly like LINK.IO or Cowrie. They were left out of the
  per-project sections to keep this map bounded to the corridor set the
  project's own case study and prior surveys cover; the pattern transfers
  unchanged.
- **Exchanges/DeFi frontends** (e.g. Aquarius, Blend) appear in the codebase
  only as bridge-asset issuers (`asset/known.go`, checked 2026-09-25). No
  evidence was sought or found that they consume third-party corridor
  measurements; entering them would have been speculation.

---

## What remains unanswered or inconclusive

Reported plainly, per the issue's own acceptance criteria:

1. **The actual demand question is untouched.** This map names projects and
   matches verified capabilities; it does not know whether any of them *want*
   what Wayfare publishes. Backlog `#157` (GitHub #217) was closed
   inconclusive on 2026-09-24 for exactly this reason, and this document
   supplies its input, not its answer. Every "what it would need" above is a
   hypothesis for outreach, with the project list here as the call sheet.
2. **Three PSPs are corridor-overlapping but not verified Stellar
   integrations.** Kotani Pay, Eversend and HoneyCoin all do stablecoin
   off-ramping into the fiat currencies Wayfare measures, and Eversend lists
   Stellar among its input networks — but none publishes a `stellar.toml`,
   and Kotani's and HoneyCoin's use of Stellar specifically could not be
   confirmed from their own sites (checks dated 2026-09-25).
3. **The anchor-quote angle has no live subject.** No reachable African-fiat
   anchor publishes a SEP-38 server advertising its own fiat asset (see the
   negative finding above; checks 2026-09-25). Any "anchor compares Wayfare's
   DEX reading to its own RFQ quote" story is **future** twice over: the
   Wayfare side is not wired (`route/cost.go:83-87`), and no named anchor
   currently offers the other side of the trade.
4. **ClickPesa remains undetermined** — DNS/404 on both candidate toml
   domains, 2026-09-25, consistent with the 2026-08-30 survey.
5. **LOBSTR is an unmapped wallet** — real, plausible, but no verifiable
   mapping was established without deeper product inspection; recorded
   rather than padded.
6. **Two repository discrepancies were found while verifying and are flagged,
   not fixed here** (this is a docs-only issue):
   - The README "Verification status" table states the live SEP-38
     round-trip is **"Verified — recorded fixture from testanchor.stellar.org
     in `sep38/testdata/live/`"**, and `sep38/live_roundtrip_test.go`
     supports that (both checked 2026-09-25) — but the README's own "HTTP
     API" section still says "a live SEP-38 round-trip has never been
     performed — see #180" (checked 2026-09-25), and #180 is closed. The two
     README passages contradict each other; the table's newer claim is the
     one the code supports.
   - The README's v2 section says "three [counterparty checks] run per
     corridor"; `checks/runner.go` `Default()` (checked 2026-09-25) defines
     **seven**. The code is the stronger evidence; the README count is stale.

---

## Sources

Wayfare repository, all checked **2026-09-25** at commit `295085b`:
`README.md` (Non-goals, HTTP API, Verification status, Roadmap),
`CONTRIBUTING.md` (Invariants), `docs/api.md`, `docs/non-goals.md`,
`docs/checks.md`, `docs/about.md`, `docs/backlog.md` (#157, #273),
`docs/spike-api-consumers.md`, `docs/spike-sdk-surface.md`,
`docs/sep38-african-fiat-survey.md`, `anchor/anchor.go`, `dex/dex.go`,
`refrate/refrate.go`, `refrate/cross.go`, `sep38/sep38.go`,
`sep38/live_roundtrip_test.go`, `route/route.go`, `route/cost.go`,
`checks/runner.go`, `checks/sep38_quote_server.go`, `asset/known.go`.

GitHub API (Wayfare-labs/wayfare), checked **2026-09-25**: issues #326, #157,
#180, #217 and their comments.

External, each with its check date:

| Source | Checked |
|:---|:---|
| [freighter.app](https://freighter.app/) / [docs.freighter.app](https://docs.freighter.app/) | 2026-09-25 |
| [kotanipay.com/apis](https://kotanipay.com/apis) | 2026-09-25 |
| [eversend.co/platform/apis/usdc-off-ramp](https://eversend.co/platform/apis/usdc-off-ramp) | 2026-09-25 |
| [honeycoin.app/stablecoin-payments-africa](https://honeycoin.app/stablecoin-payments-africa) | 2026-09-25 |
| [lobstr.co](https://lobstr.co/) (existence and general purpose only) | 2026-09-25 |
| [ngnc.online/.well-known/stellar.toml](https://ngnc.online/.well-known/stellar.toml) | 2026-09-25 |
| [cowrie.exchange/.well-known/stellar.toml](https://cowrie.exchange/.well-known/stellar.toml) | 2026-09-25 |
| [zeam.money/.well-known/stellar.toml](https://zeam.money/.well-known/stellar.toml) | 2026-09-25 |
| [anchor.zeam.money/sep38/info](https://anchor.zeam.money/sep38/info) | 2026-09-25 |
| [testanchor.stellar.org/sep38/info](https://testanchor.stellar.org/sep38/info) | 2026-09-25 |
| connect.clickpesa.com and clickpesa.com `stellar.toml` (DNS failure / HTTP 404) | 2026-09-25 |
| kotanipay.com, eversend.co, honeycoin.app `/.well-known/stellar.toml` (HTTP 404) | 2026-09-25 |
| [spark.money — Africa's Mobile Money Networks Meet Stablecoins](https://www.spark.money/research/africa-mobile-money-stablecoin-bridge) (third-party analysis; outreach-target names only) | 2026-09-25 |

## Related

- Issue [#326](https://github.com/Wayfare-labs/wayfare/issues/326) — this task
- Backlog `#157` / GitHub [#217](https://github.com/Wayfare-labs/wayfare/issues/217) —
  the consumer spike this map is input to
  ([docs/spike-api-consumers.md](spike-api-consumers.md))
- [docs/sep38-african-fiat-survey.md](sep38-african-fiat-survey.md) — the
  anchor survey whose findings this document re-verified on 2026-09-25
- [docs/spike-sdk-surface.md](spike-sdk-surface.md) — what a client library
  would need to expose, once consumers exist
- [docs/about.md](about.md) — the audience statement this map tests against
  named projects
