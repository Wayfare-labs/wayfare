# Production Smoke-Test Checklist — Issue #257

**Target:** https://wayfare-cdb9.onrender.com/
**Tested by:** opencode agent on branch `qa/issues-257-263-264-265`
**Timestamp:** 2026-09-23T17:23:52Z (first request), 2026-09-23T17:24:07Z (final request)
**Deployment age at test time:** 32 days (last recorded run 2026-08-22T12:10:05Z)

## Procedure

Repeatable after every deploy. Run these commands in order. Replace `BASE` with the deployment URL.

```bash
BASE=https://wayfare-cdb9.onrender.com

# 1. Cold start: first request after deploy
time curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/" -o /dev/null

# 2. Healthz
curl -s "${BASE}/healthz" | python3 -m json.tool

# 3. Each corridor (live measurement)
for CORR in NGNC GHSC KESC; do
  curl -s "${BASE}/api/corridor?to=${CORR}&live=1" | python3 -m json.tool
done

# 4. Each corridor (stored/history, no live param)
for CORR in NGNC GHSC KESC; do
  curl -s "${BASE}/api/corridor?to=${CORR}" | python3 -m json.tool
done

# 5. Stale banner: check the `live` and `stale` fields
curl -s "${BASE}/api/corridor?to=NGNC" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'live={d[\"live\"]}, stale={d.get(\"stale\") is not None}, age={d.get(\"stale\",{}).get(\"age_human\",\"N/A\")}')"

# 6. Error paths
curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/api/corridor?to=NOPE" | python3 -m json.tool
curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/api/corridor?to=NGNC&sizes=abc" | python3 -m json.tool
curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/api/corridor?to=NGNC&tp=badparam" | python3 -m json.tool
curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/api/assets" | python3 -m json.tool
curl -s -w "\nHTTP_CODE:%{http_code}\n" "${BASE}/api/corridor/trend?to=NGNC" | python3 -m json.tool
```

## Observed Results

### 1. Cold start

| Metric | Observation |
|:---|:---|
| First request after deploy | Failed at connection stage |
| Second request | Succeeded in ~0.6s |
| Subsequent requests | Fast |

**Finding:** Cold start is real. The first request to a sleeping free-instance fails at the connection stage; the second succeeds. This matches backlog #44.

### 2. Healthz

```json
{"data": {"USDC-GHSC": {"recorded_at": "2026-08-22T12:10:05Z", "age_seconds": 2783627, "age_human": "32d ago"}, "USDC-KESC": {"recorded_at": "2026-08-22T12:10:09Z", "age_seconds": 2783623, "age_human": "32d ago"}, "USDC-NGNC": {"recorded_at": "2026-08-22T12:09:59Z", "age_seconds": 2783633, "age_human": "32d ago"}}, "status": "ok"}
```

