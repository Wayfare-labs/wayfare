# Spike: minimum observations for a defensible trend claim

**Issue:** [#337](https://github.com/Wayfare-labs/wayfare/issues/337), backlog
entry #139 ([docs/backlog.md](backlog.md)), Initiative E / E1 — Historical
intelligence (V4). **Checked:** 2026-10-23 against tree commit `cfd9c48` and
the sources named inline.
**Status:** Research finding only. No implementation, no threshold change and
no run-record change is proposed or made.

---

## Summary

**The number is 100 observations**, and it is a floor, not a target: the
fewest observations of one recorded series, for one corridor, below which the
project should decline to publish a trend direction under any circumstances.
It is the sample size at which a two-sided Mann–Kendall test at α = 0.05 first
has about 80% power to detect a monotone drift equal to one noise standard
deviation across the window, **assuming independent observations**. Positive
serial correlation raises the requirement by a factor of about
(1 + ρ)/(1 − ρ); at ρ = 0.5 it is about 300.

Two qualifications are part of the finding, not footnotes to it:

1. **The floor cannot be applied to anything today.** Every committed chain
   holds exactly one record (§1.4). The repository has never had a series
   long enough to test against any floor, so whether real corridor series
   are close to independent — the assumption the number rests on — is
   unknown.
2. **The repository already publishes a trend direction at a lower bar.**
   `analysis.MinSampleSizeForTrend = 60` gates a least-squares slope that is
   labelled `improving` / `stable` / `worsening` by a fixed dead zone, with
   no significance test (§1.2). At 60 independent observations a one-sigma
   drift is detected about 58% of the time (§2.3). Changing that gate is
   outside this issue; it is listed as follow-up (§5).

So the result is **mixed**: a defensible floor can be given and its reasoning
written out (positive), but the data needed to check its central assumption
does not exist in the repository, and the existing gate is below it
(negative). Neither part is padded.

---

## 1. What the repository supports today

Every claim in this section was checked against the tree on 2026-10-23.

### 1.1 What a run record holds

`runstore.Record` (`runstore/runstore.go`, lines 138–184) stores, per run:
`seq`, `recorded_at`, `corridor`, `integrity`, `depends_on`, a `reference`
block (primary and secondary mid, sources, `divergence_pct`,
`scored_against`), the headline figures `floor_loss_pct`, `floor_size`,
`worst_loss_pct`, `worst_size`, the recommended rung, `finding`, the per-size
`rungs`, optional `checks` / `metrics` / `snapshot`, and the hash pair. The
numeric fields are decimal strings: `floor_loss_pct` to two places,
`divergence_pct` to four (observed in the committed records, §1.4).

Each corridor's committed chain is bounded at `runstore.MaxWindow = 366`
records (`runstore/file.go`, lines 414–427; ADR 007). At a six-hour cadence
that is about 91 days.

### 1.2 What "trend" means in the code as it is

The only trend computation in the tree is `analysis.computeTrend`
(`analysis/analysis.go`, lines 243–297):

- **Method.** Ordinary least squares of value on **observation index**
  (`x = 0, 1, 2, …`, line 251), not on elapsed time.
- **Classification.** `|slope| > 0.1` per observation is `worsening` or
  `improving`; otherwise `stable` (lines 275–290). The 0.1 is a dead zone in
  the series' own units, not a test statistic. No standard error, p-value or
  interval is computed.
- **Gate.** Computed only when `n ≥ MinSampleSizeForTrend = 60`
  (lines 45–48, 203–207). Below 30 the whole result is undetermined with a
  `Reason`; **between 30 and 59 the mean and standard deviation are
  published and the trend is silently absent** — no reason string names the
  trend shortfall.
- **Doc/code mismatch.** The constant comments say 30 observations "≈12.5
  days" and 60 "≈25 days" at a six-hour cadence (lines 18–19, 42, 47). At
  four runs a day those are 7.5 and 15 days. `docs/regime-classification.md`
  (line 60) already gives the correct 7.5 days. The `TrendStable` comment
  ("no statistically significant change", line 59) describes a test the code
  does not perform.

Where it reaches a reader: `GET /api/corridor/trend` runs
`analysis.DivergenceHistory` over the returned window (`server/trend.go`,
lines 370–379) and publishes `trend_direction` and `trend_magnitude_pct`
when the gate is met (lines 112–117). The window defaults to 100 runs and is
capped at 500 (lines 28–33). The UI renders it as "The trend is
**worsening** by … pp per run" (`server/index.html`, lines 2362–2366). The
series is reference-provider divergence, which only counts runs with a
recorded `divergence_pct` (`analysis/divergence.go`, lines 34–56). No trend
is computed for any loss series. Charting a corridor-health trend (#129 /
#10) and tracking the structural floor over time (#119) are **future**
backlog items, not built.

### 1.3 Cadence, as scheduled and as achieved

`.github/workflows/measure.yml` schedules `cron: "0 */6 * * *"` (line 83) —
four runs a day, as planned. As achieved: the GitHub Actions API lists **224
runs of `measure.yml` from 2026-08-22T18:21:52Z to 2026-10-22T20:56:27Z, and
all 224 concluded `failure`** (127 `schedule`, 97 `push`). The most recent
(run `39853127639`) failed at the step "Open or update the measurement pull
request". Issue #782 ("measure: the scheduled corridor sweep is failing") is
open. README line 779 states continuous measurement is "Not yet running — no
sweep has reached `main` since 2026-08-22".

**Sources:** GitHub REST API
`/repos/Wayfare-labs/wayfare/actions/workflows/measure.yml/runs` and
`/actions/runs/39853127639/jobs`, both read 2026-10-23; `README.md` line 779,
checked 2026-10-23.

### 1.4 The data that exists

| File | Records | `recorded_at` | Version |
|:---|---:|:---|---:|
| `data/USDC-NGNC.ndjson` | 1 | 2026-08-22T12:09:59Z | 1 |
| `data/USDC-GHSC.ndjson` | 1 | 2026-08-22T12:10:05Z | 1 |
| `data/USDC-KESC.ndjson` | 1 | 2026-08-22T12:10:09Z | 1 |
| `data/USDC-NGNT.ndjson` | 1 | 2026-10-17T23:36:46Z | 3 |

Read directly 2026-10-23. Only two commits have ever touched `data/` (the
initial import, 2026-08-22, and #770, 2026-10-18), neither from the scheduled
workflow (GitHub commits API, `path=data`, read 2026-10-23).

**Every corridor is 99 observations short of the floor proposed here and 59
short of the floor already in the code.** Nothing in this finding was tested
against real corridor data, because there is no corridor series to test.

---

## 2. The statistical reasoning

### 2.1 What a trend claim would assert

For this project a trend claim is: *across this corridor's stored runs, in
this window, the series X has moved monotonically in one direction, by more
than noise would explain.* That is a claim about a single corridor's single
series (not pooled across corridors — the 90-day spike explains why pooling
is wrong), and it is a claim about the past window, not a forecast.

The natural test for that claim, with no assumption that the change is linear
or the noise normal, is Mann–Kendall: the statistic
S = Σ_{i<j} sign(x_j − x_i), compared to its distribution under "no trend".
The natural magnitude is the Theil–Sen slope (the median of pairwise slopes),
which can be computed against `recorded_at` rather than index and so does not
pretend an irregular series is regular.

**Literature** (bibliographic details from standard references; not
re-retrieved online for this finding; noted 2026-10-23): Mann, H. B. (1945),
"Nonparametric tests against trend", *Econometrica* 13:245–259; Kendall,
M. G. (1975), *Rank Correlation Methods*, 4th ed., Griffin; Theil, H. (1950),
"A rank-invariant method of linear and polynomial regression analysis",
*Proc. KNAW* 53; Sen, P. K. (1968), "Estimates of the regression coefficient
based on Kendall's tau", *JASA* 63:1379–1389; Helsel, D. R. et al. (2020),
*Statistical Methods in Water Resources*, USGS Techniques and Methods 4-A3,
ch. 12.

### 2.2 The absolute floor: below this, no test can even reject

Under the null of exchangeable observations with no ties, every ordering of
n values is equally likely, so the most extreme S (every pair concordant) has
probability exactly 1/n!. A test can only reject at level α if 1/n! ≤ α
(one-sided) or 2/n! ≤ α (two-sided).

| α | One-sided: smallest n | Two-sided: smallest n |
|:---|:---|:---|
| 0.05 | **4** (p_min = 1/24 ≈ 0.042) | **5** (p_min = 2/120 ≈ 0.017) |
| 0.01 | 5 (p_min = 1/120 ≈ 0.0083) | 6 (p_min = 2/720 ≈ 0.0028) |

At n = 4 the smallest two-sided p is 2/24 ≈ 0.083, so **no two-sided
Mann–Kendall claim at 0.05 is possible with four or fewer observations,
however clean they look**. At n = 5 the only rejection is a perfectly
monotone sequence. Ties (two-decimal `floor_loss_pct`, repeated `0.00`
values) shrink the set of distinct orderings and raise these minimums; a
series that is constant cannot support a trend claim at any n.

**Source:** exact null distribution computed for this finding from the
inversion-count (Mahonian) recurrence, using integer arithmetic, 2026-10-23.
The script is not repository code and is not proposed for the repository.

This floor is real but useless as a publication rule: it says when a test is
*possible*, not when a result is *worth publishing*. The backlog's own
phrase — "a trend line from six observations is a decoration" (#114) — is
correct, and this table shows why: at n = 6 the test can only fire on a near-
perfect sequence and misses almost every real drift.

### 2.3 The defensible floor: enough power to see a modest drift

A floor that the project can point to has to answer "how many observations
before a real trend of a size we care about would usually be seen?" That
needs an effect size. This finding proposes, as an explicit judgment and not
a derived fact:

> **Effect size:** a total monotone drift across the window equal to one
> standard deviation of the run-to-run noise (Δ = 1σ).
> **Error rates:** two-sided α = 0.05, power 0.80.

Smaller drifts than one sigma are hard to distinguish from noise by eye or by
any test at these sample sizes; larger ones the project would want to see
sooner. The table shows how the number moves with that choice.

| Total drift Δ (in noise σ) | Mann–Kendall n for 80% power, independent noise | OLS slope t-test, normal approximation |
|:---|:---|:---|
| 2σ | ≈ 26 | 22 |
| **1σ** | **≈ 100** | 93 |
| 0.5σ | ≈ 400 | 375 |

And the power at the gate the code uses today:

| n (independent noise, Δ = 1σ) | 30 | 60 | 96 | 100 | 120 |
|:---|---:|---:|---:|---:|---:|
| Mann–Kendall power | 0.35 | 0.58 | 0.78 | 0.80 | 0.86 |

**Source:** Monte Carlo simulation written for this finding (Gaussian noise,
linear drift, two-sided α = 0.05 with the normal approximation and continuity
correction to S; 10,000 replicates near the 80% points and at n = 60, 2,000
elsewhere, 3,000 for Δ = 0.5σ; Monte Carlo standard error about 0.01 or less), run
2026-10-23. The OLS column is the analytic normal approximation with
Σ(x − x̄)² = n(n² − 1)/12. The two agree with the textbook asymptotic
relative efficiency of Kendall-type tests against least squares under
normal errors, 9/π² ≈ 0.91 (literature: Kendall 1975, above), which predicts
93 / 0.91 ≈ 102. **Literature** on Mann–Kendall power by simulation:
Yue, S., Pilon, P. and Cavadias, G. (2002), "Power of the Mann-Kendall and
Spearman's rho tests for detecting monotonic trends in hydrological series",
*J. Hydrology* 259:254–271 (noted 2026-10-23, not re-retrieved).

Two things follow for the number:

- **100 is where a one-sigma drift is first seen more often than not by a
  comfortable margin (80%).** At 60 it is a coin flip with a slight bias
  (58%). That is the case for the floor.
- **0.5σ drifts are out of reach** inside one rotation window even with
  perfectly independent data: about 400 observations are needed, and
  `MaxWindow` is 366.

### 2.4 Serial correlation: why 100 raw observations is not always 100

Consecutive six-hourly measurements of the same order book and the same
reference providers are unlikely to be independent. Positive lag-1
autocorrelation ρ has two effects:

1. **The naive test lies.** Simulated with AR(1) noise and *no trend*, the
   naive Mann–Kendall test rejects at two-sided "α = 0.05" about **14%** of
   the time at ρ = 0.3, **25%** at ρ = 0.5 and **40%** at ρ = 0.7 (n = 60 and
   n = 100 give similar rates; 2,000 replicates each; simulation for this
   finding, 2026-10-23). A trend claim from a correlated series, tested as if
   independent, is a false positive several times as often as it says.
2. **The effective sample is smaller.** To first order the variance of a mean
   — and, for large n, of a regression slope — inflates by (1 + ρ)/(1 − ρ),
   so the information in n correlated observations is about
   n_eff = n · (1 − ρ)/(1 + ρ). The 100-observation floor should be read as
   **n_eff ≥ 100**:

| Lag-1 ρ | Raw observations for n_eff = 100 | At six-hour cadence | Fits in `MaxWindow` (366)? |
|:---|---:|:---|:---|
| 0 | 100 | 25 days | yes |
| 0.3 | ≈ 186 | ≈ 46 days | yes |
| 0.5 | 300 | 75 days | yes |
| 0.57 | ≈ 365 | ≈ 91 days | at the limit |
| 0.7 | ≈ 567 | ≈ 142 days | **no** |

The OLS normal approximation gives 173, 281 and 532 for ρ = 0.3, 0.5 and 0.7
at Δ = 1σ; the table uses the simpler n_eff rule and is slightly more
conservative. Above ρ ≈ 0.57, a one-sigma trend claim at this standard
cannot be made from a single committed window at all.

**Literature** (noted 2026-10-23, not re-retrieved): Bayley, G. V. and
Hammersley, J. M. (1946), "The 'effective' number of independent
observations in an autocorrelated time series", *JRSS Supplement* 8:184–197;
Zwiers, F. W. and von Storch, H. (1995), "Taking serial correlation into
account in tests of the mean", *J. Climate* 8:336–351; Hamed, K. H. and Rao,
A. R. (1998), "A modified Mann-Kendall trend test for autocorrelated data",
*J. Hydrology* 204:182–196; Weatherhead, E. C. et al. (1998), "Factors
affecting the detection of trends", *JGR* 103(D14):17149–17161.

Estimating ρ is itself a sample-size problem: from n observations its
standard error is roughly 1/√n and the usual estimator is biased toward zero
in short series, which **understates** the correction. That is one more
reason the raw floor of 100 is stated as a minimum that holds regardless of
ρ, with correlation only ever raising it.

**What is unknown:** the actual ρ of any Wayfare series. No corridor has more
than one record (§1.4), so this cannot be estimated from the repository.

### 2.5 Irregular sampling, gaps and step changes

These do not change the number, but they decide whether a count of 100 means
what it says.

- **Gaps.** The workflow has already produced a multi-day gap (#63) and has
  since produced no records at all (§1.3). The existing slope uses index as x
  (§1.2), so a gap of a week counts the same as six hours, and "per run" is
  not "per day". Theil–Sen against `recorded_at` avoids that; a count of 100
  that spans a long gap is two short series, not one long one. Gap visibility
  is backlog #117 (**future**).
- **Missing values are not zeros.** A NO-MARKET record carries
  `floor_loss_pct: "0.00"` (`data/USDC-KESC.ndjson`, read 2026-10-23); a
  loss series that included such runs as observations would record "no
  market" as "no loss". Single-provider runs carry no `divergence_pct`, and
  the divergence series already excludes them (§1.2). Any count toward the
  floor must count only runs where the field is a genuine measurement.
- **Step changes look like trends.** A change in `scored_against` provider,
  a corridor's integrity state, or the official-rate regime
  (`docs/spike-fx-corridor-seasonality.md`) produces a level shift.
  Mann–Kendall and OLS both read a single step as a monotone trend. A window
  that spans a recorded provider or integrity change is not a trend sample.
- **Many series, many claims.** With four corridors and several series per
  corridor, about one in twenty null series would show a "trend" at 0.05 by
  chance. A family-wise correction (e.g. Bonferroni over four corridors,
  α = 0.0125) raises the independent-noise requirement at Δ = 1σ to about
  132 by the OLS normal approximation, or about 145 for Mann–Kendall after the
  9/π² efficiency adjustment (neither simulated). This finding states the
  per-series floor and notes the multiplicity; it does not choose a
  correction.

---

## 3. The recommended floor

> **A trend direction for one recorded series of one corridor is not
> published from fewer than 100 observations.** 100 is the floor under
> independence; with lag-1 autocorrelation ρ > 0 the requirement is
> 100 · (1 + ρ)/(1 − ρ). An observation counts only if the field was
> genuinely measured on that run, and the window must not span a recorded
> change of reference provider or integrity state.

Assumptions the number rests on, stated so they can be disputed:

| Assumption | Why it matters | Checked? |
|:---|:---|:---|
| Effect size of interest is a 1σ total drift | sets the 100; 2σ gives ~26, 0.5σ gives ~400 | **judgment**, not derived |
| α = 0.05 two-sided, power 0.80 | conventional; stricter values raise the floor | judgment |
| Observations are close to independent | below this, the floor scales up | **unknown** — no data |
| One series, one corridor, no pooling | pooling shared-issuer corridors fakes replication | consistent with `docs/spike-90-day-history.md` |
| Monotone (not necessarily linear) change | Mann–Kendall's alternative | method choice |

### What to say when declining

Proposed wording for a future reader that declines (**future** — no code
emits this today, and §1.2 notes that the 30–59 range currently declines
silently):

> **Trend not determined** — *N* of the 100 observations a trend claim
> requires (series: *X*; window: *first* to *last* `recorded_at`). With fewer,
> a drift as large as one standard deviation of the run-to-run noise would
> be missed more than one time in five, and any direction shown would not be
> distinguishable from noise. See docs/spike-trend-minimum-observations.md.

and, where a correlation estimate exists (**future**):

> **Trend not determined** — *N* observations, but successive runs are
> correlated (lag-1 ≈ ρ̂), so they carry the information of about *n_eff*
> independent ones; 100 are required.

---

## 4. Scope check: what adopting this would touch

- **Verdict thresholds:** no. The analysis layer is downstream of verdicts
  (`docs/regime-classification.md`, "They are not verdict thresholds",
  checked 2026-10-23).
- **Integrity semantics:** no.
- **Check composition rule:** no.
- **Run-record layout:** no. Everything above is derivable from fields
  already stored (`recorded_at`, the series value, `scored_against`,
  `integrity`). A complete missing-sweep ledger would need new data (see
  `docs/spike-90-day-history.md`), but the floor does not depend on it.
- **Published wire behaviour:** **yes.** Raising
  `analysis.MinSampleSizeForTrend` from 60 to 100, or replacing the dead-zone
  classification with a test, would change when `trend_direction` appears on
  `GET /api/corridor/trend`. That is a maintainer decision on the analysis
  layer and belongs to #114 / [#185](https://github.com/Wayfare-labs/wayfare/issues/185),
  not to this spike. Nothing is changed here.

## 5. Follow-up (not done here)

1. #185 (backlog #114): decide whether to raise `MinSampleSizeForTrend` to
   100, and whether the published direction should come from a test
   (Mann–Kendall with a correlation correction) rather than a dead zone.
2. Correct the day equivalents in the `analysis.go` comments (30 runs ≈ 7.5
   days, 60 ≈ 15 days) and the `TrendStable` comment, which describes a
   significance test the code does not run. Documentation-only.
3. Give the 30–59 range a reason string for the absent trend, so declining is
   stated rather than silent.
4. Re-examine the 1σ assumption and estimate ρ once any corridor has at least
   100 consecutive records. That depends on the measure workflow succeeding
   (§1.3, #782, #555).

## Related

- [spike-90-day-history.md](spike-90-day-history.md) — what ~360 records per corridor could support
- [spike-fx-corridor-seasonality.md](spike-fx-corridor-seasonality.md) — calendar structure, a negative finding
- [regime-classification.md](regime-classification.md) — the analysis-layer gates and why they are not verdict thresholds
- [run-store.md](run-store.md) — record layout and rotation
- [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md) — the 366-record window
