# Spike: which V2 measurements are worth storing longitudinally

**Issue:** [#334](https://github.com/Wayfare-labs/wayfare/issues/334), backlog
entry #136 (Initiative E1 — Historical intelligence, V4). **Checked:**
2026-09-28 against tree commit `10317b1`.
**Status:** Research finding only. No implementation and no run-record change
is proposed or made. The ranking in §5 is an input to a maintainer decision,
not the decision itself.

## 1. Why the choice is expensive to undo

Two properties of the record make a bad choice costly, both verified in the
tree:

- **The layout is hash-pinned and versioned.** Every record carries
  `PrevHash`/`Hash` over its contents and a `Version` field
  (`runstore/runstore.go:122–164`, checked 2026-09-28). Fields added later
  mean another version migration (the project has already done the
  Version 2 → 3 one, documented in [docs/run-store.md](run-store.md),
  "Migration to version 3"); fields removed or redefined after history has
  accumulated quietly break cross-run comparability of the series.
- **The book state is ephemeral.** Everything the V2 metrics observe —
  order books, path sets — exists only at measurement time. A figure not
  recorded then can never be recomputed later. Storage is therefore not
  competing with "fetch it again when needed"; it is competing with "this
  fact never exists."

So the question decomposes into: *which ephemeral facts change decisions,
have stable definitions, and are cheap enough to keep?*

## 2. The measurement inventory as the tree actually has it

From `checks/metric*.go` and `checks/wire.go` at commit `10317b1` (checked
2026-09-28). This already differs from the backlog snapshot
(`docs/backlog.md`, verified 2026-08-24): metrics are now **recorded** —
`Record.Metrics []checks.MetricJSON` exists at `runstore/runstore.go:161`,
with `Record.Checks` beside it — so the "nothing yet to analyse" blocker has
partly moved. What follows describes the tree as it is.

| Metric (descriptor ID) | Upstream cost (`Descriptor.Cost`) | Emits |
|:---|:---|:---|
| `spread.bid-ask` (`checks/metric_spread.go:32`) | CostOneRequest | spread over the book mid |
| `depth.observed-executable` (`checks/metric_depth.go:45,66,118` — two descriptors) | CostExpensive / CostOneRequest | executable depth at size rungs |
| `price-impact.size` (`checks/metric_price_impact.go:88`) | CostExpensive | impact curve over probe→full sizes |
| `concentration.liquidity` (`checks/metric_concentration.go:37`) | CostOneRequest | HHI over **price levels** (documented limitation) |
| `deviation.*` book-mid vs reference (`checks/metric_deviation.go:67`) | CostOneRequest | mispricing of the book vs the benchmark |

Also record-level: headline rung figures (`FloorLossPct`, `WorstLossPct`,
per-rung `EffectiveRate`/`LossPct`/`Verdict`), `Reference` (both mids,
sources, `as_of`, `fetched_at`, `DivergencePct`) and the integrity state —
all already stored (`runstore/runstore.go:95–145`).

**Current record size, measured:** the committed chains hold one record per
corridor of **1,573–3,665 bytes** (counted from `data/*.ndjson`,
2026-09-28). At the nominal four observations/day, headlines-only history
costs ≈ **4–5 MB per corridor per year** before any metric fields are
appended.

## 3. Criteria

Four tests, in the order they are applied. The first three decide; the
fourth is a tiebreaker.

1. **Ephemeral and decision-bearing.** Does the figure describe market state
   that disappears, and would its movement change what a corridor user does?
   Figures derivable later from stored inputs fail this test — store the
   inputs instead.
2. **Definitionally stable.** Is the quantity's definition pinned in code and
   unlikely to move? A stored series whose definition churns is worse than no
   series: it invites comparing numbers that are not the same measurement.
   Evidence of churn in the tree: depth sizes vs the ladder
   (`checks/metric_depth.go` private size list vs `route.DefaultSizes`),
   exhausted-vs-unmeasured semantics, probe/full size defaults in
   `metric_price_impact.go:72–80`.
3. **Cheap per record.** Bytes at 1,460 records/corridor/year, against a
   measured baseline of ~1.6–3.7 KB/record. Anything adding kilobytes per
   record costs megabytes per corridor per year.
4. **Attribution-safe.** The figure must keep its venue recorded or a
   longitudinal comparison lies: the book excludes AMM liquidity while
   pathfinding includes it (`checks/wire.go:44–50`,
   [docs/liquidity-venues.md](liquidity-venues.md)); consumers must never
   reconcile the two by arithmetic.

## 4. Per-measurement analysis

| Measurement | Ephemeral & decision-bearing | Stable definition | Cost/record | Verdict |
|:---|:---|:---|:---|:---|
| **Book mid** (input to spread/deviation) | Yes — the book is gone the moment it is read | Yes — a mid at a recorded timestamp | ~10 bytes (a decimal string) | **Store.** The single best candidate: an input, not a derived figure |
| **Spread** (derived from stored bid/ask or mid) | Yes | Yes | ~10 bytes | **Store** if bid/ask are not both stored; otherwise re-derivable. Storing the inputs makes the ratio optional |
| **Depth at stated sizes** | Yes | **No — sizes and exhausted/unmeasured semantics are still moving** | ~50–100 bytes with a size vector | **Store conditionally:** only once the size set follows the ladder and "exhausted" is distinguishable from "unmeasured", else the series breaks on the first definition change |
| **Price-impact curve** | Yes — the whole curve is the fact; one summary figure loses the shape | Partly — never-interpolate discipline exists but probe/full defaults are not pinned | ~100–300 bytes at a coarse size set | **Store at coarse sizes only**, explicitly labelled "at measured sizes"; a stored curve that implies interpolation is a lie |
| **Book-vs-reference deviation** | Yes | Yes — ratio of two stored mids | ~0 bytes if both mids stored | **Do not store the ratio; store the book mid** (row 1) and the reference mids (already stored). Re-derivable forever |
| **Concentration (HHI over price levels)** | Yes, but decision value is weak while the metric can only see levels, not participants | Partly — the limitation is documented, not resolved | ~10 bytes | **Defer.** Honest verdict: today it answers a question (participant concentration) it cannot measure. Storing it invites over-reading HHI later |
| **Reference mids, divergence, agreement** | Benchmark state is ephemeral too | Yes | Already stored | Keep — and `reference_as_of` vs `fetched_at` both matter (backlog #102: only one reaches the wire today) |
| **Headline rung figures & verdicts** | Yes | Yes — verdict thresholds are the project's most-pinned numbers | Already stored | Keep |
| **Cost decomposition** (if/when wired) | Yes | Unsettled — fee-as-zero and slippage-undetermined semantics are open (`needs-maintainer-review` items) | ~100 bytes | **Defer** until the component semantics stop moving |

## 5. Finding

**Ranked answer to the issue's question**, with the two negative results
first because they are the load-bearing part:

- **Worth storing longitudinally:** the *raw book inputs* — book mid (and
  bid/ask where the venue publishes them), at the recorded timestamp, with
  venue attached. They are few bytes each, their definitions are stable, they
  are unrecoverable once the book is gone, and from them the spread and the
  book-vs-reference deviation — the two series with the strongest claim on
  future analysis — are re-derived without ever risking a stored ratio whose
  definition drifted. **Store inputs, derive ratios.**
- **Not worth storing (negative findings, stated as such):** the
  concentration figure today, and any deviation/spread *ratio* as a stored
  field. The first cannot yet answer the question it exists for; the second
  is free to recompute and expensive to keep definitionally honest.
- **Conditional:** depth and the price-impact curve — decision-bearing and
  ephemeral, but their definitions are still moving in the tree. Storing
  them now risks a series that changes meaning mid-stream; pinning the
  definitions is the cheaper first move and is not part of this spike.

**The storage arithmetic that frames the maintainer decision:** adding the
recommended fields costs on the order of **tens of bytes per record** — under
~5% growth on the measured 1.6–3.7 KB baseline, roughly 200 KB per corridor
per year. Adding every candidate metric indiscriminately roughly triples the
metric block for figures half of which are re-derivable. Against a rolling
window of `MaxWindow = 366` records per corridor (`runstore/file.go:355`),
the committed-chain cost is bounded either way; the unbounded cost lands in
Git history as windows rotate (ADR 007), which is where the "storage is not
free" premise of the issue actually bites.

**Limitations of this finding, plainly stated:**

- The ranking rests on the four criteria in §3, which this spike chose; a
  maintainer may weight ephemeralness against definitional stability
  differently, and the decision is flagged `needs-maintainer-review` in
  spirit — the issue assigns no implementer authority to this document.
- No long-run dataset exists to test any of this against (one record per
  corridor in the committed chains, counted 2026-09-28), so the byte
  estimates are arithmetic on current record sizes, not measurements of a
  populated history.
- Whether depth/curve definitions are "about to settle" is a judgment read
  from open definition-level questions, not a schedule.

## 6. Sources

| # | Source | Date checked |
|---|---|---|
| 1 | `runstore/runstore.go:64–164` (`Reference`, `Rung`, `Record` incl. `Checks`/`Metrics` fields) at commit `10317b1` | 2026-09-28 |
| 2 | `runstore/file.go:342–355` (`MaxWindow = 366`) at commit `10317b1` | 2026-09-28 |
| 3 | [ADR 007 — why the committed chain is a rolling window](adr/007-why-the-committed-chain-is-a-rolling-window.md) | 2026-09-28 |
| 4 | `checks/metric_spread.go`, `metric_depth.go`, `metric_price_impact.go:72–80`, `metric_concentration.go`, `metric_deviation.go` (descriptor IDs, costs, defaults) at commit `10317b1` | 2026-09-28 |
| 5 | `checks/wire.go:34–54` (`MetricJSON`, venue semantics) at commit `10317b1` | 2026-09-28 |
| 6 | [docs/liquidity-venues.md](liquidity-venues.md) | 2026-09-28 |
| 7 | [docs/run-store.md](run-store.md) (Version 3 migration precedent) | 2026-09-28 |
| 8 | [docs/backlog.md](backlog.md) entries #62, #64, #66, #73, #102, #119 (definition-level questions and wire gaps; snapshot dated 2026-08-24, re-checked against the tree 2026-09-28) | 2026-09-28 |
| 9 | `data/*.ndjson` byte counts (1,573 / 3,407 / 3,665 bytes, one record each) | 2026-09-28 |
