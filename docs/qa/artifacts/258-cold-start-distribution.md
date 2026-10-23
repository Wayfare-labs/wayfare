# Record the cold-start behaviour properly — Issue #258

**Target:** https://wayfare-cdb9.onrender.com/ (Render free instance)
**Tested by:** chiprime, branch `verify-claims`
**Timestamp:** 2026-09-26T01:31:15Z (run start) – 2026-09-26T02:16:54Z (run end);
each cycle's first request is listed below. All times UTC.
**Endpoints:** `GET /healthz` (first request of every cycle), then `GET /`,
`GET /api/corridor?to=NGNC`; raw cycles in
[`../api/results/cold-start.json`](../api/results/cold-start.json)

## What the issue asks

Record the cold-start behaviour properly — more than the single anecdote the
backlog started from.

## Why a distribution and not one sample

The Render free plan sleeps an otherwise-idle instance after fifteen minutes
without traffic. A single "time curl" measures one wake and cannot separate a
real wake from a warm instance. This run idles the instance for fifteen minutes
**three times** and records a fixed request timeline after each idle window, so
the number reported is a distribution over three independent wake attempts, not
one observation.

## Method (repeatable)

```bash
# Idles 900s, then issues a fixed six-request timeline; repeats 3 cycles.
node docs/qa/api/run-cold-start.mjs --idle=900 --cycles=3
```

The script records each request's status, wall time, start timestamp and raw
body after every cycle, so an interrupted run still leaves what it saw behind.
`run-cold-start.mjs` is shared with [#262](262-healthz-cold-start.md); the same
result file backs both artifacts.

**Do not** call the deployment from anything else while the script is idling,
or the instance will not sleep and the cycle records a warm instance instead.

## Result: three wake attempts, three slow first requests

Every cycle's **first** request was `GET /healthz`. In all three cycles it
returned `200`, but only after roughly thirteen seconds; every request after it
answered in tens of milliseconds.

| Cycle | Idle wait | First request at (UTC) | `/healthz` #1 | Subsequent requests | Cold start seen? |
|:---:|:---:|:---|:---:|:---|:---:|
| 1 | 900s | 2026-09-26T01:46:15.191Z | **12323 ms** | 31, 31, 35, 30, 63 ms | yes |
| 2 | 900s | 2026-09-26T02:01:27.711Z | **13373 ms** | 27, 37, 27, 37, 27 ms | yes |
| 3 | 900s | 2026-09-26T02:16:41.243Z | **13248 ms** | 34, 78, 29, 28, 27 ms | yes |

Summary over the three samples:

| Statistic | Value |
|:---|:---|
| Samples (cycles) | 3 |
| Cycles that failed at the connection stage | 0 |
| Cycles whose first request took > 2 s | 3 / 3 |
| First-request latency, min / median / max | 12323 ms / 13248 ms / 13373 ms |
| First-request latency, mean | ≈12981 ms |
| Cold-request latency remainder of the cycle | 27 ms – 78 ms |
| `GET /` (third request, always after `/healthz` warmed) | 31, 37, 78 ms |

**Confirmed, with a caveat on sample size.** On this deployment a cold start is
real and repeatable: fifteen minutes of silence reliably costs the *next* caller
~13 seconds on the first request. It is not a connection failure and not a
timeout — the request simply takes ~13 s and then the instance is warm.

## Contradiction with the earlier record

The previous single-sample check
([`257-production-smoke-test.md`](257-production-smoke-test.md), 2026-09-23) timed
the first request at **~58 s** (`real 0m58.342s`) and described the first
request as *failing at the connection stage*. This three-cycle measurement
observes neither: no cycle failed, and the first request completed in
**12–13 s**, roughly a quarter of the earlier figure.

The two are not necessarily incompatible — different request (`/` vs
`/healthz`), different instance state, and a one-sample measurement has no
spread to compare against — but the earlier figure should not be quoted as the
cold-start cost. The observed distribution is the one to use. This is recorded
as a contradiction, not fixed here.

## What this does and does not establish

- **Does:** on 2026-09-26, three 15-minute idle windows each produced a ~12–13 s
  first-request latency followed by fast responses; no cycle failed.
- **Does not:** establish a full statistical distribution. Three cycles are
  three samples, not thirty. A single cycle whose first request answers
  instantly would be recorded as "no cold start observed" and is **not** treated
  as a pass; no such cycle occurred here.

## Anything broken

Nothing new. No verdict threshold, integrity semantics, check composition or
run-record layout was touched; this artifact only records a measurement.

## Timestamps and endpoints

| Item | Timestamp (UTC) | Endpoint / command |
|:---|:---|:---|
| Run start | 2026-09-26T01:31:15Z | `node docs/qa/api/run-cold-start.mjs --idle=900 --cycles=3` |
| Cycle 1 first request | 2026-09-26T01:46:15Z | `GET /healthz` |
| Cycle 2 first request | 2026-09-26T02:01:27Z | `GET /healthz` |
| Cycle 3 first request | 2026-09-26T02:16:41Z | `GET /healthz` |
| Run finished | 2026-09-26T02:16:54Z | — |
| Full recorded set | 2026-09-26T02:16:54Z | `docs/qa/api/results/cold-start.json` |