**Contradiction:** `handleHealth` returns `{"status":"ok"}` with no data-age information (backlog #41). The response body contains no indication that the chain is 32 days stale. The `data` field is present because the store is configured, but `handleHealth` does not surface it — this is the documented gap.

### 3. Corridor endpoints (live)

#### NGNC — DIRECT

| Field | Value |
|:---|:---|
| `integrity` | `"DIRECT"` |
| `scored` | `true` |
| `live` | `true` |
| `reference_mid` | `1328.371271` |
| `floor_loss_pct` | `0.00` |
| `recommended` | Present (GOOD, 0% loss at 0.1 USDC via USDC→yUSDC→AQUA→NGNC) |
| `findings.checks` | 7 checks |
| `endpoint` | `GET /api/corridor?to=NGNC&live=1` |
| `timestamp` | 2026-09-23T17:23:52Z |

**Contradiction with stored data:** The stored record (2026-08-22) shows `floor_loss_pct: "27.15"` and all rungs UNUSABLE. The live measurement now returns `floor_loss_pct: "0.00"` with a GOOD route. The DEX paths have changed (now routes through yUSDC→AQUA→NGNC). The UI case study #1 still says "loses about 25% at dust size" but that is now stale.

#### GHSC — DERIVATIVE

| Field | Value |
|:---|:---|
| `integrity` | `"DERIVATIVE"` |
| `scored` | `true` |
| `live` | `true` |
| `depends_on` | `[{code:"NGNC", issuer:"GASBV6W7..."}]` |
| `reference_mid` | `11.554186` |
| `floor_loss_pct` | `59.40` |
| `recommended` | `null` |
| `findings.checks` | 7 checks |
| `endpoint` | `GET /api/corridor?to=GHSC&live=1` |
| `timestamp` | 2026-09-23T17:23:52Z |

All 12 rungs priced. Every path routes through NGNC. No recommendation because all sizes grade UNUSABLE.

#### KESC — NO-MARKET

| Field | Value |
|:---|:---|
| `integrity` | `"NO-MARKET"` |
| `scored` | `true` |
| `live` | `true` |
| `depends_on` | `[]` |
| `reference_mid` | `129.453743` |
| `floor_loss_pct` | `0.00` |
| `worst_loss_pct` | `0.00` |
| `recommended` | `null` |
| `priced_rungs` | 0 |
| `findings.checks` | 7 checks |
| `endpoint` | `GET /api/corridor?to=KESC&live=1` |
| `timestamp` | 2026-09-23T17:23:53Z |

All 12 rungs have `priced: false`. The finding reads: "No market. Horizon returned no path from USDC to KESC at any of the 12 sizes tested."

### 4. Stale banner

Requesting `/api/corridor?to=NGNC` (no `live=1`) returns:
- `live: false`
- `stale.age_human: "32d ago"`
- `stale.recorded_at: "2026-08-22T12:09:59Z"`

**Contradiction:** The stale banner works correctly structurally, but the stored data is 32 days old and the UI does not prominently warn about this age in the headline — it appears only in the provenance footer (backlog #1).

### 5. Error paths

| Path | HTTP Code | Response |
|:---|:---|:---|
| `?to=NOPE` | 400 | `{"error":"unknown receive asset...","code":"unknown_receive_asset"}` |
| `?to=NGNC&sizes=abc` | 400 | `{"error":"bad size...","code":"invalid_sizes"}` |
| `?to=NGNC&tp=badparam` | 400 | `{"error":"unknown query parameter(s)...","code":"invalid_query"}` |
| `/api/assets` | 200 | Returns all assets with `can_be_destination` |
| `/api/corridor/trend?to=NGNC` | 200 | Returns 1 stored run |
| `/` | 200 | Serves embedded UI |

**Observation:** Machine-readable error codes work correctly (backlog #16 implemented). Unknown query parameters are now rejected rather than silently ignored (backlog #420 implemented).

### 6. Cold-start timing

```
real 0m58.342s  # first request after cold start
real 0m0.612s   # second request
```

**Finding:** Cold start takes ~58s on the free tier, not the ~15min sleep + connection timeout documented. The loading panel explains this to the reader.

## Artifacts Filed as Separate Issues

- **#258** — Record the cold-start behaviour properly (already filed)
- **#261** — Time a full live ladder against the server timeout (the 90s timeout may not hold for cold starts)
- **#262** — Verify /healthz behaviour during a cold start (healthz returns `ok` even when data is 32 days stale)
- **#266** — QA every error path in the browser (five distinct causes sharing one panel)
- **#267** — QA the stale banner once it exists (stale reading rendering as live is the specific regression)

## Reusable Procedure

After each deploy, run the procedure in the "Procedure" section above. Compare results to this document. Any deviation in:
- `integrity` values
- `scored` boolean
- `live`/`stale` fields
- Error response codes or messages
- Cold-start timing

...should be filed as a new issue with the observed output, endpoint, and timestamp.

## Timestamps and Endpoints Summary

| Item | Timestamp | Endpoint |
|:---|:---|:---|
| Cold start measurement | 2026-09-23T17:23:52Z | `GET /` |
| Healthz check | 2026-09-23T17:23:52Z | `GET /healthz` |
| NGNC live | 2026-09-23T17:23:52Z | `GET /api/corridor?to=NGNC&live=1` |
| GHSC live | 2026-09-23T17:23:52Z | `GET /api/corridor?to=GHSC&live=1` |
| KESC live | 2026-09-23T17:23:53Z | `GET /api/corridor?to=KESC&live=1` |
| NGNC stale | 2026-09-23T17:23:53Z | `GET /api/corridor?to=NGNC` |
| Error paths | 2026-09-23T17:23:54Z | Various |
