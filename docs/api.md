# HTTP API reference

**Status:** implemented in the repository as of 2026-08-27. This document describes the handlers and wire fields present in `server/` at that date. It does not describe roadmap capabilities.

All endpoints are read-only. The service does not hold funds, issue tokens, sign transactions, or execute payments.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/corridor` | Measure or retrieve one corridor |
| `GET` | `/api/corridor/trend` | Read stored measurements for a corridor |
| `GET` | `/api/assets` | List verified assets configured in the binary |
| `GET` | `/healthz` | Return service health |
| `GET` | `/` | Serve the embedded single-file UI |

Unsupported methods return `405`. JSON errors have the shape `{ "error": "..." }`.

## `GET /api/corridor`

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `sizes` — optional comma-separated positive decimal send amounts. The server accepts at most 24 values. When omitted, the route ladder's default sizes are used.
- `live=1` — when history-first mode is enabled, bypass stored history and request a live measurement.

The destination must be a configured asset with a verified fiat peg. Amounts, rates, percentages, and sizes are decimal strings, not JSON numbers.

A successful response contains:

- `send_asset`, `receive_asset` — asset code, issuer where applicable, and verified fiat peg.
- `integrity` — `DIRECT`, `DERIVATIVE`, `NO-MARKET`, or `UNKNOWN`; this describes corridor structure and is independent of verdict.
- `depends_on` — fiat-token dependencies for a derivative corridor.
- `reference_mid`, `reference_source`, `reference_pair` — the benchmark used for scoring.
- `reference_agreement`, optional secondary mid/source/divergence/note, and `scored` — the two-provider benchmark cross-check.
- `reference_fetched_at` — when the benchmark was obtained, when available.
- `floor_loss_pct`, `floor_size`, `worst_loss_pct`, `worst_size` — ladder summary values.
- `recommended` — the best acceptable quote, or JSON `null` when no size is recommendable.
- `recommended_size` — the send size of the recommendation, when one exists.
- `finding` — explanatory prose about the measurement.
- `rungs` — one entry per requested size.
- `measured_at` — timestamp of the response measurement or stored reading.
- `live` — `true` for a fresh measurement and `false` for history.
- `stale` — present only when `live` is `false`; contains `recorded_at`, `age_seconds`, and `age_human`.
- `findings` — present only when counterparty checks or metrics were run; findings qualify the headline and do not change integrity or verdict.

Each rung includes `send_amount`, `priced`, `integrity`, optional `quote`, notes, and an error when that size could not be measured. A priced rung may also include:

- `marginal_cost` — the change in effective receive-asset cost from the previous valid priced rung.
- `marginal_from` and `marginal_to` — the valid ladder sizes defining that marginal measurement.
- `cost` — the available cost decomposition.

The first valid priced rung has no marginal cost. Missing rungs are skipped, never treated as zero. The current marginal classification is available on the internal ladder result as `improving`, `flat`, `worsening`, or `undetermined`; fewer than two valid priced points are undetermined.

A live measurement failure is served from the latest stored run when one exists, labelled `live: false`. If no stored run exists, the request returns an error rather than fabricating a reading.

## `GET /api/corridor/trend`

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `limit` — positive whole number; defaults to 100 and is capped at 500.

This endpoint reads stored history only; it never measures. It returns `200` with `count: 0` and `runs: []` for an empty history. Runs are returned oldest first. Each run carries its sequence, timestamp, integrity, dependencies, reference details, ladder summary, finding, and rung loss/verdict values.

## `GET /api/assets`

Returns an `assets` array. Each entry contains the asset fields and `can_be_destination`, which is true when the binary has a verified fiat peg for that asset.

## `GET /healthz`

A healthy service returns status `200` with:

```json
{ "status": "ok" }
```

This endpoint checks that the HTTP service is responding. It does not perform a live corridor measurement or validate upstream availability.

## Freshness and provenance

`live` is not a verdict. It describes where the response came from. A response with `live: false` is historical, and its `stale` envelope is authoritative for age. Consumers must not infer freshness from deployment time, request time, or the absence of an error.

Reference rates are never averaged. The response identifies the provider and, when available, the second provider and divergence. `NO-MARKET` means no path was returned; it is not the same claim as a priced route with an `UNUSABLE` verdict.

## Related contracts

- [Run store](run-store.md) — stored record and hash-chain format
- [Snapshot format](snapshot-format.md) — recorded upstream bytes
- [Checks](checks.md) — tri-state counterparty findings and metrics
- [Contributing](../CONTRIBUTING.md) — invariants for changes
