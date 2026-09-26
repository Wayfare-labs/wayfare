# Verify the deployed instance against the repository it claims to be — Issue #259

**Target:** https://wayfare-cdb9.onrender.com/ (Render free instance)
**Tested by:** chiprime, branch `verify-claims`
**Timestamp:** 2026-09-26T01:27:07Z – 2026-09-26T01:27:12Z (all UTC)
**Endpoints:** `GET /api/corridor/trend?from=USDC&to={NGNC,GHSC,KESC}&limit=500`,
`GET /api/corridor?from=USDC&to={NGNC,GHSC,KESC}`, local
`go run ./cmd/wayfared -verify-store -data ./data`

## What the issue asks

The container embeds `data/` at build time; confirm the served history matches
the committed chain.

## Method (repeatable)

```bash
# 1. Verify the committed chain with the repository's own verifier.
go run ./cmd/wayfared -verify-store -data ./data

# 2. Fetch the served history and compare it field by field to data/*.ndjson.
node docs/qa/api/run-history-verification.mjs
```

The script compares, per corridor and per record: `recorded_at`, `integrity`,
`depends_on`, the whole reference block, `floor_loss_pct`/`floor_size`,
`worst_loss_pct`/`worst_size`, `recommended_size`, `finding`, and every rung's
`send_amount`, `priced`, `loss_pct` and `verdict`; then the history-first
`/api/corridor` response against the newest committed record, including
`live: false`, `measured_at` and `stale.recorded_at`. The raw responses are in
[`../api/results/history-verification.json`](../api/results/history-verification.json).

## Result: the committed chain verifies

`go run ./cmd/wayfared -verify-store -data ./data` (run 2026-09-26T01:27:07Z):

```
ok   USDC-GHSC: 1 records, latest 2026-08-22T12:10:05Z
ok   USDC-KESC: 1 records, latest 2026-08-22T12:10:09Z
ok   USDC-NGNC: 1 records, latest 2026-08-22T12:09:59Z
```

One record per corridor, each a Version 1 record sealed with `sha256:`.

## Result: the served history matches the committed chain

| Corridor | Committed (data/) | Served (`/api/corridor/trend`) | Field mismatches |
|:---|:---|:---|:---|
| USDC-NGNC | seq 1, 2026-08-22T12:09:59Z, DIRECT, floor 27.15@0.1, worst 97.52@5000 | seq 1, 2026-08-22T12:09:59Z, DIRECT, floor 27.15@0.1, worst 97.52@5000 | none |
| USDC-GHSC | seq 1, 2026-08-22T12:10:05Z, DERIVATIVE, floor 74.63@0.1, worst 99.41@5000 | seq 1, 2026-08-22T12:10:05Z, DERIVATIVE, floor 74.63@0.1, worst 99.41@5000 | none |
| USDC-KESC | seq 1, 2026-08-22T12:10:09Z, NO-MARKET, floor 0.00@0, worst 0.00@0 | seq 1, 2026-08-22T12:10:09Z, NO-MARKET, floor 0.00@0, worst 0.00@0 | none |

`corridors_all_match: true` in the result file. The history-first `/api/corridor`
response for each corridor also matched its newest committed record on
`integrity`, the floor/worst figures, `measured_at` and `stale.recorded_at`, and
carried `live: false`.

**Confirmed.** For every field the public API exposes, the deployed instance
serves the committed chain. This is consistent with
[`docs/embedded-history.md`](../../embedded-history.md), which records that the
deployed history is the `data/` committed when the image was built, and with the
README's statement that the served history has not advanced since the measure
workflow's push failure ([#63](https://github.com/Wayfare-labs/wayfare/issues/63)).

## What this does and does not establish

- **Does:** the served records are byte-compatible, field for field, with the
  committed `data/*.ndjson`; and the committed chain verifies locally with the
  same verifier the binary uses at load.
- **Does not:** prove *which commit* the running image was built from. The
  build revision is not exposed on any endpoint, so the match is against the
  repository's current committed chain, which is also what
  `TestEmbeddedHistoryVerifies` checks in CI. It also cannot re-hash the served
  bytes, because the record hash is not part of the wire shape — only storage
  carries it.

## Contradictions with the documentation

None found in this check. The served freshness (`stale.age_human: "34d ago"`
at 2026-09-26T01:26Z) matches the recorded timestamps in `data/`, and no served
figure disagreed with the committed record.

## Anything broken

Nothing. No code, threshold, integrity semantics, check composition or
run-record layout was touched; this artifact only records an observation.

## Timestamps and endpoints

| Item | Timestamp (UTC) | Endpoint / command |
|:---|:---|:---|
| Local chain verification | 2026-09-26T01:27:07Z | `go run ./cmd/wayfared -verify-store -data ./data` |
| Served trend, all corridors | 2026-09-26T01:27:07Z | `GET /api/corridor/trend?...&limit=500` |
| Served history-first, all corridors | 2026-09-26T01:27:07Z | `GET /api/corridor?...` |
| Full recorded set | 2026-09-26T01:27:12Z | `docs/qa/api/results/history-verification.json` |
