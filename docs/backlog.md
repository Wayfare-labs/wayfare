# Wayfare — contributor backlog

A repository-grounded backlog. Every issue below references a file, function or
observed response that was verified against the tree at commit `93f5cda` and
against the live deployment on 2026-08-24. Nothing here describes a component
that does not exist without saying so explicitly.

**Live:** https://wayfare-cdb9.onrender.com/ · **Source:**
https://github.com/Wayfare-labs/wayfare

**Every entry below is filed as a GitHub issue** and links to it. Entries whose
work was already tracked by an earlier issue link to that issue rather than a
duplicate. Numbering here (`#1`–`#276`) is this document's own; the linked
number is the tracker's.

---

## A. Architecture snapshot

### What exists today

This is the flow as implemented, not as aspired to. Package names are the real
ones; every arrow is a call that exists in the tree.

```
  REFERENCE PROVIDERS (×2)              HORIZON (mainnet)
  exchangerate-api, currency-api        strict-send paths, /order_book
  refrate/exchangerate.go               dex/dex.go
  refrate/currencyapi.go                dex/health.go
         │                                       │
         │ refrate/cached.go   (bounded age)     │
         │ refrate/cross.go    (divergence)      │
         └───────────────┬───────────────────────┘
                         ▼
              route.Engine — route/route.go
              route.Ladder — route/ladder.go
              DefaultSizes 0.1 … 5000
              per rung:     effective rate → loss vs mid → verdictFor()
              per corridor: integrity (DIRECT / DERIVATIVE / NO-MARKET / UNKNOWN)
                         │
                         ▼
              checks.Runner — checks/runner.go
              ForAsset() → 3 checks:
                AnchorAssetISO4217, SEP10EndpointResponds, IssuerAuthFlags
                         │
                         ▼
              route.WithFindings — route/wire.go
              the ONLY composition point; branches on nothing
                         │
           ┌─────────────┴─────────────┐
           ▼                           ▼
    monitor.Scheduler            server.Server
    monitor/monitor.go           server/api.go
    every 6h, no HTTP            /api/corridor  /api/assets  /healthz
           │                     /  → embedded index.html (server/ui.go)
           ▼                           │
    runstore  ◀──────────────────────  ┘  on live failure: staleJSON(), live:false
    hash-chained NDJSON
    runstore/runstore.go
    data/USDC-{NGNC,GHSC,KESC}.ndjson
```

Recording and replay sit beside this rather than inside it: `snapshot/record.go`
captures upstream bytes, `snapshot/replay.go` serves them back, and CI runs the
whole suite inside a network namespace with no route out
(`.github/workflows/ci.yml`, job `offline-tests`).

### What is implemented but not connected

Two blocks of merged code have no caller. This is the largest single finding of
the sweep and it shapes the V2 initiative.

```
  checks/metric.go          Metric interface, RunMetric panic guard
  checks/metric_spread.go           spread.bid-ask
  checks/metric_depth.go            depth.observed-executable
  checks/metric_price_impact.go     price-impact.size
  checks/metric_concentration.go    concentration.liquidity
  checks/wire.go            MetricJSON, FindingsJSON.Metrics
        │
        ╳  NOT REACHABLE
           checks.Runner has no Metrics field.
           Runner.Default() returns three checks and no metrics.
           Runner.ForAsset() never calls RunMetric.
           Subject.Send / Subject.Receive are never populated.

  route/cost.go             Decompose(), CostDecomposition, four components
        │
        ╳  NOT REACHABLE
           No non-test caller anywhere in the tree.
           route.CorridorJSON has no field for it.
```

Confirmed on the wire: `GET /api/corridor?to=NGNC` on the live deployment
returns no `findings.metrics` key and no cost block.

### What does not exist

```
  ┌─────────────────────────────────────────────────────────┐
  │  NOT YET IMPLEMENTED — no packages, no stubs            │
  │                                                          │
  │  HISTORICAL ANALYSIS                                     │
  │    statistics over runstore records                      │
  │    runstore holds headline figures only — no metrics,    │
  │    no checks — so there is nothing yet to analyse        │
  │                          ↓                               │
  │  PREDICTIVE INTELLIGENCE                                 │
  │    failure probability, expected slippage, anomalies     │
  │    blocked on history that does not exist                │
  │                          ↓                               │
  │  VERIFIABLE INTELLIGENCE                                 │
  │    signed or on-chain corridor attestation               │
  │    blocked on a publisher-trust model the project        │
  │    does not currently have                               │
  └─────────────────────────────────────────────────────────┘
```

The absence is deliberate. From the README: *"Layers 3 and 4 have no packages
and no stubs, deliberately. Speculative structure is worse than none: an empty
package invites code that has no inputs yet."* No issue in this backlog creates
one.

### V1–V6 against the four-layer epistemic model

| Version | Layer | State | Evidence |
|:---|:---|:---|:---|
| **V1** Quote integrity | 1 Observable fact + 2 Deterministic calculation | **DONE, hardening** | `route`, `dex`, `refrate`, `runstore`, `snapshot` all implemented and tested; 89.1% coverage in `route` |
| **V2** Execution economics | 2 Deterministic calculation | **IN PROGRESS** | four metrics + cost decomposition merged; **none reachable** — see initiative B |
| **V3** Market structure & history | 2 Deterministic, over time | **BACKLOG** | `runstore` has records; no analysis layer, and no metrics recorded to analyse |
| **V4** Historical intelligence | 2→3 boundary | **NOT YET** | needs V3 plus months of accumulated observations |
| **V5** Predictive intelligence | 3 Probabilistic inference | **NOT YET** | research spikes only in this backlog |
| **V6** Verifiable intelligence | 4 Verifiable output | **NOT YET** | research spikes only in this backlog |

The governing rule, from the README: **a layer can never be more certain than
the layer beneath it.** A layer-2 calculation on an unavailable layer-1 fact is
unknown, not a default.

### How to read an entry

```
#12 — Title in the project's style
Description, naming the file or function it touches.
`V2` `area:pricing` `difficulty:medium` `ready`
```

State markers: `ready` — start now. `blocked` — waits on a named issue.
`research` — produces a written finding, not code. `future` — V4+ exploration,
deliberately not an implementation task.

Labels are the repository's real taxonomy (`gh label list`): `area:pricing`,
`area:corridor`, `area:ui`, `area:tests`, `area:docs`, `area:data`,
`area:design`, `area:devops`, `area:research`, `area:ecosystem`,
`difficulty:easy|medium|hard`, `good first issue`, `help wanted`,
`needs-maintainer-review`, `blocked`.

---

## B. Initiative A — V1 Hardening

The quote engine works. This initiative makes it defensible: contract fidelity,
boundary correctness, upstream robustness, and coverage where the published
claims live. Nothing here adds a capability.

### A1 — Wire-contract fidelity

`server/api.go` has two independent producers of `route.CorridorJSON`:
`route.ToCorridorJSON` for a live measurement and `staleJSON` for a stored one.
They have drifted, and the deployed instance serves only the second.

**#1 — UI presents a stored reading as a live measurement** *(filed: [#79](https://github.com/Wayfare-labs/wayfare/issues/79))*
`server/index.html` never reads `live` or `stale`; a 45-hour-old reading renders
as current under a footer claiming nothing is cached.
`V1` `area:ui` `bug` `difficulty:medium` `ready`

**#2 — Stale responses drop reference_agreement and report scored:false** *(filed: [#80](https://github.com/Wayfare-labs/wayfare/issues/80))*
`staleJSON` leaves `ReferenceAgreement` empty and `Scored` at its zero value, so
every history-served corridor claims to be unscorable.
`V1` `area:pricing` `bug` `needs-maintainer-review` `difficulty:medium` `ready`

**#3 — Stale responses emit loss_amount as an empty string** *(filed: [#81](https://github.com/Wayfare-labs/wayfare/issues/81))*
`runstore.Rung` stores no loss amount, so `QuoteJSON.LossAmount` marshals as
`""` — not a decimal string, and not absent either.
`V1` `area:pricing` `bug` `good first issue` `difficulty:easy` `ready`

**#4 — Stale responses carry no findings block** *(filed: [#112](https://github.com/Wayfare-labs/wayfare/issues/112))*
`staleJSON` cannot attach checks because `runstore.Record` has nowhere to store
them; a history-served corridor silently loses every counterparty fact.
`V1` `area:data` `difficulty:medium` `blocked` — on #62

**#5 — One test should compare all three producers of the corridor shape** *(filed: [#113](https://github.com/Wayfare-labs/wayfare/issues/113))*
`ToCorridorJSON`, `staleJSON` and `cmd/ladder -json` must agree on the field
set; today none of them is compared with another.
`V1` `area:tests` `difficulty:medium` `ready`

**#6 — Assert every money field is a parseable decimal, on every path** *(filed: [#114](https://github.com/Wayfare-labs/wayfare/issues/114))*
The README says there is a test at the boundary; extend it to walk the stale
document and the CLI document, not only the live one.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#7 — reference_fetched_at is dropped on the stale path** *(filed: [#115](https://github.com/Wayfare-labs/wayfare/issues/115))*
`staleJSON` never sets it, so a reader cannot tell how old the benchmark behind
a stored reading was when it was taken.
`V1` `area:pricing` `difficulty:easy` `ready`

> **Implemented.** This entry's fix required a run-record layout change — the
> record had nowhere to store the fetch timestamp — so it was done as the
> Version 3 migration it names as its own review bar: `runstore.Reference`
> gained `fetched_at` (omitempty, after every Version 2 field), Version 2
> chains still load and verify unchanged, and `staleJSON` now publishes
> `reference_fetched_at` from the record. See `docs/run-store.md`,
> "Migration to version 3".

**#8 — depends_on loses issuer identity on the stale path** *(filed: [#116](https://github.com/Wayfare-labs/wayfare/issues/116))*
`staleJSON` builds `route.AssetJSON{Code: code}` from stored codes alone; an
asset code identifies nothing, and the issuer is the identity.
`V1` `area:corridor` `difficulty:easy` `ready`

### A2 — Verdict and threshold correctness

**#9 — Loss displayed at 2dp while the verdict grades at full precision** *(filed: [#83](https://github.com/Wayfare-labs/wayfare/issues/83))*
`ToQuoteJSON` uses `StringFixed(2)`; `verdictFor` grades the unrounded value, so
20.001% publishes as `"20.00"` and grades UNUSABLE.
`V1` `area:pricing` `needs-maintainer-review` `difficulty:medium` `ready`

**#10 — Verdict thresholds have no test at exactly 3%, 8% and 20%** *(filed: [#88](https://github.com/Wayfare-labs/wayfare/issues/88))*
`verdictFor` uses `LessThanOrEqual` at each band; nothing fails if a refactor
changes one to `LessThan`.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#11 — Reference divergence has no test at the exact 2% and 10% boundaries** *(filed: [#87](https://github.com/Wayfare-labs/wayfare/issues/87))*
Those two numbers decide which mid every published verdict was scored against.
`refrate` is at 47.9% coverage.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#12 — The recommendation rule needs a test at exactly the POOR boundary** *(filed: [#117](https://github.com/Wayfare-labs/wayfare/issues/117))*
At 20.0% a quote is POOR, therefore acceptable, therefore recommendable; one
operator separates "we recommend this" from "we recommend nothing".
`V1` `area:tests` `difficulty:easy` `ready`

**#13 — Pin the STALE reference-agreement selection rule** *(filed: [#118](https://github.com/Wayfare-labs/wayfare/issues/118))*
`refrate/cross.go` picks the fresher feed when the two describe different
moments; `cross_test.go` does not exercise the selection.
`V1` `area:tests` `difficulty:medium` `ready`

**#14 — Pin the DISAGREE conservative-mid selection** *(filed: [#119](https://github.com/Wayfare-labs/wayfare/issues/119))*
Between 2% and 10% the *more conservative* mid — the one producing the higher
loss — must be scored against, in both provider orderings.
`V1` `area:tests` `difficulty:easy` `ready`

### A3 — Upstream robustness and error paths

**#15 — No route-level fixtures for malformed strict-send responses** *(filed: [#89](https://github.com/Wayfare-labs/wayfare/issues/89))*
#69 covers `dex` health; the layer that turns paths into published verdicts has
no equivalent. Zeros that parse are the dangerous case.
`V1` `area:tests` `difficulty:medium` `ready`

