# Timing a full live ladder against the server timeout — Issue #261

**Target:** local `wayfared` (live mainnet) and the deployed free instance
https://wayfare-cdb9.onrender.com/
**Tested by:** goodness-cpu, branch `full-live-ladder`, revision `81da004`
**Timestamps:** 2026-09-24T05:13:25Z – 2026-09-24T05:27:51Z (all UTC)
**Endpoints:** `GET /api/corridor?to={NGNC,GHSC,KESC}&live=1`,
`https://horizon.stellar.org/paths/strict-send`

## What the issue asks

`Server.timeout()` defaults to 90s and a ladder is a dozen Horizon round trips;
whether that holds on a free instance is untested.

## Where the 90s comes from

Checked in the tree at `81da004`:

- `server/api.go:72-77` — `func (s *Server) timeout()` returns `s.Timeout` when
  set and `90 * time.Second` otherwise. The context for the whole measurement
  is built from it at `server/api.go:156`.
- `cmd/wayfared/main.go` — `-timeout` also defaults to `90s`.
- `server/api.go:51-53` — the field's own doc comment says "A full ladder is a
  dozen round trips to Horizon, so this is generous by HTTP standards."

That last comment is the claim under test, and it is the one figure this run
contradicts. Measured below: **28** Horizon round trips per corridor, not twelve.

## How the ladder spends its round trips

`route.Engine.Ladder` prices `route.DefaultSizes` — twelve rungs — each through
`Engine.Quote`. `quoteDEX` (`route/route.go:591`) makes one `/paths/strict-send`
call per rung, and then, **for every rung above the slippage-probe threshold
(10, `route/route.go:649-658`)**, calls `DEX.MeasureSlippage`, which itself
prices the full amount and a small probe — two more requests
(`dex/dex.go:296-309`).

That predicts `12 + 2×(number of rungs above 10)`. The default ladder has eight
rungs above 10, so 28 — which is what a counting proxy in front of Horizon
observed, exactly, on both market corridors. The NO-MARKET corridor
short-circuits at `route/route.go:597` before the probe, so it makes 12.

## Method

Repeatable; no repository state is required beyond a built binary.

1. Build: `go build -o /tmp/ladder ./cmd/ladder && go build -o /tmp/wayfared ./cmd/wayfared`
2. **Wall time, direct.** For each corridor, `time /tmp/ladder -to <CODE> -json`.
3. **Wall time + round trips, through the server.** Put a counting reverse proxy
   in front of `https://horizon.stellar.org` and start the server against it, so
   every Horizon call is observed rather than inferred:
   ```bash
   /tmp/wayfared -addr 127.0.0.1:8094 -horizon http://127.0.0.1:9113 -schedule=0
   curl -s -o /dev/null -w "%{http_code} %{time_total}s\n" \
     "http://127.0.0.1:8094/api/corridor?to=NGNC&live=1"
   ```
4. **Wall time, deployed.** `curl` the free instance with `live=1` so the
   response is measured rather than served from embedded history.
5. For attribution, `curl -w "%{http_code} %{time_total}s"` records the endpoint
   and the time; the counting proxy attributes the calls.

## Results

All figures are wall-clock from the requesting host, warm, live mainnet.

### A. `cmd/ladder`, direct (no server, no timeout applied)

| Corridor | Timestamp (UTC) | Wall | Rungs priced | Exit |
|:---|:---|---:|---:|---:|
| NGNC | 2026-09-24T05:27:27Z | 2.821s | 12 | 0 |
| GHSC | 2026-09-24T05:27:30Z | 3.933s | 12 | 1 |
| KESC | 2026-09-24T05:27:33Z | 1.999s | 0 | 1 |

### B. `wayfared`, live mainnet, 90s default timeout, Horizon calls counted

| Corridor | Timestamp (UTC) | HTTP | Wall | Horizon calls |
|:---|:---|:---|---:|---:|
| NGNC (DIRECT) | 2026-09-24T05:27:36Z | 200 | 1.818s | **28** |
| GHSC (DERIVATIVE) | 2026-09-24T05:27:38Z | 200 | 1.558s | **28** |
| KESC (NO-MARKET) | 2026-09-24T05:27:40Z | 200 | 0.866s | **12** |

Repeated run at 05:14:21Z–05:14:28Z (server's own structured log, `duration=`
field) agreed: NGNC `2.958s`, GHSC `2.522s`, KESC `2.038s`.

### C. Deployed free instance (`wayfare-cdb9.onrender.com`), warm

| Request | Timestamp (UTC) | HTTP | Wall |
|:---|:---|:---|---:|
| `GET /healthz` | 2026-09-24T05:27:42Z | 200 | 0.102s |
| `GET /api/corridor?to=NGNC&live=1` | 2026-09-24T05:27:42Z | 200 | 3.558s |
| `GET /api/corridor?to=GHSC&live=1` | 2026-09-24T05:27:45Z | 200 | 3.112s |
| `GET /api/corridor?to=KESC&live=1` | 2026-09-24T05:27:49Z | 200 | 2.279s |

## Findings

### 1. The timeout holds, with roughly 25× headroom ✅

The slowest single ladder observed anywhere in this run was **3.933s**
(`cmd/ladder`, GHSC, direct) and the slowest deployed ladder was **3.558s**.
Against the 90s default that is **3.7% of the budget** — the ladder would have
to be about 25× slower before the context deadline dropped it. No rung errored
and no request returned 504 in any of the runs above.

