# Wayfare

A corridor-integrity monitor for Stellar.

Wayfare prices a stablecoin → fiat-token corridor across trade sizes, scores
every route against an independent mid-market rate, and states plainly when
none of them are worth taking — including when the honest answer is *don't
send this*.

**Status:** pre-MVP, read-only. Non-custodial: no funds held, no tokens
issued, no KYC, no keys.

---

## Try Wayfare

| | |
|:---|:---|
| **Live** | **https://wayfare-cdb9.onrender.com/** |
| **Source** | https://github.com/Wayfare-labs/wayfare |
| **Health** | https://wayfare-cdb9.onrender.com/healthz |

That is a real deployed instance of the code in this repository — the same
container image CI builds and verifies. Three things about it are worth knowing
before you read a figure off it:

**It serves recorded measurements, not live ones.** The instance runs with
`-schedule=0 -history-first`, so a request answers from the hash-chained history
embedded in the binary at build time. Every response carries `live: false` and a
`stale` block with the reading's age. Ask for a live measurement explicitly with
`?live=1` — that prices a full ladder against Horizon and takes tens of seconds.

**Freshness depends on the measure workflow, not on the deployment.** New
records are written by `.github/workflows/measure.yml` and committed to `data/`.
That workflow is currently unable to push ([#63](https://github.com/Wayfare-labs/wayfare/issues/63)),
so the served history is older than its six-hour cadence implies. Read
`stale.age_human` rather than assuming.

**It sleeps.** The free instance sleeps after fifteen minutes without traffic,
so the first request after a quiet period may take several seconds or fail
outright before the instance wakes. Retry once.

It runs the current system, and only the current system. Nothing in the v2–v6
roadmap below is deployed there.

To reproduce it locally, `go run ./cmd/wayfared` and open
`http://127.0.0.1:8080/` — that measures live against mainnet rather than
serving history. Deployment details: **[docs/deployment.md](docs/deployment.md)**.

---

## Why a monitor and not a router

The project began as a router — find the cheapest path, rank the results. Live
mainnet measurement killed that thesis, and the measurement is the reason the
tool exists in its current form.

Sending 100 USDC to NGNC returns roughly 62,900 NGNC against a USD/NGN mid
near 1,364. The best available route delivers about 46% of fair value. A tool
that displayed *"best route: 62,900 NGNC"* would be accurate, useful-looking,
and would have cost the sender more than half of what they sent.

So a ranking is not enough. A ranking carries a hidden assumption — that its
winner is worth taking — and on this corridor that assumption is false at
every size tested. Every quote is scored against an independent reference
rate, and a corridor whose best option is unusable says so instead of
presenting a winner.

That is also why the reference rate is a required dependency rather than an
optional enrichment. Without it the engine can rank, but it cannot tell a good
deal from a disaster.

---

## What the measurements found

Full figures, with timestamps and raw output:
**[docs/corridor-measurements.md](docs/corridor-measurements.md)**

Measured live on 2026-08-08 against Horizon pathfinding, three corridors from
one issuer, all within a 60-second window:

| Corridor | Issuer status | Best result, any size | Mode |
|:---|:---|:---|:---|
| USDC → NGNC | `live` | 25.02% loss at 0.1 USDC, 97.68% at 5000 | Live, value-destroying |
| USDC → GHSC | `pending` | 74.14% loss at 0.1 USDC, 99.47% at 5000 | Derivative — every path runs through NGNC |
| USDC → KESC | `pending` | no route at any size | No market |

Three findings shaped the design:

**The loss has a structural floor.** At 0.1 USDC price impact is negligible,
and NGNC still loses 25%. That floor is the corridor's spread, not its depth —
which means no trade size can be acceptable, because the zero-size limit is
already unacceptable. Slippage then stacks on top, reaching 97.68% at 5000.

**The three corridors fail in three different ways.** One prices continuously
and prices badly. One has no independent market and inherits another token's
failure modes. One has no market at all. Reporting all three as "Unusable"
would be accurate and would discard the reason each is unusable — so the
monitor carries an integrity state alongside the loss grade.

**The benchmark is the charitable one.** The reference is the official
USD/NGN rate. If the rate people actually transact at is weaker, every figure
above *understates* the loss. Whether a defensible parallel-rate source exists
that could provide a less charitable benchmark was investigated — no usable
source was found. See
[docs/parallel-rate-research.md](docs/parallel-rate-research.md).

The issuer set is case study #1, not the product. Wayfare measures any
stablecoin → fiat-token corridor.

---

## Architecture

Organised by **epistemic status** — what is observed, what is computed from
observation, what is inferred, and what is published. The ordering carries a
rule:

> **A layer can never be more certain than the layer beneath it.**

A layer 2 calculation built on an unavailable layer 1 fact is *unknown*, not a
default. A layer 4 output publishing a layer 3 estimate as fact is the specific
failure this project exists to avoid.

```
LAYER 1 — OBSERVABLE FACTS                                        [live]
  Horizon pathfinding, order books, issuer flags, SEP-1 documents
  packages: dex, anchor, asset, sep38, snapshot

        │  every figure below traces to one of these, or is unknown
        ▼

LAYER 2 — DETERMINISTIC CALCULATION                               [live]
  effective rate, loss vs mid, integrity state,
  divergence between reference providers
  packages: route, refrate, checks

  spread, depth, price impact, concentration, cost decomposition
                                             [implemented, not yet reachable]

        │
        ▼

LAYER 3 — PROBABILISTIC INTELLIGENCE            [not built — needs history]
  failure probability, expected slippage, anomaly detection,
  route deterioration, VaR/CVaR, route optimisation

        │  blocked on months of runstore history that does not exist yet
        ▼

LAYER 4 — VERIFIABLE OUTPUT                 [not built — needs a trust model]
  signed or on-chain corridor attestation consumable by other protocols
```

Layers 3 and 4 have **no packages and no stubs**, deliberately. Speculative
structure is worse than none: an empty package invites code that has no inputs
yet.

### How the pieces fit

```
     Horizon pathfinding          Reference providers (×2)
     order books, issuer flags    cached, cross-checked for divergence
              │                              │
              └──────────────┬───────────────┘
                             ▼
                   route.Engine  ── ladder sweep 0.1 → 5000
                             │      per size:     rate → loss → verdict
                             │      per corridor: integrity
                             ▼
                        checks.Runner ── counterparty facts
                             │           (qualify; never move the headline)
              ┌──────────────┴───────────────┐
              ▼                              ▼
     monitor.Scheduler                 server.Server
     every 6h, headless                /api/corridor, single-file UI
              │                              │ on failure: last stored run,
              ▼                              │ labelled live:false
     runstore ── hash-chained NDJSON ◀───────┘
```

Two things about this shape are deliberate.

**The scheduler does not depend on the server.** `monitor` imports nothing from
`server`, and `wayfared -serve=false` measures with no HTTP at all. A monitor
that only measures while somebody has a page open would leave holes in its
history exactly where nobody was looking.

**Checks sit downstream of the measurement.** They observe the counterparties a
corridor depends on and are attached to the result; nothing they report can
alter an integrity state or a verdict. See the composition rule below.

Significant architectural decisions are recorded as ADRs in
**[docs/adr/](docs/adr/)**.

---

## Shared contracts

These are the agreements other code and other people depend on. **Changing any
of them is a breaking change**, not a refactor.

A glossary of every state a reader can meet: **[docs/glossary.md](docs/glossary.md)**

### Verdict thresholds — breaking if altered

Loss is how far the achieved rate falls below the reference mid.

| Verdict | Loss | |
|:---|:---|:---|
| `GOOD` | ≤ 3% | Comparable to a competitive remittance service |
| `FAIR` | ≤ 8% | Worse than the best providers, not unreasonable |
| `POOR` | ≤ 20% | Expensive, and the user is told so plainly |
| `UNUSABLE` | > 20% | Not a fee — value destruction |

The bands are anchored to what the incumbent market actually achieves.
Established remittance corridors run a total cost of 3–8%, so `GOOD` means
"as good as what already exists" rather than "good for a DEX". Anchoring to
on-chain norms instead would have graded this project's own findings as
acceptable.

### The recommendation rule — breaking if altered

> **When no size produces a verdict of `POOR` or better, the monitor
> recommends nothing.**

Not the best of a bad set. Nothing.

This is the product thesis, so it is stated here rather than left in a doc
comment. On the wire, `recommended` is **always present and `null`** in that
case — never omitted — so a client cannot read its absence as an oversight and
substitute the best-scoring quote.

### Integrity states — breaking if altered

Integrity describes a corridor's structure. It is carried **alongside** the
verdict, never folded into it, because collapsing them discards the reason a
corridor failed — and the reason is the useful part.

| State | Assigned when |
|:---|:---|
| `DIRECT` | At least one path reaches the destination without traversing another fiat-pegged token |
| `DERIVATIVE` | **Every** path traverses another fiat token. `depends_on` names it |
| `NO-MARKET` | Horizon returns no path at any size. The absence of a price, not a bad one |
| `UNKNOWN` | Structure not established — normally an unreachable upstream |

`DERIVATIVE` examines every path, not the best one: a single path avoiding
fiat intermediaries disproves the claim. `UNKNOWN` is what separates "nothing
was learned" from "nothing exists", which matters because both produce
identical zero-valued figures.

### Reference agreement — breaking if altered

Two providers are queried per measurement. Rates are **never averaged**: a
blended mid names no provider, and every figure has to be traceable to a source
a reader can check.

| Divergence | State | Scored against |
|:---|:---|:---|
| ≤ 2% | `AGREE` | The primary |
| 2–10% | `DISAGREE` | The **more conservative** mid — the one producing the higher loss |
| > 10% | `MALFUNCTION` | **Nothing.** No verdict is issued |
| — | `STALE` | The fresher feed, when the two describe different moments |
| — | `SINGLE` | The one that answered; uncorroborated, and says so |

Beyond 10% the feeds are not disagreeing about the rate, they are measuring
different things, and a verdict would be an artefact of which one was believed.
`scored_against` names the mid that produced the verdicts you are reading.

### Check results — breaking if altered

Checks observe facts about the counterparties a corridor depends on. Three
states, and the third is the point:

| State | Meaning |
|:---|:---|
| determined + passed | The check ran and the fact holds |
| determined + failed | The check ran and the fact does not hold |
| **not determined** | The check could not establish either way — **not a failure** |

An anchor that publishes no SEP-10 endpoint is a different fact from one whose
endpoint is dead, and both differ from one that works. `determined` is a
separate field from `passed`, so unknown cannot be expressed as a zero or a
false, and every undetermined result carries a reason.

> **Checks qualify the headline. They never move it.**

No result, at any severity, may change `integrity` or a verdict. Those are
derived from pathfinding and a reference rate; letting observations about third
parties rewrite them would make the headline unfalsifiable — a reader could no
longer tell whether a corridor was downgraded because its liquidity moved or
because someone added a check. Enforced in code, not documented: `WithFindings`
branches on nothing.

**Metrics are a separate shape from checks.** A quantity like spread or depth
returns a value and a unit, not a verdict. Forcing a measurement through
pass/fail discards the number that carries the meaning. Thresholding a metric
into a verdict is maintainer-owned.

Full spec: **[docs/checks.md](docs/checks.md)**

### Asset identity — breaking if altered

An asset code identifies nothing; **the issuer account is the identity.**
Anyone can issue a token called `USDC`. Every issuer is read from the issuer's
own `stellar.toml` per SEP-1, with the verification date recorded, because
issuers rotate. Wire form is `stellar:CODE:ISSUER`, `stellar:native`, or
`iso4217:CODE` — the same SEP-38 asset identification format used everywhere
else in this project. Every asset object on the wire (`send_asset`,
`receive_asset`, `depends_on` entries) carries this form in its `asset`
field, alongside the separate `code` and `issuer` fields for a reader who
wants one or the other. `asset` is omitted when the producer has only a bare
code to work from and cannot verify the asset's kind or issuer — never
guessed at.

### Money on the wire — breaking if altered

Every amount, rate and percentage is a **decimal string**, never a JSON number.
A JSON number invites a client to parse it into a `float64`, reintroducing the
rounding error the engine avoids internally. There is a test at the boundary.

### Snapshot format — version 1

Recorded upstream bytes, verified by hash on load. A replayer **must refuse a
version it does not know.** Full spec: **[docs/snapshot-format.md](docs/snapshot-format.md)**

### Run record — version 3

```
hash = sha256(record JSON with the hash field omitted)
```

`prev_hash` is inside the hashed preimage — that is what makes the history a
chain rather than a pile. **The field set and their declaration order are part
of every hash**, so adding, removing or reordering a field is a version bump
plus a migration, never a tidy-up. `TestRecordHashIsPinned` fails in CI on the
commit that would have broken it.

Version 1 and Version 2 chains still load and still verify under this build:
each migration added its fields with `omitempty` after every earlier field, so
a legacy record encodes byte-for-byte as it did when it was written.
Version 3 added `reference.fetched_at`, which lets a stored reading say how
old its benchmark was when the reading was taken.

Full spec: **[docs/run-store.md](docs/run-store.md)**

What verification looks like — including broken-chain output: **[docs/verify-store.md](docs/verify-store.md)**

---

## Packages

| Package | Responsibility |
|:---|:---|
| `asset` | Corridor endpoints, verified issuers, the fiat-peg registry |
| `refrate` | Reference mid-market rates: two providers, cached, cross-checked |
| `anchor` | SEP-1 discovery — can this anchor be priced at all, and which SEPs does it advertise? |
| `sep38` | Anchor RFQ client, with the fee-denomination identity |
| `dex` | On-chain pricing via Horizon pathfinding, plus market health |
| `route` | Ladder sweep, verdicts, integrity, and the shared wire shape |
| `checks` | Counterparty checks and metrics; qualify the headline, never move it |
| `runstore` | Hash-chained measurement history |
| `monitor` | Scheduled measurement, independent of HTTP |
| `snapshot` | Record and replay upstream responses |
| `server` | HTTP surface and the embedded single-file UI |
| `cmd/ladder` | Measurement CLI |
| `cmd/wayfared` | Server and scheduler |

`anchor.Profile.SEPs()` returns the numbers of the SEPs an anchor advertises
in its `stellar.toml` — SEP-1 (the document itself), 6, 10, 12, 24, 31, 38 —
derived from the same fields `Priceable`, `SEP24`, `SEP31`, `SEP6`, `SEP10`
and `SEP12` already read, so the capability picture is legible in one call
rather than six separate booleans read by hand. `SEPCapabilities()` renders
the same list with a short name per SEP, and `Explain()` includes it.

---

## Running it

```bash
make run                        # measure USDC -> NGNC against live mainnet
go run ./cmd/ladder -to GHSC    # any verified corridor
go run ./cmd/ladder -to GHSC -json | jq
go run ./cmd/ladder -checks=false    # skip counterparty checks (no findings block)

go run ./cmd/wayfared                       # serve + measure every 6h
go run ./cmd/wayfared -serve=false          # scheduler only, no HTTP
go run ./cmd/wayfared -verify-store -data ./data
```

Every `cmd/ladder` run also runs the same counterparty checks the server runs
(anchor toml, SEP-10/SEP-24, issuer flags), so `-json` output and
`/api/corridor` carry the same `findings` block for the same corridor.
`-checks=false` skips them when the extra latency is unwanted, and the JSON
then carries no findings block — the difference is a flag the operator chose,
not an accident of which binary produced the document.

Go 1.22+. Dependencies: `shopspring/decimal` and `BurntSushi/toml`. Both
binaries need live network access — there are no cached figures to fall back
on, by design.

Deployment, cost and backup: **[docs/deployment.md](docs/deployment.md)**

### HTTP API

The complete field-by-field reference is in **[docs/api.md](docs/api.md)**.

```
GET /api/corridor?to=NGNC[&from=USDC][&sizes=1,10,100]
GET /api/corridor/trend?to=NGNC[&from=USDC][&limit=100]
GET /api/assets
GET /healthz
GET /                            single-file UI, no build step
```

The API is public, keyless and read-only, and answers cross-origin requests
from any origin (`Access-Control-Allow-Origin: *`), so browser consumers on
another origin can call it directly. No credentials are ever attached to a
cross-origin read.

Beyond the contracts above, one field to know: **`live`** is on every response.
`false` means the reading came from history because a live measurement failed,
and `stale` then carries its age. With no stored run, the request errors —
nothing is ever synthesised to fill the gap.

**The trend endpoint** answers "is this getting worse?" from the stored runs:
every run comes back oldest first, each carrying its integrity state, its
headline figures, the full reference it was scored against (both mids, the
divergence, and `scored_against`), and each rung's loss and verdict. Runs are
irregular snapshots, not a continuous series — the UI plots them as points at
named times and says so. An empty history is a `200` with zero runs, not an
error: a missing history is the answer, and the first day of a deployment is
exactly when a monitor is most read. `limit` (default 100, max 500) keeps the
most recent runs; the store is read, never measured.

The response also carries `divergence_stats`: how far the corridor's two
reference providers have disagreed across those same runs — a fact about the
**benchmark**, not the corridor, and it never feeds back into any run's
verdict or integrity state above. A run scored against a single provider has
no divergence to report and is excluded from the sample rather than counted
as zero. Below the documented minimum sample size (30 observations —
[docs/glossary.md](docs/glossary.md#metric-determination) has the general
rule), `determined` is `false` and `reason` says why; `mean_pct`, `stddev_pct`
and the trend fields are then absent rather than a precise-looking number.

---

## Roadmap

Capability-based, no dates. The version names track the layer model above.

Where the project is now, and what moves it:

```
  CURRENT STATE          v1 done, hardening
        ↓                contract fidelity, boundary correctness, coverage
  V1 HARDENING
        ↓                metrics reachable, recorded, and rendered
  V2 COMPLETION
        ↓                statistics over a history that records measurements
  V3 PREPARATION
```

A capability is marked **DONE** only where the repository supports it end to
end — implemented, reachable from the API, and tested. Merged-but-unreachable
code is marked as what it is.

**v1 — Quote engine.** **DONE**, in hardening. Ladder sweep, verdicts, integrity
taxonomy, cross-checked reference rates, recorded snapshots, pinned arithmetic.

**v2 — Corridor intelligence.** **IN PROGRESS**, and further from done than the
merge log suggests. Counterparty checks are **DONE**: three run per corridor and
appear in every live response. Market-quality metrics — spread, observed versus
executable depth, price impact, liquidity concentration — are **implemented but
not reachable**: `checks.Runner` has no way to run a `Metric`, so none of them
has ever appeared in a response, been recorded, or been rendered. Effective
transfer cost (`route.Decompose`) is in the same state — merged, with no caller.
Wiring that path is [#91](https://github.com/Wayfare-labs/wayfare/issues/91) and
it blocks the rest of v2. Layers 1 and 2.

**v3 — Quantitative execution risk.** **BACKLOG.** Effective transfer cost decomposed into
FX loss, fees, slippage and expected failure cost, each computed and reported
separately. A route with a worse headline rate can be cheaper all-in, and
showing that is the point. Expected failure cost stays **explicitly unknown**
until failure history exists.

**v4 — ML-assisted prediction.** **NOT YET.** Layer 3. Failure probability,
expected slippage, anomaly detection, route deterioration. Blocked twice over.
First on months of history: failure prediction needs observed failures, anomaly
detection needs a baseline of normal, and `runstore` has been collecting for
days. Second, and less obviously, on *what* is being collected — a run record
stores headline figures and no metrics, so today's history could not support
this analysis however long it ran. Training on that and publishing the output
would break the project's central rule.

**v5 — Verifiable attestations.** **NOT YET.** Layer 4. Signed or on-chain corridor
integrity a contract could read. Deferred because it introduces a
publisher-trust assumption the project does not currently have: today every
figure is independently reproducible from recorded bytes, and an oracle asks
readers to trust the publisher instead. That trade needs to be worth something
first.

**Explicitly not planned.** Settlement primitives — escrow, custody, payment
execution. Wayfare stopped being a router because measurement proved the
corridor structurally broken at every size. It analyses corridors; it does not
move money through them. If settlement ever earns a place it is a new project
with its own evidence, not an extension of this one.

## Where to start

Issues are labelled by area and by difficulty, and assigned to a roadmap
milestone so you can see which part of the project your work moves. Start with
[`good first issue`](https://github.com/Wayfare-labs/wayfare/labels/good%20first%20issue).

The full contributor backlog — every gap found in the current tree, with the
file or response that evidences it — is **[docs/backlog.md](docs/backlog.md)**.

**Milestones:**

- [V1 — Hardening](https://github.com/Wayfare-labs/wayfare/milestone/1) —
  contract fidelity, boundary correctness, coverage, deployment reliability
- [V2 — Execution economics](https://github.com/Wayfare-labs/wayfare/milestone/2) —
  making the market-quality measurements reachable, recorded and rendered
- [V3 — Market structure & history](https://github.com/Wayfare-labs/wayfare/milestone/3) —
  statistics over recorded history, once records carry measurements
- [V4+ — Future (not active)](https://github.com/Wayfare-labs/wayfare/milestone/4) —
  research spikes only; nothing here is an implementation task

**Labelling convention:**

- `difficulty:easy` — well-scoped, a few hours
- `difficulty:medium` — multi-file, needs design judgement
- `difficulty:hard` — architectural; discuss before building
- `needs-maintainer-review` — the design is **not settled**. A PR may be
  rejected on approach rather than on execution, so agree the shape first
- `blocked` — **do not start.** Waiting on something that does not exist yet

**Maintainer-owned areas.** These are not closed to contribution, but an error
in them invalidates published measurements rather than breaking a feature, so
expect close review and discuss the approach first:

- dex pricing arithmetic
- verdict thresholds
- the integrity taxonomy
- SEP-38 fee handling
- the check engine and how results compose
- the corridor health score — how signals become one published number. Not yet
  designed, and deliberately so: it needs its components to exist first, and it
  is a judgement of the same class as the verdict bands

Everything else — UI, CLI, docs, tests, new corridors, reference providers,
storage backends — is open. Adding a corridor is the highest-value first
contribution and has its own guide:
**[docs/adding-a-corridor.md](docs/adding-a-corridor.md)**

Read [CONTRIBUTING.md](CONTRIBUTING.md) first. The invariants there are hard
constraints, not style preferences.

---

## Non-goals

These keep the project shippable and legal for a small team:

- **Not an anchor.** Never issues tokens or holds reserves.
- **Not custodial.** Never takes possession of funds.
- **Not a money transmitter.** No custody, so no licensing surface.
- **Not a KYC provider.** Delegated to anchors via SEP-12.

---

## Verification status

| Claim | Status |
|---|---|
| NGNC / GHSC / KESC issuer `GASBV6W7…FQGXZY6` | Verified from ngnc.online stellar.toml, 2026-08-08 |
| GHSC and KESC are `status="pending"` | Verified from the same document |
| NGNC anchor lacks SEP-38 | Verified from live stellar.toml |
| Corridor figures in docs/ | Measured, live Horizon strict-send, timestamped |
| Recorded snapshots | Hash-verified on load; provenance refuses a dirty tree |
| SEP-38 fee identity | Verified against SEP-0038 spec text, pinned in golden files |
| USDC issuer is Circle's | **Not yet verified** against circle.com stellar.toml |
| Live SEP-38 round-trip | **Not done** — no anchor on this corridor publishes a quote server |
| Public deployment | Running at [wayfare-cdb9.onrender.com](https://wayfare-cdb9.onrender.com/); `/healthz` verified 200 on 2026-08-24 |
| Continuous measurement | **Not currently running** — the measure workflow cannot push ([#63](https://github.com/Wayfare-labs/wayfare/issues/63)), so the served history is frozen at its last successful sweep |

Unverified claims are marked in the code at the point they are used.

Reference rates come from official/interbank figures. For currencies under
exchange controls the rate people actually transact at can diverge, so this is
a defensible benchmark rather than ground truth — and it is the charitable
direction for the corridors measured here.

---

## License

Apache-2.0. See [LICENSE](LICENSE).
