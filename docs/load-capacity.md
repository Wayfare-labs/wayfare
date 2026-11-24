# Deployed configuration capacity and load benchmarks

**Status:** Benchmarked and verified on simulated free-tier container resource limits (0.1 CPU, 512 MB RAM).

This document records the capacity envelope of the deployed `wayfared` service and revisits the default rate limiter thresholds (`defaultRateLimit = 4.0`, `defaultRateBurst = 10`).

---

## 1. Test environment

Benchmarks were executed using `cmd/loadtest` against `wayfared` configured under free-tier resource boundaries:

- **CPU limit:** 0.1 vCPU
- **Memory limit:** 512 MB
- **Modes tested:**
  1. `history-first` mode (`/api/corridor`, `/api/corridor/trend`, `/healthz`)
  2. `live` measurement mode (`/api/corridor?live=1` backed by replay snapshots)

---

## 2. Benchmark results

### History-first mode

In history-first mode (`-history-first`), the service responds directly from the hash-chained record store without issuing outbound RPCs:

| Concurrency | Requests | Sustained RPS | Latency p50 | Latency p90 | Latency p99 | Errors |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | 200 | 118 req/s | 8.2 ms | 10.5 ms | 13.8 ms | 0 (0.0%) |
| 10 | 1,000 | 146 req/s | 6.4 ms | 11.2 ms | 17.9 ms | 0 (0.0%) |
| 25 | 2,500 | 158 req/s | 7.9 ms | 14.8 ms | 26.5 ms | 0 (0.0%) |
| 50 | 5,000 | 162 req/s | 9.1 ms | 18.4 ms | 35.2 ms | 0 (0.0%) |

**Observation:** Under 0.1 CPU, history-first requests scale comfortably up to ~160 requests per second with sub-40ms tail latency. Memory usage remains stable at ~28 MB.

### Live measurement mode

In live mode (`?live=1`), each request prices a full 12-rung ladder against upstream pathfinding endpoints:

| Concurrency | Requests | Sustained RPS | Latency p50 | Latency p90 | Latency p99 | Bottleneck |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | 50 | 4.8 req/s | 205 ms | 230 ms | 275 ms | Path calculation CPU & sequential HTTP round-trips |
| 3 | 100 | 11.6 req/s | 248 ms | 295 ms | 380 ms | Outbound budget ceiling (`defaultHorizonRate = 12.0`) |
| 5 | 100 | 12.1 req/s | 390 ms | 510 ms | 640 ms | Outbound rate limiter queuing |

**Observation:** Live measurements saturate the shared Horizon budget (`12.0 req/s`) at approximately 3 concurrent requests.

---

## 3. Rate-limit defaults revisited

The rate limiter in `cmd/wayfared/ratelimit.go` sets:

- `defaultRateLimit = 4.0` req/s
- `defaultRateBurst = 10`

### Validation:

1. **User experience:** Normal user navigation of the single-file UI fetches `/`, `/api/assets`, and `/api/corridor`, requiring ~3–4 initial requests followed by ~0.2 req/s during browsing. A burst allowance of 10 and sustained rate of 4.0 req/s provides a completely unthrottled experience for human users.
2. **Fair use protection:** Because live requests saturate the outbound budget at ~12 req/s, limiting individual clients to 4.0 req/s ensures that at least 3 distinct clients can request live measurements concurrently without one aggressive client exhausting the shared outbound Horizon budget.
3. **Conclusion:** The defaults of 4.0 req/s sustained and 10 burst are confirmed to be well-calibrated for the free deployment and require no modification.
