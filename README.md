# Wayfare

A corridor-integrity monitor for Stellar.

Wayfare prices a stablecoin → fiat-token corridor across trade sizes, scores
every route against an independent mid-market rate, and states plainly when
none of them are worth taking — including when the honest answer is *don't
send this*.

**Status:** pre-MVP, read-only. Non-custodial: no funds held, no tokens
issued, no KYC, no keys.

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
above *understates* the loss.

The issuer set is case study #1, not the product. Wayfare measures any
stablecoin → fiat-token corridor.

---

## Architecture

```
        ┌──────────────────┐        ┌────────────────────────────┐
        │ Horizon          │        │ Reference providers        │
        │ /paths/strict-   │        │ exchangerate-api           │
        │ send  (mainnet)  │        │ currency-api               │
        └────────┬─────────┘        └──────────────┬─────────────┘
                 │                        cached, cross-checked
                 │                        for divergence
                 │                                 │
                 ▼                                 ▼
        ┌────────────────────────────────────────────────────────┐
        │  route.Engine                                          │
        │    ladder sweep: 0.1 → 5000, priced concurrently       │
        │    per size:  effective rate → loss vs mid → verdict   │
        │    per corridor:  integrity  (structure, not price)    │
        └───────────────────────────┬────────────────────────────┘
                                    │
                     ┌──────────────┴──────────────┐
                     │                             │
                     ▼                             ▼
        ┌────────────────────────┐   ┌──────────────────────────┐
        │ monitor.Scheduler      │   │ server.Server            │
        │   every 6h, headless   │   │   /api/corridor  (live)  │
        └───────────┬────────────┘   │   /  single-file UI      │
                    │                └────────────┬─────────────┘
                    ▼                             │ on failure
        ┌────────────────────────┐                │ serve last run,
        │ runstore  (hash chain) │◀───────────────┘ labelled stale
        │   NDJSON, append-only  │
        │   prev_hash → hash     │
        └────────────────────────┘
```

Two things about this shape are deliberate.

**The scheduler does not depend on the server.** `monitor` imports nothing
from `server`, and `wayfared -serve=false` runs measurements with no HTTP at all.
A monitor that only measures while somebody has a page open would leave holes
in its history exactly where nobody was looking.

**The store is downstream of everything.** Nothing reads from it to produce a
measurement; it is read only when a live measurement fails, and then the
response says so.

---

## Shared contracts

These are the agreements other code and other people depend on. **Changing any
of them is a breaking change**, not a refactor.

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

### Asset identity — breaking if altered

An asset code identifies nothing; **the issuer account is the identity.**
Anyone can issue a token called `USDC`. Every issuer is read from the issuer's
own `stellar.toml` per SEP-1, with the verification date recorded, because
issuers rotate. Wire form is `stellar:CODE:ISSUER`, `stellar:native`, or
`iso4217:CODE`.

### Money on the wire — breaking if altered

Every amount, rate and percentage is a **decimal string**, never a JSON number.
A JSON number invites a client to parse it into a `float64`, reintroducing the
rounding error the engine avoids internally. There is a test at the boundary.

### Snapshot format — version 1

Recorded upstream bytes, verified by hash on load. A replayer **must refuse a
version it does not know.** Full spec: **[docs/snapshot-format.md](docs/snapshot-format.md)**

### Run record — version 1

```
hash = sha256(record JSON with the hash field omitted)
```

`prev_hash` is inside the hashed preimage — that is what makes the history a
chain rather than a pile. **The field set and their declaration order are part
of every hash**, so adding, removing or reordering a field is a version bump
plus a migration, never a tidy-up. `TestRecordHashIsPinned` fails in CI on the
commit that would have broken it.

Full spec: **[docs/run-store.md](docs/run-store.md)**

---

## Packages

| Package | Responsibility |
|:---|:---|
| `asset` | Corridor endpoints, verified issuers, the fiat-peg registry |
| `refrate` | Reference mid-market rates: two providers, cached, cross-checked |
| `anchor` | SEP-1 discovery — can this anchor be priced at all? |
| `sep38` | Anchor RFQ client, with the fee-denomination identity |
| `dex` | On-chain pricing via Horizon pathfinding, plus market health |
| `route` | Ladder sweep, verdicts, integrity, and the shared wire shape |
| `runstore` | Hash-chained measurement history |
| `monitor` | Scheduled measurement, independent of HTTP |
| `snapshot` | Record and replay upstream responses |
| `server` | HTTP surface and the embedded single-file UI |
| `cmd/ladder` | Measurement CLI |
| `cmd/wayfared` | Server and scheduler |

---

## Running it

```bash
make run                        # measure USDC -> NGNC against live mainnet
go run ./cmd/ladder -to GHSC    # any verified corridor
go run ./cmd/ladder -to GHSC -json | jq

go run ./cmd/wayfared                       # serve + measure every 6h
go run ./cmd/wayfared -serve=false          # scheduler only, no HTTP
go run ./cmd/wayfared -verify-store -data ./data
```

Go 1.22+. Dependencies: `shopspring/decimal` and `BurntSushi/toml`. Both
binaries need live network access — there are no cached figures to fall back
on, by design.

Deployment, cost and backup: **[docs/deployment.md](docs/deployment.md)**

### HTTP API

```
GET /api/corridor?to=NGNC[&from=USDC][&sizes=1,10,100]
GET /api/assets
GET /healthz
GET /                            single-file UI, no build step
```

Beyond the contracts above, one field to know: **`live`** is on every response.
`false` means the reading came from history because a live measurement failed,
and `stale` then carries its age. With no stored run, the request errors —
nothing is ever synthesised to fill the gap.

---

## Roadmap

Capability-based, no dates.

**v0.1 — Measure.** *Largely done.* Ladder sweep, verdicts, the integrity
taxonomy, cross-checked reference rates, recorded snapshots, pinned
arithmetic. Remaining: `refrate` unit tests, fixture consolidation, and
verifying the USDC issuer against Circle's own `stellar.toml`.

**v0.2 — Observe.** Run continuously at a public URL; chart the trend; alert
when a corridor's integrity state changes. The store and scheduler exist; the
deployment does not yet.

**v0.3 — Broaden.** More corridors from more issuers, chained fiat
dependencies beyond one intermediate, an explicit bridge-asset registry. The
value of the taxonomy is comparative, and one issuer is not a comparison.

**Later — direction only.**

*An on-chain attestation oracle* — publishing corridor integrity where
contracts could read it. Deferred because it introduces a publisher-trust
assumption the project does not currently have: today every figure is
independently reproducible from recorded bytes, and an oracle asks readers to
trust the publisher instead. That trade needs to be worth something first.

*Integrator tooling* — SDKs, embeddable widgets. Deferred because it should
follow demand rather than precede it. Building an SDK for users who do not
exist is how a measurement tool becomes a platform nobody asked for.

---

## Where to start

Issues are labelled by area and by difficulty. Start with
[`good first issue`](https://github.com/Wayfare-labs/wayfare/labels/good%20first%20issue).

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
| Continuous public deployment | **Not yet running** |

Unverified claims are marked in the code at the point they are used.

Reference rates come from official/interbank figures. For currencies under
exchange controls the rate people actually transact at can diverge, so this is
a defensible benchmark rather than ground truth — and it is the charitable
direction for the corridors measured here.

---

## License

Apache-2.0. See [LICENSE](LICENSE).
