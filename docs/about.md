# About Wayfare

Issue [#252](https://github.com/Wayfare-labs/wayfare/issues/252), backlog
`#192` ([docs/backlog.md](backlog.md)).

Every claim below was checked against the code and documents of this
repository at commit `a89d985` on 2026-09-25. Where a claim depends on a
measurement, the measurement's own date is carried. Nothing here describes a
capability the repository does not have.

---

## What Wayfare is

Wayfare is a **corridor-integrity monitor for Stellar**. It measures what it
actually costs to move value across a stablecoin → fiat-token corridor — not
what a quoted rate suggests it should cost — and it states plainly when none
of the available routes are worth taking, up to and including corridors where
the honest answer is *do not send this*.

The distinction it exists to make is the one between a quoted price and an
executed one. Sending 100 USDC toward NGNC was measured live on mainnet on
2026-08-08: the best route available returned roughly 46% of fair value
against the USD/NGN mid ([docs/corridor-measurements.md](corridor-measurements.md),
measured 2026-08-08). A tool that displays "best route: 62,900 NGNC" would be
accurate, useful-looking, and would have cost the sender more than half of
what they sent. Wayfare scores every route against an independent mid-market
rate instead, so a corridor whose best option is unusable is reported as
unusable rather than presented with a winner.

## Who it is for

- **Wallets and payment applications** deciding whether a corridor is fit to
  offer their users at all, and at which sizes. The API is public, keyless
  and read-only, and answers cross-origin browser requests
  (`README.md` HTTP API section, checked 2026-09-25).
- **Anchors and issuers** who want an independent, reproducible reading of
  their own corridor's market quality — priced from the same public
  infrastructure their users traverse.
- **Researchers and analysts** studying Stellar corridor structure. Every
  figure is reproducible from recorded bytes; the measurement history is a
  hash-chained store whose verification fails loudly on tampering
  ([docs/run-store.md](run-store.md), checked 2026-09-25).

Wayfare is a measurement and reporting tool. It is not a trading interface,
a router, or a wallet.

## What it measures

One corridor at a time, across a ladder of twelve sizes from 0.1 to 5000
units (`dex/sizes.go`, checked 2026-09-25), each rung priced by Horizon's
own strict-send pathfinding — the same engine that would execute the payment
(`dex/dex.go`, checked 2026-09-25). For each corridor it publishes:

- **The best achievable result at each size**, as an effective rate and a
  loss percentage against an independent mid-market rate.
- **A verdict per size** — GOOD (≤3%), FAIR (≤8%), POOR (≤20%), UNUSABLE
  (>20%) — with bands anchored to what established remittance corridors
  actually achieve in total cost (`route/route.go`, checked 2026-09-25).
- **A corridor integrity state** carried alongside the verdict: DIRECT (an
  independent market exists), DERIVATIVE (every path runs through another
  fiat token, named in `depends_on`), NO-MARKET (no path exists at any
  size), or UNKNOWN (nothing was learned — deliberately distinct from
  "nothing exists") (`route/route.go`, checked 2026-09-25).
- **A cross-checked reference rate.** Two independent providers are queried,
  never averaged; when they diverge badly enough (>10%), the corridor is
  published with no verdict at all rather than a number that depends on
  which feed was believed (`refrate/cross.go`, checked 2026-09-25).
- **Counterparty findings** — checks on the anchor's published documents,
  auth endpoints and issuer flags, run with the measurement. These qualify
  the headline and can never move it (`checks/`, [docs/checks.md](checks.md),
  checked 2026-09-25).

## What it refuses to do

The full register, with reasons, is [docs/non-goals.md](non-goals.md). In
brief:

- **It does not move money.** No escrow, custody, payment execution, or send
  button. The project began as a router and stopped being one when live
  measurement showed the corridor structurally broken at every size; a
  ranking implies its winner is worth taking, and here that assumption is
  false ([docs/non-goals.md](non-goals.md), checked 2026-09-25).
- **It does not predict.** No failure probabilities, no price forecasts, no
  ML. The predictive layer is designed against on the roadmap and has no
  packages in the tree; a prediction published as fact is the specific
  failure this project exists to avoid (README Architecture, checked
  2026-09-25).
- **It does not average its benchmarks.** Two reference providers are
  queried and never blended, because a blended mid names no provider and
  every figure must be traceable to a source a reader can check
  ([docs/adr-reference-mids.md](adr-reference-mids.md), checked 2026-09-25).
- **It does not fill gaps.** An unmeasured fact is published as unknown with
  a reason, never as a zero, a false, or a plausible-looking number. When a
  live measurement fails, the response says it is serving recorded history
  and carries the reading's age; nothing is ever synthesised to fill the gap
  (`server/api.go`, checked 2026-09-25).
- **It does not recommend on a broken corridor.** When no size produces a
  verdict of POOR or better, `recommended` is published as `null` — the
  absence of a recommendation is itself the published answer
  (`route/route.go`, checked 2026-09-25).

## The non-custodial position

**Wayfare holds no funds, issues no tokens, takes no possession of anything,
and holds no keys.** It performs read-only queries against public APIs:
Horizon for pathfinding and order books, published `stellar.toml` documents
for issuer identity, two reference-rate providers for the benchmark, and a
snapshot-replay store for recorded upstream responses. There is no account
system, no transaction signing, no KYC collection, and no fee of any kind.
This is not a policy that might change with growth; it is what makes the
project shippable by a small team without becoming a licensed money
transmitter ([docs/non-goals.md](non-goals.md), checked 2026-09-25).

Identity in Wayfare is an issuer's own published document, verified per
SEP-1 with the verification date recorded — an asset code identifies nothing;
the issuer account is the identity (`asset/`, checked 2026-09-25).

## What it is honest about not having

- **Market-quality metrics (spread, depth, price impact, concentration) are
  implemented but not reachable**: no production code path runs them, so
  none has appeared in a response (README v2 section, checked 2026-09-25;
  [#91](https://github.com/Wayfare-labs/wayfare/issues/91) tracks the
  wiring).
- **The cost decomposition** (FX loss, network fees, anchor fee, slippage,
  expected failure cost) is computed per priced rung, with three of five
  components published as undetermined-with-reason until their inputs exist
  (`route/cost.go`, checked 2026-09-25).
- **Statistics and trends** are computed only above minimum sample sizes (30
  observations for a mean, 60 for a trend); below that the fields are absent
  rather than precise-looking (`analysis/analysis.go`, checked 2026-09-25).
- **The deployed public instance serves recorded measurements, not live
  ones**, and its freshness depends on the measure workflow — read
  `stale.age_human` rather than assuming (README "Try Wayfare", checked
  2026-09-25).

## Start here

- Reproduce a measurement in fifteen minutes:
  [docs/first-15-minutes.md](first-15-minutes.md)
- The HTTP API: [docs/api.md](api.md)
- Why a monitor and not a router: the README's [Why a monitor](../README.md#why-a-monitor-and-not-a-router)
  section
- What this project will never build: [docs/non-goals.md](non-goals.md)
- The contributor backlog: [docs/backlog.md](backlog.md)
