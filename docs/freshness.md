# Live and stale responses

This document defines how API consumers must interpret `live` and `stale` in a corridor response. The behavior described here is implemented as of 2026-08-27.

## `live` is provenance, not quality

`live` answers one question: did this response come from a fresh ladder measurement during this request?

- `live: true` means the server completed a current measurement against its configured upstreams.
- `live: false` means the response was replayed from stored history.

Neither value says that the market is good, that the route is usable, or that upstreams are healthy. Use `integrity`, quote verdicts, reference agreement, and findings for those questions.

The field is always present. Do not infer freshness from its absence, from `measured_at`, from deployment time, or from HTTP success. A successful `200` can intentionally contain a stale reading.

## `stale` is conditional and authoritative

When `live` is `false`, the response includes a `stale` object:

```json
{
  "live": false,
  "measured_at": "2026-08-21T22:30:40Z",
  "stale": {
    "recorded_at": "2026-08-21T22:30:40Z",
    "age_seconds": 518400,
    "age_human": "6d ago"
  }
}
```

- `recorded_at` is when the stored run was recorded.
- `age_seconds` is the server's calculated age at response time.
- `age_human` is display text only; use `age_seconds` for program logic.

A live response does not include `stale`. Consumers should branch on the boolean `live`, not treat a missing `stale` object as proof of freshness.

## How history is selected

A deployment may run in history-first mode. In that mode, `/api/corridor` serves the latest stored run by default. Add `live=1` to explicitly request a fresh measurement.

In normal mode, the server attempts a live measurement first. If it fails and a stored run exists, it returns that run as `live: false` with `stale`. The stored result is not promoted into a live result, and its recommendation remains exactly as recorded—including JSON `null` when no route was recommendable.

If a live measurement fails and no stored run exists, the server returns an error. It never synthesizes, interpolates, or substitutes zero values to fill the gap.

The trend endpoint is different: `/api/corridor/trend` always reads stored history and never measures. Its empty history is a successful response with zero runs.

## Consumer rules

1. Check `live` before presenting a response as current market data.
2. If `live` is `false`, show the age from `stale.age_seconds` or `stale.age_human` and make clear that the figures are historical.
3. Do not retry a stale response as though it were malformed. Request `?live=1` when current data is required.
4. Do not replace a stale response with a guessed current rate or choose a different quote because the stored recommendation is `null`.
5. Treat `integrity`, verdicts, and findings as independent fields. Staleness does not convert `NO-MARKET` into `UNUSABLE`, or vice versa.

The embedded UI follows these rules by visibly labelling recorded responses as `RECORDED — NOT CURRENT MARKET DATA` and offering an explicit “Measure live” action.

## Related references

- [HTTP API reference](api.md) — endpoint and field definitions
- [Run store](run-store.md) — how stored measurements are hash-chained
- [Snapshot format](snapshot-format.md) — provenance of upstream bytes
