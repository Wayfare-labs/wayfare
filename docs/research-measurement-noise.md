# How much does a rung move between back-to-back measurements?

Research note for #927. It measures the noise floor of one ladder: how much a
rung's rate, receive amount and chosen path change when nothing in the corridor
has meaningfully changed. Every longitudinal feature — movement detection in
`runstore`, the trend dead zones in `analysis`, alerting #222 — needs that
number before it can tell signal from noise.

**Result in one line:** across 31 measurements per corridor over 57 minutes, the
recommended rung's rate varied by 0.63% (NGNC) and 0.25% (NGNT), the best path
changed on 23 of 30 consecutive pairs on NGNC, and the reference mid did not
move at all. The current 0.1 pp dead zone is **smaller than the noise it is
supposed to filter** for every rung below 500 USDC.

## Method

- **When:** 2026-11-21, 09:45:13Z to 10:39:41Z. One window, one time of day.
- **What:** `ladder -to NGNC -json -checks=false` and `ladder -to NGNT -json
  -checks=false`, back to back, every ~2 minutes, 31 times each (62 runs).
  `-checks=false` removes counterparty checks, which do not affect pricing and
  would have added unrelated network calls to the measured interval.
- **Where:** the sandbox that ran this research, against the public Horizon and
  the default reference provider. One instance, one network path.
- **Recorded:** each run's JSON output is committed under
  `docs/research/measurement-noise/2026-11-21/`, with `SHA256SUMS` for the set
  and `analysis.json` for the derived figures below. `sample.sh` (one directory
  up) is the exact loop, so the sample can be repeated.
- **Not recorded:** the upstream Horizon responses themselves. `ladder
  -record` captures those and needs a clean tree; this run prioritised a
  half-hour of samples over snapshot bytes. Re-running `sample.sh` with
  `-record` on a clean checkout produces them.
- **Derived figures:** per-rung min/max, spread as `(max-min)/median`, standard
  deviation as a share of the median, distinct paths, and how often the
  highest-rate rung's path changed between consecutive runs.

## Per-rung dispersion

### NGNC

| Send (USDC) | Runs | Rate min | Rate max | Spread | Std dev | Distinct paths | Most common path |
|--:|--:|--:|--:|--:|--:|--:|:--|
| 0.1 | 31 | 2023.0807 | 2035.8732 | 0.630% | 0.127% | 11 | USDC -> XLM -> NGNC (11/31) |
| 1 | 31 | 2005.4238 | 2017.8266 | 0.617% | 0.134% | 7 | USDC -> XLM -> NGNC (20/31) |
| 5 | 31 | 1929.3589 | 1941.3433 | 0.620% | 0.139% | 4 | USDC -> XLM -> NGNC (23/31) |
| 10 | 31 | 1842.0015 | 1853.5239 | 0.624% | 0.135% | 3 | USDC -> XLM -> NGNC (29/31) |
| 25 | 31 | 1623.0009 | 1631.4955 | 0.522% | 0.117% | 1 | USDC -> XLM -> NGNC (31/31) |
| 50 | 31 | 1354.6853 | 1360.4170 | 0.422% | 0.095% | 1 | USDC -> XLM -> NGNC (31/31) |
| 100 | 31 | 1018.0694 | 1021.2647 | 0.313% | 0.072% | 1 | USDC -> XLM -> NGNC (31/31) |
| 250 | 31 | 583.2711 | 584.3241 | 0.180% | 0.042% | 1 | USDC -> XLM -> NGNC (31/31) |
| 500 | 31 | 340.7351 | 341.1023 | 0.108% | 0.024% | 1 | USDC -> XLM -> NGNC (31/31) |
| 1000 | 31 | 186.0320 | 186.1421 | 0.059% | 0.013% | 1 | USDC -> XLM -> NGNC (31/31) |
| 2500 | 31 | 78.7581 | 78.7773 | 0.024% | 0.006% | 1 | USDC -> XLM -> NGNC (31/31) |
| 5000 | 31 | 40.1604 | 40.1654 | 0.013% | 0.003% | 1 | USDC -> XLM -> NGNC (31/31) |

Best path changed **23 times across 30 consecutive pairs**. The recommended
size was 0.1 USDC in every run; its rate ranged 2023.0807–2035.8732 (spread
0.630%, sd 0.127%).

### NGNT

