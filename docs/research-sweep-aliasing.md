# Research: do six-hourly sweeps alias daily patterns?

Research note for issue #686. It analyses whether the fixed cron
`0 */6 * * *` (00, 06, 12, 18 UTC) systematically misses or overweights
intraday liquidity patterns in the corridors this project measures, and
proposes a sampling design that avoids the aliasing trap.

**Result in one line:** off-schedule measurements at 03, 09, 15, 21 UTC
show the same structural behaviour as on-schedule hours for the NGNC and
NGNT corridors — the dominant variance is measurement noise (#927, spread
~0.6%), not time-of-day — but the fixed four-phase design remains a
theoretical aliasing trap for any daily cycle with a period of exactly 6 h,
and a jittered schedule is recommended before longitudinal trend detection
(#336, #337) is introduced.

---

## Background: why fixed-phase sampling is a problem

A sweep that fires at the same four UTC times every day samples a 24-hour
cycle at exactly 4 equidistant points. If corridor liquidity has a
dominant daily oscillation with period near 6 hours, the four measurement
points will alias to a constant — the oscillation will appear to have
disappeared. Shannon's theorem requires sampling faster than twice the
highest frequency of interest; for a 6-hour cycle, four samples per day
is exactly at the Nyquist limit, not above it.

For the corridors this project measures (NGNC, NGNT, GHSC), the liquidity
providers operate in West Africa (UTC+1) and their active hours overlap
the morning sessions in London and New York. A 6-hour oscillation centred
on activity windows is plausible.

---

## Method

**Scheduled measurements** were taken as part of the normal sweep at
00, 06, 12 and 18 UTC on **2026-11-20** (four probes per corridor).

**Off-schedule measurements** were taken manually at 03, 09, 15 and 21 UTC
on **2026-11-20** using `go run ./cmd/ladder -to NGNC -json -checks=false`
and `go run ./cmd/ladder -to NGNT -json -checks=false` (four probes per
corridor, interleaved with the schedule).

Each measurement records the best path and the effective rate at the
recommended size (0.1 USDC per the measurement-noise research in
`docs/research-measurement-noise.md`).

---

## Measurements: USDC → NGNC, 0.1 USDC, 2026-11-20

| UTC time | Type | Rate (NGNC/USDC) | Best path | Note |
|---:|:---|---:|:---|:---|
| 00:02 | scheduled | 2,027.4821 | USDC → XLM → NGNC | |
| 03:08 | off-schedule | 2,029.1033 | USDC → XLM → NGNC | |
| 06:03 | scheduled | 2,026.3415 | USDC → XLM → NGNC | |
| 09:11 | off-schedule | 2,031.8762 | USDC → XLM → NGNC | |
| 12:04 | scheduled | 2,023.5190 | USDC → XLM → NGNC | |
| 15:09 | off-schedule | 2,025.0048 | USDC → XLM → NGNC | |
| 18:02 | scheduled | 2,028.7034 | USDC → XLM → NGNC | |
| 21:14 | off-schedule | 2,030.2219 | USDC → XLM → NGNC | |

Range across all 8 measurements: 2,023.52–2,031.88 (spread 0.41%).

---

## Measurements: USDC → NGNT, 0.1 USDC, 2026-11-20

| UTC time | Type | Rate (NGNT/USDC) | Best path | Note |
|---:|:---|---:|:---|:---|
| 00:02 | scheduled | 14,541.3302 | USDC → NGNT | |
| 03:08 | off-schedule | 14,548.0017 | USDC → NGNT | |
| 06:03 | scheduled | 14,536.8841 | USDC → NGNT | |
| 09:11 | off-schedule | 14,552.3119 | USDC → NGNT | |
| 12:04 | scheduled | 14,530.1047 | USDC → NGNT | |
| 15:09 | off-schedule | 14,544.9203 | USDC → NGNT | |
| 18:02 | scheduled | 14,538.7761 | USDC → NGNT | |
| 21:14 | off-schedule | 14,549.1583 | USDC → NGNT | |

Range across all 8 measurements: 14,530.10–14,552.31 (spread 0.15%).

---

## Analysis

**No consistent intraday pattern was detected.** The rates in both corridors
at off-schedule times fall within the same range as on-schedule times. The
variation (0.41% for NGNC, 0.15% for NGNT) is consistent with the noise
floor measured in `docs/research-measurement-noise.md` (0.63% for NGNC,
0.25% for NGNT over 57 minutes), which establishes that the within-day
variation seen here is dominated by noise, not a time-of-day signal.

**The absence of a detectable signal does not make aliasing safe.** It says
only that this day, these corridors, did not exhibit a dominant 6-hour
oscillation. Future corridors (GHS, KES), liquidity changes, or a
shift from one anchor to multiple anchors can introduce patterns that the
current fixed schedule would alias.

**Where aliasing risk is highest:** the moment longitudinal trend detection
is introduced (#336, #337), the sampling schedule becomes load-bearing for
correct trend attribution. A trend or regime change that coincides with the
swap of a liquidity provider will show up differently depending on whether
the detection window averages across times of day or is frozen to the same
four UTC hours. Fixing the design now is cheaper than correcting a
published trend that turns out to be a measurement artefact.

---

## Recommended schedule

Replace the fixed cron `0 */6 * * *` with a **rotating-offset** schedule
that shifts the measurement window by 1.5 hours each day, covering all 24
hours over a 16-day cycle:

| Day of cycle | UTC times |
|---:|:---|
| 0 | 00:00, 06:00, 12:00, 18:00 |
| 1 | 01:30, 07:30, 13:30, 19:30 |
| 2 | 03:00, 09:00, 15:00, 21:00 |
| 3 | 04:30, 10:30, 16:30, 22:30 |
| …repeats | |

An implementation that is compatible with the existing cron-based scheduler
can approximate this by deploying four cron entries with offsets, rotating
the offset group weekly using a date-based modulo.

Alternatively, add **±15 min jitter** to the existing four daily sweeps.
Jitter prevents aliasing of an exact 6-hour signal and is easier to
implement in the existing scheduler. It trades per-day completeness for
overall coverage and is the simpler option before longitudinal features ship.

**Minimum recommendation before #336 ships:** add per-sweep ±15 min jitter.
The rotating-offset schedule is the cleaner long-term design.
