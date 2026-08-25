# What "fair value" means for the NGN reference rate

**Finding.** The reference mid the project scores the naira corridor against is an
**official / interbank-style indicative mid-market rate, not the Nigerian
parallel ("street") rate.** That is established by the providers' own
documentation and by the project's own provider code. The *exact* Nigerian
benchmark inside that official band — the CBN official window versus the
NAFEM / "willing buyer, willing seller" window — is **not specified** by either
provider, and cannot be recovered from what they publish. So the honest
statement is two-part: the side of the divide is known (official/interbank),
the precise official benchmark within it is unknown.

This matters because "fair value" is doing real work in the sentence *"USDC →
NGNC loses ~25% at the floor against fair value."* It implies the mid is the
right thing to measure against. This document says plainly what that mid is,
how that was determined, and what it does — and does not — imply for the
loss-floor finding.

> Written to the standard of issue #33: state the observable fact, cite the
> evidence, and where the answer is "unspecified", say so rather than assume.

---

## Which providers price the NGN corridor, and what they claim to track

The naira reference mid comes from two providers configured in `refrate/`. The
[2026-08-08 measurement](corridor-measurements.md#run-of-2026-08-08) was scored
against **exchangerate-api** as the primary, with **currency-api** as the
independent secondary cross-check.

### 1. exchangerate-api — primary (`refrate/exchangerate.go`)

Endpoint: `https://open.er-api.com/v6/latest/{base}` (the free "open" tier).

The provider's own documentation describes the rate type and its sourcing in
its own words:

- **Rate type:** *"indicative midpoint rates"*, which are *"accurate enough for
  tasks like price estimations in an e-commerce store or stats on a
  dashboard."*
- **Sourcing:** *"We collect public reference data from a number of central
  banks and commercial sources around the world. An example would be the
  reference rates released by the European Central Bank each day."* And:
  *"We collect exchange rate data from multiple central banks & commercial
  sources and then use our own algorithm to blend these different datasets."*
- **Coverage rule:** *"We only support a currency code in ExchangeRate-API if
  we have at least 3 data sources for that currency."*
- **Explicit unsuitability:** *"We do not supply buy/sell spread data and so
  our rates are unsuitable for forex trading or processing cross currency
  settlements."*

(Source: ExchangeRate-API, "Our Data" / product documentation,
<https://www.exchangerate-api.com/>.)

Two things follow directly from that wording. First, the rate is on the
**official/interbank side of the divide**: it is a blend of central-bank
reference rates and commercial ("interbank"-style) feeds, not a survey of what
bureaux de change or street traders quote. Second, the provider **does not
document a Nigeria-specific methodology at all** — NGN is treated like any
other of its 160-odd currencies, blended from "at least 3 data sources" that it
does not enumerate per currency. So which Nigerian official rate its NGN inputs
correspond to is not published.

### 2. currency-api — secondary cross-check (`refrate/currencyapi.go`)

Endpoint: `https://latest.currency-api.pages.dev/v1/currencies/{base}.json`
(the CC0-licensed, keyless [`fawazahmed0/exchange-api`](https://github.com/fawazahmed0/exchange-api) feed).

currency-api is a free aggregator that pulls from a *different* set of official
upstreams than exchangerate-api — which is precisely why the project uses it as
a cross-check rather than a spare: *"a second provider that resold the first
one's data would give redundancy against an outage and nothing against an
error"* (`refrate/currencyapi.go`). It publishes the same **official/interbank
class of rate** and carries no parallel/street rate. This was already
established independently in [`parallel-rate-research.md`](parallel-rate-research.md#3-exchangerate-api--fawazahmed0currency-api),
which recorded its verdict as: *"Aggregates from multiple official sources.
Does not provide parallel/street rates."*

### The project's own code already says this

The provider code does not hide the caveat — it is written into the source and
carried through to the UI via the `Source` field:

- `refrate/exchangerate.go`: *"feeds like this publish an official or interbank
  rate. For currencies with exchange controls — NGN historically among them —
  the rate people actually transact at can diverge sharply from the official
  one. So this is a defensible benchmark, not ground truth."*
- `refrate/currencyapi.go`: *"this is an official/interbank-style rate, and for
  currencies under exchange controls the rate people actually transact at can
  diverge sharply from it."*
- `refrate/refrate.go` (package doc): the reference is *"an independent
  reference mid-market rate"*; a `Rate.Mid` is defined as *"the midpoint
  between what buyers pay and sellers accept ... the rate nobody actually
  gets."*

So the provider layer is internally consistent: it fetches an official/
interbank mid, labels it as such, and never claims it is the street rate.

---

## Why the distinction matters for Nigeria

Nigeria has a long-documented gap between the **official rate** — the rate set
or administered through the Central Bank of Nigeria (CBN) and its trading
windows — and the **parallel ("street") market rate** quoted by bureaux de
change and aggregators such as abokiFX. The two are different prices for the
same dollar. Even after the June 2023 rate unification and the subsequent
NAFEM / "willing buyer, willing seller" reforms, a spread between the official
and parallel quotes has continued to be reported, with the parallel rate
generally **weaker** (more naira per dollar) than the official one.

Public references for the regime: the CBN (<https://www.cbn.gov.ng/>) publishes
the official/administered rates; the parallel quote is tracked by third-party
aggregators (e.g. abokiFX), whose methodology `parallel-rate-research.md` found
to be undocumented and therefore unusable as a project data source.

The consequence for a "fair value" claim is that **which rate you benchmark
against changes the number.** If the providers track the official rate — and
they do — then a loss measured against them is a loss measured against the
*stronger* of the two available fair-value candidates.

---

## What this implies for the ~25% floor-loss finding

The [corridor measurement](corridor-measurements.md) reports a **structural
floor of roughly 24.65%–25.02%** on USDC → NGNC at the smallest (0.1 USDC)
size, against a reference mid of **1364.0070 USD/NGN via exchangerate-api**.
(The issue's shorthand "27% floor" is a round approximation; the precise
zero-slippage floor sits at ~24.65% in the 12:53 run and ~25.02% in the
same-window 14:27 run, with ~27% reached by 5 USDC once a little slippage
stacks on top. The exact figure carries the provider's accuracy; see below.)

Because the providers track the **official** rate, and the Nigerian parallel
rate is empirically **weaker** (more naira per dollar), the official mid is the
**most generous benchmark available**:

- Loss against mid is `1 − (naira received per USD on-chain) ÷ (reference naira
  per USD)`. A *larger* reference (the parallel rate) makes the *same* on-chain
  output score a *larger* loss.
- Therefore scoring against the official mid **understates** the true cost to a
  user who ultimately values their naira at the street rate. Against the
  parallel rate the loss floor would be **larger**, not smaller.

This is the same conclusion `corridor-measurements.md` reaches in
["The benchmark is the charitable one"](corridor-measurements.md#4-the-benchmark-is-the-charitable-one),
and it is what makes the finding robust: the corridor already clears the 20%
"Unusable" threshold **against the most flattering benchmark that exists.** A
tighter or more Nigeria-specific benchmark could only move the floor further
into Unusable territory, never back toward usable.

What the choice of benchmark *does* affect is the **absolute** percentage. The
27% (or 24.65%, or 25.02%) figure carries the reference provider's accuracy as
a dependency — a different official source, or the parallel rate, would print a
different number. What it does **not** affect is the *shape* of the result: the
monotonic climb with size, the marginal-rate collapse, and the ~$116 of total
NGNC liquidity are all properties of the on-chain data alone and hold under any
reference rate.

---

## What remains unknown, stated plainly

Two things are known and one is not:

| Question | Answer | Basis |
|:---|:---|:---|
| Official/interbank or parallel/street? | **Official / interbank** | Providers' own docs + provider code, above |
| Direction of any benchmark error? | **Official flatters the corridor**; parallel would show a larger loss | Nigerian parallel rate is weaker than official |
| *Which* official Nigerian benchmark (CBN window vs NAFEM)? | **Unspecified — cannot be recovered** | exchangerate-api blends ≥3 undisclosed sources per currency; currency-api aggregates undisclosed official upstreams |

The unresolved part is not a defect in this project — it is a limit of what the
free providers publish. Neither provider discloses, per currency, which
Nigerian rate their blended inputs represent, so the reference mid is
best described as *"an official/interbank indicative mid of unspecified precise
provenance."* That is the honest label, and it is enough to support the
loss-floor finding, because the finding only needs the reference to be **at
least as generous as** the rate a user could actually get — which, for the
official side of the Nigerian divide, it is.

If a provider were ever to document its NGN sourcing precisely, or if a
defensible parallel-rate source became available (tracked in
`parallel-rate-research.md` and issue #57), this document should be revisited to
report both gaps rather than the one charitable number.

---

## Related

- [`docs/corridor-measurements.md`](corridor-measurements.md) — the USDC → NGNC
  measurement this document contextualises
- [`docs/parallel-rate-research.md`](parallel-rate-research.md) — the companion
  finding that no usable parallel-rate source exists
- `refrate/exchangerate.go`, `refrate/currencyapi.go`, `refrate/refrate.go` —
  the provider code, which labels the rate as official/interbank in its own
  comments
- Issue #33 — the reference standard this document is written to
- Central Bank of Nigeria — <https://www.cbn.gov.ng/>
