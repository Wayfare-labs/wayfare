# Spike: real-time versus scheduled measurement economics

Issue [\#220](https://github.com/Wayfare-labs/wayfare/issues/220), backlog `#160`.

**Status: completed.** The 6-hour cadence is defensible on economics: the
marginal value of a tighter recording cadence is unquantifiable from the data
this repository holds, while the marginal cost multiplies by 360× at 1-minute
cadence (3 corridors × ~36 Horizon calls per sweep). The finding is therefore
**inconclusive-to-negative for tightening**: no evidence in the tree justifies
spending more upstream budget per day for resolution nobody has demonstrated
needing. "Real time" is best purchased on demand with the existing `live=1`
path, not by raising the recording cadence.

---

## Why this matters

`monitor.DefaultInterval` is 6 hours because the corridors the project has been
measuring "move on the scale of days" (`monitor/monitor.go:58-66`). The backlog
grew a spike because that choice's cost of being wrong — corridors that
deteriorate *within* a six-hour window go unrecorded — was never quantified.
This finding prices both sides: what a tighter or looser cadence would cost, and
what evidence exists that corridors actually move fast enough to justify it.

---

## What the scheduled path costs today

### Per-sweep cost

- A sweep calls Horizon for pathfinding. The `monitor` package comment states
  "one run is roughly three dozen Horizon calls per corridor"
  (`monitor/monitor.go:63-65`, checked 2026-09-23). The twelve-rung ladder
  (`dex.DefaultSizes`, `dex/sizes.go:16-28`) prices each size through
  `StrictSendPaths`, and the ladder runs four sizes concurrently
  (`route/ladder.go:22-25`, `ladderConcurrency = 4`).
- The reference-rate leg is a fetch of a *pair*, not a per-size fetch, and
  `refrate.Cached` collapses the twelve rungs' reference lookups to at most one
  fetch per pair per TTL window (`refrate/cached.go:11-21`, `DefaultCacheTTL =
  time.Hour`, checked 2026-09-23).
- Default scope is three corridors: USDC→NGNC, USDC→GHSC, USDC→KESC
  (`monitor/monitor.go:50-56`).

### Per-day cost at 6 hours

| Cadence | Sweeps/day/corridor | Horizon calls/day | vs 6h |
|---|---:|---:|---:|
| 6 h (current) | 4 | 3 × 4 × ~36 ≈ **432** | 1× |
| 1 h | 24 | 3 × 24 × ~36 ≈ **2,592** | 6× |
| 15 min | 96 | 3 × 96 × ~36 ≈ **10,368** | 24× |
| 1 min | 1,440 | 3 × 1440 × ~36 ≈ **155,520** | 360× |

The 1-minute case is roughly 360× today's Horizon load for three corridors, and
Horizon is a shared public service the codebase explicitly says it wants to
avoid bursting (`monitor/monitor.go:60-65`). The reference providers publish
roughly daily, so *their* load is capped by the TTL at well under the ladder's;
the Horizon leg is the scaling term.

### Why the recording foot is in the code at all

The `measure.yml` workflow schedules the same sweep with `cron: "0 */6 * * *"`
(`.github/workflows/measure.yml:14-19`, checked 2026-09-23), appending to the
hash-chained store. The gap to "real-time" is entirely a scheduling question:
there is no architecture reason a sub-6-hour cadence cannot run — it is a
`time.Duration` on `Scheduler.Interval` plus a cron string. The question this
spike answers is whether there is *evidence* the extra sweeps are worth their
cost.

---

## What was investigated

### 1. How fast do these corridors actually move?

- `docs/corridor-measurements.md` records USDC→NGNC at 100 USDC on 2026-08-04
  and 2026-08-08: 52.3% loss vs 53.89% loss, "four days apart, the corridor is
  stable and slightly worse" (`docs/corridor-measurements.md:150-158`, checked
  against the same document 2026-09-23).
- The same document's same-window re-measurement is the nearest thing to a
  "real-time delta": 0.1 USDC moved from 24.65% to 25.02% loss between the
  12:53 and 14:27 runs on 2026-08-08 — 0.37 points in ~1.5 hours
  (`docs/corridor-measurements.md:228-232`). That is the *largest recorded
  intra-day movement in the repo*, and it did not cross any verdict boundary
  (both measurements are UNUSABLE past the 20% threshold).
