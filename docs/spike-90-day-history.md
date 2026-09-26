# Spike: what can be learned from 90 days of history

**Issue:** [#333](https://github.com/Wayfare-labs/wayfare/issues/333), backlog
[#135](backlog.md). **Checked:** 2026-09-26.
**Status:** Research finding only. No implementation or run-record change is
proposed.

## Question and sample arithmetic

The issue's planning assumption is a measurement every six hours for three
corridors. Four scheduled observations per day gives about **360 records per
corridor, or 1,080 across three corridors, in 90 days**. These are not 1,080
replicates of one market: each corridor is a separate series. Pooling them into
one sample would mix distinct markets and the corridors share issuer and
benchmark dependencies.

The current storage design keeps a rolling window of at most
`runstore.MaxWindow = 366` records **per corridor**, so an uninterrupted
90-day, six-hour series fits in today's committed window. Older entries are
re-sealed out of the current window on rotation and remain in earlier Git
history; a fresh checkout at HEAD is not a lifetime archive.

**Sources:** issue #333 description; `runstore/file.go` (`MaxWindow`, checked
2026-09-26); [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md)
(checked 2026-09-26).

## What the sample could support

The following are descriptive claims, not causal or predictive claims, and
require the corresponding field to be present and valid in each record.

| Question | At approximately 360 observations per corridor | Boundary |
|:---|:---|:---|
| How many records, and which timestamps, were actually retained? | Directly answerable from the chain. Sequence, recorded time and hash are stored per run. | A missing scheduled run leaves no record saying why it is missing. Timestamp gaps can identify a long interval, not distinguish an outage, skipped workflow or unmeasurable corridor. |
| What are the observed average and sample standard deviation of a recorded numeric series? | The existing `analysis.AnalyzeDecimal` computes these when there are at least 30 valid observations. At the nominal sample size, this threshold is attainable per corridor. | This is a summary of observed values, not a confidence interval, forecast, or evidence that the observations are independent. Missing values are not zero. |
| Is there an elementary direction-of-change summary? | The existing analysis computes a linear-regression slope after at least 60 values. A nominal 360-point series clears that software gate. | The regression's x-coordinate is the observation index, not elapsed time. Its interpretation assumes cadence is sufficiently regular; gaps or uneven intervals make “per observation” different from “per day.” The 60-point gate is an implemented minimum, not a study proving inferential validity. |
| How often did the stored integrity state or a recorded field change? | A deterministic count/transition table can be made from the recorded fields, without changing the chain schema. | The current analysis package does not turn that count into a cause, severity score or prediction. |
| Do weekdays look different in this one observed quarter? | A descriptive split is possible: a complete 13-week quarter supplies about 52 scheduled observations for each weekday per corridor. | That is only about 13 repetitions of each weekday condition, with adjacent measurements serially related. It can motivate a hypothesis, not establish stable weekday seasonality. |

The mean/standard-deviation gate is `analysis.MinSampleSizeForMeanStdDev = 30`;
the trend gate is `analysis.MinSampleSizeForTrend = 60`. For reference
provider divergence, only runs with a recorded `divergence_pct` count: a
single-provider run is missing evidence of disagreement, not a zero. The
existing divergence code applies the same sample gates after excluding those
runs.

**Sources:** `analysis/analysis.go` (thresholds and index-based regression),
`analysis/divergence.go` and `analysis/divergence_test.go` (missing divergence
is excluded; checked 2026-09-26); `runstore/runstore.go` (`Record` fields,
checked 2026-09-26).

## What it cannot support

- **A general seasonal claim from one quarter.** Ninety days contain only
  about three month-end events. That is too few repetitions to establish a
  recurring month-end effect, much less an annual seasonal pattern. Weekday
  splits have more repetitions but remain one corridor's short, serially
  observed history.
- **A forecast, probability of failure, expected slippage, or confidence
  interval.** The repository's current statistical reader computes descriptive
  statistics and a simple slope; it has no forecasting model, calibration set,
  or validation result. The sample count alone cannot supply these.
- **A causal explanation.** A change in a loss figure cannot, from this series
  alone, be attributed to the corridor, reference provider, issuer, liquidity
  venue, or an external event. The record carries some benchmark identity, but
  not every explanatory market factor or a controlled comparison.
- **An uptime or completeness rate from the chain alone.** The run store records
  successful appended measurements, not a complete ledger of scheduled
  attempts. A timestamp gap is observable; its cause and the denominator of
  expected sweeps are not recorded in the chain.
- **Evidence beyond the retained horizon from a current checkout.** Rotation
  bounds each corridor at 366 records. Recovering earlier records requires
  walking repository history, which is a different and more expensive dataset
  than `data/` at HEAD.
- **A pooled “n = 1,080” result.** The three series answer corridor-specific
  questions. Combining them would not create 1,080 interchangeable samples and
  could make shared dependencies look like independent evidence.

These limits are about the evidence and current implementation, not proof that
such questions can never be answered. A later study would need a longer or
purpose-built dataset, explicit missing-sweep accounting where completeness is
claimed, and a predeclared statistical method validated for the dependence and
sampling cadence.

## Current data is not the projected sample

At this checkout, each of the three committed files contains one record at
sequence 1, recorded on 2026-08-22 (the individual timestamps are 12:09:59,
12:10:05 and 12:10:09 UTC). That is not a 90-day time series and cannot be used
to estimate variability or change. The nominal 1,080 is therefore a planning
calculation, not an observed sample or a promise that the workflow will
successfully collect every six-hour point.

**Sources:** `data/USDC-NGNC.ndjson`, `data/USDC-GHSC.ndjson`,
`data/USDC-KESC.ndjson` (checked directly 2026-09-26); `.github/workflows/measure.yml`
and [docs/embedded-history.md](embedded-history.md) (cadence, deployment and
workflow behavior checked 2026-09-26). The committed files establish their
record counts/timestamps; they do not establish future collection success.

## Finding

**At 90 days, the useful answer is descriptive and corridor-specific:** the
chain can contain enough observations to summarize recorded values with the
existing mean, sample standard deviation and gated slope calculations, and to
tabulate recorded state changes. This is not enough to claim general
seasonality, causality, a calibrated probability, or future behavior. In
particular, the three-month-end repetitions and the absence of a scheduled-run
ledger are hard limits, not gaps that a more elaborate statistic can repair.

The current `analysis` thresholds are software abstention rules, not new
statistical evidence produced by this spike. No thresholds are changed, no
model is proposed, and no run-record field is added. This is a mildly positive
finding for descriptive summaries and a negative finding for stronger
longitudinal or predictive claims.

## Related

- [run-store.md](run-store.md) — chain format and rolling-window behavior
- [analysis package](../analysis/analysis.go) — implemented descriptive summaries and minimum counts
- [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md) — retention decision