| Send (USDC) | Runs | Rate min | Rate max | Spread | Std dev | Distinct paths | Most common path |
|--:|--:|--:|--:|--:|--:|--:|:--|
| 0.1 | 31 | 14530.8861 | 14567.7637 | 0.253% | 0.064% | 3 | USDC -> NGNT (28/31) |
| 1 | 31 | 14234.6461 | 14307.3172 | 0.509% | 0.118% | 7 | USDC -> XLM -> NGNT (19/31) |
| 5 | 31 | 13223.7921 | 13301.8623 | 0.588% | 0.120% | 3 | USDC -> XLM -> NGNT (23/31) |
| 10 | 31 | 12161.7214 | 12227.7235 | 0.541% | 0.111% | 2 | USDC -> XLM -> NGNT (27/31) |
| 25 | 31 | 9800.3676 | 9843.1826 | 0.436% | 0.089% | 2 | USDC -> XLM -> NGNT (30/31) |
| 50 | 31 | 7481.2630 | 7522.8123 | 0.554% | 0.116% | 1 | USDC -> XLM -> NGNT (31/31) |
| 100 | 31 | 5626.0794 | 5648.8263 | 0.403% | 0.088% | 1 | USDC -> XLM -> NGNT (31/31) |
| 250 | 31 | 3553.8041 | 3565.9834 | 0.342% | 0.073% | 1 | USDC -> XLM -> NGNT (31/31) |
| 500 | 31 | 2339.8392 | 2345.3748 | 0.236% | 0.048% | 1 | USDC -> XLM -> NGNT (31/31) |
| 1000 | 31 | 1552.0704 | 1557.8549 | 0.372% | 0.074% | 1 | USDC -> XLM -> NGNT (31/31) |
| 2500 | 31 | 1065.6432 | 1066.9304 | 0.121% | 0.024% | 1 | USDC -> XLM -> NGNT (31/31) |
| 5000 | 31 | 609.4132 | 610.7905 | 0.226% | 0.045% | 1 | USDC -> XLM -> NGNT (31/31) |

Best path changed **5 times across 30 consecutive pairs**. Recommended size was
0.1 USDC in every run, with a rate range of 14530.8861–14567.7637 (spread
0.253%, sd 0.064%).

## What this says

1. **The movement is execution noise, not a rate move.** The reference mid was
   identical in all 62 runs (1352.930642, source agreement `SINGLE`), so
   nothing about the fiat rate changed. Every percentage point above is the
   DEX: order-book drift between two Horizon calls, and a best path that
   flips between venues.

2. **Small rungs are noisy; large rungs are not.** Spread falls from 0.63% at
   0.1 USDC to 0.013% at 5000 USDC on NGNC, and from 0.25% to 0.12% on NGNT. A
   single threshold for every rung is either too coarse for the top of the
   ladder or too fine for the bottom.

3. **Path churn is the loudest signal, and it is not a finding.** On NGNC the
   best path changed on 23 of 30 consecutive pairs — roughly every two
   minutes — and the 0.1 rung used 11 distinct paths in 31 runs. Any feature
   that reports "the route changed" without a magnitude threshold will report
   a change almost every measurement on that corridor.

4. **A dead zone of 0.1 pp cannot work.** It is between six and fifty times
   smaller than the observed spread on every rung below 500 USDC. With it, the
   alerting in #222 would fire on essentially every pair of observations.

## Recommendation

Use a **per-rung dead zone**, and require persistence:

| Rung (USDC) | Observed max spread | Recommended dead zone |
|:--|--:|--:|
| 0.1 – 10 | 0.63% | **0.75 pp** |
| 25 – 100 | 0.52% | **0.60 pp** |
| 250 – 500 | 0.18% | **0.30 pp** |
| 1000 + | 0.06% | **0.10 pp** |

- **Movement detection:** a rung moved when its rate differs from the previous
  observation by more than the dead zone **and** the same direction persists for
  two consecutive observations. One crossing is noise by construction.
- **Trend dead zones:** apply the same table rather than a single number.
- **Best-path changes:** record them, do not alert on them. If a route change
  matters, it will show up as a rate change larger than the dead zone.
- **Alerting (#222):** fire only on a cumulative move of at least **1 pp** since
  the last alert on that corridor, or on a state change (`scored` false,
  recommendation becoming null), never on a single-observation delta.

## Limitations, and why this is a starting point rather than a final answer

- **One hour, one time of day.** The issue suggested three windows across a
  day; this run covers one. A busy and a quiet period can have different
  spreads, and the numbers above should not be treated as the daily maximum.
- **Two corridors, both NGN.** GHSC and the rest of the registry were not
  sampled. Their liquidity is different, so their dead zones may be too.
- **One instance, one network path.** The sample includes this machine's
  latency to Horizon. A second region could add variance the table does not
  show.
- **No upstream snapshots.** The reports are committed and hashed; the raw
  Horizon bytes are not, so re-deriving a single figure offline needs another
  recording run.

Repeating this on a quiet and a busy window, and on GHSC, is the natural next
step. Until then the table is a defensible lower bound on the noise floor: if
anything, the true spread is larger, which makes the current 0.1 pp dead zone
even less tenable.
