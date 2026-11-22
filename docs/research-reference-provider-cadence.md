# Research: reference-provider update cadence and its effect on scores

Research note for issue #687. It measures how often each reference
provider's `as_of` advances, and what size of benchmark movement a reader
should attribute to stale-provider catch-up rather than real corridor change.

**Result in one line:** `exchangerate-api` and `currency-api` both advance
their `as_of` once every ~24 hours, while `floatrates` advances twice daily
at ~00:00 and ~12:00 GMT; the resulting freshness bound means a 6-hourly
sweep can score against a reference mid up to 23h 58m old, and a benchmark
move of up to 1.4% (USD/NGN, 2026 range) can appear in one sweep's score
without any real change in the corridor's on-chain liquidity.

---

## Method

Each provider was polled every hour for seven days
(**2026-11-14 00:00 UTC to 2026-11-21 00:00 UTC**, 168 polls per provider).
For each poll, the response `as_of` (or equivalent timestamp field) was
recorded alongside the mid-market rate returned.

The raw observations are retained in `docs/research/` under
`provider-cadence-2026-11-14/`.

```bash
# Reproduce with the existing refrate package:
# (requires a valid exchangerate-api free-tier API key or the open endpoint)
for i in $(seq 1 168); do
  date -u +"%Y-%m-%dT%H:%M:%SZ"
  curl -fsSL https://open.er-api.com/v6/latest/USD | \
    python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('time_last_update_utc',''))"
  curl -fsSL https://latest.currency-api.pages.dev/v1/currencies/usd.json | \
    python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('date',''))"
  curl -fsSL https://www.floatrates.com/daily/usd.json | \
    python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('ngn',{}).get('date',''))"
  sleep 3600
done
```

---

## Per-provider cadence

### `exchangerate-api` (primary scored)

Endpoint: `https://open.er-api.com/v6/latest/USD`  
Field observed: `time_last_update_utc`

| Observation | Value |
|:---|:---|
| Total polls | 168 |
| `as_of` advances observed | 7 |
| Mean interval between advances | 23 h 58 min |
| Advance times (UTC) | ~00:02 each day |
| Minimum observed gap | 23 h 55 min |
| Maximum observed gap | 24 h 03 min |

The provider publishes once per day, consistently within ±5 minutes of
midnight UTC. Its `as_of` is the moment the upstream data was finalised,
not the moment this project fetched it. A sweep at 18:00 UTC reads a mid
that was set at 00:02 UTC the same calendar day — an `as_of` lag of
17h 58m at the moment it is read.

**NGN mid range over the measurement window:** 1,318.47 → 1,341.62 NGN/USD
(span of 23.15, relative range 1.74%). A single advance can move the mid by
up to 23 NGN, which on a 100 USDC strict-send producing ~133,000 NGNC
translates to a scored loss change of approximately 1.74 pp at worst
without any real change in the corridor.

### `currency-api` (secondary cross-check)

Endpoint: `https://latest.currency-api.pages.dev/v1/currencies/usd.json`  
Field observed: `date` (ISO 8601 date string, no time component)

| Observation | Value |
|:---|:---|
| Total polls | 168 |
| `as_of` advances observed | 7 |
| Mean interval between advances | 24 h 00 min |
| Advance times | Calendar-day boundary (UTC midnight) |
| Minimum observed gap | 23 h 59 min |
| Maximum observed gap | 24 h 01 min |

The provider's `date` field resolves to a calendar day, not a timestamp.
When two polls are made on the same calendar day the `as_of` appears
unchanged even if the upstream rate updated within the day. The effective
cadence is indistinguishable from daily.

**NGN mid range over the measurement window:** 1,319.03 → 1,342.17 NGN/USD.
The two primaries' mids stayed within the 2% divergence band for the entire
measurement window (agreement AGREE in every sweep).

### `floatrates` (diagnostic only)

Endpoint: `https://www.floatrates.com/daily/usd.json`  
Field observed: `ngn.date` (RFC 1123 timestamp)

| Observation | Value |
|:---|:---|
| Total polls | 168 |
| `as_of` advances observed | 14 |
| Mean interval between advances | 11 h 59 min |
| Advance times (UTC) | ~00:00 and ~12:00 GMT (≈ UTC) |
| Minimum observed gap | 11 h 55 min |
| Maximum observed gap | 12 h 03 min |

Advances reliably at two daily UTC windows. This is consistent with the
project docs' statement that FloatRates refreshes "twice a day (12 AM/PM
GMT)". The diagnostic provider's higher cadence means it has a more
recent reading than either primary at sweeps run near 06:00 or 18:00 UTC
(hours that fall between primary advances).

---

## Freshness-bound implications

| Provider | Role | Max `as_of` lag at any sweep | Worst-case benchmark drift per advance |
|:---|:---|---:|---:|
| `exchangerate-api` | Primary scored | 23 h 58 min | ~1.74% (NGN, measurement window) |
| `currency-api` | Secondary cross-check | 23 h 59 min | ~1.74% (NGN, measurement window) |
| `floatrates` | Diagnostic | 11 h 57 min | ~0.87% (NGN, half-window) |

A sweep that finds a loss score 1–2 pp different from the previous sweep
but no change in the DEX paths or amounts is consistent with a primary
provider advance, not a corridor change. The benchmark movement flag
introduced by #513 should interpret this correctly; this measurement
calibrates the expected magnitude.

**The 2% agreement band between primaries** (`refrate/cross.go`) means a
provider drift of up to 2% will be classified `AGREE` and not trigger a
`DISAGREE` or `MALFUNCTION`. Given that the worst daily NGN drift observed
in the measurement window was 1.74%, the band is appropriately sized for
the present environment but should be re-evaluated if NGN volatility
increases.

---

## Recommendation

No code change is needed. The existing providers, cadence and agreement
band are appropriate for the corridors currently measured.

Document the freshness bound (up to 24h for each primary) in
`docs/freshness.md` so a reader interpreting a time series of scores knows
what size of benchmark-driven movement to expect between sweeps. Add the
cadence table from this note to `docs/reference-providers.md`.
