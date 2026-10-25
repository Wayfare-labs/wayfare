# Case study: the USDC → NGNC structural floor

**Finding.** The USDC → NGNC corridor loses roughly a quarter of its value at
the smallest size Wayfare prices, on a route where price impact is not the
cause. The floor has now been observed on **three separate dates across three
completely different route shapes**, and on every one of them it sits above the
20% `UNUSABLE` threshold on its own. That makes it a property of the corridor
rather than of any single route or pool — and it is the observation that turned
a routing tool into a monitor.

**Checked against the code and the committed measurement chain at commit
`74c1f17`, 2026-09-30.**

This is a case study, so it is worth saying what it is not: it is not a
[spread-versus-depth decomposition](#what-the-tool-cannot-yet-establish), and
it does not claim the tool measured one. The engine has never run a market-quality
metric for this corridor. What follows separates what was measured from what is
inferred, because the inference is doing real work in the argument and the
reader is entitled to know which part is which.

---

## The observation

The default ladder prices a corridor at twelve sizes, from 0.1 to 5000 send
units (`dex.DefaultSizes`, `dex/sizes.go:16`), shared by the route ladder and
the depth metric so both measure the same corridor at the same sizes. The bottom
rung is there to isolate a floor: at 0.1 units the trade is small enough that
price impact is not what is being measured.

On USDC → NGNC, that bottom rung does not return a good rate. It returns a loss
in the mid-to-high twenties — and the grading bands put `UNUSABLE` at anything
above 20% (Good ≤3%, Fair ≤8%, Poor ≤20%, declared in `route/route.go` as
`ThresholdGood`, `ThresholdFair` and `ThresholdPoor`, and applied in
`verdictFor`). So the floor clears the unusable line on its own, before a single
unit of size is added.

| Date | Source | 0.1 USDC rung's best path | Floor loss at 0.1 | Worst loss |
|:---|:---|:---|---:|---:|
| 2026-08-04 | `docs/corridor-measurements.md` | — | — | 52.3% at 100 USDC |
| 2026-08-08 | [recorded run](corridor-measurements.md#run-of-2026-08-08) | `USDC → NGNC` (direct, 1 hop) | **24.65%** | 97.68% at 5000 |
| 2026-08-21 | [snapshot replay](https://github.com/Wayfare-labs/wayfare/tree/main/testdata/snapshots/usdc-ngnc-20260821T223040Z) | `USDC → Cleanshave → AQUA → NGNC` (3 hops) | **28.18%** | 97.50% at 5000 |
| 2026-08-22 | [committed record](https://github.com/Wayfare-labs/wayfare/blob/main/data/USDC-NGNC.ndjson) | `USDC → BLND → XLM → NGNC` (3 hops) | **27.15%** | 97.52% at 5000 |

All twelve rungs grade `UNUSABLE` on all three dated observations, and the
integrity state is `DIRECT` on all three: an independent NGNC market exists and
the loss is not inherited from another token.

**Sources, in full.**

- **2026-08-08** — measured live at `2026-08-08T12:53:56Z` via Horizon
  `/paths/strict-send` (mainnet), scored against a reference mid of
  `1364.0070` USD/NGN from `exchangerate-api` as of `2026-08-08T00:02:31Z`.
  Raw figures in [docs/corridor-measurements.md](corridor-measurements.md).
- **2026-08-21** — recorded Horizon bytes in
  `testdata/snapshots/usdc-ngnc-20260821T223040Z`, replayed offline through the
  real `route.Engine` on 2026-09-30 and scored against the mid recorded in the
  2026-08-22 record (`1349.669672` USD/NGN, `exchangerate-api`, as of
  `2026-08-22T00:02:31Z`). The prices are dated 2026-08-21 and the benchmark is
  dated 2026-08-22; this row is a replay against a recorded mid, not a
  measurement taken on 2026-08-21, and it is labelled that way in the test that
  reproduces it.
- **2026-08-22** — a record in the committed hash-chained run store,
  `data/USDC-NGNC.ndjson`, recorded at `2026-08-22T12:09:59Z`, scored against
  `1349.669672` USD/NGN via `exchangerate-api` as of `2026-08-22T00:02:31Z`.
  This chain is embedded in the binary (`embed.go`), so a reviewer holding a
  build can verify the record itself rather than take this document's word for
  it: `go run ./cmd/wayfared -verify-store -data data`.

The 2026-08-04 row is from the `route` package's own documentation and is
included for continuity only; it is a 100 USDC observation, not a floor
measurement, and it is marked with an em dash rather than a number because the
repository does not hold a dust-size rung for that run.

---

## Why the route shape is the interesting part

Read the three dated rows again and notice that the 0.1 rung's best path is not
the same path three times. On 2026-08-08 it went **direct**, `USDC → NGNC`, with
no bridge hop at all. On 2026-08-21 it went through two intermediates
(`Cleanshave`, `AQUA`) to reach NGNC. On 2026-08-22 it went through two
different ones (`BLND`, `XLM`).

The floor barely moves: 24.65%, 28.18%, 27.15%. Whatever is producing it is not
attached to a particular route, and it is not attached to a particular pool —
`BLND` and `Cleanshave` share no liquidity. It tracks the NGNC/USD relationship
itself.

This is also why a single-size quote would have got the conclusion wrong. A
quote at 5000 USDC returns 97.52% loss, and 97.52% reads as a depth problem: the
pool is thin, get more liquidity, the number improves. The dust rung is what
rules that out. A corridor whose *smallest* trade is already a quarter of the
way to unusable is not a corridor that more liquidity rescues.

---

## The same issuer, three different failures

The issuer of NGNC also issues GHSC and KESC from the same Stellar account. The
committed chain holds all three as recorded within seven seconds of each other
on 2026-08-22, so the comparison is not confounded by market timing:

| Corridor | Integrity | 0.1 USDC rung | Rungs graded `UNUSABLE` |
|:---|:---|:---|---:|
| USDC → NGNC | `DIRECT` | priced, 27.15% loss | 12 of 12 |
| USDC → GHSC | `DERIVATIVE` | priced, 74.63% loss, every path via NGNC | 12 of 12 |
| USDC → KESC | `NO-MARKET` | **not priced** — no path exists | n/a |

Three corridors, one issuer, three different reasons a trade is a bad idea. One
prices continuously and prices badly. One has no independent market and
compounds NGNC's cost with its own. One has no market at all.

Reporting all three as "Unusable" would be accurate and would discard the
reason each is unusable, so the monitor carries an **integrity state** next to
the loss grade. `DIRECT`, `DERIVATIVE` and `NO-MARKET` are structural facts
about a corridor's shape, not severities — the taxonomy is in
[docs/glossary.md](glossary.md) and the wire contract is in the README.

One detail worth not misreading: KESC's record carries
`floor_loss_pct: "0.00"` at `floor_size: "0"`. That is not a measurement of zero
loss. It is the sentinel for *nothing was measured*, and the rung-level
`priced: false` is the honest form of the same fact. A zero that means "no
answer" is exactly the kind of thing this project refuses to publish as a
number, and it is called out here so nobody reads it as a good corridor.

---

## What the tool cannot yet establish

The sentence in the README is that the floor *"is the corridor's spread, not its
depth"*. That is a reasonable reading of the table above, but the tool does not
compute a spread/depth attribution. It now measures the loss at the dust rung
against the reference mid, which makes the structural floor a metric with
evidence without claiming to explain its cause. Three code facts, each
checkable:

**1. Market-quality metrics are reachable, and reachability is not
decomposition.** Since 2026-10-22 the runner carries a `Metrics []Metric` field
and `ForCorridor` runs them beside the checks (`checks/runner.go`), so a live
`/api/corridor?live=1` response and every recorded run behind it can carry a
`findings.metrics` block. Four default metrics read the direct order book —
spread, concentration, observed depth and deviation from the reference mid.
The new floor-loss metric reuses the smallest priced rung from the ladder, and
reports undetermined when its reference is unscorable or the ladder produced no
priced rung.

The floor-loss metric records the rung, reference mid, and provider. A spread on
the book and a loss on the ladder are still two measurements of the same
corridor. None of that attributes the floor to spread or depth: the metric
measures the loss against the reference, not its cause. Reachability was also
the repository's V2 milestone (`docs/why-wayfare.md`, `docs/market-structure.md`),
and the chains this document reads predate it: their records carry no metrics
at all, so a recorded-history reader still cannot check the attribution here.

**2. Price impact is defined against its own baseline, not against the
reference.** `PriceImpactMetric` defines impact as degradation *from the rate
at the smallest size it probes* — `defaultPriceImpactSizes` starts at 1
(`checks/metric_price_impact.go:28`), and `points[0].ImpactPct` is set to zero
by construction (`checks/metric_price_impact.go:251-253`). Impact is therefore
zero at its own baseline and cannot distinguish a size-independent floor from
size-dependent slippage. The separate `floor-loss.reference-mid` metric
compares the ladder's smallest priced rung against the *reference mid* and
reuses that rung rather than making another probe.
The floor measurement has been promoted from inference to an evidence-bearing
reference-mid figure; its spread/depth attribution remains an inference, rather
than a result.

**3. The attribution is fixed prose, not a computed result.** When no size is
viable, `route/ladder.go:528-536` emits:

> No usable size. Loss against the *{source}* mid is *{floor}%* at *{size}* —
> where price impact is negligible, so that is the corridor's structural floor,
> not a depth effect — rising to *{worst}%* at *{worst size}*.

The clause "where price impact is negligible, so that is the corridor's
structural floor, not a depth effect" is unconditional. It is not conditioned on
an impact measurement, because none is taken. The same rationale appears as a
design comment at `dex/sizes.go:7-8`. Both files are maintainer-owned
(`route/ladder.go`, `dex/`), so this document reports the discrepancy and does
not change it.

**So, precisely:** the *measurements* are the ladder, the verdicts, the
integrity states, the reference mids and the recorded paths — all real, all
dated above. The *attribution* of the floor to spread rather than depth is an
inference, and a well-supported one given that the figure survives a change of
route shape and of date, but an inference nonetheless. A reader who needs the
decomposition should read the floor as **"a loss of roughly a quarter that the
current measurements cannot attribute"**, and treat "spread" as the leading
hypothesis rather than a result.

---

## The benchmark is the charitable one

Every figure above is loss against the **official** USD/NGN mid-market rate.
The reference providers track the official rate, and the Nigerian parallel
("street") rate is empirically weaker — more naira per dollar. A larger
reference naira per dollar makes the *same* on-chain naira output score a
*larger* loss, so scoring against the official rate **understates** what a
sender who ultimately values naira at the street rate would lose.

The corridor therefore fails against the most flattering benchmark available.
A tighter or more Nigeria-specific benchmark could only move the floor further
into `UNUSABLE`, never back toward usable. The evidence for the official rate
being the right family of benchmark, and the search for a defensible
parallel-rate source that did not find one, are in
[docs/fair-value-ngn.md](fair-value-ngn.md) and
[docs/parallel-rate-research.md](parallel-rate-research.md).

The exact percentages do carry the provider's accuracy as a dependency. The
ordering, the reproduction across dates, and the route-shape independence do
not — those are properties of the on-chain data and the recorded benchmark
alone.

---

## What would close this

Not a roadmap promise, and not a claim about what is built. Listed so the gap is
concrete:

- ~~Wire a `Metric` into `checks.Runner` so `findings.metrics` can be non-empty,
  and let the recorded chain carry metrics.~~ Done since 2026-10-22: the runner
  measures the default order-book set and recorded runs carry it. The chains
  this document cites predate the change, so they do not carry one.
- ~~Add a metric that measures the dust-size rung's loss **against the reference
  mid** rather than against the smallest probed size.~~ Done:
  `floor-loss.reference-mid` reports the metric with evidence for the rung,
  reference mid, and provider.
- Spread/depth attribution remains an inference: the metric does not compute
  the share of loss attributable to either.
- Record that metric into the hash-chained store so the decomposition is
  reproducible from committed bytes rather than from this document.

The honest summary remains that at 0.1 USDC, on three dates and three different
routes, USDC → NGNC loses about a quarter of its value. The metric now records
that loss against the reference with evidence, but still cannot tell which part
of the market causes it.

---

## Longitudinal tracking

The floor is the single most meaningful series the store could produce: its
movement over time tells a reader whether the corridor's spread is widening,
narrowing or stable. Tracking it requires that each recorded run carry the
dust-size loss and its benchmark in a form that can be compared across dates.
The three dated observations above are the seed of that series; they are not
yet a continuous one, and they are not yet computed by the tool.

---

## Related documents

- [docs/corridor-measurements.md](corridor-measurements.md) — the raw ladders,
  with timestamps and the endpoint each came from
- [docs/why-wayfare.md](why-wayfare.md) — why this finding changed the product
  thesis, and what that story does not claim
- [docs/ladder-sizes.md](ladder-sizes.md) — why the ladder is 0.1 → 5000 and
  what the dust rung is for
- [docs/fair-value-ngn.md](fair-value-ngn.md) — what the reference mid is and
  why it is the charitable benchmark
- [docs/market-structure.md](market-structure.md) — which market-structure
  questions the current code can and cannot answer
- [docs/glossary.md](glossary.md) — verdicts, integrity states, agreement bands,
  and what *not determined* means
- [README.md](../README.md) — the shared contracts: thresholds, the
  recommendation rule, integrity states, and the layer model