### 2. Contradiction: a ladder is 28 Horizon round trips, not "a dozen"

The comment at `server/api.go:51-53` and the issue text both say a dozen. The
count is **28** on both priced corridors. The mechanism is not in doubt and the
comment's *reasoning* — that 90s is generous — survives; the number does not.

The decomposition (28 = 12 rungs + 2 per probe on the 8 rungs above 10) is the
more useful statement than either figure, because it changes when the ladder
changes. `docs/ladder-sizes.md` fixes the ladder at 12 rungs, so today the rule
is "12, plus double for every rung above the probe threshold."

### 3. Contradiction: `docs/deployment.md` overstates the per-corridor load

`docs/deployment.md:191-193` says "roughly three dozen Horizon calls per
corridor — twelve sizes, each with pathfinding plus a slippage probe — across
three corridors, so about 110 requests every six hours."

- The *mechanism* is right: pathfinding plus a slippage probe.
- The *count* is overstated. Measured across the three corridors: **68 Horizon
  calls** (28 + 28 + 12), not ~108. Adding the ~4 per-corridor counterparty
  requests (issuer `stellar.toml`, SEP-24 `/info`, SEP-12 `/auth`, Horizon
  `/accounts`) and ~2 reference-provider calls puts a sweep near **80**, not 110.

The doc's conclusion ("negligible for Horizon at this cadence") is unaffected;
only its arithmetic is wrong.

### 4. Not reproduced: cold start

The instance was warm for every request above (`/healthz` answered in 0.102s on
the first call), so **this run does not bound the cold-start case**. The
existing note records a first request that failed at connection on 2026-08-28
and a second that succeeded in 0.6s
([`docs/cold-start-reliability.md`](../../cold-start-reliability.md)), and the
prior smoke test recorded a ~58s cold start
([`257-production-smoke-test.md`](257-production-smoke-test.md), 2026-09-23).

Even taking the pessimistic 58s cold start from that artifact and adding the
slowest ladder measured here (3.6s), the total stays inside 90s — but that is
an arithmetic combination of two differently-sourced figures, not a
measurement, and it is labelled as such rather than presented as a result.
Forcing the sleep would have taken 15 idle minutes, which this run did not do.

### 5. Side observation, not a finding: the NGNC curve has moved

The live NGNC ladder priced five rungs GOOD and one FAIR at 2026-09-24T05:27Z,
with `floor_loss_pct: "0.00"`. `docs/corridor-measurements.md` records all
twelve rungs UNUSABLE at 2026-08-08. That discrepancy is already recorded
against the case study by the earlier smoke test (line 3 of its "Key
Findings"), so it is repeated here only for chronology and is **not** claimed
as a new finding.

## Procedures not repeatable here, and why

- **Cold start** needs a 15-minute idle window on the free plan plus access
  that does not itself keep the instance awake. Not done.
- **A per-rung latency distribution** would need many repetitions; each ladder
  above is a single sample, so the figures are indicative of magnitude, not a
  distribution.

## Anything broken

No functional breakage was found: every live request returned 200 with a full
ladder inside the timeout. The two documentation discrepancies in findings 2
and 3 are filed separately with their reproduction steps, as the issue
requires: **[#481](https://github.com/Wayfare-labs/wayfare/issues/481)** (filed
2026-09-24). This artifact changes no code, no threshold, no integrity
semantics, no check composition and no run-record layout.

## Reproducing

```bash
go build -o /tmp/ladder ./cmd/ladder
go build -o /tmp/wayfared ./cmd/wayfared

# A. direct
for c in NGNC GHSC KESC; do time /tmp/ladder -to $c; done

# B. through the server (counts Horizon calls with any proxy in front)
/tmp/wayfared -addr 127.0.0.1:8094 -schedule=0 &
for c in NGNC GHSC KESC; do
  curl -s -o /dev/null -w "$c %{http_code} %{time_total}s\n" \
    "http://127.0.0.1:8094/api/corridor?to=$c&live=1"
done

# C. deployed (retry once if the free instance is asleep)
for c in NGNC GHSC KESC; do
  curl -s -o /dev/null -w "$c %{http_code} %{time_total}s\n" \
    "https://wayfare-cdb9.onrender.com/api/corridor?to=$c&live=1"
done
```

Figures will differ from those above; the shape of the result — seconds, not
tens of seconds, and well inside 90 — should not.

## Timestamps and endpoints

| Item | Timestamp (UTC) | Endpoint |
|:---|:---|:---|
| `Server.timeout()` default read | 2026-09-24T05:12Z | `server/api.go:72-77` |
| `cmd/ladder` NGNC / GHSC / KESC | 05:27:27 / 05:27:30 / 05:27:33 | local bin, `horizon.stellar.org` |
| Server NGNC / GHSC / KESC (live) | 05:27:36 / 05:27:38 / 05:27:40 | `GET /api/corridor?to=…&live=1` |
| Horizon call counts | 05:27:36 – 05:27:40 | `horizon.stellar.org/paths/strict-send` |
| Deployed healthz | 05:27:42 | `https://wayfare-cdb9.onrender.com/healthz` |
| Deployed NGNC / GHSC / KESC | 05:27:42 / 05:27:45 / 05:27:49 | `GET /api/corridor?to=…&live=1` |