**#16 — API errors carry no machine-readable code** *(filed: [#86](https://github.com/Wayfare-labs/wayfare/issues/86))*
`writeError` emits prose only; a client cannot distinguish an unknown asset from
an upstream timeout without substring matching.
`V1` `area:ui` `difficulty:easy` `ready`

**#17 — Partial upstream failure has no defined contract** *(filed: [#120](https://github.com/Wayfare-labs/wayfare/issues/120))*
`Ladder` returns a result when some rungs error and `res.Failed()` only catches
the all-failed case; what a half-measured ladder means is undocumented.
`V1` `area:pricing` `needs-maintainer-review` `difficulty:medium` `ready`

**#18 — Horizon rate-limit responses are not distinguished from failures** *(filed: [#121](https://github.com/Wayfare-labs/wayfare/issues/121))*
A 429 and a 500 both surface as a generic rung error, so a corridor that is
merely throttled looks like one that is broken.
`V1` `area:pricing` `difficulty:medium` `ready`

**#19 — Reference provider timeout behaviour is untested** *(filed: [#122](https://github.com/Wayfare-labs/wayfare/issues/122))*
`refrate/cached.go` bounds rate age; what happens when both providers time out
and a cached rate is still within its bound has no test.
`V1` `area:tests` `difficulty:medium` `ready`

**#20 — anchor/salvage.go has no direct test** *(filed: [#123](https://github.com/Wayfare-labs/wayfare/issues/123))*
The salvage path exists to recover a partially-broken `stellar.toml`; it is
exercised only indirectly, and `anchor` sits at 67.2% coverage.
`V1` `area:tests` `difficulty:medium` `ready`

**#21 — GuardedClient's internal-address refusal needs adversarial cases** *(filed: [#124](https://github.com/Wayfare-labs/wayfare/issues/124))*
`checks/transport.go` refuses to probe internal addresses; add cases for
redirects to private ranges, DNS rebinding shapes, and IPv6 literals.
`V1` `area:tests` `difficulty:medium` `ready`

**#22 — Context cancellation is not exercised across the engine** *(filed: [#125](https://github.com/Wayfare-labs/wayfare/issues/125))*
`RunMetric` checks `ctx.Err()` before running; the equivalent paths in `route`,
`dex` and `refrate` have no cancellation test.
`V1` `area:tests` `difficulty:medium` `ready`

### A4 — Coverage where the claims live

Baseline from `go test ./... -cover`, all green: `route` 89.1 · `dex` 88.5 ·
`server` 76.7 · `snapshot` 75.7 · `asset` 74.5 · `monitor` 72.3 · `anchor` 67.2 ·
`checks` 65.4 · `sep38` 55.0 · `runstore` 48.8 · `refrate` 47.9 · `cmd/*` 0.0.

**#23 — refrate: cache expiry and eviction edge cases** *(filed: [#126](https://github.com/Wayfare-labs/wayfare/issues/126))*
`refrate/cached.go` is the least-covered file in the least-covered package, and
every measurement depends on it.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#24 — refrate: provider error taxonomy** *(filed: [#127](https://github.com/Wayfare-labs/wayfare/issues/127))*
Distinguish a provider that answered with an error, one that answered with an
unparseable body, and one that did not answer.
`V1` `area:tests` `difficulty:medium` `ready`

**#25 — refrate: SINGLE agreement when only one provider answers** *(filed: [#128](https://github.com/Wayfare-labs/wayfare/issues/128))*
The uncorroborated case is the one most likely to occur in production and is
thinly covered.
`V1` `area:tests` `difficulty:easy` `ready`

**#26 — runstore: chain verification against a tampered middle record** *(filed: [#129](https://github.com/Wayfare-labs/wayfare/issues/129))*
`runstore` is at 48.8%; the property the package exists for — naming the first
record that does not reconcile — deserves a direct adversarial test.
`V1` `area:tests` `area:data` `difficulty:medium` `ready`

**#27 — runstore: partial write and truncated final line** *(filed: [#130](https://github.com/Wayfare-labs/wayfare/issues/130))*
NDJSON appended by a process killed mid-write is the realistic corruption; #66
covers the family, this is the specific case worth pinning first.
`V1` `area:tests` `area:data` `difficulty:medium` `ready`

**#28 — runstore: Version mismatch must refuse, not coerce** *(filed: [#131](https://github.com/Wayfare-labs/wayfare/issues/131))*
A replayer must refuse a version it does not know; assert the refusal rather
than assuming it.
`V1` `area:tests` `area:data` `difficulty:easy` `ready`

**#29 — sep38: fee-denomination identity against adversarial quotes** *(filed: [#132](https://github.com/Wayfare-labs/wayfare/issues/132))*
`sep38/golden_test.go` pins the spec's worked example; add quotes where the fee
is denominated in the other asset and where it is absent.
`V1` `area:tests` `area:corridor` `difficulty:medium` `ready`

**#30 — snapshot: refuse a snapshot whose hash does not match** *(filed: [#133](https://github.com/Wayfare-labs/wayfare/issues/133))*
`snapshot/snapshot.go` verifies on load; the negative path is what makes the
guarantee real.
`V1` `area:tests` `difficulty:easy` `ready`

**#31 — snapshot: provenance refuses a dirty tree** *(filed: [#134](https://github.com/Wayfare-labs/wayfare/issues/134))*
The README claims this; assert it, including the case where only an untracked
file is present.
`V1` `area:tests` `difficulty:medium` `ready`

**#32 — monitor: a failing sweep must not break the chain** *(filed: [#135](https://github.com/Wayfare-labs/wayfare/issues/135))*
`monitor/monitor.go` writes on a schedule; assert that a corridor that fails to
measure leaves the chain valid and the gap visible.
`V1` `area:tests` `area:data` `difficulty:medium` `ready`

**#33 — cmd/ladder and cmd/wayfared have zero coverage** *(filed: [#136](https://github.com/Wayfare-labs/wayfare/issues/136))*
Both are thin, but both parse flags that change what is measured; a smoke test
per binary is cheap and currently absent.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#34 — asset: reject a lookup that differs only by issuer** *(filed: [#137](https://github.com/Wayfare-labs/wayfare/issues/137))*
`asset.Lookup` resolves by code; assert that two assets sharing a code and
differing by issuer are never conflated.
`V1` `area:tests` `area:corridor` `difficulty:easy` `ready`

### A5 — API surface hardening

**#35 — /api/corridor accepts sizes of any magnitude** *(filed: [#90](https://github.com/Wayfare-labs/wayfare/issues/90))*
`parseSizes` bounds the count at 24 and rejects non-positives, but not
magnitude, precision, or duplicates — each size is a Horizon round trip.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#36 — cmd/ladder runs no checks, so CLI and API disagree** *(filed: [#85](https://github.com/Wayfare-labs/wayfare/issues/85))*
`grep 'checks\.' cmd/ladder/main.go` returns nothing while `cmd/wayfared`
constructs a `checks.Runner`.
`V1` `area:pricing` `difficulty:medium` `ready`

**#37 — /api/assets exposes no corridor state** *(filed: [#138](https://github.com/Wayfare-labs/wayfare/issues/138))*
It reports `can_be_destination` from `asset.FiatPeg` and nothing about whether
the corridor has ever priced; the UI needs the latter to build a selector.
`V1` `area:ui` `difficulty:medium` `ready`

**#38 — No CORS policy is stated** *(filed: [#139](https://github.com/Wayfare-labs/wayfare/issues/139))*
`server.Handler` sets no CORS headers, so browser consumers on another origin
cannot call the API and no decision has been recorded either way.
`V1` `area:devops` `difficulty:easy` `ready`

**#39 — Response bodies are indented on every request** *(filed: [#140](https://github.com/Wayfare-labs/wayfare/issues/140))*
`writeJSON` calls `enc.SetIndent("", "  ")` unconditionally; a `?pretty` opt-in
would cut payload size for consumers without losing readability for humans.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#40 — Unknown query parameters are silently ignored** *(filed: [#141](https://github.com/Wayfare-labs/wayfare/issues/141))*
A typo like `?tp=NGNC` silently measures the default corridor; strict parameter
handling would turn a silent wrong answer into an error.
`V1` `area:ui` `difficulty:easy` `ready`

### A6 — Deployment and data reliability

**#41 — /healthz reports process liveness and nothing about data** *(filed: [#142](https://github.com/Wayfare-labs/wayfare/issues/142))*
`handleHealth` returns a constant `{"status":"ok"}`; on a `-history-first`
deployment the thing at risk is the age of the data, which it cannot express.
`V1` `area:devops` `difficulty:easy` `ready`

**#42 — The deployed instance serves history embedded at build time** *(filed: [#143](https://github.com/Wayfare-labs/wayfare/issues/143))*
`render.yaml` runs `-schedule=0 -history-first` and `embed.go` embeds `data/`,
so freshness depends on redeploys, not on the scheduler. Undocumented.
`V1` `area:devops` `area:docs` `difficulty:medium` `ready`

**#43 — docs/deployment.md documents Fly.io; production is Render** *(filed: [#144](https://github.com/Wayfare-labs/wayfare/issues/144))*
`fly.toml` and `render.yaml` both exist; the document describes only the one
that is not deployed.
`V1` `area:docs` `difficulty:medium` `ready`

**#44 — Cold start is real and undocumented** *(filed: [#145](https://github.com/Wayfare-labs/wayfare/issues/145))*
The first request to the sleeping free instance failed at the connection stage
during this sweep; the second succeeded in 0.6s. #53 covers the UI half.
`V1` `area:devops` `area:docs` `difficulty:easy` `ready`

**#45 — No deployment smoke check runs after a deploy** *(filed: [#146](https://github.com/Wayfare-labs/wayfare/issues/146))*
CI builds and tests the image (`ci.yml`, job `docker`) but nothing verifies the
deployed instance answers correctly afterwards.
`V1` `area:devops` `difficulty:medium` `ready`