- The committed `data/*.ndjson` contains **one record per corridor** (seq 1,
  version 1, `2026-08-22`), so this checkout holds no longitudinal sample of
  6-hour gaps to measure what a tighter window would have caught
  (`data/USDC-NGNC.ndjson`, `data/USDC-GHSC.ndjson`, `data/USDC-KESC.ndjson`,
  checked 2026-09-23).

### 2. What does the statistical layer assume about cadence?

- `analysis` requires **30 observations** before mean/std dev are determined and
  **60** before trend is (§`analysis/analysis.go:39-48`, checked 2026-09-23).
  At 6-hour cadence those are ~12.5 and ~25 days respectively — the constants'
  own doc comments make the cadence mapping explicit.
- A tighter cadence does **not** therefore produce determinations sooner in an
  *informational* sense: 30 observations taken once a minute are 30 samples of
  the same market state, not 30 independent measurements. The meaningful
  improvement a tight cadence offers is catching a *state change* at higher
  temporal resolution (`/api/corridor/trend` lists runs; `trend.go:117-138`),
  not computing statistics faster.
- Severing this is exactly what the alerting spike (\#222) constrains: a
  notification is a comparison of recorded facts, never a prediction
  (`docs/spike-alerting-semantics.md`). A sub-6h cadence adds data points that
  make that comparison finer, but the comparison only fires on a recorded
  change, so the value of tighter recording is bounded by how often corridors
  change state — question (1).

### 3. Is "real-time" already available on demand?

- Yes, as a pull. `live=1` on `GET /api/corridor` forces a fresh measurement
  and bypasses stored history (`server/api.go:172-178`, `docs/api.md:26`,
  checked 2026-09-23). A consumer needing a current number today can request one
  at the moment of need and pay the full ladder cost *only then*.
- History-first mode (`HistoryFirst`) exists for deployments whose request
  timeout cannot hold a ladder (`server/api.go:42-51`), and a failed live
  measurement degrades to the latest stored run labelled `live: false`
  (`server/api.go:211-237`). The two existing mechanisms split "scheduled
  history" from "request a live reading" cleanly.
- What does **not** exist, and must be marked future: any push/subscription
  channel. There is no websocket, no webhook, no notification queue in the tree
  (grep of `server/`, `monitor/`, `runstore/`, `checks/` on 2026-09-23 for
  `pubsub`, `subscribe`, `webhook`, `SSE`, `Notifier` — none present).

---

## Verdict

**Inconclusive-to-negative for changing the scheduled cadence.** Concretely:

- **The cost side is quantitative and material.** Sub-hourly cadence multiplies
  Horizon load 6×–360× against a shared public service, for no additional
  *statistical* information (the `analysis` minima are observation-count-based,
  not time-based) and no additional *reference* freshness (providers publish
  daily; `refrate.DefaultCacheTTL` already caps reference calls).
- **The benefit side has no evidence in the tree.** The only recorded intra-day
  movement (0.37 loss points at the 0.1 rung, 2026-08-08) crossed no verdict
  boundary, and the repo's own retained data contains exactly one measurement
  per corridor — so no one can point at a missed event that a tighter window
  would have caught.
- **The on-demand path already covers the "real time now" need** with `live=1`,
  paying full cost only when asked.
- Therefore: **recommendation — keep 6 h as the scheduled recording cadence, and
  treat `live=1` pull, not a tighter cron, as the "real-time" answer.** Revisit
  only if both (a) a corridor is observed moving across a verdict threshold
  within a 6-hour gap on the recorded/`live` data, and (b) a consumer
  demonstrates needing that observation sooner than the on-demand path gives it.
  A push/subscription channel, if ever wanted, is future work and is absent from
  the tree today.

## Related

- [corridor-measurements.md](corridor-measurements.md) — recorded movement
  evidence and the same-window re-measurement
- `monitor/monitor.go`, `.github/workflows/measure.yml` — the 6-hour wiring
- `refrate/cached.go`, `refrate/cross.go` — reference cadence and cache bounds
- `analysis/analysis.go` — observation-count minima and their cadence mapping
- `server/api.go`, [api.md](api.md) — `live=1` on-demand measurement
- [spike-alerting-semantics.md](spike-alerting-semantics.md) — the alerting
  constraint that keeps cadence economics from becoming prediction