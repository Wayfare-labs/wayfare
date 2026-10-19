# Plan: the Horizon → Stellar RPC migration (RPC has no pathfinding)

**Status: plan only — no code changes.** This document decides nothing by
writing code; it establishes the facts, costs and risks that a later decision
issue should be judged against. Tracked as
[#11](https://github.com/Wayfare-labs/wayfare/issues/11) (backlog #112).

**Headline finding:** the capability Wayfare's pricing depends on —
`/paths/strict-send` — does not exist anywhere in Stellar RPC, and SDF's own
migration guide says so by omission: the endpoint does not appear in the
Horizon→RPC mapping table at all, because there is nothing to map it to.
Meanwhile **no shutdown date for Horizon has been announced**, and Horizon
will keep receiving protocol-compatibility updates. This is a long-horizon
problem, not an imminent one — which makes the correct move a cheap seam now
and a decision only when (or if) a date appears. Details and reasoning below.

---

## 1. The single fact that decides urgency

**As of 2026-08-26, SDF has announced no Horizon shutdown or end-of-service
date.**

Sources read on 2026-08-26:

- **`developers.stellar.org/docs/tools/lab/api-explorer/horizon-endpoint`** —
  the official deprecation statement: *"Horizon is nearing end-of-life and
  will eventually be deprecated in favor of Stellar RPC and Portfolio APIs.
  While it will continue to receive updates to maintain compatibility with
  upcoming protocol releases, it won't receive new feature development."*
  "Eventually" is the only timetable given; there is no date.
- **`developers.stellar.org/docs/data/apis/migrate-from-horizon-to-rpc`** —
  the official migration guide. It maps Horizon endpoints to RPC methods
  endpoint-by-endpoint. `/paths/strict-send` and `/order_book` appear nowhere
  in the table; `/offers` maps to *"No direct RPC equivalent"*. The guide's
  own framing: *"Endpoints without mappings do not have a direct replacement
  in the RPC API. To build similar functionality in an application, please
  consider partnering with an indexer or using the information listed below
  to build your own indexed representation of horizon endpoints."*
- A search for an announced retirement date (SDF blog, developer docs,
  protocol discussions) returns nothing beyond the "nearing end-of-life"
  statement above.

**Consequence:** this is **long-horizon**, not urgent. Nothing forces a
decision today, and any plan that pretends otherwise is manufacturing urgency
SDF has not created. The risk is real but scheduled by an unknown date, which
is precisely the situation a swappable seam is designed for. This fact — and
its source and date — should be re-checked before any future decision issue
is opened, because it is the one input that flips every recommendation below
from "prepare" to "act".

---

## 2. What the code actually depends on

The entire coupling surface is `dex.Client`:

| Call | Endpoint | Used by | Role |
|:---|:---|:---|:---|
| `Client.StrictSendPaths` | `GET /paths/strict-send` | `route.Engine.quoteDEX`, via `BestPath`/`MeasureSlippage` | **The pricing engine.** Scores what a payment would deliver |
| `Client.OrderBook` | `GET /order_book` | `checks` metrics (`metric_spread`, `metric_depth`, `metric_deviation`, `metric_concentration`) | Market-health diagnostic only — never a pricing input |
| `Client.BestPath` | built on `StrictSendPaths` | `checks` (`metric_price_impact`, `metric_depth`) | Best-path selection, used by metrics |
| `Client.MeasureSlippage` | two `StrictSendPaths` calls | `route.Engine.quoteDEX` | Thickness warning |

The seam that matters is `StrictSendPaths`, and it has a hard requirement
beyond "returns a price": **it must return the full path set, not just the
best one.** `route.classify` decides DIRECT / DERIVATIVE / NO-MARKET from
*every* path Horizon returns, and `quoteDEX` picks the maximum itself because
"best first" is not a documented guarantee. Any replacement that returns one
path would silently change corridor classifications — the integrity verdict
is the first casualty of a "good enough" pathfinder.

A second, subtler requirement: the returned paths must include **AMM liquidity
pool routing**. The package doc comment in `dex/dex.go` records why this is
non-negotiable — on 2026-08-04, the live USDC/NGNC order book showed a best
bid of 333.33 NGNC/USDC with 2184.54 units of depth, while Horizon's own
strict-send pathfinder priced 100 USDC at 21,785.78 NGNC. That figure cannot
be reconstructed from the order book levels under either reading of the
amount field, because path settlement on Stellar draws on offers **and** AMM
pools together, and the order book endpoint reports only offers. **An
order-book-only replacement was already measured to be wrong on this
corridor.**

---

## 3. The options, with real costs and real risks

### Option A — Stay on SDF's hosted Horizon

Keep using `https://horizon.stellar.org` exactly as today.

- **Cost:** zero today. The cadence in `docs/deployment.md` (~110 requests
  per sweep, three dozen per corridor) is a planning estimate recorded there
  without an observation date — an order-of-magnitude figure, not a
  measurement — and is negligible for a public instance.
- **Risk:** the dependency sits on a service SDF has formally flagged for
  eventual deprecation. No feature development, and an announced shutdown
  would arrive with *some* notice — but the entire pricing engine dies the
  day the hosted service does, and there is no plan for that day.
- **What is lost:** nothing today. **Everything** on shutdown day, with AMM
  routing lost in the same stroke as everything else.
- **Verdict:** correct as the *default*, wrong as the *only* option.

### Option B — Third-party Horizon-compatible hosting

Providers that run Horizon themselves and expose its endpoints (e.g.
Validation Cloud's Stellar Horizon API, which documents
`/paths/strict-send` unchanged).

- **Cost:** per-request fees (Validation Cloud bills "compute units" per
  call); a commercial dependency where there is currently a free public one.
- **Risk:** the provider runs the same deprecated software, so it inherits
  Horizon's end-of-life status; the provider may lag protocol releases, drop
  endpoints, or change pricing. A single vendor becomes a new failure domain
  on top of the existing one.
- **What is lost:** nothing functionally — it is Horizon behind an API key.
  AMM routing survives. What is gained is a migration of *where* the request
  lands, not *what* the code does.
- **Verdict:** a stopgap, not a destination. Useful as redundancy; it does
  not answer the question "what happens when Horizon itself is gone".

### Option C — Third-party indexers (Stellar Expert, Hubble, etc.)

Indexers that expose ledgers, transactions, operations, effects and contract
events.

- **Cost:** subscription or self-host; then **all** pathfinding engineering
  is on Wayfare, because these products do not do pathfinding — they expose
  raw or lightly-transformed ledger data.
- **Risk:** rebuilding and *maintaining* pathfinding is a second product.
  The correctness bar is already set: order-book-only pricing was measured
  wrong on 2026-08-04, so the rebuild must track liquidity pool reserves and
  run a real path search over offers + pools, or it reproduces the same error
  it was meant to avoid.
- **What is lost:** **AMM liquidity pool routing**, unless it is rebuilt by
  hand — and the measured evidence says that is not optional. Corridor
  integrity classification additionally depends on the *full* path set, so a
  "best path" approximation changes verdicts, not just numbers.
- **Verdict:** fails the AMM test unless combined with Option D's state
  maintenance, at which point it is Option D with a worse data source.

### Option D — Self-hosted order book + AMM state (Galexie or captive core + own pathfinder)

Build and run the pathfinding service ourselves.

- **Cost:** this is the honest version of Option C. SDF's own Galexie docs
  state it plainly: Galexie is *"not intended or designed for live
  ingestion"* — it is a data-lake extractor. Their published numbers
  (stellar.org blog, 2024-08-29): a single instance takes **150 days to
  backfill full history**; the full public-network lake is **~3 TB**; running
  40+ parallel instances backfilled 10 years in under 5 days at ~$600 of
  compute, and continuous operation runs **~$160/month** for compute and
  storage. Then the pathfinding engine itself: maintain offer state and pool
  reserves from ledger data, run the search, keep it correct as the protocol
  evolves. Note the scope of the cited figure: **~$160/month is the Galexie
  continuous-export baseline** (compute + storage, per SDF's announcement) —
  the pathfinder itself, live order-book/pool state maintenance, database,
  monitoring and production operations are additional and unquantified here.
- **Risk:** a second product, with a correctness bar that is already
  demonstrated to be hard. Estimated effort is months of engineering plus
  permanent maintenance; that is the real cost, and the Galexie baseline
  above is the small part of it.
- **What is lost:** nothing, *if* pool state is maintained and the full path
  set is searched — but only if. An order-book-only implementation loses AMM
  routing and is already known to be wrong on this corridor.
- **Verdict:** the only option that survives the world where Horizon is
  gone. It is also the only one that is genuinely expensive. This is why the
  issue calls it "arguably a public good": the capability is ecosystem
  infrastructure nobody currently provides.

### Option E — Hybrid: stay on Horizon, make it swappable

Keep Horizon as the primary source, but remove the concrete
`*dex.Client` dependency so a replacement can be slotted in when a date is
announced — or never, if none is.

- **Cost:** one small refactor (Section 5). No behavior change.
- **Risk:** the seam is only insurance if it is exercised — a swap that is
  never tested rots. The mitigation is to keep the interface implemented by
  exactly one type today (`dex.Client`), so the contract is real without a
  second implementation to maintain.
- **What is lost:** nothing. AMM routing and full-path classification are
  untouched because the primary source is untouched.
- **Verdict:** the correct shape for a long-horizon, undated risk.

---

## 4. What is lost under each option, at a glance

| Option | AMM pool routing survives? | Full path set (integrity verdicts) survives? | Real cost | Time to deploy |
|:---|:---:|:---:|:---|:---|
| A — SDF Horizon (today) | ✅ | ✅ | zero | already live |
| B — third-party Horizon host | ✅ | ✅ | per-request fees | days |
| C — third-party indexers | ❌ unless rebuilt | ❌ unless rebuilt | rebuild + subscription | months |
| D — self-hosted book + AMM state | ✅ if pools tracked | ✅ if full search | months of engineering + ~$160/mo Galexie baseline (pathfinder, DB, monitoring additional, unquantified) | months |
| E — hybrid (A + seam) | ✅ | ✅ | one small refactor | days |

The AMM row is the one that eliminates options on its own. It is not a
preference — it is a measurement: order-book-only pricing was already wrong
on the flagship corridor.

---

## 5. The smallest change that keeps dex swappable

The minimal seam is to make the engine depend on what it calls, not on the
type that currently answers.

```go
// route: what the engine actually needs from the DEX
type Pathfinder interface {
    StrictSendPaths(ctx context.Context, source asset.Asset,
        amount decimal.Decimal, dest asset.Asset) ([]dex.Path, error)
    MeasureSlippage(ctx context.Context, source asset.Asset,
        amount decimal.Decimal, dest asset.Asset,
        probe decimal.Decimal) (*dex.Slippage, error)
}

// route.Engine.DEX changes from *dex.Client to Pathfinder.
```

`dex.Client` already satisfies this — the change is mechanical and changes
no behavior. It deliberately does **not** include `OrderBook`: that method is
a diagnostic for `checks`, a separate consumer, and keeping the interface
narrow to exactly what `route` calls means the contract is the surface that
must be reproduced, nothing more. The same treatment can be applied to
`checks` metrics if desired, but `route` is the critical path.

Why this shape:

- **The full path set is a semantic contract, not a compile-time one.**
  `StrictSendPaths` returns `[]dex.Path`, and `route` passes that slice
  whole to `classify` — but nothing in the type forces an implementation to
  return *every* path. A "best path only" implementation would compile, and
  would silently change DIRECT / DERIVATIVE / NO-MARKET verdicts. Completeness
  must therefore be verified, not assumed: a conformance test over a fixture
  holding multiple valid paths (including an AMM-routed and a native-hop
  case) must assert the replacement returns all of them, alongside the
  existing recorded-snapshot integrity tests that already pin full-set
  classification against real bytes. The interface is what makes that test
  enforceable for any future implementer.
- **Slippage is part of the contract.** `MeasureSlippage` is two
  pathfinding calls with a probe, so a replacement must handle the
  size-dependence pricing the engine already warns about.
- **One implementer today.** No dead abstraction: `dex.Client` is the only
  type satisfying it, so the interface is a statement of the current
  contract, not speculative generality.

Cost: a few hours including test updates — an unvalidated estimate, with no
observation basis beyond the size of the change itself. (The existing tests
construct `&dex.Client{...}` and keep working unchanged because the concrete
type still satisfies the interface.) **This is the entire scope of the
immediate work** — defer the decision without forgetting it.

---

## 6. Recommendation

**Do Option E now; re-open the decision only when the facts change.**

1. **Immediately** — land the `Pathfinder` interface seam (Section 5). It is
   the whole answer to "defer without forgetting": the moment a replacement
   exists, `route` accepts it with no further refactor, and the moment a
   shutdown date is announced, the question is reduced from "how do we
   migrate" to "which implementer do we point at".
2. **Do not build the second product yet.** No announced date means no
   deadline, and Option D is months of engineering whose only trigger is a
   date that does not exist. When the trigger fires, the reasoning is
   already written: full path set, AMM pools included, or the replacement
   fails the corridor's own measurement bar.
3. **Re-check the date before any future decision.** The one input that
   changes everything is a shutdown announcement. The sources in Section 1
   are the checklist; a future issue should start by re-reading them and
   recording what changed.
4. **Watch two signals:** (a) an SDF announcement of a Horizon retirement
   date, and (b) whether Horizon's "protocol-compatibility updates" continue
   — the second is the leading indicator for the first. Option B (a
   third-party Horizon host) is a reasonable stopgap if either signal
   appears before a decision is made, because it preserves AMM routing with
   zero behavior change while the real decision is taken.

**Why not the alternatives:** Option C is disqualified by measurement (AMM
routing is lost and was already proven to matter); Option D is the correct
long-run answer but is expensive and has no trigger today; Option B is a
redirection, not a migration, and inherits Horizon's end-of-life status.
Option E costs a few hours, loses nothing, and keeps every future option
open.

---

## 7. Related

- [`dex/dex.go`](../dex/dex.go) — the coupling surface and the 2026-08-04
  AMM-vs-order-book measurement that sets the correctness bar
- [`dex/health.go`](../dex/health.go) — the `/order_book` diagnostic
- [`route/route.go`](../route/route.go) — `quoteDEX` and `classify`, the
  consumers that require the full path set
- [`docs/upstream-failures.md`](upstream-failures.md) — how Horizon failure
  shapes are already contracted
- [`docs/deployment.md`](deployment.md) — request cadence against Horizon
- [`docs/snapshot-format.md`](snapshot-format.md) — the test seam that keeps
  pathfinding tests offline today