**#46 — The measure workflow's failure is silent** *(filed: [#147](https://github.com/Wayfare-labs/wayfare/issues/147))*
`measure.yml` failing to push (#63) produced no alert; the only symptom was data
quietly ceasing to advance.
`V1` `area:devops` `difficulty:medium` `ready`

**#47 — No rollback procedure is documented** *(filed: [#148](https://github.com/Wayfare-labs/wayfare/issues/148))*
Render redeploys from the Dockerfile; what to do when a bad build ships, and how
to verify the chain afterwards, is not written down.
`V1` `area:docs` `area:devops` `difficulty:easy` `ready`

**#48 — Verify the embedded chain at startup, and say so** *(filed: [#149](https://github.com/Wayfare-labs/wayfare/issues/149))*
`embed.go` says the history is "verified at startup"; assert it, and make the
result observable rather than implicit.
`V1` `area:data` `difficulty:medium` `ready`

---

## C. Initiative B — V2 Execution Economics

The measurements exist; the plumbing does not. Everything in B1 unblocks
everything after it, so B1 is the critical path for the entire V2 milestone.

### B1 — Metric plumbing (critical path)

**#49 — checks.Runner cannot run metrics, so every V2 metric is unreachable** *(filed: [#91](https://github.com/Wayfare-labs/wayfare/issues/91))*
Add `Metrics []Metric`, a corridor-scoped subject populating `Subject.Send` and
`Subject.Receive`, and a `RunMetric` sweep with a bounded request budget.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:hard` `ready`

**#50 — Expose metrics on /api/corridor** *(filed: [#92](https://github.com/Wayfare-labs/wayfare/issues/92))*
`checks.FindingsJSON.Metrics` already serialises; it has never been non-empty.
`V2` `area:pricing` `difficulty:medium` `blocked` — on #49

**#51 — Render metrics in the UI** *(filed: [#94](https://github.com/Wayfare-labs/wayfare/issues/94))*
`findings(f)` in `server/index.html` maps `f.checks` and never reads
`f.metrics`; an undetermined metric must not look like a failed check.
`V2` `area:ui` `difficulty:medium` `blocked` — on #50

**#52 — cmd/ladder should emit metrics too** *(filed: [#150](https://github.com/Wayfare-labs/wayfare/issues/150))*
Once metrics run, the CLI must carry them or the two producers of the shape
diverge again.
`V2` `area:pricing` `difficulty:easy` `blocked` — on #49

**#53 — Bound and document the upstream request cost of a metric sweep** *(filed: [#151](https://github.com/Wayfare-labs/wayfare/issues/151))*
`Descriptor.Cost` distinguishes `CostOneRequest` from `CostExpensive`; the depth
metric alone sweeps five sizes against a shared public Horizon.
`V2` `area:pricing` `difficulty:medium` `blocked` — on #49

**#54 — Recorded snapshot fixtures for every metric** *(filed: [#106](https://github.com/Wayfare-labs/wayfare/issues/106))*
Every determined path and every undetermined branch needs a recorded fixture so
the suite still passes inside the no-network CI job.
`V2` `area:tests` `difficulty:medium` `ready`

**#55 — Define what book metrics report for DERIVATIVE and NO-MARKET corridors** *(filed: [#105](https://github.com/Wayfare-labs/wayfare/issues/105))*
Two of three supported corridors have no direct book; "no pair by construction",
"empty book" and "fetch failed" are three different facts.
`V2` `area:pricing` `difficulty:medium` `ready`

**#56 — Book-based metrics exclude AMM liquidity while the ladder prices through it** *(filed: [#104](https://github.com/Wayfare-labs/wayfare/issues/104))*
`/order_book` is offers-only; strict-send pathfinding is not. The two describe
different markets and are about to be published side by side.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:hard` `ready`

**#57 — Make each metric's liquidity venue machine-readable** *(filed: [#152](https://github.com/Wayfare-labs/wayfare/issues/152))*
The limitation currently lives in `Descriptor.CannotDetermine` prose; a consumer
cannot act on prose.
`V2` `area:pricing` `difficulty:easy` `blocked` — on #56

### B2 — Spread

**#58 — Publish the book mid alongside the spread** *(filed: [#153](https://github.com/Wayfare-labs/wayfare/issues/153))*
`SpreadMetric.Run` computes a mid to derive `(ask-bid)/mid` and discards it; the
mid is independently useful.
`V2` `area:pricing` `good first issue` `difficulty:easy` `ready`

**#59 — Metric: deviation between the book mid and the reference mid** *(filed: [#103](https://github.com/Wayfare-labs/wayfare/issues/103))*
Splits market mispricing from routing cost — where a corridor's loss actually
lives. Same order book, no extra request.
`V2` `area:pricing` `difficulty:medium` `ready`

**#60 — Spread on the underlying pair for DERIVATIVE corridors** *(filed: [#154](https://github.com/Wayfare-labs/wayfare/issues/154))*
GHSC routes through NGNC; measuring NGNC's book and saying so explicitly is more
useful than reporting undetermined.
`V2` `area:pricing` `difficulty:medium` `blocked` — on #55

**#61 — Record spread provenance: which endpoint, which pair, which timestamp** *(filed: [#155](https://github.com/Wayfare-labs/wayfare/issues/155))*
Evidence validation in `RunMetric` requires non-blank source and observed and a
non-zero `ObservedAt`; assert the spread metric's evidence satisfies a reader.
`V2` `area:tests` `difficulty:easy` `ready`

### B3 — Executable depth

**#62 — Record check and metric results in runstore** *(filed: [#93](https://github.com/Wayfare-labs/wayfare/issues/93))*
Version 2 record plus migration. Without it no metric is ever written down and
V3 has nothing to analyse.
`V3` `area:data` `needs-maintainer-review` `difficulty:hard` `ready`

**#63 — Observed vs executable depth, reported separately** *(filed: [#37](https://github.com/Wayfare-labs/wayfare/issues/37))*
Tracked as #37 with an open PR; do not duplicate. Listed here for the capability
map.
`V2` `area:pricing` `difficulty:hard` `ready`

**#64 — Depth sizes should follow the ladder, not a private list** *(filed: [#156](https://github.com/Wayfare-labs/wayfare/issues/156))*
`defaultDepthSizes` in `metric_depth.go` is `1,10,100,1000,5000`;
`route.DefaultSizes` starts at 0.1. Two ladders measuring one corridor.
`V2` `area:pricing` `difficulty:easy` `ready`

**#65 — Depth at the dust size is the structural-floor probe** *(filed: [#157](https://github.com/Wayfare-labs/wayfare/issues/157))*
The project's founding finding is that 0.1 USDC isolates the floor; the depth
metric never measures there.
`V2` `area:pricing` `difficulty:medium` `ready`

**#66 — Distinguish depth exhausted from depth unmeasured** *(filed: [#158](https://github.com/Wayfare-labs/wayfare/issues/158))*
A book that runs out at 1000 and a book that could not be read must not produce
the same undetermined result.
`V2` `area:pricing` `difficulty:medium` `ready`

### B4 — Price impact

**#67 — Price impact reports one figure, not a curve** *(filed: [#159](https://github.com/Wayfare-labs/wayfare/issues/159))*
`PriceImpactMetric` compares a probe size against a full size; the shape between
them is where the corridor's behaviour lives.
`V2` `area:pricing` `difficulty:medium` `ready`

**#68 — Probe and full size are unset by default** *(filed: [#160](https://github.com/Wayfare-labs/wayfare/issues/160))*
`ProbeSize` and `FullSize` are plain fields with no defaults documented; a
zero-valued probe silently changes what is measured.
`V2` `area:pricing` `good first issue` `difficulty:easy` `ready`

**#69 — Price impact must not be computed against an unscorable reference** *(filed: [#161](https://github.com/Wayfare-labs/wayfare/issues/161))*
When the reference cross-check is MALFUNCTION no verdict may be issued; the same
discipline should govern derived quantities.
`V2` `area:pricing` `difficulty:medium` `ready`

### B5 — Liquidity concentration

**#70 — Concentration is measured over price levels, not participants** *(filed: [#162](https://github.com/Wayfare-labs/wayfare/issues/162))*
`metric_concentration.go` documents this honestly; the limitation should reach
the wire so a consumer cannot over-read HHI.
`V2` `area:pricing` `difficulty:easy` `ready`

**#71 — Research: can account-level concentration be measured at all?** *(filed: [#163](https://github.com/Wayfare-labs/wayfare/issues/163))*
`/order_book` does not expose the offering account. Whether `/offers` or another
endpoint can, and at what cost, is unknown.
`V2` `area:research` `difficulty:medium` `research`

**#72 — Concentration needs a deep-book fixture** *(filed: [#164](https://github.com/Wayfare-labs/wayfare/issues/164))*
Existing fixtures cover empty and one-sided books; HHI is uninteresting on both.
`V2` `area:tests` `difficulty:easy` `ready`

### B6 — Execution curves and marginal cost

**#73 — Execution-rate curve P_exec(q) over the ladder** *(filed: [#98](https://github.com/Wayfare-labs/wayfare/issues/98))*
Publish the rate-versus-size relationship as an object, with monotonicity
explicitly not assumed — a larger size can open a better path.
`V2` `area:pricing` `difficulty:medium` `ready`

**#74 — Marginal execution cost from adjacent ladder points** *(filed: [#70](https://github.com/Wayfare-labs/wayfare/issues/70))*
Tracked as #70 in the repository. Builds directly on the curve.
`V2` `area:pricing` `difficulty:medium` `ready`

**#75 — Non-monotonic curves are a finding, not an error** *(filed: [#165](https://github.com/Wayfare-labs/wayfare/issues/165))*
If a measured curve improves with size, that must be reportable rather than
smoothed, sorted or discarded.
`V2` `area:pricing` `difficulty:medium` `blocked` — on #73

**#76 — Never interpolate between measured rungs** *(filed: [#166](https://github.com/Wayfare-labs/wayfare/issues/166))*
The curve has holes where rungs did not price; a drawn line between two measured
points is an inference and must be labelled as one.
`V2` `area:pricing` `difficulty:easy` `blocked` — on #73

### B7 — Effective transfer cost

**#77 — route.Decompose is dead code — wire it into the ladder and the response** *(filed: [#95](https://github.com/Wayfare-labs/wayfare/issues/95))*
No non-test caller; `CorridorJSON` has no field for it.
`V2` `area:pricing` `difficulty:medium` `ready`

**#78 — Cost decomposition reports fees as a determined zero** *(filed: [#96](https://github.com/Wayfare-labs/wayfare/issues/96))*
`Determined: true` with `decimal.Zero` and the comment "negligible for DEX
routes" — the one default-to-zero in the pricing path.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:medium` `ready`

**#79 — Cost decomposition leaves slippage undetermined although the ladder measures it** *(filed: [#97](https://github.com/Wayfare-labs/wayfare/issues/97))*
`Decompose` takes one `Quote` and structurally cannot see a neighbour; decompose
at the ladder level instead.
`V2` `area:pricing` `difficulty:medium` `ready`

**#80 — Expected failure cost must stay undetermined, and say why** *(filed: [#167](https://github.com/Wayfare-labs/wayfare/issues/167))*
`route/cost.go` already does this correctly; add the test that stops a future
change from quietly defaulting it to zero.
`V2` `area:tests` `good first issue` `difficulty:easy` `ready`

**#81 — Components must reconcile with the published total** *(filed: [#168](https://github.com/Wayfare-labs/wayfare/issues/168))*
Assert that determined components sum to the total loss within a stated
tolerance, or that the shortfall is explicitly attributed to undetermined parts.
`V2` `area:tests` `difficulty:medium` `blocked` — on #79

**#82 — Anchor fees are a separate component from network fees** *(filed: [#169](https://github.com/Wayfare-labs/wayfare/issues/169))*
`sep38` can price an anchor's fee where one is published; NGNC's anchor
publishes no `ANCHOR_QUOTE_SERVER`, which is a fact worth reporting.
`V2` `area:corridor` `difficulty:medium` `ready`

### B8 — Route structure

**#83 — The response keeps only the best path and discards the alternatives** *(filed: [#99](https://github.com/Wayfare-labs/wayfare/issues/99))*
`ToCorridorJSON` emits `Quotes[0]`; the engine computes the rest to establish
integrity and then throws them away.
`V2` `area:pricing` `difficulty:medium` `ready`

**#84 — Publish structured hop-level route data** *(filed: [#100](https://github.com/Wayfare-labs/wayfare/issues/100))*
A path is published as `"USDC -> BLND -> XLM -> NGNC"`; codes alone cannot
identify assets, and issuers are the identity.
`V2` `area:pricing` `difficulty:medium` `ready`

**#85 — Measure whether native XLM routing changes execution quality** *(filed: [#101](https://github.com/Wayfare-labs/wayfare/issues/101))*
The current best path runs through XLM and an unrelated token. Whether that
helps is unknown; a negative finding is a valid outcome.
`V2` `area:research` `difficulty:medium` `research`

**#86 — Classify each hop: native, wrapped, bridged, fiat-pegged** *(filed: [#170](https://github.com/Wayfare-labs/wayfare/issues/170))*
`asset.FiatPeg` covers one category; the rest are needed before fragmentation
analysis means anything.
`V2` `area:corridor` `difficulty:medium` `blocked` — on #84

**#87 — Count distinct paths per rung as a published measurement** *(filed: [#171](https://github.com/Wayfare-labs/wayfare/issues/171))*
The simplest fragmentation signal, and it needs no new upstream call.
`V2` `area:pricing` `good first issue` `difficulty:easy` `blocked` — on #83

**#88 — Detect chained fiat dependencies beyond one intermediate** *(filed: [#22](https://github.com/Wayfare-labs/wayfare/issues/22))*
Tracked as #22. `DERIVATIVE` currently names one intermediate; a two-hop fiat
chain is a different structural risk.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:hard` `ready`

### B9 — Benchmark provenance

**#89 — reference_as_of is recorded but never published** *(filed: [#102](https://github.com/Wayfare-labs/wayfare/issues/102))*
`runstore.Reference.AsOf` holds the provider's own stamp; only `fetched_at`
reaches the wire, so a reused rate looks current.
`V2` `area:pricing` `good first issue` `difficulty:easy` `ready`

**#90 — Document what fair value means for the NGN reference rate** *(filed: [#51](https://github.com/Wayfare-labs/wayfare/issues/51))*
Tracked as #51. The reference is the official rate; under exchange controls the
transacted rate may differ, and every figure then understates the loss.
`V2` `area:docs` `needs-maintainer-review` `difficulty:medium` `ready`

**#91 — Research: a usable parallel-rate source for NGN** *(filed: [#56](https://github.com/Wayfare-labs/wayfare/issues/56))*
Tracked as #56, with findings already recorded in
`docs/parallel-rate-research.md`. #57 is the blocked implementation.
`V2` `area:research` `difficulty:medium` `research`

**#92 — Never average two provider mids, and test that we do not** *(filed: [#172](https://github.com/Wayfare-labs/wayfare/issues/172))*
The rule is documented — a blended mid names no provider — and deserves an
explicit adversarial test rather than trust.
`V2` `area:tests` `good first issue` `difficulty:easy` `ready`

---

## D. Initiative C — V2 Market Intelligence

Corridor coverage, asset identity and anchor capability. The rule from
`docs/adding-a-corridor.md` holds throughout: **one corridor at a time, and a
negative finding is a result.** No roster, no coverage matrix.

### C1 — Corridor research (one at a time)

Five research issues already exist and are not duplicated here: #58 ZAR,
#59 BRL, #60 PHP, #61 MXN, #62 INR. Each must establish asset, issuer,
representation, anchor, SEP-38, market, order book, paths, liquidity, benchmark,
what can be measured and what cannot.

**#93 — Publish a corridor research template as a document** *(filed: [#173](https://github.com/Wayfare-labs/wayfare/issues/173))*
The five open research issues each restate the same questions; a template in
`docs/` makes the sixth cheap and the answers comparable.
`V2` `area:docs` `good first issue` `difficulty:easy` `ready`

**#94 — Record snapshots for any corridor that research qualifies** *(filed: [#174](https://github.com/Wayfare-labs/wayfare/issues/174))*
`testdata/snapshots/` holds three corridors recorded on 2026-08-21; a newly
qualified corridor is not supported until it has fixtures.
`V2` `area:tests` `difficulty:medium` `ready`

**#95 — Make adding a corridor a single registration point** *(filed: [#4](https://github.com/Wayfare-labs/wayfare/issues/4))*
Tracked as #4. Today an asset touches `asset/known.go`, the peg registry and the
UI's hardcoded select.
`V2` `area:corridor` `good first issue` `difficulty:easy` `ready`

**#96 — Expand the fiat-peg registry and define bridge assets explicitly** *(filed: [#23](https://github.com/Wayfare-labs/wayfare/issues/23))*
Tracked as #23. `BLND` appears in a live best path and is classified by nothing.
`V2` `area:corridor` `needs-maintainer-review` `difficulty:medium` `ready`

**#97 — Research: which Stellar fiat tokens have a two-sided book at all** *(filed: [#175](https://github.com/Wayfare-labs/wayfare/issues/175))*
A bounded survey producing a written finding, not a roster: how many fiat-pegged
assets have any executable market, as of a stated date.
`V2` `area:research` `difficulty:medium` `research`

### C2 — Asset identity

**#98 — Verify the USDC issuer against circle.com's stellar.toml** *(filed: [#6](https://github.com/Wayfare-labs/wayfare/issues/6))*
Tracked as #6. The README's own verification table marks this **not yet
verified**, and USDC is the send asset in every measurement.
`V2` `area:data` `good first issue` `difficulty:easy` `ready`

**#99 — Record the verification date with every issuer** *(filed: [#176](https://github.com/Wayfare-labs/wayfare/issues/176))*
`asset/known.go` carries verified issuers; issuers rotate, and a verification
without a date decays silently.
`V2` `area:corridor` `difficulty:easy` `ready`

**#100 — Re-verify issuers on a schedule and report drift** *(filed: [#177](https://github.com/Wayfare-labs/wayfare/issues/177))*
A `stellar.toml` that changes after verification is exactly the event the asset
identity rule exists to catch.
`V2` `area:data` `difficulty:medium` `ready`

**#101 — Publish asset identity in wire form consistently** *(filed: [#178](https://github.com/Wayfare-labs/wayfare/issues/178))*
The README specifies `stellar:CODE:ISSUER`, `stellar:native` and
`iso4217:CODE`; `AssetJSON` publishes code and issuer as separate fields.
`V2` `area:corridor` `difficulty:easy` `ready`

**#102 — Check that an issuer's home_domain round-trips to the same stellar.toml** *(filed: [#34](https://github.com/Wayfare-labs/wayfare/issues/34))*
Tracked as #34. A domain that does not round-trip means every TOML-derived fact
describes somebody else's document.
`V2` `area:corridor` `difficulty:medium` `ready`

**#103 — Report auth_immutable: whether an issuer's flags can still change** *(filed: [#33](https://github.com/Wayfare-labs/wayfare/issues/33))*
Tracked as #33. An issuer that can still enable clawback is a materially
different counterparty from one that cannot.
`V2` `area:corridor` `good first issue` `difficulty:easy` `ready`

**#104 — Check that SEP-24 /info lists the asset the TOML claims** *(filed: [#35](https://github.com/Wayfare-labs/wayfare/issues/35))*
Tracked as #35. A TOML claim unsupported by the anchor's own API is a
discrepancy worth reporting.
`V2` `area:corridor` `difficulty:medium` `ready`

### C3 — Anchor capability

**#105 — Report SEP-38 availability as a first-class corridor fact** *(filed: [#179](https://github.com/Wayfare-labs/wayfare/issues/179))*
NGNC's anchor publishes no `ANCHOR_QUOTE_SERVER`, so its rails cannot be priced
programmatically. That is a measurement, currently only prose in the UI.
`V2` `area:corridor` `difficulty:medium` `ready`

**#106 — A live SEP-38 round-trip has never been performed** *(filed: [#180](https://github.com/Wayfare-labs/wayfare/issues/180))*
The README's verification table says so explicitly. Until one anchor on a
supported corridor publishes a quote server, `sep38` is spec-verified only.
`V2` `area:corridor` `difficulty:hard` `blocked` — on a corridor with SEP-38

**#107 — Distinguish anchor rails from DEX execution in the response** *(filed: [#181](https://github.com/Wayfare-labs/wayfare/issues/181))*
Wayfare measures on-chain DEX liquidity; an anchor's own rails may price
differently, and conflating them would misattribute the loss.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:medium` `ready`

**#108 — Research: do any Stellar anchors publish SEP-38 for African fiat?** *(filed: [#182](https://github.com/Wayfare-labs/wayfare/issues/182))*
Bounded survey, written finding, negative result acceptable. Determines whether
#106 is reachable at all.
`V2` `area:research` `difficulty:medium` `research`

**#109 — Anchor checks should distinguish "not published" from "published and dead"** *(filed: [#183](https://github.com/Wayfare-labs/wayfare/issues/183))*
`checks/sep10_endpoint.go` already models this correctly; assert the same
discipline across all anchor checks.
`V2` `area:tests` `difficulty:easy` `ready`

**#110 — Record which SEP versions an anchor advertises** *(filed: [#184](https://github.com/Wayfare-labs/wayfare/issues/184))*
A capability inventory per anchor makes the counterparty picture legible without
re-reading TOMLs by hand.
`V2` `area:corridor` `difficulty:medium` `ready`

**#111 — Alert when a corridor's integrity state changes across runs** *(filed: [#24](https://github.com/Wayfare-labs/wayfare/issues/24))*
Tracked as #24. A corridor moving DIRECT → DERIVATIVE is a market-structure
event and the chain already records enough to detect it.
`V3` `area:data` `difficulty:medium` `ready`

**#112 — Plan the Horizon to Stellar RPC migration** *(filed: [#11](https://github.com/Wayfare-labs/wayfare/issues/11))*
Tracked as #11. RPC has no pathfinding, so this is an architectural question
with no obvious answer and a real deadline attached.
`V2` `area:pricing` `needs-maintainer-review` `difficulty:hard` `ready`

---

## E. Initiative D — V3 Foundations

`runstore` already **is** the historical baseline: `data/USDC-NGNC.ndjson` and
its siblings hold hash-chained records. No issue here builds a new collection
system. The blocker is different and specific: the records carry headline
figures only, so there is nothing longitudinal to analyse until #62 lands.

### D1 — Reading history

**#113 — Build a statistical reader over existing runstore history** *(filed: [#71](https://github.com/Wayfare-labs/wayfare/issues/71))*
Tracked as #71. Observation count, mean, standard deviation, trend, regime — all
over records that already exist.
`V3` `area:data` `needs-maintainer-review` `difficulty:hard` `ready`

**#114 — Define and defend the minimum sample size** *(filed: [#185](https://github.com/Wayfare-labs/wayfare/issues/185))*
Below it the answer is UNDETERMINED plus the observation count. A trend line
from six observations is a decoration.
`V3` `area:data` `difficulty:medium` `blocked` — on #113

**#115 — Expose history over the API** *(filed: [#186](https://github.com/Wayfare-labs/wayfare/issues/186))*
There is no endpoint that returns more than the latest record; `runstore.Latest`
is the only reader the server uses.
`V3` `area:ui` `difficulty:medium` `blocked` — on #113

**#116 — Paginate and bound any history endpoint** *(filed: [#187](https://github.com/Wayfare-labs/wayfare/issues/187))*
The chain grows without limit; an unbounded reader is a denial-of-service
surface on a free instance.
`V3` `area:devops` `difficulty:medium` `blocked` — on #115

**#117 — Gaps in history must be visible, not smoothed** *(filed: [#188](https://github.com/Wayfare-labs/wayfare/issues/188))*
The measure workflow has already produced a multi-day gap (#63); any series that
hides it is lying about coverage.
`V3` `area:data` `difficulty:medium` `blocked` — on #113

**#118 — Distinguish a corridor that was not measured from one that could not be** *(filed: [#189](https://github.com/Wayfare-labs/wayfare/issues/189))*
Both produce an absent record; only one is a finding about the corridor.
`V3` `area:data` `difficulty:medium` `blocked` — on #113

### D2 — Longitudinal measurement

**#119 — Track the structural floor over time** *(filed: [#190](https://github.com/Wayfare-labs/wayfare/issues/190))*
The floor at the dust size is the corridor's spread; its movement is the single
most meaningful series the store could produce.
`V3` `area:pricing` `difficulty:medium` `blocked` — on #62

**#120 — Track reference divergence over time** *(filed: [#191](https://github.com/Wayfare-labs/wayfare/issues/191))*
`DivergencePct` is already recorded per run; a benchmark whose providers
increasingly disagree is a fact about the benchmark, not the corridor.
`V3` `area:pricing` `difficulty:medium` `ready`

**#121 — Separate benchmark movement from corridor movement** *(filed: [#192](https://github.com/Wayfare-labs/wayfare/issues/192))*
`runstore.Reference` records both mids deliberately, so that "the corridor moved"
and "the benchmark moved" stay distinguishable afterwards. Nothing uses it yet.
`V3` `area:pricing` `difficulty:hard` `ready`

**#122 — Track integrity-state transitions as a series** *(filed: [#193](https://github.com/Wayfare-labs/wayfare/issues/193))*
DIRECT → DERIVATIVE → NO-MARKET is a structural narrative the chain can already
reconstruct.
`V3` `area:data` `difficulty:medium` `ready`

**#123 — Track path composition over time** *(filed: [#194](https://github.com/Wayfare-labs/wayfare/issues/194))*
Requires hop-level data (#84) to be recorded; whether corridors reroute through
different intermediates is unknown today.
`V3` `area:pricing` `difficulty:medium` `blocked` — on #84, #62

**#124 — Measure how often a corridor is measurable at all** *(filed: [#195](https://github.com/Wayfare-labs/wayfare/issues/195))*
Uptime of the measurement, not of the service: how many scheduled sweeps
produced a priced ladder.
`V3` `area:data` `difficulty:easy` `ready`

### D3 — Market structure

**#125 — Define what "market structure" means for this project, in writing** *(filed: [#196](https://github.com/Wayfare-labs/wayfare/issues/196))*
Before analysis is built, the vocabulary needs pinning — fragmentation,
concentration, depth distribution, venue.
`V3` `area:docs` `needs-maintainer-review` `difficulty:medium` `ready`

**#126 — Fragmentation over time** *(filed: [#197](https://github.com/Wayfare-labs/wayfare/issues/197))*
Whether a corridor's executable value is concentrating into fewer paths is a
structural risk signal that needs no inference.
`V3` `area:pricing` `difficulty:medium` `blocked` — on #83, #62

**#127 — Cross-corridor comparison on shared fields only** *(filed: [#198](https://github.com/Wayfare-labs/wayfare/issues/198))*
Two corridors may be compared only on measurements both actually have; a missing
field is not a zero and must not be filled to complete a table.
`V3` `area:ui` `difficulty:medium` `ready`

**#128 — Identify the shared dependencies across corridors** *(filed: [#199](https://github.com/Wayfare-labs/wayfare/issues/199))*
NGNC, GHSC and KESC share one issuer; that concentration is itself a
market-structure fact about the case study.
`V3` `area:corridor` `difficulty:medium` `ready`

**#129 — Chart the corridor-health trend over time** *(filed: [#10](https://github.com/Wayfare-labs/wayfare/issues/10))*
Tracked as #10. Blocked on there being a defensible series to chart — the chart
must not exist before the data does.
`V3` `area:ui` `difficulty:medium` `blocked` — on #113

**#130 — Regime classification from documented thresholds only** *(filed: [#200](https://github.com/Wayfare-labs/wayfare/issues/200))*
Thresholds must be written down and justified before they are applied; a regime
label derived from an unexplained cutoff is a verdict in disguise.
`V3` `area:pricing` `needs-maintainer-review` `difficulty:hard` `blocked` — on #113

### D4 — Storage and scale

**#131 — Decide what happens when the chain outgrows the repository** *(filed: [#201](https://github.com/Wayfare-labs/wayfare/issues/201))*
`data/*.ndjson` is committed by `measure.yml`; at a six-hour cadence this grows
forever and the deployment embeds it at build time.
`V3` `area:data` `needs-maintainer-review` `difficulty:hard` `ready`

**#132 — Support a storage backend behind the Store interface** *(filed: [#202](https://github.com/Wayfare-labs/wayfare/issues/202))*
`runstore/fsstore.go` implements the interface; the README lists storage
backends as open contributor territory.
`V3` `area:data` `difficulty:hard` `ready`

**#133 — Compaction must preserve verifiability** *(filed: [#331](https://github.com/Wayfare-labs/wayfare/issues/331))*
Any scheme that drops records must keep the chain checkable, or the property the
package exists for is lost.
`V3` `area:data` `needs-maintainer-review` `difficulty:hard` `blocked` — on #131

**#134 — Publish the chain head so a third party can pin it** *(filed: [#332](https://github.com/Wayfare-labs/wayfare/issues/332))*
A reader who records today's head can prove later that no earlier record
changed; nothing currently exposes it.
`V3` `area:data` `difficulty:medium` `ready`

---

## F. Initiative E — V4+ Research and design spikes

**Every issue in this section produces a written document, not code.** None of
them is an implementation task, and none should be started as one. The
repository has no packages for layers 3 and 4 and this backlog creates none.

The gate each spike must respect: *a layer can never be more certain than the
layer beneath it.* Most of these are premature today because the data beneath
them does not exist. Writing down **what would have to be true first** is the
contribution.

### E1 — Historical intelligence (V4)

**#135 — Spike: what can be learned from the history that will exist in 90 days** *(filed: [#333](https://github.com/Wayfare-labs/wayfare/issues/333))*
At a six-hour cadence, three corridors, that is roughly 1,080 records. Determine
what is answerable at that sample size and what is not.
`V4+` `area:research` `difficulty:medium` `research`

**#136 — Spike: which V2 measurements are worth storing longitudinally** *(filed: [#334](https://github.com/Wayfare-labs/wayfare/issues/334))*
Storage is not free and the record layout is hash-pinned; choosing badly is
expensive to undo.
`V4+` `area:research` `difficulty:medium` `research`

**#137 — Spike: seasonality in FX corridors under exchange controls** *(filed: [#335](https://github.com/Wayfare-labs/wayfare/issues/335))*
Whether NGN corridors show day-of-week or month-end structure, from published
literature and from the chain once it is long enough.
`V4+` `area:research` `difficulty:medium` `research`

**#138 — Spike: what a corridor "baseline of normal" would require** *(filed: [#336](https://github.com/Wayfare-labs/wayfare/issues/336))*
Anomaly detection needs a baseline; defining one for a corridor that has never
been acceptable is not obvious.
`V4+` `area:research` `difficulty:hard` `research`

**#139 — Spike: minimum observations for a defensible trend claim** *(filed: [#337](https://github.com/Wayfare-labs/wayfare/issues/337))*
A number, with the statistical reasoning written out, that the project can point
to when it declines to publish a trend.
`V4+` `area:research` `difficulty:medium` `research`

**#140 — Spike: how to express uncertainty in a published figure** *(filed: [#338](https://github.com/Wayfare-labs/wayfare/issues/338))*
Today a figure is either measured or unknown. A third category — measured with
an interval — needs a wire representation before it needs a calculation.
`V4+` `area:research` `difficulty:hard` `research`

### E2 — Predictive intelligence (V5)

**#141 — Spike: what a route failure actually is, observationally** *(filed: [#339](https://github.com/Wayfare-labs/wayfare/issues/339))*
Failure prediction needs observed failures. The store records unpriced rungs;
whether those are failures is a definitional question nobody has answered.
`V4+` `area:research` `difficulty:medium` `research`

**#142 — Spike: could expected slippage be predicted from book shape alone?** *(filed: [#204](https://github.com/Wayfare-labs/wayfare/issues/204))*
A feasibility question, answered from recorded snapshots, with an explicit
negative outcome permitted.
`V4+` `area:research` `difficulty:hard` `research`

**#143 — Spike: what would make a prediction publishable under this project's rules** *(filed: [#340](https://github.com/Wayfare-labs/wayfare/issues/340))*
The four-layer model says a layer-3 estimate must never be published as fact.
What the published shape would look like is undesigned.
`V4+` `area:research` `difficulty:hard` `research`

**#144 — Spike: the failure modes of publishing a wrong prediction** *(filed: [#205](https://github.com/Wayfare-labs/wayfare/issues/205))*
For a tool whose thesis is that misleading figures cost people money, this needs
writing down before any model exists.
`V4+` `area:research` `difficulty:medium` `research`

**#145 — Spike: survey how other market-quality tools express confidence** *(filed: [#206](https://github.com/Wayfare-labs/wayfare/issues/206))*
Prior art, cited, with what is adoptable and what is not.
`V4+` `area:research` `difficulty:medium` `research`

**#146 — Spike: would a model add anything over the deterministic measurements?** *(filed: [#207](https://github.com/Wayfare-labs/wayfare/issues/207))*
The honest possible answer is no, and establishing that would save the project
an entire version.
`V4+` `area:research` `difficulty:hard` `research`

**#147 — Design: the boundary between measurement and inference in the UI** *(filed: [#208](https://github.com/Wayfare-labs/wayfare/issues/208))*
If inference is ever published it must be unmistakable at a glance; that visual
contract can be designed now.
`V4+` `area:design` `difficulty:medium` `research`

### E3 — Corridor health score

**#148 — Corridor health score composition** *(filed: [#55](https://github.com/Wayfare-labs/wayfare/issues/55))*
Tracked as #55 and correctly `blocked`. It needs its components to exist first,
and it is a judgement of the same class as the verdict bands.
`V4+` `area:pricing` `needs-maintainer-review` `difficulty:hard` `blocked`

**#149 — Spike: what a single number would destroy** *(filed: [#209](https://github.com/Wayfare-labs/wayfare/issues/209))*
Integrity is deliberately carried alongside the verdict because collapsing them
discards the reason. A score collapses further. Write down the cost.
`V4+` `area:research` `difficulty:medium` `research`

**#150 — Spike: how a composite score would handle undetermined components** *(filed: [#210](https://github.com/Wayfare-labs/wayfare/issues/210))*
The default-to-zero failure has an obvious new home here.
`V4+` `area:research` `difficulty:medium` `research`

### E4 — Verifiable intelligence (V6)

**#151 — Spike: what a corridor attestation would actually assert** *(filed: [#211](https://github.com/Wayfare-labs/wayfare/issues/211))*
Signing "this corridor is bad" requires deciding what the claim is, over what
window, and what falsifies it.
`V4+` `area:research` `difficulty:hard` `research`

**#152 — Spike: the publisher-trust tradeoff, written out** *(filed: [#212](https://github.com/Wayfare-labs/wayfare/issues/212))*
Today every figure is independently reproducible from recorded bytes; an oracle
asks readers to trust the publisher instead.
`V4+` `area:research` `difficulty:hard` `research`

**#153 — Spike: who would consume a corridor attestation, and why** *(filed: [#213](https://github.com/Wayfare-labs/wayfare/issues/213))*
If no contract would read it, the trade is not worth making. Interview-based,
with named candidates.
`V4+` `area:research` `area:ecosystem` `difficulty:medium` `research`

**#154 — Spike: could the hash chain be anchored on-chain without a trust model?** *(filed: [#214](https://github.com/Wayfare-labs/wayfare/issues/214))*
Publishing the chain head costs nothing and asserts nothing beyond "this history
existed at this time".
`V4+` `area:research` `difficulty:medium` `research`

**#155 — Spike: Soroban cost of verifying a corridor claim** *(filed: [#215](https://github.com/Wayfare-labs/wayfare/issues/215))*
A feasibility number, not a contract. The README defers implementation
deliberately.
`V4+` `area:research` `difficulty:hard` `research`

**#156 — Spike: signature scheme and key custody for a non-custodial project** *(filed: [#216](https://github.com/Wayfare-labs/wayfare/issues/216))*
A project that holds no keys today would begin holding one; that is a change in
kind, not degree.
`V4+` `area:research` `difficulty:hard` `research`

### E5 — Ecosystem and architecture spikes

**#157 — Spike: who would consume this API, and what shape do they need** *(filed: [#217](https://github.com/Wayfare-labs/wayfare/issues/217))*
Wallets, PSPs and anchors are named as intended users; none has been asked.
`V4+` `area:research` `area:ecosystem` `difficulty:medium` `research`

**#158 — Spike: could Wayfare measure a non-Stellar corridor** *(filed: [#218](https://github.com/Wayfare-labs/wayfare/issues/218))*
The README avoids claiming Stellar exclusivity; what would actually have to
change is unexamined.
`V4+` `area:research` `difficulty:hard` `research`

**#159 — Spike: multi-region measurement** *(filed: [#219](https://github.com/Wayfare-labs/wayfare/issues/219))*
Whether a corridor prices differently when measured from another region, and
whether that would invalidate single-region history.
`V4+` `area:research` `difficulty:medium` `research`

**#160 — Spike: real-time versus scheduled measurement economics** *(filed: [#220](https://github.com/Wayfare-labs/wayfare/issues/220))*
Six hours is chosen because corridors move on the scale of days; the cost of
being wrong about that is unquantified.
`V4+` `area:research` `difficulty:medium` `research`

**#161 — Spike: what a Wayfare SDK would need to expose** *(filed: [#221](https://github.com/Wayfare-labs/wayfare/issues/221))*
Only worth building if #157 finds consumers; the shape is a design question
either way.
`V4+` `area:research` `area:ecosystem` `difficulty:medium` `research`

**#162 — Spike: alerting semantics for corridor deterioration** *(filed: [#222](https://github.com/Wayfare-labs/wayfare/issues/222))*
What threshold crossing deserves a notification, and to whom, without becoming a
prediction.
`V4+` `area:research` `difficulty:medium` `research`

**#163 — Spike: how would a second maintainer verify a published claim?** *(filed: [#223](https://github.com/Wayfare-labs/wayfare/issues/223))*
The reproducibility story is strong in code and untested by a stranger.
`V4+` `area:research` `difficulty:medium` `research`

**#164 — Spike: the cost of being wrong, per figure published** *(filed: [#224](https://github.com/Wayfare-labs/wayfare/issues/224))*
An explicit register of which published numbers would do the most damage if
incorrect, to direct hardening effort.
`V4+` `area:research` `difficulty:medium` `research`

**#165 — Spike: adversarial review — how would someone game a Wayfare verdict?** *(filed: [#225](https://github.com/Wayfare-labs/wayfare/issues/225))*
An issuer wanting a better grade has a small attack surface; enumerate it.
`V4+` `area:research` `difficulty:hard` `research`

**#166 — Spike: what this project should refuse to build, and why** *(filed: [#226](https://github.com/Wayfare-labs/wayfare/issues/226))*
Settlement, custody and KYC are already refused; the reasoning belongs in one
citable document rather than scattered across README sections.
`V4+` `area:research` `area:docs` `difficulty:medium` `research`

---

## G. Cross-cutting — Documentation and onboarding

`docs/` currently holds seven documents: `adding-a-corridor.md`, `checks.md`,
`corridor-measurements.md`, `deployment.md`, `parallel-rate-research.md`,
`run-store.md`, `snapshot-format.md`. They are good and they are incomplete.
The gaps below are what a stranger hits.

### G1 — Getting started

**#167 — A "first 15 minutes" walkthrough** *(filed: [#227](https://github.com/Wayfare-labs/wayfare/issues/227))*
Clone to a reproduced measurement, with the expected output pasted in so a
contributor knows whether it worked.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#168 — Document the local development loop** *(filed: [#228](https://github.com/Wayfare-labs/wayfare/issues/228))*
`make`, `make test`, `make race`, `make cover`, `make lint`, `make run` exist in
the Makefile and are documented only as a list.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#169 — Document what to do when a live measurement fails locally** *(filed: [#229](https://github.com/Wayfare-labs/wayfare/issues/229))*
Both binaries need live network access by design; the failure mode is confusing
the first time and has no troubleshooting entry.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#170 — A troubleshooting page** *(filed: [#230](https://github.com/Wayfare-labs/wayfare/issues/230))*
Rate limits, a sleeping deployment, a `stellar.toml` that will not resolve, a
chain that will not verify — the four recurring stumbles.
`V1` `area:docs` `difficulty:easy` `ready`

**#171 — Document the offline test requirement** *(filed: [#231](https://github.com/Wayfare-labs/wayfare/issues/231))*
CI runs the suite with no route out; a contributor whose test reaches the
network will fail in CI without understanding why.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#172 — Explain the snapshot record-and-replay workflow end to end** *(filed: [#232](https://github.com/Wayfare-labs/wayfare/issues/232))*
`docs/snapshot-format.md` specifies the format; nothing walks through recording
a new one and using it in a test.
`V1` `area:docs` `difficulty:medium` `ready`

**#173 — Document `ladder -replay`** *(filed: [#233](https://github.com/Wayfare-labs/wayfare/issues/233))*
The flag landed via #21/#42 and appears in no document.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#174 — Document `-verify-store` and what a broken chain looks like** *(filed: [#234](https://github.com/Wayfare-labs/wayfare/issues/234))*
Including the output when verification fails, which is the moment a reader most
needs the document.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

### G2 — Reference

**#175 — A glossary of every state a reader can meet** *(filed: [#235](https://github.com/Wayfare-labs/wayfare/issues/235))*
GOOD/FAIR/POOR/UNUSABLE, DIRECT/DERIVATIVE/NO-MARKET/UNKNOWN,
AGREE/DISAGREE/STALE/MALFUNCTION/SINGLE, determined/passed, live/stale. Five
vocabularies, no single page.
`V1` `area:docs` `difficulty:medium` `ready`

**#176 — An API reference** *(filed: [#236](https://github.com/Wayfare-labs/wayfare/issues/236))*
The README lists four endpoints in four lines; there is no field-by-field
document for `/api/corridor`.
`V1` `area:docs` `difficulty:medium` `ready`

**#177 — Document the error responses** *(filed: [#237](https://github.com/Wayfare-labs/wayfare/issues/237))*
Depends on error codes existing (#16); the document is what makes them a
contract rather than an implementation detail.
`V1` `area:docs` `difficulty:easy` `blocked` — on #16

**#178 — Document the `live` and `stale` semantics for consumers** *(filed: [#238](https://github.com/Wayfare-labs/wayfare/issues/238))*
The single most misreadable pair of fields on the wire, and the one the UI
already gets wrong.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#179 — A metrics methodology document** *(filed: [#239](https://github.com/Wayfare-labs/wayfare/issues/239))*
One page per metric: definition, unit, data source, what it cannot determine,
and what undetermined means for it.
`V2` `area:docs` `difficulty:medium` `ready`

**#180 — Document the cost decomposition's components** *(filed: [#240](https://github.com/Wayfare-labs/wayfare/issues/240))*
Including, explicitly, which components are currently undetermined and why.
`V2` `area:docs` `difficulty:easy` `blocked` — on #77

**#181 — Document the ladder sizes and why they are those sizes** *(filed: [#241](https://github.com/Wayfare-labs/wayfare/issues/241))*
`route.DefaultSizes` runs 0.1 → 5000; the dust rung exists to isolate the
structural floor, which is a methodological choice worth stating.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#182 — Document the SEP-38 fee-denomination identity** *(filed: [#242](https://github.com/Wayfare-labs/wayfare/issues/242))*
Pinned in `sep38/golden_test.go` against the spec's worked example; the reason
it matters is not written down anywhere a reader will find it.
`V2` `area:docs` `difficulty:medium` `ready`

### G3 — Architecture and decisions

**#183 — Start an ADR series** *(filed: [#243](https://github.com/Wayfare-labs/wayfare/issues/243))*
Several large decisions are recorded only as README prose or commit messages.
The first four are listed below.
`V1` `area:docs` `difficulty:easy` `ready`

**#184 — ADR: why a monitor and not a router** *(filed: [#244](https://github.com/Wayfare-labs/wayfare/issues/244))*
The founding decision, currently a README section.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#185 — ADR: why reference mids are never averaged** *(filed: [#245](https://github.com/Wayfare-labs/wayfare/issues/245))*
A blended mid names no provider; every figure must trace to a checkable source.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#186 — ADR: why checks never move the headline** *(filed: [#246](https://github.com/Wayfare-labs/wayfare/issues/246))*
Enforced in code by `route.WithFindings` branching on nothing, and defended by
`TestFindingsDoNotMoveTheHeadline`.
`V1` `area:docs` `difficulty:easy` `ready`

**#187 — ADR: why layers 3 and 4 have no packages** *(filed: [#247](https://github.com/Wayfare-labs/wayfare/issues/247))*
"Speculative structure is worse than none." Worth citing when someone proposes
an empty package.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#188 — ADR: why the scheduler does not depend on the server** *(filed: [#248](https://github.com/Wayfare-labs/wayfare/issues/248))*
`monitor` imports nothing from `server`; a monitor that only measures while
somebody is watching leaves holes exactly where nobody looked.
`V1` `area:docs` `difficulty:easy` `ready`

**#189 — ADR: why money crosses the wire as decimal strings** *(filed: [#249](https://github.com/Wayfare-labs/wayfare/issues/249))*
And why the boundary test exists.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#190 — Document the maintainer-owned areas and why each is owned** *(filed: [#250](https://github.com/Wayfare-labs/wayfare/issues/250))*
CONTRIBUTING lists them; the blast-radius reasoning is one sentence and deserves
to be per-area.
`V1` `area:docs` `difficulty:easy` `ready`

### G4 — Product story

**#191 — "Why Wayfare?" as a document** *(filed: [#251](https://github.com/Wayfare-labs/wayfare/issues/251))*
Why quoted rates mislead, why executable value matters, and why "do not send
this" can be the correct answer.
`V1` `area:docs` `difficulty:medium` `ready`

**#192 — "About Wayfare" as a document** *(filed: [#252](https://github.com/Wayfare-labs/wayfare/issues/252))*
What it measures, what it refuses to do, who it is for, and the non-custodial
position stated once, clearly.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#193 — "Why Stellar-native?" grounded in what the code uses** *(filed: [#253](https://github.com/Wayfare-labs/wayfare/issues/253))*
On-chain assets, pathfinding, order books, anchors, SEP-1 and SEP-38 — with no
claim of exclusivity the repository cannot support.
`V1` `area:docs` `difficulty:medium` `ready`

**#194 — "How Wayfare works" for a non-engineer** *(filed: [#254](https://github.com/Wayfare-labs/wayfare/issues/254))*
Reference rate → market data → executable quote → comparison → checks → verdict,
in prose a policy reader can follow.
`V1` `area:docs` `difficulty:medium` `ready`

**#195 — An interpretation guide: how to read a measurement** *(filed: [#255](https://github.com/Wayfare-labs/wayfare/issues/255))*
Given a corridor response, what each figure licenses you to conclude — and what
it does not.
`V1` `area:docs` `difficulty:medium` `ready`

**#196 — Fold the quoted-price-versus-executable-liquidity framing into the roadmap** *(filed: [#49](https://github.com/Wayfare-labs/wayfare/issues/49))*
Tracked as #49.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#197 — Add the breadth-follows-evidence rule to CONTRIBUTING.md** *(filed: [#50](https://github.com/Wayfare-labs/wayfare/issues/50))*
Tracked as #50.
`V1` `area:docs` `good first issue` `difficulty:easy` `ready`

**#198 — A contributor FAQ, seeded in Discussions** *(filed: [#256](https://github.com/Wayfare-labs/wayfare/issues/256))*
Discussions was enabled during this sweep and is empty; CONTRIBUTING points
nowhere for questions.
`V1` `area:docs` `area:ecosystem` `good first issue` `difficulty:easy` `ready`

---

## H. Cross-cutting — QA and reproducibility

QA here is not "run the unit tests" — CI already does that on every push. It is
the work Go tests cannot do: exercising the deployed product, the browser, and
the reproducibility claims. **Every issue in this section produces a reusable
artifact** — a checklist, a recorded result, a screenshot set, or a filed bug
with steps.

### H1 — Production QA

**#199 — A production smoke-test checklist** *(filed: [#257](https://github.com/Wayfare-labs/wayfare/issues/257))*
Against `https://wayfare-cdb9.onrender.com/`: cold start, each corridor, each
endpoint, the stale banner, the error paths. Reusable after every deploy.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#200 — Record the cold-start behaviour properly** *(filed: [#258](https://github.com/Wayfare-labs/wayfare/issues/258))*
During this sweep the first request failed at connection and the second
succeeded in 0.6s. A measured distribution beats one anecdote.
`V1` `area:tests` `difficulty:easy` `ready`

**#201 — Verify the deployed instance against the repository it claims to be** *(filed: [#259](https://github.com/Wayfare-labs/wayfare/issues/259))*
The container embeds `data/` at build time; confirm the served history matches
the committed chain.
`V1` `area:tests` `area:devops` `difficulty:medium` `ready`

**#202 — QA the API from a consumer's perspective, not the UI's** *(filed: [#260](https://github.com/Wayfare-labs/wayfare/issues/260))*
Every documented endpoint and parameter, with the responses recorded as
fixtures for later comparison.
`V1` `area:tests` `difficulty:medium` `ready`

**#203 — Time a full live ladder against the server timeout** *(filed: [#261](https://github.com/Wayfare-labs/wayfare/issues/261))*
`Server.timeout()` defaults to 90s and a ladder is a dozen Horizon round trips;
whether that holds on a free instance is untested.
`V1` `area:tests` `difficulty:easy` `ready`

**#204 — Verify /healthz behaviour during a cold start** *(filed: [#262](https://github.com/Wayfare-labs/wayfare/issues/262))*
Render uses it as the health check path; what it returns while the instance is
waking determines whether the platform believes the service is up.
`V1` `area:tests` `area:devops` `difficulty:easy` `ready`

### H2 — UI state matrix

**#205 — Build the UI state matrix and test every cell** *(filed: [#263](https://github.com/Wayfare-labs/wayfare/issues/263))*
integrity {DIRECT, DERIVATIVE, NO-MARKET, UNKNOWN} × scored {true, false} ×
live {true, false} × findings {present, absent, empty}. Most cells have never
been rendered.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

**#206 — QA the NO-MARKET corridor end to end** *(filed: [#264](https://github.com/Wayfare-labs/wayfare/issues/264))*
KESC returns no path at any size; the whole page must degrade honestly rather
than render an empty table.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#207 — QA the DERIVATIVE corridor end to end** *(filed: [#265](https://github.com/Wayfare-labs/wayfare/issues/265))*
GHSC's figures compound NGNC's; the UI says so in prose and nowhere in
structure.
`V1` `area:tests` `difficulty:easy` `ready`

**#208 — QA every error path in the browser** *(filed: [#266](https://github.com/Wayfare-labs/wayfare/issues/266))*
Network failure, upstream failure, unknown asset, malformed size, server error —
five distinct causes currently sharing one panel.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

**#209 — QA the stale banner once it exists** *(filed: [#267](https://github.com/Wayfare-labs/wayfare/issues/267))*
The specific regression to guard: a stale reading rendering as live.
`V1` `area:tests` `difficulty:easy` `blocked` — on #1

**#210 — QA metric rendering including undetermined metrics** *(filed: [#268](https://github.com/Wayfare-labs/wayfare/issues/268))*
An undetermined metric must not read as a failure.
`V2` `area:tests` `difficulty:easy` `blocked` — on #51

### H3 — Cross-browser and device

**#211 — Cross-browser QA matrix** *(filed: [#269](https://github.com/Wayfare-labs/wayfare/issues/269))*
Chrome, Firefox, Safari, Edge, current and one prior major. Recorded results,
not impressions.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

**#212 — Mobile device QA on real hardware** *(filed: [#270](https://github.com/Wayfare-labs/wayfare/issues/270))*
Emulator width is not touch behaviour; the `.scroll` table is the thing to
watch.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

**#213 — QA at each defined breakpoint** *(filed: [#271](https://github.com/Wayfare-labs/wayfare/issues/271))*
320, 375, 768, 1024, 1440. Screenshot set per corridor state.
`V1` `area:tests` `area:ui` `difficulty:easy` `blocked` — on #16

**#214 — QA both colour schemes** *(filed: [#272](https://github.com/Wayfare-labs/wayfare/issues/272))*
The stylesheet has a `prefers-color-scheme: dark` block that has never been
systematically reviewed.
`V1` `area:tests` `area:design` `difficulty:easy` `ready`

**#215 — Accessibility audit against WCAG 2.2 AA** *(filed: [#273](https://github.com/Wayfare-labs/wayfare/issues/273))*
Producing a findings document; the fixes are separate issues.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

**#216 — Screen-reader walkthrough of a full measurement** *(filed: [#274](https://github.com/Wayfare-labs/wayfare/issues/274))*
The result is injected via `innerHTML` with no live region, so it is announced
to nobody.
`V1` `area:tests` `area:ui` `difficulty:medium` `ready`

### H4 — Reproducibility

**#217 — Reproduce every figure in docs/corridor-measurements.md** *(filed: [#275](https://github.com/Wayfare-labs/wayfare/issues/275))*
The document carries timestamps and raw output; whether a stranger can reproduce
the method is the claim that matters.
`V1` `area:tests` `difficulty:medium` `ready`

**#218 — Audit every recorded snapshot for provenance** *(filed: [#276](https://github.com/Wayfare-labs/wayfare/issues/276))*
Eight snapshot directories exist across `testdata/` and `checks/testdata/`;
confirm each verifies on load and records how it was taken.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#219 — Verify the committed chain independently** *(filed: [#277](https://github.com/Wayfare-labs/wayfare/issues/277))*
`wayfared -verify-store -data ./data` is the project's strongest evidence claim;
have someone outside the project run it and write down what they saw.
`V1` `area:tests` `good first issue` `difficulty:easy` `ready`

**#220 — Property-based tests for money arithmetic** *(filed: [#278](https://github.com/Wayfare-labs/wayfare/issues/278))*
Rate, loss and percentage conversions should hold for arbitrary decimals, not
only for the values someone thought to write down.
`V1` `area:tests` `difficulty:medium` `ready`

---

## I. Cross-cutting — UI, UX and visual design

`server/index.html` is a single 15KB file with no build step — a deliberate
architecture (`server/ui.go`: "the binary is the whole deployment"). Everything
here must preserve that.

**The semantic boundary, which no UI issue may cross:** designers and frontend
contributors decide **how a state looks**. The measurement engine and the check
contract decide **when it fires**. Colour, layout, copy, motion and interaction
are open. Verdict thresholds, integrity semantics, check semantics, route
semantics and runstore semantics are not. A UI issue that requires changing
`route/`, `dex/`, `checks/` composition or `runstore/` is mis-scoped and needs
splitting into a backend contract issue plus a dependent UI issue.

### I1 — Design system

**#221 — Establish the visual identity and design system** *(filed: [#279](https://github.com/Wayfare-labs/wayfare/issues/279))*
Working direction: the Trustworthy Navigator. Starting palette — Navy `#0F172A`,
Teal `#10B981`, Amber `#F59E0B`, Slate `#334155`, Cloud `#F8FAFC`, Coral
`#F97316`, plus an accessible critical red to be defined. Starting values, not
final branding.
`V1` `area:design` `difficulty:medium` `ready`

**#222 — Separate brand colours from financial semantics** *(filed: [#280](https://github.com/Wayfare-labs/wayfare/issues/280))*
Teal must not mean "good investment" and amber must not mean "bad". UNDETERMINED
must never look like failure. This rule governs every issue below it.
`V1` `area:design` `difficulty:medium` `blocked` — on #221

**#223 — Define design tokens as CSS custom properties** *(filed: [#281](https://github.com/Wayfare-labs/wayfare/issues/281))*
The stylesheet already has a `:root` block with eleven variables; extend it into
a real token set rather than introducing a build step.
`V1` `area:design` `difficulty:medium` `ready`

**#224 — A typographic scale** *(filed: [#282](https://github.com/Wayfare-labs/wayfare/issues/282))*
Current sizes are ad hoc: `.72rem`, `.74rem`, `.76rem`, `.78rem`, `.8rem`,
`.82rem`, `.85rem`, `.88rem`, `.9rem`, `.92rem`, `.95rem`, `1.02rem`, `1.35rem`.
`V1` `area:design` `good first issue` `difficulty:easy` `ready`

**#225 — A spacing and radius scale** *(filed: [#283](https://github.com/Wayfare-labs/wayfare/issues/283))*
Same problem, same fix, currently spread across a dozen rules.
`V1` `area:design` `good first issue` `difficulty:easy` `ready`

**#226 — A chart palette that is not the semantic palette** *(filed: [#284](https://github.com/Wayfare-labs/wayfare/issues/284))*
`curve()` draws everything in `var(--bad)` — the line, the dots, the threshold —
so the chart says "danger" before it says anything.
`V1` `area:design` `difficulty:medium` `ready`

**#227 — Define focus, hover, active and disabled states** *(filed: [#285](https://github.com/Wayfare-labs/wayfare/issues/285))*
Only `button:hover:not(:disabled)` and `button:disabled` exist; there is no
focus style at all beyond the browser default.
`V1` `area:design` `area:ui` `difficulty:easy` `ready`

**#228 — Contrast audit of both schemes against the token set** *(filed: [#286](https://github.com/Wayfare-labs/wayfare/issues/286))*
Including `--muted` on `--panel`, which is the smallest text in the interface.
`V1` `area:design` `difficulty:medium` `blocked` — on #223

### I2 — Theming

**#229 — Light, Dark and System with an explicit toggle** *(filed: [#287](https://github.com/Wayfare-labs/wayfare/issues/287))*
Today the only mechanism is `@media (prefers-color-scheme: dark)`; a reader
cannot choose, and the choice cannot persist.
`V1` `area:ui` `difficulty:medium` `ready`

**#230 — Design dark mode rather than inverting light mode** *(filed: [#288](https://github.com/Wayfare-labs/wayfare/issues/288))*
Surfaces, borders and chart colours each need intent; the current dark block is
a palette swap.
`V1` `area:design` `difficulty:medium` `blocked` — on #229

**#231 — Persist the theme choice** *(filed: [#289](https://github.com/Wayfare-labs/wayfare/issues/289))*
`localStorage`, defaulting to System, with the stored value ignored gracefully
when it is unreadable.
`V1` `area:ui` `good first issue` `difficulty:easy` `blocked` — on #229

### I3 — Core interaction

**#232 — Drive the corridor selector from /api/assets** *(filed: [#290](https://github.com/Wayfare-labs/wayfare/issues/290))*
The `<select>` hardcodes NGNC, GHSC and KESC while the endpoint already returns
the verified set with `can_be_destination`.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#233 — Let the reader choose the transfer size** *(filed: [#291](https://github.com/Wayfare-labs/wayfare/issues/291))*
`/api/corridor` accepts `sizes=`; the UI never sends it, so the ladder is fixed
at `DefaultSizes` with no way to ask about $250.
`V2` `area:ui` `difficulty:medium` `ready`

**#234 — Make the corridor state URL-addressable** *(filed: [#292](https://github.com/Wayfare-labs/wayfare/issues/292))*
`?to=NGNC&sizes=…` in the address bar, so a measurement can be linked to and
reloaded.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#235 — Preserve selection and input across an error** *(filed: [#293](https://github.com/Wayfare-labs/wayfare/issues/293))*
`measure()` clears `#out` before fetching, so a failure leaves an empty page and
the reader re-enters everything.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#236 — Progressive disclosure of evidence** *(filed: [#294](https://github.com/Wayfare-labs/wayfare/issues/294))*
Every field needed for an evidence drawer already ships: reference source and
mid, fetched-at, path description, check evidence with source and timestamp.
`V1` `area:ui` `difficulty:medium` `ready`

**#237 — Visualise benchmark versus executable** *(filed: [#295](https://github.com/Wayfare-labs/wayfare/issues/295))*
The core insight — reference mid, achieved rate, and the gap — has no visual
form; the reader must subtract two numbers from different panels.
`V2` `area:ui` `difficulty:medium` `ready`

**#238 — An execution-curve visualisation** *(filed: [#296](https://github.com/Wayfare-labs/wayfare/issues/296))*
`P_exec(q)`, `Loss(q)` and `MC(q)` with hover revealing measured values only.
Must not draw a line through sizes that did not price.
`V2` `area:ui` `difficulty:medium` `blocked` — on #73

**#239 — A route explorer** *(filed: [#297](https://github.com/Wayfare-labs/wayfare/issues/297))*
Hop-level exploration, once hops are structured data. Never fabricate a hop.
`V2` `area:ui` `difficulty:medium` `blocked` — on #84

**#240 — Corridor comparison on shared fields only** *(filed: [#298](https://github.com/Wayfare-labs/wayfare/issues/298))*
Missing is not zero, and no synthetic score may fill a blank cell.
`V3` `area:ui` `difficulty:medium` `blocked` — on #127

### I4 — States

**#241 — A state vocabulary for the interface** *(filed: [#299](https://github.com/Wayfare-labs/wayfare/issues/299))*
UNDETERMINED, LIVE, RECORDED, STALE, UNAVAILABLE, DIRECT, DERIVATIVE,
NO-MARKET, GOOD, FAIR, POOR, UNUSABLE. Not one traffic light — structural states
are not severities.
`V1` `area:design` `difficulty:medium` `ready`

**#242 — Distinct empty states** *(filed: [#300](https://github.com/Wayfare-labs/wayfare/issues/300))*
No corridor selected, no market, no route at this size, insufficient
observations, metric unavailable, capability not yet built.
`V1` `area:ui` `difficulty:medium` `ready`

**#243 — Distinct error states** *(filed: [#301](https://github.com/Wayfare-labs/wayfare/issues/301))*
Currently every failure renders the same red panel via one `catch`.
`V1` `area:ui` `difficulty:medium` `blocked` — on #16

**#244 — A cold-start experience** *(filed: [#53](https://github.com/Wayfare-labs/wayfare/issues/53))*
Tracked as #53. The free instance sleeps after fifteen minutes; the first
request can take seconds and currently looks like a hang.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#245 — Make integrity states visually distinct** *(filed: [#14](https://github.com/Wayfare-labs/wayfare/issues/14))*
Tracked as #14. `badgeClass()` maps three states to three classes and UNKNOWN
falls through to the DIRECT style — a real mis-render.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#246 — A style pass on the three finding states** *(filed: [#52](https://github.com/Wayfare-labs/wayfare/issues/52))*
Tracked as #52.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

### I5 — Accessibility and motion

**#247 — Announce results to assistive technology** *(filed: [#302](https://github.com/Wayfare-labs/wayfare/issues/302))*
`render()` replaces `#out.innerHTML` with no live region, so a screen-reader user
gets no indication that anything happened.
`V1` `area:ui` `difficulty:medium` `ready`

**#248 — Keyboard navigation and visible focus throughout** *(filed: [#303](https://github.com/Wayfare-labs/wayfare/issues/303))*
`V1` `area:ui` `difficulty:medium` `ready`

**#249 — Communicate every state without relying on colour** *(filed: [#304](https://github.com/Wayfare-labs/wayfare/issues/304))*
Verdicts are conveyed by `.v-good` / `.v-poor` / `.v-unusable` colour classes
alone.
`V1` `area:ui` `difficulty:medium` `ready`

**#250 — Give the loss curve a text alternative** *(filed: [#305](https://github.com/Wayfare-labs/wayfare/issues/305))*
`curve()` emits one `aria-label` for the whole SVG; the underlying numbers are
in the table, and the two should be associated.
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#251 — Touch targets and hit areas** *(filed: [#306](https://github.com/Wayfare-labs/wayfare/issues/306))*
`V1` `area:ui` `good first issue` `difficulty:easy` `ready`

**#252 — Subtle motion, with reduced-motion support** *(filed: [#307](https://github.com/Wayfare-labs/wayfare/issues/307))*
Panel expansion and state transitions only; nothing that delays reading a
financial figure.
`V1` `area:design` `difficulty:medium` `ready`

**#253 — Semantic HTML pass** *(filed: [#308](https://github.com/Wayfare-labs/wayfare/issues/308))*
The results region is a div soup assembled from template strings; headings,
lists and tables are available and mostly unused.
`V1` `area:ui` `difficulty:medium` `ready`

**#254 — A first-impression pass on the landing state** *(filed: [#309](https://github.com/Wayfare-labs/wayfare/issues/309))*
Before a measurement runs the page is a heading, a select and a button. It
should state what Wayfare is and what pressing the button will do.
`V1` `area:design` `area:ui` `difficulty:medium` `ready`

---

## J. Cross-cutting — DevOps and operations

CI is unusually strong for a project this size: `ci.yml` runs gofmt, vet,
`go test -race`, build, golangci-lint pinned to v2.1.6, a no-network test job
inside a network namespace, and a container build that runs the binary. The gaps
are in what happens *after* CI.

**#255 — Fix the measure workflow's push rejection** *(filed: [#63](https://github.com/Wayfare-labs/wayfare/issues/63))*
Tracked as #63. `measure.yml` measures and commits successfully and cannot push;
the ruleset requires a pull request and four status checks. Data has not
advanced since 2026-08-22.
`V1` `area:devops` `needs-maintainer-review` `difficulty:medium` `ready`

**#256 — Alert when a scheduled measurement fails** *(filed: [#310](https://github.com/Wayfare-labs/wayfare/issues/310))*
The failure above was silent for days. A workflow failure that stops the
project's data collection should be loud.
`V1` `area:devops` `difficulty:easy` `ready`

**#257 — Expose data freshness from /healthz** *(filed: [#311](https://github.com/Wayfare-labs/wayfare/issues/311))*
Chain head, newest record timestamp, and record count, so freshness is checkable
without parsing a corridor response.
`V1` `area:devops` `difficulty:medium` `ready`

**#258 — Post-deploy smoke check** *(filed: [#312](https://github.com/Wayfare-labs/wayfare/issues/312))*
`ci.yml` proves the image builds and runs; nothing proves the deployed instance
answers correctly afterwards.
`V1` `area:devops` `difficulty:medium` `ready`

**#259 — Structured logging in wayfared** *(filed: [#12](https://github.com/Wayfare-labs/wayfare/issues/12))*
Tracked as #12. `WAYFARE_LOG_LEVEL` is set in `render.yaml` and there is no
structured logger to consume it.
`V1` `area:devops` `good first issue` `difficulty:easy` `ready`

**#260 — Decide and document the staging story** *(filed: [#313](https://github.com/Wayfare-labs/wayfare/issues/313))*
`fly.toml` and `render.yaml` both exist; whether there is a staging target, and
what it is for, is undecided.
`V1` `area:devops` `difficulty:medium` `ready`

**#261 — Track test coverage in CI** *(filed: [#314](https://github.com/Wayfare-labs/wayfare/issues/314))*
Coverage moves silently today; `runstore` at 48.8% and `refrate` at 47.9% would
be visible if it were reported per run.
`V1` `area:devops` `area:tests` `difficulty:easy` `ready`

**#262 — Cache Go modules consistently across jobs** *(filed: [#315](https://github.com/Wayfare-labs/wayfare/issues/315))*
Four jobs each set up Go; `offline-tests` warms modules deliberately, the others
rely on the action's cache.
`V1` `area:devops` `good first issue` `difficulty:easy` `ready`

**#263 — Scan the container image** *(filed: [#316](https://github.com/Wayfare-labs/wayfare/issues/316))*
The Dockerfile builds the deployed artifact and nothing inspects it for known
vulnerabilities.
`V1` `area:devops` `difficulty:easy` `ready`

**#264 — Pin and document the dependency surface** *(filed: [#317](https://github.com/Wayfare-labs/wayfare/issues/317))*
Two direct dependencies — `shopspring/decimal` and `BurntSushi/toml` — is a
deliberate and defensible position that should be stated and defended in CI.
`V1` `area:devops` `good first issue` `difficulty:easy` `ready`

**#265 — Document and test the rollback path** *(filed: [#318](https://github.com/Wayfare-labs/wayfare/issues/318))*
Including how to verify the chain after rolling back to an image with older
embedded history.
`V1` `area:devops` `difficulty:medium` `ready`

**#266 — Exercise the auto-merge gate deliberately** *(filed: [#319](https://github.com/Wayfare-labs/wayfare/issues/319))*
`auto-merge.yml` is 19KB of gating logic that, per the audit in #48, has never
merged a pull request. Untested gates fail when first trusted.
`V1` `area:devops` `needs-maintainer-review` `difficulty:medium` `ready`

**#267 — Bound what a single request can cost the service** *(filed: [#320](https://github.com/Wayfare-labs/wayfare/issues/320))*
A twelve-rung ladder is a dozen Horizon round trips and `sizes=` accepts 24;
there is no rate limiting on a free instance.
`V1` `area:devops` `difficulty:medium` `ready`

---

## K. Cross-cutting — Ecosystem and community

Every issue here must produce a concrete artifact. No general outreach.

**#268 — Publish curl examples for every endpoint** *(filed: [#321](https://github.com/Wayfare-labs/wayfare/issues/321))*
Copy-pasteable, with real responses from the live instance, including the stale
and error shapes.
`V1` `area:ecosystem` `good first issue` `difficulty:easy` `ready`

**#269 — A minimal API consumer example** *(filed: [#322](https://github.com/Wayfare-labs/wayfare/issues/322))*
One small program that fetches a corridor, respects `live` and `scored`, and
refuses to render a verdict it should not. The reference implementation of
reading Wayfare correctly.
`V1` `area:ecosystem` `difficulty:medium` `ready`

**#270 — A worked example of adding a check** *(filed: [#323](https://github.com/Wayfare-labs/wayfare/issues/323))*
`docs/checks.md` specifies the contract; the three existing checks are the
worked examples and no document walks through writing a fourth.
`V1` `area:docs` `difficulty:medium` `ready`

**#271 — A worked example of adding a metric** *(filed: [#324](https://github.com/Wayfare-labs/wayfare/issues/324))*
Same gap, newer contract. `checks/metric.go` documents the interface;
`metric_spread.go` is the smallest example.
`V2` `area:docs` `difficulty:medium` `ready`

**#272 — A case study written up from the NGNC finding** *(filed: [#325](https://github.com/Wayfare-labs/wayfare/issues/325))*
The 25% structural floor at dust size is a genuine, reproducible market finding
that currently lives in a README section.
`V1` `area:ecosystem` `difficulty:medium` `ready`

**#273 — Map the Stellar ecosystem projects Wayfare could inform** *(filed: [#326](https://github.com/Wayfare-labs/wayfare/issues/326))*
Named wallets, PSPs and anchors, with what each would need from the API — the
input #157 needs to be worth answering.
`V1` `area:ecosystem` `area:research` `difficulty:medium` `research`

**#274 — Seed Discussions with the questions contributors actually ask** *(filed: [#327](https://github.com/Wayfare-labs/wayfare/issues/327))*
Enabled during this sweep and currently empty.
`V1` `area:ecosystem` `good first issue` `difficulty:easy` `ready`

**#275 — Issue and pull-request templates** *(filed: [#328](https://github.com/Wayfare-labs/wayfare/issues/328))*
`.github/` has workflows and no templates; the issue quality bar in this
repository is high and currently transmitted by example only.
`V1` `area:ecosystem` `good first issue` `difficulty:easy` `ready`

**#276 — A "good first issue" audit** *(filed: [#329](https://github.com/Wayfare-labs/wayfare/issues/329))*
Seven issues carry the label; confirm each is genuinely completable by a
stranger with only the README and CONTRIBUTING.
`V1` `area:ecosystem` `good first issue` `difficulty:easy` `ready`

---

## L. Summary

### By initiative

| Initiative | Issues | Numbers |
|:---|---:|:---|
| A — V1 Hardening | 48 | #1–#48 |
| B — V2 Execution Economics | 44 | #49–#92 |
| C — V2 Market Intelligence | 20 | #93–#112 |
| D — V3 Foundations | 22 | #113–#134 |
| E — V4+ Research and design spikes | 32 | #135–#166 |
| G — Documentation and onboarding | 32 | #167–#198 |
| H — QA and reproducibility | 22 | #199–#220 |
| I — UI, UX and visual design | 34 | #221–#254 |
| J — DevOps and operations | 13 | #255–#267 |
| K — Ecosystem and community | 9 | #268–#276 |
| M — Appendix, filed outside the numbering | 2 | — |
| **Total** | **278** | |

### By state

| State | Count | Meaning |
|:---|---:|:---|
| `ready` | 201 | Start now; no dependency outstanding |
| `blocked` | 38 | Waits on a named issue in this document |
| `research` | 37 | Produces a written finding, not code |
| `future` | 0 | Folded into `research`; no V4+ implementation task exists in this backlog |

### By roadmap stage

| Stage | Count |
|:---|---:|
| V1 — Hardening | 148 |
| V2 — Execution economics | 71 |
| V3 — Market structure & history | 25 |
| V4+ — Future (not active) | 32 |

### By contributor skill — where to start

| If you are strong in | Start at |
|:---|:---|
| Backend / Go | #49 (the V2 keystone), #62, #77 |
| Financial engineering | #73, #79, #59, #56 |
| Quantitative research | #113, #114, #121 |
| Frontend | #1, #232, #234, #235 |
| UI/UX | #241, #242, #244, #254 |
| Visual design | #221, #223, #226, #230 |
| QA | #199, #205, #211, #219 |
| Documentation | #167, #175, #191, #194 |
| First contribution | #3, #10, #11, #224, #232, #268 |
| DevOps | #255, #257, #258 |
| Research | #71, #85, #108, #135 |
| Ecosystem | #268, #269, #273 |

### The critical path

Four issues unblock disproportionately more than they cost:

1. **#49** — `checks.Runner` cannot run metrics. Unblocks #50, #51, #52, #53 and
   every metric issue in B2–B5. Four merged metrics stay dead code until it
   lands.
2. **#62** — record checks and metrics in `runstore`. Unblocks #4 and every
   longitudinal issue in D2; nothing in V3 is possible without it.
3. **#1** — the UI reports a stored reading as live. The product's honesty
   claim fails at the last inch until this lands.
4. **#255** — the measure workflow cannot push. Every freshness fix downstream
   is cosmetic while the chain is frozen.

### What this backlog deliberately does not contain

No implementation issue for: VaR, CVaR, Monte Carlo, a composite 0–100 health
score, anomaly detection, route-failure prediction, predictive liquidity,
forecasting, generic AI risk scoring, ML datasets or labelling, deep learning,
Soroban attestation, settlement, custody, money transmission, or a corridor
roster. Each appears in the roadmap, and several appear in section F as research
spikes whose deliverable is a written finding.

No issue creates an empty package for a layer that has no inputs yet.

### Verification of this document

Every file, function, field and figure cited was read from the tree at commit
`93f5cda` or observed on the wire from `https://wayfare-cdb9.onrender.com/` on
2026-08-24. Coverage figures are from `go test ./... -cover`. The tracker
numbers in parentheses are issues opened during the sweep that produced this
document (#79–#106).

---

## M. Appendix — filed outside this numbering

Two issues were opened during the sweep that produced this document, before the
numbering above was settled. They are listed here rather than renumbered into
section B, so that every backlog number in this document keeps pointing at the
same issue it was filed as.

**[#82 — UI renders verdicts when scored is false](https://github.com/Wayfare-labs/wayfare/issues/82)**
`render()` draws the loss curve, the verdict column and the recommendation block
without checking `d.scored`. When the reference cross-check reaches MALFUNCTION
the contract says no verdict is issued — and the UI would publish one anyway.
`V1` `area:ui` `bug` `difficulty:easy` `ready`

**[#84 — The "Measure live" button does not request a live measurement](https://github.com/Wayfare-labs/wayfare/issues/84)**
The UI fetches `/api/corridor?to=<code>` with no `live` parameter, and the
deployed instance runs `-history-first`. The button is labelled "Measure live"
and reads a file embedded at build time.
`V1` `area:ui` `bug` `good first issue` `difficulty:easy` `ready`

Counting these, the sweep filed **278** issues in total: the 276 numbered
entries above, plus these two.
