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

**cURL:**

```bash
# History-first (fast, may be stale)
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?from=USDC&to=NGNC"

# Live measurement
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?from=USDC&to=NGNC&live=1"

# Custom sizes
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?from=USDC&to=NGNC&sizes=10,100,500&live=1"
```

**Example Response (200 OK):**

```json
{
  "send_asset": {
    "code": "USDC",
    "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
  },
  "receive_asset": {
    "code": "NGNC",
    "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
    "peg": "NGN"
  },
  "integrity": "DIRECT",
  "depends_on": [],
  "reference_mid": "1350.753432",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/NGN",
  "reference_agreement": "AGREE",
  "reference_secondary_mid": "1346.90659134",
  "reference_secondary_source": "currency-api",
  "reference_divergence_pct": "0.2856",
  "scored": true,
  "reference_fetched_at": "2026-08-26T14:47:15Z",
  "floor_loss_pct": "4.31",
  "floor_size": "0.1",
  "worst_loss_pct": "97.23",
  "worst_size": "5000",
  "recommended": {
    "description": "USDC -> XRP -> XLM -> NGNC",
    "source": "stellar-dex",
    "receive_amount": "129.2574648",
    "effective_rate": "1292.574648",
    "loss_pct": "4.31",
    "loss_amount": "5.82",
    "verdict": "FAIR",
    "warnings": [
      "delivers NGNC tokens, not NGN in a bank account; redeeming to fiat is a separate step with its own cost"
    ]
  },
  "recommended_size": "0.1",
  "live": true,
  "measured_at": "2026-08-26T14:47:17Z",
  "finding": "Best available: 4.31% below the exchangerate-api mid at 0.1 USDC, graded FAIR. Loss reaches 97.23% at 5000 USDC.",
  "findings": {
    "checks": [
      {
        "id": "sep10.endpoint-responds",
        "scope": "anchor",
        "subject": "NGNC (GASB\u2026)",
        "severity": "warning",
        "determined": true,
        "passed": false,
        "summary": "the declared SEP-10 endpoint returned HTTP 403 rather than a challenge",
        "evidence": [
          {
            "source": "https://anchor.ngnc.online/auth?account=GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF",
            "observed": "HTTP 403",
            "observed_at": "2026-08-26T14:47:17Z"
          }
        ],
        "observed_at": "2026-08-26T14:47:17Z"
      },
      {
        "id": "issuer.auth-flags",
        "scope": "asset",
        "subject": "NGNC (GASB\u2026)",
        "severity": "critical",
        "determined": true,
        "passed": true,
        "summary": "the issuer can neither freeze nor claw back this asset",
        "evidence": [
          {
            "source": "https://horizon.stellar.org/accounts/GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6 \u2192 flags",
            "observed": "auth_required=false auth_revocable=false auth_clawback_enabled=false auth_immutable=false",
            "observed_at": "2026-08-26T14:47:17Z"
          }
        ],
        "observed_at": "2026-08-26T14:47:18Z"
      }
    ],
    "passed": 3,
    "failed": 2,
    "undetermined": 0,
    "worst_severity": "warning"
  },
  "rungs": [
    {
      "send_amount": "5000",
      "priced": true,
      "integrity": "DIRECT",
      "quote": {
        "description": "USDC -> XLM -> NGNC",
        "source": "stellar-dex",
        "receive_amount": "186947.8515264",
        "effective_rate": "37.38957030528",
        "loss_pct": "97.23",
        "loss_amount": "6566819.31",
        "verdict": "UNUSABLE",
        "warnings": [
          "delivers NGNC tokens, not NGN in a bank account; redeeming to fiat is a separate step with its own cost",
          "thin liquidity: this size gets 96.9% worse pricing than a 10 USDC trade"
        ]
      },
      "cost": {
        "parts": [
          {
            "component": "fx_loss",
            "amount": "6566819.3084736",
            "pct": "97.23194704381251",
            "determined": true
          }
        ],
        "total_loss_pct": "97.23194704381251"
      },
      "notes": [
        "No viable route. The best of 1 priced route(s) still loses 97.2% against the exchangerate-api mid-market rate. Sending through this corridor at this size is not recommended."
      ]
    }
  ]
}
```

## `GET /api/corridor/trend`

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `limit` — positive whole number; defaults to 100 and is capped at 500.

This endpoint reads stored history only; it never measures. It returns `200` with `count: 0` and `runs: []` for an empty history. Runs are returned oldest first. Each run carries its sequence, timestamp, integrity, dependencies, reference details, ladder summary, finding, and rung loss/verdict values.

**cURL:**

```bash
curl -s "https://wayfare-cdb9.onrender.com/api/corridor/trend?from=USDC&to=NGNC&limit=30"
curl -s "https://wayfare-cdb9.onrender.com/api/corridor/trend?to=NGNC&limit=7"
```

**Example Response (200 OK):**

```json
{
  "corridor": "USDC-NGNC",
  "send_asset": {"code": "USDC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
  "receive_asset": {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", "peg": "NGN"},
  "reference_pair": "USD/NGN",
  "count": 1,
  "runs": [
    {
      "seq": 1,
      "recorded_at": "2026-08-22T12:09:59Z",
      "integrity": "DIRECT",
      "reference": {
        "mid": "1349.669672",
        "source": "exchangerate-api",
        "as_of": "2026-08-22T00:02:31Z",
        "divergence_pct": "0.0340"
      },
      "floor_loss_pct": "27.15",
      "worst_loss_pct": "97.52",
      "finding": "No usable size. Loss is 27.15% at 0.1 USDC...",
      "rungs": [
        {"send_amount": "0.1", "priced": true, "loss_pct": "27.15", "verdict": "UNUSABLE"},
        {"send_amount": "5000", "priced": true, "loss_pct": "97.52", "verdict": "UNUSABLE"}
      ]
    }
  ]
}
```

## `GET /api/assets`

Returns an `assets` array. Each entry contains the asset fields and `can_be_destination`, which is true when the binary has a verified fiat peg for that asset.

**cURL:**

```bash
curl -s https://wayfare-cdb9.onrender.com/api/assets
```

**Example Response (200 OK):**

```json
{
  "assets": [
    {
      "code": "NGNC",
      "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
      "peg": "NGN",
      "can_be_destination": true
    },
    {
      "code": "USDC",
      "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
      "peg": "USD",
      "can_be_destination": true
    },
    {
      "code": "GHSC",
      "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
      "peg": "GHS",
      "can_be_destination": true
    },
    {
      "code": "KESC",
      "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
      "peg": "KES",
      "can_be_destination": true
    }
  ]
```

## `GET /healthz`

A healthy service returns status `200` with:

**cURL:**
```bash
curl -s https://wayfare-cdb9.onrender.com/healthz 
```

**Example Response (200 OK):**

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