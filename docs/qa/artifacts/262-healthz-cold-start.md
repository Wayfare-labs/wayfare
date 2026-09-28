# Verify /healthz behaviour during a cold start — Issue #262

**Target:** https://wayfare-cdb9.onrender.com/ (Render free instance)
**Tested by:** chiprime, branch `verify-claims`
**Timestamp:** 2026-09-26T01:46:15Z – 2026-09-26T02:16:54Z (all UTC; see the
per-request table)
**Endpoint:** `GET /healthz` (issued four times per cycle, first in the wake
timeline). Raw cycles in
[`../api/results/cold-start.json`](../api/results/cold-start.json)

## What the issue asks

Verify `/healthz` behaviour during a cold start — specifically, what the health
check sees while the instance is waking.

## Why this is asked

The Render health check polls `/healthz`, and the earlier smoke test
([`257-production-smoke-test.md`](257-production-smoke-test.md)) observed that
`/healthz` answers `ok` even though the stored chain is weeks stale. If
`/healthz` is the liveness probe, the question is whether it stays writable
while the process is coming up, and whether it tells a consumer anything about
freshness.

## Method (repeatable)

```bash
# /healthz is the first request in the timeline, before any other traffic.
node docs/qa/api/run-cold-start.mjs --idle=900 --cycles=3
```

The shared cold-start script idles the instance for fifteen minutes, then issues
`GET /healthz` first, twice more, `GET /`, `GET /api/corridor?to=NGNC`, and
`GET /healthz` again. Its first request is `/healthz` precisely so the wake
observation is the health check's, not some other endpoint's.

## Result: /healthz never failed and never degraded during the wake

Across three idle windows, all twelve `/healthz` requests returned **HTTP 200**
with a well-formed JSON body. The first request of each cycle was slow (~13 s)
because it *is* the wake; every later one was fast.

| Cycle | `/healthz` #1 (wake) | #2 | #3 | #4 | Body |
|:---:|:---:|:---:|:---:|:---:|:---|
| 1 | 12323 ms | 31 ms | 35 ms | 63 ms | `{"data":{…},"status":"ok"}` |
| 2 | 13373 ms | 27 ms | 27 ms | 27 ms | `{"data":{…},"status":"ok"}` |
| 3 | 13248 ms | 34 ms | 29 ms | 27 ms | `{"data":{…},"status":"ok"}` |

- **No cold-start failure was observed.** There is no window in which the
  process is up enough to answer but reports unhealthy; the probe simply waits
  ~13 s and then gets a normal `200 ok`.
- **No `5xx`, empty body or partial JSON** appeared at any point.
- **The wake latency is the only cold-start signal.** `/healthz` response time,
  not its body, is what changes during a cold start.

## What `/healthz` says while waking: status ok, data 34 days old

The cycle-1 wake response at 2026-09-26T01:46:15Z was:

```json
{"data":{"USDC-GHSC":{"recorded_at":"2026-08-22T12:10:05Z","age_seconds":2986582,"age_human":"34d ago"},"USDC-KESC":{"recorded_at":"2026-08-22T12:10:09Z","age_seconds":2986578,"age_human":"34d ago"},"USDC-NGNC":{"recorded_at":"2026-08-22T12:09:59Z","age_seconds":2986588,"age_human":"34d ago"}},"status":"ok"}
```

So while waking, the health check reports `status: "ok"` and, in the same body,
that every stored corridor is **34 days old**. This reproduces the
`257-production-smoke-test.md` observation (32 days old there) and confirms it
is not a cold-start artefact: the `data` block is present warm and cold, and it
is the only place the staleness surfaces.

`docs/api.md` documents this endpoint's body as `{ "status": "ok" }` and does
**not** mention the `data` block at all. That contradiction is the finding of
[#260](260-api-consumer-qa.md) (Finding 3, undocumented behaviour); it is
repeated here only because this check met it during a cold start.

## What this does and does not establish

- **Does:** on 2026-09-26, `/healthz` answered `200 ok` before, during and after
  the wake of three idle cycles; it exposed the stale `data` block throughout;
  the wake cost ~12–13 s of latency and nothing else.
- **Does not:** exercise the Render health-check timeout (the script is a
  client, not Render's prober), and cannot say whether Render considered the ~13 s
  wake acceptable. It also cannot say whether `/healthz` is a *good* liveness
  signal — only what it returns.

## Anything broken

Nothing broken by this check, and nothing fixed here. The two observations it
carries forward — `/healthz` serving an undocumented `data` block, and
`/healthz` reporting `ok` independently of data freshness — are already on
record under [#260](260-api-consumer-qa.md) and
[#257](257-production-smoke-test.md); no new issue was warranted.

## Timestamps and endpoints

| Item | Timestamp (UTC) | Endpoint |
|:---|:---|:---|
| Cycle 1 wake `/healthz` → 200, 12323 ms | 2026-09-26T01:46:15Z | `GET /healthz` |
| Cycle 1 post-wake `/healthz` ×3 | 2026-09-26T01:46:27Z | `GET /healthz` |
| Cycle 2 wake `/healthz` → 200, 13373 ms | 2026-09-26T02:01:27Z | `GET /healthz` |
| Cycle 3 wake `/healthz` → 200, 13248 ms | 2026-09-26T02:16:41Z | `GET /healthz` |
| Full recorded set | 2026-09-26T02:16:54Z | `docs/qa/api/results/cold-start.json` |
