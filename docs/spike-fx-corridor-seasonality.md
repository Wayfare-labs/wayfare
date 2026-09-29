# Spike: seasonality in FX corridors under exchange controls

**Issue:** [#335](https://github.com/Wayfare-labs/wayfare/issues/335), backlog
entry #137 (Initiative E1 — Historical intelligence, V4). **Checked:**
2026-09-28 against tree commit `10317b1` and the sources named inline.
**Status:** Research finding only. No implementation and no run-record change
is proposed or made.

## The question, and what this finding actually reports

The issue asks whether NGN corridors show day-of-week or month-end structure,
"from published literature and from the chain once it is long enough." This
document reports two separable results and keeps them separable throughout:

1. **What the published literature supports** — whether prior work gives any
   reason to expect calendar structure in NGN-related FX series. Result:
   partial and conditional; see §2.
2. **What the chain can support** — whether Wayfare's recorded history could
   detect such structure today. Result: **no — a negative finding**, and the
   reasons are structural, not a matter of waiting longer under the current
   record layout; see §3 and §4.

No seasonality claim about NGN corridors is made here, positive or negative,
from Wayfare data: the data to make one does not exist in the repository
checked on 2026-09-28.

## 1. What Wayfare records today (the measurement surface)

Ground truth from the tree at commit `10317b1`:

- `monitor.DefaultInterval = 6 * time.Hour` (`monitor/monitor.go:66`,
  checked 2026-09-28) — four scheduled observations per corridor per day,
  nominal cadence.
- The committed chain is a **rolling window of at most 366 records per
  corridor** — `runstore.MaxWindow = 366` (`runstore/file.go:355`, checked
  2026-09-28), with the design rationale in
  [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md).
- The committed `data/*.ndjson` files currently hold **one record each**
  (USDC-NGNC, USDC-GHSC, USDC-KESC, all recorded 2026-08-22; counted
  2026-09-28). Earlier records remain in Git history, not at HEAD.
- Each record carries headline figures per rung — `SendAmount`, `Priced`,
  `EffectiveRate`, `LossPct`, `Verdict`, plus `FloorLossPct`, `WorstLossPct`,
  reference mids, divergence and checks/metrics fields
  (`runstore/runstore.go:64–164`, checked 2026-09-28).
- The reference benchmark for NGN is the official rate; two providers are
  corroborated in `refrate/cross.go`, and a parallel-rate source now exists as
  `refrate/parallel.go` (file present in tree, checked 2026-09-28; design
  discussion in [docs/parallel-rate-research.md](parallel-rate-research.md)).

Implication for any calendar study: **366 six-hour observations ≈ 91 days per
corridor** is the maximum depth available at HEAD, and only if the rolling
window is full. A month-end analysis needs the window aligned so that several
month-ends fall inside it; a day-of-week analysis consumes the same window.
Nothing older is reachable from a fresh checkout.

## 2. What the literature supports — and what it does not

Checked 2026-09-28. Every entry names its source and what it actually
establishes. **No published study of calendar seasonality in the USD/NGN
parallel-market premium was found** in this pass; the literature below is the
closest admissible evidence and none of it is about NGN corridors directly.

| Source | What it establishes | Checked |
|---|---|---|
| Lakonishok & Smidt, "Are Seasonal Anomalies Real? A Ninety-Year Perspective", *Review of Financial Studies* 1(4), 1988 ([DOI page](https://academic.oup.com/rfs/article-abstract/1/4/403/1566965)) | Persistent turn-of-week / turn-of-month / turn-of-year structure in **DJIA equity returns** over 90 years. Asset class: equities, not FX. Establishes that calendar anomalies can survive long samples and scrutiny; it does not establish FX seasonality. | 2026-09-28 |
| Hansen, Lunde & Nason, "Testing the Significance of Calendar Effects" (Brown Univ. working paper 2003-3, [PDF](https://economics.brown.edu/sites/default/files/papers/2003-3_paper.pdf)) | The multiple-testing problem: with a large universe of candidate calendar effects, some will appear significant by construction. Any Wayfare calendar scan needs this correction or its "findings" are noise generators. | 2026-09-28 |
| IMF Selected Issues Paper, "Potential Drivers of Post-Reform Parallel Market Premium" (2025, [elibrary](https://www.elibrary.imf.org/view/journals/018/2025/105/article-A001-en.xml)) | Ethiopia-focused; documents the parallel premium collapsing toward zero after Ethiopia's 2024 unification, with a comparative section on **Nigeria's June 2023 unification**, where the premium collapsed as the official rate aligned with market conditions. Establishes that the NGN premium is **regime-dependent**: its level and dynamics changed materially in June 2023. | 2026-09-28 |
| IMF eLibrary, "Nigeria's Shift to a Floating Regime" (Working Paper WP/26/126, 2026, [elibrary](https://www.elibrary.imf.org/view/journals/002/2026/126/article-A002-en.xml)) | Confirms June 2023 as the structural break date for the NGN regime (end of the multi-window managed system). | 2026-09-28 |
| SSRN preprint on inflation/exchange-rate shocks in Nigeria (abstract page, [SSRN](https://papers.ssrn.com/sol3/Delivery.cfm/81ce96cd-987f-4003-a261-30c7d97afb10-MECA.pdf?abstractid=7359227&mirid=1&type=2)) | Pre-unification sample (Nov 2014–May 2023): parallel premium averaging ~30%, peaking above 77%. Working-paper figures; useful for magnitude context only. | 2026-09-28 |

**What follows, stated carefully.** The literature gives two conditional
reasons a calendar effect *could* exist in an NGN corridor series: (a)
calendar anomalies are a robust empirical family in financial series
generally (Lakonishok & Smidt), and (b) under exchange controls, month-end FX
demand from importers and obligors is a documented market-structure story —
though the sources found here establish the *premium's level and regime
shifts*, not its intra-month timing. **Nothing found establishes day-of-week
or month-end structure in any NGN series itself.** That is an open empirical
question, not a prior the project can lean on.

**Regime caveat that dominates sample-size concerns:** the June 2023
unification is a structural break (IMF WP/26/126). Any premium-adjacent series
spanning that date mixes two regimes; seasonality estimated across the break
describes neither regime. For Wayfare this matters only for the parallel-rate
series (`refrate/parallel.go`), since the official-rate benchmark moved at the
break; for measurements recorded *after* 2026 the regime is the current one.

## 3. Can the chain detect seasonality today? — No (negative finding)

Assume the rolling window is full: 366 six-hour observations per corridor.
Checking against the project's own analysis gates
(`analysis/analysis.go:43–48`, checked 2026-09-28:
`MinSampleSizeForMeanStdDev = 30`, `MinSampleSizeForTrend = 60`):

| Analysis | Observations needed | Available in a full window | Verdict |
|:---|:---|:---|:---|
| Mean/std-dev of a rung series (30-gate) | ≥30 | ~366 | Computable — but this is a level summary, not seasonality |
| Linear trend (60-gate) | ≥60 | ~366 | Computable — trend, not calendar structure |
| Day-of-week: 7 weekday cells | ~52 obs/cell/year, but window holds ~91 days → **~13 obs/cell** | ~13 | **Under-powered by the project's own standards** — 13 obs/cell is below the 30-observation gate the repo already applies to any summary |
| Month-end vs rest: one window holds ~3 month-ends | ~3–6 obs per month-end cell | ~3–6 | **Not estimable.** Each month-end is also a distinct calendar event; 3–6 draws cannot support a claim |
| Full monthly cycle × year-of-months | ≥12 month-ends, ideally 24+ | ~3 | **Not reachable at HEAD** — requires 1–2+ years of retained records, i.e. retention beyond `MaxWindow` |

The structural blockers, each checked against the tree on 2026-09-28:

1. **Window depth vs calendar depth.** 366 six-hour records ≈ 91 days.
   Day-of-week detection wants ≥52 weeks; month-end detection wants ≥12
   month-ends. The committed window cannot hold either by roughly 4× (weeks)
   and 4–8× (month-ends). This is a retention-design fact
   (`runstore.MaxWindow`, ADR 007), not a cadence problem.
2. **Multi-testing exposure.** Scanning 7 weekday cells × several rung series
   × several corridors multiplies candidate tests exactly as Hansen et al.
   warn; without a correction, spurious "findings" are guaranteed over enough
   scans. The repository has no multiple-comparison machinery
   (`analysis/` checked 2026-09-28), and this finding does not propose
   building any (see §5).
3. **Observation-index vs time regression.** The existing trend uses
   observation index as x; calendar analysis needs true timestamps and gap
   handling. Scheduled runs that fail or are skipped leave no record saying
   why (the general limitation recorded in
   [docs/spike-90-day-history.md](spike-90-day-history.md)), and irregular
   intervals make "weekday of observation" ill-defined for the skipped slot.

## 4. What would have to be true for this question to become answerable

Stated as preconditions, not recommendations, and requiring no run-record
layout change (that bar is outside this issue):

- **Retention.** Either a deliberately extended per-corridor retention
  (≫ `MaxWindow`) or an externally archived series outside the rolling
  window, so that ≥24 month-ends are observable. ADR 007 exists because this
  is a storage-cost decision, not a code task.
- **Samples per cell.** ≥30 observations per weekday cell and per month-end
  cell before any cell-level summary is even reported — the project's own
  30-observation gate applied per cell, not per series.
- **Multiple-testing control.** A pre-registered, small set of calendar
  hypotheses with a correction applied, so a green cell means something.
- **Regime tagging.** Any premium-adjacent series must carry (or be separable
  by) the FX regime it was measured under, given the June 2023 break.

Until all four hold, the correct published answer to "is there month-end
structure in the NGN corridor?" is **undetermined** — which under this
project's vocabulary is a real state, not a failure.

## 5. Negative findings and what this spike deliberately does not do

- **No claim is made about actual NGN seasonality** — neither presence nor
  absence. The chain cannot support either claim (§3), and the literature
  does not address NGN calendar structure (§2).
- **No implementation is attempted.** No code, no new recorded field, no
  change to `MaxWindow`, no new analysis entry point. The preconditions in
  §4 are recorded for whoever picks up E1 for real.
- **No third-party data source is asserted as integrated.** Parallel-rate
  literature and tooling are cited as context only; what
  `refrate/parallel.go` does is described from the tree, not extended.

## 6. Sources

| # | Source | Date checked |
|---|---|---|
| 1 | `monitor/monitor.go:58–66` (`DefaultInterval = 6h`) at commit `10317b1` | 2026-09-28 |
| 2 | `runstore/file.go:342–355` (`MaxWindow = 366`) at commit `10317b1` | 2026-09-28 |
| 3 | [ADR 007 — why the committed chain is a rolling window](adr/007-why-the-committed-chain-is-a-rolling-window.md) | 2026-09-28 |
| 4 | `data/*.ndjson` record counts (1 record per corridor, recorded 2026-08-22) | 2026-09-28 |
| 5 | `runstore/runstore.go:64–164` (record fields) at commit `10317b1` | 2026-09-28 |
| 6 | `analysis/analysis.go:43–48` (sample-size gates) at commit `10317b1` | 2026-09-28 |
| 7 | `refrate/` directory listing incl. `parallel.go` at commit `10317b1` | 2026-09-28 |
| 8 | Lakonishok & Smidt (1988), *RFS* 1(4) — [publisher page](https://academic.oup.com/rfs/article-abstract/1/4/403/1566965) | 2026-09-28 |
| 9 | Hansen, Lunde & Nason (2003), Brown WP 2003-3 — [PDF](https://economics.brown.edu/sites/default/files/papers/2003-3_paper.pdf) | 2026-09-28 |
| 10 | IMF SIP 2025/105, post-reform parallel premium (Ethiopia; Nigeria comparison) — [elibrary](https://www.elibrary.imf.org/view/journals/018/2025/105/article-A001-en.xml) | 2026-09-28 |
| 11 | IMF WP/26/126, Nigeria's shift to a floating regime — [elibrary](https://www.elibrary.imf.org/view/journals/002/2026/126/article-A002-en.xml) | 2026-09-28 |
| 12 | SSRN preprint 7359227 (pre-unification premium magnitudes; working paper) | 2026-09-28 |
| 13 | [docs/parallel-rate-research.md](parallel-rate-research.md), [docs/spike-90-day-history.md](spike-90-day-history.md) | 2026-09-28 |
