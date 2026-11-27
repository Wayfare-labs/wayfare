# HTTP API reference

**Status:** implemented API contracts, including `GET /api/chain-heads` added
2026-09-26. This document does not describe roadmap capabilities.

All endpoints are read-only. The service does not hold funds, issue tokens, sign transactions, or execute payments. The machine-readable OpenAPI 3.0 contract is defined in [`openapi.yaml`](../openapi.yaml).

Every `curl` example below is executed in CI against a local server replaying
committed snapshots (issue #669). The marker comment above each block names the
example and the top-level JSON keys it is expected to produce; the test rewrites
the deployed host to the local server and fails the job if a request errors or a
declared key is absent. Examples are what consumers copy, so an example that no
longer runs is a documentation bug that reports itself.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/corridor` | Measure or retrieve one corridor |
| `GET` | `/api/corridor/trend` | Read stored measurements for a corridor |
| `GET` | `/api/corridor/image` | Render dynamic SVG social card preview for a corridor |
| `GET` | `/api/chain-heads` | Publish the current hash-chain tip for each stored corridor |
| `GET` | `/api/assets` | List verified assets configured in the binary |
| `GET` | `/api/market-structure` | Identify shared issuer dependencies across corridors |
| `GET` | `/api/thresholds` | Publish verdict and divergence band thresholds from code constants |
| `GET` | `/og/corridor.svg` | Serve static social preview SVG card for open graph metadata |
| `GET` | `/healthz` | Return service health |
| `GET` | `/metrics` | Serve this process's operational counters |
| `GET` | `/readyz` | Return whether traffic should be routed to this process |
| `GET` | `/` | Serve the embedded single-file UI |

Unsupported methods return `405`. Requests beyond the per-client rate limit return `429` with a `Retry-After` header (seconds) and the error code `rate_limited` (see [Rate limiting](#rate-limiting)). JSON errors have the shape `{ "error": "...", "code": "..." }`; the machine-readable codes are the contract and are enumerated in [errors.md](errors.md).

### Error codes

Every endpoint emits one spelling: a lower-case `snake_case` token drawn from a single constant block in `server/api.go`, so the same cause carries the same code wherever it occurs (`invalid_query` for an unsupported query parameter on any endpoint, for example).

**Change for consumers.** `/api/corridor/trend` previously emitted upper-case twins of causes the rest of the API named in lower case — `METHOD_NOT_ALLOWED`, `INVALID_QUERY_PARAM`, `BAD_LIMIT`, `STORE_READ_ERROR`, `UNKNOWN_ASSET`. Those spellings were replaced, not emitted alongside the new ones: a client switching on the old tokens must switch on `method_not_allowed`, `invalid_query`, `invalid_limit`, `store_read_error`, `unknown_send_asset` / `unknown_receive_asset` instead. The replacement was made once, deliberately, because a code is a contract rather than a label ([errors.md](errors.md)).

## `GET /api/corridor`

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `sizes` — optional comma-separated positive decimal send amounts. The server accepts at most 24 values. When omitted, the route ladder's default sizes are used.
- `live=1` — when history-first mode is enabled, bypass stored history and request a live measurement. Without it, a history-first instance answers from the chain alone, and a corridor the scheduler has not swept yet returns `404` with `code: "no_history"` rather than a measurement the mode did not promise.

The destination must be a configured asset with a verified fiat peg. Amounts, rates, percentages, and sizes are decimal strings, not JSON numbers.

A successful response contains:

- `send_asset`, `receive_asset` — asset code, issuer where applicable, and verified fiat peg.
- `integrity` — `DIRECT`, `DERIVATIVE`, `NO-MARKET`, or `UNKNOWN`; this describes corridor structure and is independent of verdict.
- `depends_on` — fiat-token dependencies for a derivative corridor.
- `reference_mid`, `reference_source`, `reference_pair` — the benchmark used for scoring.
- `reference_agreement`, optional secondary mid/source/divergence/note, and `scored` — the two-provider benchmark cross-check.
- `reference_diagnostic` — optional third-provider attribution of a `DISAGREE`: `source`, `mid`, optional `as_of`, `outlier`, the two relative distances, and optional `reason`. It is evidence only; `reference_mid`, `reference_source` and `scored` are chosen exactly as they would be without it. See [reference providers](reference-providers.md).
- `reference_fetched_at` — when the benchmark was obtained, when available.
- `floor_loss_pct`, `floor_size`, `worst_loss_pct`, `worst_size` — scored ladder summary values. These fields are omitted when `scored` is `false`; an untrusted reference does not produce a floor or worst-loss measurement.
- `recommended` — the best acceptable quote, or JSON `null` when no size is recommendable.
- `recommended_size` — the send size of the recommendation, when one exists.
- `finding` — explanatory prose about the measurement.
- `rungs` — one entry per requested size.
- `ledger_range` — the first and last ledger observed across the rungs, as `first`, `last` and `span` (`last - first`, the number of ledgers that closed while the ladder was priced). A ladder takes several ledgers, so its rungs can be priced against different books; a large `span` marks a reading that may straddle a book change. Each ledger is the chain head Horizon's root reported right after that rung was priced. Omitted when no rung observed a ledger — never published as zeros. Stored readings carry it from record Version 6 on (see [run-store.md](run-store.md)).
- `measured_at` — timestamp of the response measurement or stored reading.
- `live` — `true` for a fresh measurement and `false` for history.
- `stale` — present only when `live` is `false`; contains `recorded_at`, `age_seconds`, and `age_human`.
- `findings` — present only when counterparty checks or metrics were run; findings qualify the headline and do not change integrity or verdict.

Each rung includes `send_amount`, `priced`, `integrity`, optional `quote`, notes, and an error when that size could not be measured. A priced rung may also include:

- `marginal_cost` — the change in effective receive-asset cost from the previous valid priced rung.
- `marginal_from` and `marginal_to` — the valid ladder sizes defining that marginal measurement.
- `cost` — the available cost decomposition (see [cost-decomposition.md](cost-decomposition.md)).

Any rung that priced may also include `ledger`, the chain head observed right after it was priced. It is omitted when unknown.

The first valid priced rung has no marginal cost. Missing rungs are skipped, never treated as zero. The current marginal classification is available on the internal ladder result as `improving`, `flat`, `worsening`, or `undetermined`; fewer than two valid priced points are undetermined.

A live measurement failure is served from the latest stored run when one exists, labelled `live: false`. If no stored run exists, the request returns an error rather than fabricating a reading.

When `scored` is `false`, no verdict or loss was derived from the reference. The
four ladder summary fields above are omitted, as are each rung's `loss_pct` and
`loss_amount`, its `cost` decomposition, and reference-dependent marginal-cost
fields. Clients must treat these fields as unknown when absent; they must not
default them to `"0"` or `"0.00"`. Structural integrity, dependencies, quoted
receive amounts and effective rates remain available. History and trend
responses apply the same omission to unscored runs, including older stored
runs whose original zero values were produced before this rule.

On a history-first instance, a corridor with no stored run is answered before anything is measured:

```http
HTTP/1.1 404 Not Found
Content-Type: application/json; charset=utf-8

{"error":"no stored measurement for corridor USDC-NGNT on this history-first instance; request with live=1 to measure it now","code":"no_history","request_id":"…"}
```

The status is `404`, the code is `no_history`, and the body carries neither `integrity` nor `rungs` nor any figure — an error is never measurement-shaped ([the note in errors.md](errors.md#a-note-on-no_history)). This is the response a first deployment gives, and a corridor newly added to the registry gives until the scheduler's next sweep records it. Asking with `live=1` opts out: an explicit live request measures, and a failed explicit live request reports `measurement_failed` rather than `no_history`, because in that case something was attempted and did fail. `NO-MARKET` remains a third thing again — a `200` finding that the corridor has no path.

### Conditional requests on history-served responses

A response with `live: false` is served from a stored record. That record is
immutable — it is hash-sealed, and a new measurement is a new link in the chain
rather than an edit of the old one — so the server can tell a client exactly
which record it is holding.

- A `live: false` response carries an `ETag`: the record's own seal hash,
  quoted (`ETag: "sha256:…"`). It is the same value the run store verifies the
  chain against, not a digest invented for HTTP, so a reader can check it with
  `sha256sum`.
- It also carries `Cache-Control: no-cache`: the body may be stored, but it
  must be revalidated before it is reused.
- A request carrying `If-None-Match` with that value is answered `304 Not
  Modified` — the same `ETag`, no body — and the client keeps the copy it has.
  The comparison is the weak one RFC 9110 asks for, so `W/"…"` and a
  comma-separated list both work, and `*` matches any current record.
- A **live** response carries **no** `ETag` and is never answered `304`. Its
  figures depend on the market at the moment of the request, so there is
  nothing about it that can be reused. The absence of the tag is the signal.
- A response that is not a measurement — `no_history`, or any error — carries no
  `ETag` either: there is no representation to revalidate.

The tag identifies the **record**, which is the measurement. The `stale`
envelope's `age_seconds` and `age_human` are recomputed on every request, so
two responses for the same record differ in those two fields and are not
byte-identical; the figures they describe are identical. A `304` therefore
leaves the client with the age its copy was served at.

The `ETag` is listed in `Access-Control-Expose-Headers`, so a browser page on
another origin can read the tag it is asked to send back.

```http
GET /api/corridor?from=USDC&to=NGNC HTTP/1.1
→ 200 OK
   ETag: "sha256:3f2c…"
   Cache-Control: no-cache

GET /api/corridor?from=USDC&to=NGNC HTTP/1.1
   If-None-Match: "sha256:3f2c…"
→ 304 Not Modified
   ETag: "sha256:3f2c…"
   Cache-Control: no-cache
```

**cURL — a polling client:**

<!-- api-example: corridor-polling -> - -->
```bash
# Fetch, and remember the tag. (-D - prints the response headers;
# the endpoint answers GET only, so the header must come from a GET.)
etag=$(curl -sD - -o /dev/null "https://wayfare-cdb9.onrender.com/api/corridor?from=USDC&to=NGNC" \
  | awk -F': ' '/^ETag/ {print $2}' | tr -d '\r')

# Ask again: nothing changed, so nothing is downloaded.
curl -s -o /dev/null -w '%{http_code}\n' -H "If-None-Match: $etag" \
  "https://wayfare-cdb9.onrender.com/api/corridor?from=USDC&to=NGNC"
```

**cURL:**

<!-- api-example: corridor -> send_asset,receive_asset,integrity,rungs,floor_loss_pct,recommended,live=true -->
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
        "hops": [
          {"code": "USDC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", "asset": "stellar:USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
          {"code": "XLM", "asset": "stellar:native"},
          {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", "peg": "NGN", "asset": "stellar:NGNC:GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"}
        ],
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

Every quote — `recommended` and each rung's `quote` — carries `hops`: the path as structured data, in execution order with both endpoints, each entry an asset with its `issuer` where one was established and its `peg` where known. `description` stays exactly as it was, for display and compatibility; `hops` is the field to consume, because an asset code identifies nothing without its issuer and a client must not parse prose or guess an issuer from a code. A hop whose issuing account could not be established says so with `issuer_unknown: true` instead of omitting the field silently; native and fiat assets have no issuer by nature and are identified in full by `asset`. A stored (stale) reading serves `hops: null` — the run store records the description string only, and “not recorded” is not the same claim as “no intermediate assets”.

### Money fields: scale and rounding

Money crosses the wire as decimal strings (ADR 006). Those strings are not all at the same scale, and a consumer that compares two of them without knowing each one's scale gets a wrong answer: `floor_loss_pct: "4.31"` beside a rung's `loss_pct: "4.31390625"` is the same measurement at two scales, not a movement. The tables below give the scale and rounding rule for every money field, field by field.

Two renderings exist and which one a field uses is a property of the field, not of the value:

- **Full precision** — `decimal.Decimal.String()`. Nothing is rounded when the value is written. Every digit the engine holds is published and trailing zeros are removed, so one field can arrive as `"4.31"` at one measurement and `"4.31390625"` at the next. A quotient (`effective_rate`, `loss_pct`, `marginal_cost`) is capped at 16 decimals by `decimal.DivisionPrecision`, and that cap is applied where the number is *computed*, not at the wire; products and sums (amounts derived by multiplying a size by a rate) keep the full scale of their inputs and can exceed 16 decimals. Amounts that come straight from Horizon carry its 7-decimal scale.
- **Fixed scale** — `decimal.Decimal.StringFixed(n)`. Exactly `n` decimals, always, including on zero: a zero renders `"0.00"`, never `"0"`. Rounding is half away from zero, so `2.675` writes `2.68` and a negative `-2.675` writes `-2.68` (reachable on `parallel_gap_pct`, which is signed). These are display figures — reconcile against a full-precision field only after allowing for the rounding the fixed field already absorbed.

An absent money field means the quantity was not determined, never that it is zero. The field is missing from the document rather than published as `"0"` — the same rule ADR 006 and `docs/metrics.md` state for the values themselves.

Corridor document, headline fields:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `reference_mid` | full precision | none at the wire |
| `reference_secondary_mid` | full precision | none at the wire |
| `reference_divergence_pct` | exactly 4 | half away from zero |
| `reference_diagnostic.mid` | full precision | none at the wire |
| `reference_diagnostic.outlier_distance_pct` | exactly 4 | half away from zero |
| `reference_diagnostic.other_distance_pct` | exactly 4 | half away from zero |
| `parallel_mid` | full precision | none at the wire |
| `parallel_gap_pct` | exactly 4 | half away from zero, signed |
| `floor_loss_pct` | exactly 2 | half away from zero |
| `worst_loss_pct` | exactly 2 | half away from zero |
| `floor_size` | full precision | none at the wire |
| `worst_size` | full precision | none at the wire |
| `recommended_size` | full precision | none at the wire |

Quote objects — `recommended` and each `rungs[].quote`, one shape used in both places:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `receive_amount` | full precision | none at the wire; a Horizon amount keeps its 7 decimals |
| `effective_rate` | full precision | quotient: 16-decimal cap applied at computation |
| `loss_pct` | full precision | quotient: 16-decimal cap applied at computation, then multiplied by 100; never negative — a route better than mid is reported as `0`, not as a negative cost |
| `loss_amount` | exactly 2 | half away from zero; never negative, for the same reason as `loss_pct` |

Rungs:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `rungs[].send_amount` | full precision | none at the wire; a requested ladder size, so it reads as entered (`"0.1"`, `"5000"`) |
| `rungs[].marginal_cost` | full precision | difference of two rates: no new rounding beyond what each rate already carries |
| `rungs[].marginal_from` | full precision | none at the wire; a size |
| `rungs[].marginal_to` | full precision | none at the wire; a size |

Cost block, `rungs[].cost`:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `total_loss_pct` | full precision | none at the wire; equals the rung's `loss_pct`, so the two always reconcile digit for digit |
| `parts[].amount` | full precision | none at the wire |
| `parts[].pct` | full precision | none at the wire |

Undetermined cost parts publish neither field — they carry `determined: false` and a `reason`, which is the absence rule above applied inside the block.

Execution-rate curve, `curve.points[]`:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `size` | full precision | none at the wire |
| `rate` | full precision | quotient: 16-decimal cap applied at computation |

An unpriced point publishes `size` and no `rate` at all, with its `reason` beside it.

The tables above cover the corridor document that `route/wire.go` renders. `route/wire_scale_test.go` prices one corridor in which every money field is the same probe value, reads the scale this section declares for each field straight out of this file, and fails if a renderer's output no longer matches what is written here — or if a money field reaches the wire that this section never names.

`route/money.go`'s `MoneyStrings` walks the corridor document's money fields and the boundary tests feed every string it returns to `decimal.NewFromString`, so a producer cannot emit a malformed figure. Its list is the headline money — the quote, cost-block and corridor scalars. `rungs[].marginal_cost`, `rungs[].marginal_from`, `rungs[].marginal_to` and `curve.points[].size`/`rate` are money on the wire that the walker does not yet collect, so a malformed one of those is caught by the scale guard above and by the wire tests, not by the boundary walk. A field added to the wire shape must be added to that walker and to the tables above.

### Trend money fields

`GET /api/corridor/trend` money fields are stored verbatim: `runstore` records the string the corridor document already rendered (`runstore/convert.go` derives a record from `route.CorridorJSON`, not from the engine's numbers), and the trend endpoint copies those stored strings into its response without re-formatting them. So every `runs[]` figure — `reference.mid`, `reference.secondary_mid`, `reference.divergence_pct`, `floor_loss_pct`, `floor_size`, `worst_loss_pct`, `worst_size`, `recommended_size`, `rungs[].send_amount`, `rungs[].loss_pct` — has exactly the scale listed above for the same field on `/api/corridor`, and reading a trend series compares like with like across runs.

The trend endpoint's own derived statistics are the exception, and they are all fixed scale:

| Field | Decimals on the wire | Rounding |
|---|---|---|
| `divergence_stats.mean_pct` | exactly 4 | half away from zero |
| `divergence_stats.stddev_pct` | exactly 4 | half away from zero |
| `divergence_stats.trend_magnitude_pct` | exactly 4 | half away from zero |

`divergence_stats.trend_magnitude_pct` is percentage points per day. Its
signed counterpart is not returned by this endpoint; `trend_direction` names
whether divergence is improving, stable, or worsening. The statistics also
include `first_at` and `last_at` when at least one divergence-bearing run was
recorded; these RFC 3339 timestamps define the covered span. The trend is an
OLS regression on elapsed time from `first_at`, not on observation index, so
gaps between recorded runs affect the slope according to their actual length.

## `GET /api/corridor/trend`

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `limit` — positive whole number; defaults to 100 and is capped at 500.
- `before` — positive whole number sequence cursor (`seq`); returns runs strictly before this sequence number (`seq < before`). Used with `limit` for backwards cursor pagination across history.
- `pretty` — optional boolean (`pretty=1`); indents the JSON response.

This endpoint reads stored history only; it never measures. It returns `200` with `count: 0` and `runs: []` for an empty history. Runs are returned oldest first. Each run carries its sequence, timestamp, integrity, dependencies, reference details, ladder summary, finding, and rung loss/verdict values.

When older runs exist before the oldest run returned in the current page, the response includes a `next` field containing the sequence cursor to pass to `?before=` to fetch the next older page. When the earliest stored run has been reached, `next` is omitted.

`integrity_transitions` is an always-present array of structural changes
between consecutive runs in that returned window, in chronological order. A
change in integrity state (for example, `DIRECT` to `DERIVATIVE` or
`DERIVATIVE` to `NO-MARKET`) is included, as is a `depends_on` change while
remaining `DERIVATIVE`. A pair involving `UNKNOWN` is omitted: it cannot
establish that structure changed. An empty array means no such transition was
established in the window. The series is descriptive and does not alter a
verdict or integrity state.

`latest_transition` is the most recent structural change in that window — the
same event as the last entry of `integrity_transitions`, named on its own
because "this corridor changed kind" is the one fact a reader should not have
to diff two records by hand to find. It carries the same fields:
`previous_integrity`, `current_integrity`, `previous_run_at`,
`current_run_at`, `previous_depends_on`, `current_depends_on` and
`transition_type` (`INTEGRITY_STATE_CHANGE` or `DEPENDS_ON_CHANGED`).

It is `null` when the window establishes no transition: a history of one run
has nothing to compare, and two runs that agree are not a change. It is
`null` rather than absent, so a client can tell "nothing changed" from "this
build does not report transitions". A window of fewer than two runs therefore
always reports `null` — raise `limit` to at least 2 for the comparison to be
possible.

The monitor writes the same comparison to its log after every sweep
(`docs/deployment.md`), so a reader who wants the event as it happened can
watch the log rather than poll the endpoint.

**cURL:**

<!-- api-example: corridor-trend -> corridor,send_asset,receive_asset,count,runs,integrity_transitions,latest_transition -->
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
  "integrity_transitions": [],
  "latest_transition": null,
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

## `GET /api/corridor/image`

Serves a corridor-specific SVG social preview card generated from the most recent stored measurement for the requested corridor. If no stored measurement exists for that corridor, returns `404` with `code: "no_corridor_data"`.

Query parameters:

- `from` — send asset code; defaults to `USDC`.
- `to` — receive asset code; defaults to `NGNC`.
- `pretty` — accepted for consistency with the JSON endpoints; it does not change the SVG.

Response headers:

- `Content-Type: image/svg+xml; charset=utf-8`
- `Cache-Control: public, max-age=60, stale-while-revalidate=300`

Errors:

- `405 method_not_allowed` — when requested with a method other than `GET`.
- `400 invalid_query` — when unknown query parameters are passed.
- `400 unknown_send_asset` — when `from` asset is not in the verified asset registry.
- `400 unknown_receive_asset` — when `to` asset is not in the verified asset registry.
- `404 no_corridor_data` — when no recorded measurement exists for the requested corridor.

**cURL:**

```bash
curl -s https://wayfare-cdb9.onrender.com/api/corridor/image?from=USDC&to=NGNC
```

## `GET /api/chain-heads`

Returns the current stored hash-chain tip for every corridor, sorted by
corridor key. This endpoint is read-only and does not measure. Each entry
contains:

**Query parameter:** `pretty=1` is optional and indents the JSON response for
humans.

Each item in `heads` contains:

- `corridor` — stable corridor key, such as `USDC-NGNC`.
- `seq` — sequence number of the current tip.
- `recorded_at` — the tip record's UTC timestamp in RFC 3339 form.
- `hash` — the full SHA-256 record hash, including its `sha256:` prefix.

An empty or unconfigured store returns `200` with `heads: []`. The endpoint
does not sign the response or provide a trusted timestamp: a third party should
retain the returned hash and the time and source at which it observed it. The
hash is a pin for comparing later observations, not proof that the underlying
measurement was correct or that the publisher included every scheduled run.
To establish that a later chain still contains a pinned head, the reader must
obtain the corresponding NDJSON history from the repository's
[committed data](../data/) and verify its links; this endpoint publishes the
tip only and does not return per-record proofs.

At present, the committed store is a rolling window. When rotation trims a
corridor, its surviving records are re-sealed from a new window head, so its
published tip hash changes and is not a continuation of the previously pinned
chain. See [ADR 007](adr/007-why-the-committed-chain-is-a-rolling-window.md).

**cURL:**

<!-- api-example: chain-heads -> heads -->
```bash
curl -s https://wayfare-cdb9.onrender.com/api/chain-heads
```

**Example Response (200 OK):**

```json
{
  "heads": [
    {
      "corridor": "USDC-NGNC",
      "seq": 1,
      "recorded_at": "2026-08-22T12:09:59Z",
      "hash": "sha256:424b33fcf1202487e493e905a7710247489ccd4d943eb182ce6f0f4f0fb4144f"
    }
  ]
}
```

## `GET /api/assets`

Returns an `assets` array. Each entry contains the asset fields and `can_be_destination`, which is true when the binary has a verified fiat peg for that asset.

**cURL:**

<!-- api-example: assets -> assets -->
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
}
```

## `GET /api/market-structure`

Returns issuer concentrations — cases where a single issuer backs multiple corridor destination tokens. This is a structural fact about the corridor set, not a measurement, and it does not change between runs.

Query parameters:

- `pretty=1` — optional; pretty-print the JSON response.

A successful response contains:

- `issuer_concentrations` — map of issuer account ID to the list of corridor asset codes it issues. Only issuers with two or more corridors are included.
- `corridor_count` — total number of registered corridor destinations (assets with a fiat peg, excluding USDC).
- `concentrated_corridor_count` — number of corridors that share an issuer with at least one other corridor.
- `generated_at` — RFC3339 timestamp when this analysis was produced.

**cURL:**

<!-- api-example: market-structure -> issuer_concentrations,corridor_count,concentrated_corridor_count,generated_at -->
```bash
curl -s https://wayfare-cdb9.onrender.com/api/market-structure
curl -s https://wayfare-cdb9.onrender.com/api/market-structure?pretty=1
```

**Example Response (200 OK):**

```json
{
  "issuer_concentrations": {
    "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6": [
      "GHSC",
      "KESC",
      "NGNC"
    ]
  },
  "corridor_count": 8,
  "concentrated_corridor_count": 3,
  "generated_at": "2026-08-27T12:00:00Z"
}
```

In the current registry, LinkIOIssuer (`GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6`) backs three corridors: NGNC (naira), GHSC (Ghanaian cedi), and KESC (Kenyan shilling). This concentration is a market-structure fact — if that issuer fails, all three corridors fail together.

## `GET /api/thresholds`

Publishes the verdict and divergence thresholds directly from code constants defined in the `route` and `refrate` packages. Clients should query this endpoint instead of hard-coding threshold values.

Query parameters:

- `pretty` — optional boolean (`pretty=1`); indents the JSON response.

Response headers:

- `Content-Type: application/json; charset=utf-8`

A successful response contains:

- `verdict_thresholds` — object with `good`, `fair`, `poor` loss percentage thresholds as decimal strings.
- `divergence_thresholds` — object with `agree` and `malfunction` percentage thresholds as decimal strings.

**cURL:**

```bash
curl -s https://wayfare-cdb9.onrender.com/api/thresholds
```

**Example Response (200 OK):**

```json
{
  "verdict_thresholds": {
    "good": "3",
    "fair": "8",
    "poor": "20"
  },
  "divergence_thresholds": {
    "agree": "2",
    "malfunction": "10"
  }
}
```

Errors:

- `405 method_not_allowed` — when requested with a method other than `GET`.
- `400 invalid_query` — when unknown query parameters are passed.

## `GET /og/corridor.svg`

Serves a static Open Graph snapshot SVG card for social previews, displaying corridor integrity, floor loss percentage at floor size, and recording timestamp.

Query parameters:

- `from` — send asset code (optional, defaults to `USDC`).
- `to` — receive asset code (e.g. `NGNC`).

Response headers:

- `Content-Type: image/svg+xml; charset=utf-8`
- `Cache-Control: public, max-age=300`

Errors:

- `405 method_not_allowed` — when requested with a method other than `GET`.
- `400 invalid_query` — when unknown query parameters or unrecognised corridor assets are supplied.
- `404 Not Found` — when the run store is absent or has no recorded data for the corridor.
- `500 store_read_error` — when reading from the run store encounters an error.

**cURL:**

```bash
curl -s "https://wayfare-cdb9.onrender.com/og/corridor.svg?from=USDC&to=NGNC"
```

## `GET /healthz`

A healthy service returns status `200` with:

**cURL:**

<!-- api-example: healthz -> status,data -->
```bash
curl -s https://wayfare-cdb9.onrender.com/healthz
```

**Example Response (200 OK):**

```json
{
  "status": "ok",
  "code_commit": "c7436e2257ae391e6177e063a9012e631540e1d7",
  "data_commit": "05b446626f6f169b927b18822bd08c700f4f80de",
  "data": {
    "USDC-NGNC": {
      "recorded_at": "2026-09-26T08:00:00Z",
      "age_seconds": 45,
      "age_human": "45s"
    }
  },
  "freshness": {
    "chain_head": 55101724,
    "newest_record_at": "2026-09-26T08:00:00Z",
    "record_count": 431
  }
}
```

`data` maps each corridor that has stored history to its newest run and the
age of that run; a corridor with no stored run is omitted rather than
reported as fresh, and the whole field is `null` when no history is
configured.

### Build provenance fields

`code_commit` is the git commit the binary was built from, and `data_commit`
is the git commit of `data/` that was embedded in the binary at build time.
Both are stamped at build time with `-ldflags -X` (see the Dockerfile and
Makefile `build-stamped` target); unstamped dev or test builds report the
empty string. Together they answer "is the deploy stale or the pipeline
dead?": if `data_commit` stops advancing while `code_commit` keeps moving, the
measurement pipeline is not landing new history.

The same pair is on the command line: `wayfared -version` and `ladder -version`
print it, each value labelled with the source that supplied it, because a
reader reproducing a published figure often has the binary and not the deploy.
An unstamped build says `unknown (unstamped)` there rather than falling back to
the checkout's revision for `data_commit`, which the toolchain cannot know.
This endpoint checks that the HTTP service is responding. It does not perform a live corridor measurement or validate upstream availability, and it does not report whether the history being served was verified: a process serving a chain that failed verification at startup still answers `200` here, because the process is up. That is what [`GET /readyz`](#get-readyz) is for.
This endpoint checks that the HTTP service is responding. It does not perform a live corridor measurement or validate upstream availability.

### Freshness fields

`freshness` describes how current the recorded data is, so a consumer can tell
"is old" apart from "is down":

- `chain_head`: the Stellar core ledger sequence reported by Horizon's root
  endpoint at request time.
- `newest_record_at`: RFC 3339 timestamp of the most recent recorded corridor
  measurement in the run store (null when the store holds no records).
- `record_count`: number of records held by the run store.

Unknown values are rendered as `null`, never as zero or a synthesised guess.
If Horizon's root is unreachable, `chain_head` is `null` while store-derived
fields are still reported; if the store cannot be read, its fields are `null`
while `chain_head` is still reported. A `200` with nulls means the check could
not be made, not that data is absent. The freshness block does not change the
endpoint's health verdict: it remains `200` as long as the HTTP service is
answering.

## `GET /metrics`

Prometheus text exposition (format 0.0.4) of what the process did while nobody
was reading its log: how each corridor sweep ended, how each upstream exchange
failed, how long a live measurement took, and how many requests the rate limiter
refused. The log answers "what happened at 04:12?"; these series answer "how
often in the last hour?", and the second question is the one an alert has to be
built from. Nothing here is a second source of truth about a corridor — no
verdict, no price, no health claim — so a deployment probe still uses
[`GET /healthz`](#get-healthz).

The exposition is written by hand rather than through a client library: the
project runs two direct dependencies (`deps_test.go` enforces it) and the format
is line-oriented plain text. Families are emitted in name order and each
family's label sets are sorted, so two scrapes of a process that observed nothing
new are byte-identical.

**cURL:**
```bash
curl -s https://wayfare-cdb9.onrender.com/metrics
```

**Example Response (200 OK):**

```text
# HELP wayfare_live_request_duration_seconds Duration of a request that asked for a live corridor measurement, by the outcome it got. Requests served from history are not live and never appear.
# TYPE wayfare_live_request_duration_seconds histogram
wayfare_live_request_duration_seconds_bucket{le="0.25",outcome="measured"} 0
wayfare_live_request_duration_seconds_bucket{le="0.5",outcome="measured"} 0
wayfare_live_request_duration_seconds_bucket{le="1",outcome="measured"} 0
wayfare_live_request_duration_seconds_bucket{le="2.5",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="5",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="10",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="20",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="40",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="60",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="90",outcome="measured"} 1
wayfare_live_request_duration_seconds_bucket{le="+Inf",outcome="measured"} 1
wayfare_live_request_duration_seconds_sum{outcome="measured"} 1.8
wayfare_live_request_duration_seconds_count{outcome="measured"} 1
# HELP wayfare_rate_limit_rejections_total Requests refused with HTTP 429 by this process's per-client rate limiter.
# TYPE wayfare_rate_limit_rejections_total counter
wayfare_rate_limit_rejections_total 2
# HELP wayfare_sweep_outcomes_total Corridor measurements completed by the scheduler, by corridor and outcome. The three outcomes are mutually exclusive.
# TYPE wayfare_sweep_outcomes_total counter
wayfare_sweep_outcomes_total{corridor="USDC-KESC",outcome="failed"} 1
wayfare_sweep_outcomes_total{corridor="USDC-NGNC",outcome="ok"} 1
# HELP wayfare_upstream_errors_total Failed HTTP exchanges with an upstream, by upstream and how it failed. One per HTTP exchange, so a redirect chain counts once per hop.
# TYPE wayfare_upstream_errors_total counter
wayfare_upstream_errors_total{kind="server_error",upstream="horizon"} 1
wayfare_upstream_errors_total{kind="timeout",upstream="horizon"} 1
# HELP wayfare_upstream_request_duration_seconds Duration of one HTTP exchange with an upstream, including failures and every redirect hop. The _count sample is the number of exchanges.
# TYPE wayfare_upstream_request_duration_seconds histogram
wayfare_upstream_request_duration_seconds_bucket{le="0.05",upstream="horizon"} 0
wayfare_upstream_request_duration_seconds_bucket{le="0.1",upstream="horizon"} 1
wayfare_upstream_request_duration_seconds_bucket{le="0.25",upstream="horizon"} 1
wayfare_upstream_request_duration_seconds_bucket{le="0.5",upstream="horizon"} 2
wayfare_upstream_request_duration_seconds_bucket{le="1",upstream="horizon"} 2
wayfare_upstream_request_duration_seconds_bucket{le="2.5",upstream="horizon"} 2
wayfare_upstream_request_duration_seconds_bucket{le="5",upstream="horizon"} 2
wayfare_upstream_request_duration_seconds_bucket{le="10",upstream="horizon"} 2
wayfare_upstream_request_duration_seconds_bucket{le="20",upstream="horizon"} 3
wayfare_upstream_request_duration_seconds_bucket{le="+Inf",upstream="horizon"} 3
wayfare_upstream_request_duration_seconds_sum{upstream="horizon"} 20.505
wayfare_upstream_request_duration_seconds_count{upstream="horizon"} 3
```

That example is a capture, not a transcription. They are the bytes the collector
writes for a process that recorded one swept corridor stored successfully, one
swept corridor that was not, three Horizon exchanges (410ms ending in a 5xx, 95ms
successful, and one cut off at the client's 20-second timeout), one live
measurement of 1.8s, and two refused requests. A test in `metrics` re-renders it
from the fixture and fails if the document and the code stop agreeing.

### Response shape

| Header | Value | Why |
|---|---|---|
| `Content-Type` | `text/plain; version=0.0.4; charset=utf-8` | the version parameter is part of the format: a scraper that cannot parse the body must know which grammar it was served |
| `X-Content-Type-Options` | `nosniff` | as on every other success path |
| `Cache-Control` | `no-store` | counters are monotonic within a process, so a cached body is not merely stale but wrong: a scraper would read a plateau as "nothing is happening" on exactly the instance that stopped being scraped |

`GET` and `HEAD` are the only accepted methods; any other method returns `405`
with `code: "method_not_allowed"`. `HEAD` answers with the same status and headers
and no body, so a monitor can check the endpoint is there without spending a
measurement. A body larger than 512 bytes is gzipped when the client offers
`Accept-Encoding`, and a gzipped response is chunked with `Content-Length`
removed, which means `HEAD` and `GET` report different lengths for the same
document unless compression is declined on both.

A server built without a collector answers with all five families declared and
nothing observed, rather than an error: a `404` would say the endpoint is absent,
and the truthful answer here is that this process has recorded nothing.

### Series

| Series | Type | Labels | One observation is |
|---|---|---|---|
| `wayfare_sweep_outcomes_total` | counter | `corridor`, `outcome` | one corridor the scheduler finished, in one sweep |
| `wayfare_upstream_errors_total` | counter | `upstream`, `kind` | one HTTP exchange with an upstream that failed |
| `wayfare_upstream_request_duration_seconds` | histogram | `upstream` | one HTTP exchange with an upstream, successful or not |
| `wayfare_live_request_duration_seconds` | histogram | `outcome` | one request that asked the engine for a live measurement |
| `wayfare_rate_limit_rejections_total` | counter | none | one request this process refused with `429` |

The rejection counter carries no label deliberately. The number an operator
alerts on is "is the limit set too tight", which is a property of the service,
and per-client counts would put request volume by identity into a document
fetched without authentication.

### Label values

Every label value comes from code, never from a request: corridors from the
binary's asset registry, upstreams from the clients wired in `cmd/wayfared`, and
the vocabularies below from constants in package `metrics`. That is what bounds
the exposition — no combination of query parameters can add a series, and an
operator's `-horizon` override cannot publish an arbitrary host as a label.

`outcome` on `wayfare_sweep_outcomes_total`. The three are mutually exclusive, so
their sum is the number of corridor measurements attempted:

| Value | Meaning |
|---|---|
| `outcome="ok"` | Measured and appended to the run store. An upstream that answered 5xx can still land here: the engine records what it could build and marks the integrity unknown, so a degraded run is still a run. That upstream shows in `wayfare_upstream_errors_total` instead. |
| `outcome="failed"` | The measurement did not happen, so there was nothing to record. This is the upstream-or-engine failure an operator alerts on. |
| `outcome="unrecorded"` | The measurement succeeded and the store refused the record. The history has a hole the corridor did not cause, which is why it is not folded into `outcome="failed"`: the remedy is disk and permissions, not upstream availability. |

`corridor` — the swept pair spelled send-asset then receive-asset, as everywhere
else in the API: `corridor="USDC-NGNC"`, `corridor="USDC-KESC"`, and so on for
each corridor in `monitor.DefaultCorridors`. A corridor this process never swept
is absent.

`upstream` — which client made the exchange:

| Value | Client |
|---|---|
| `upstream="horizon"` | the Stellar Horizon path and account queries, the one a measurement cannot happen without |
| `upstream="refrate.exchangerate-api"` | the primary reference-rate feed |
| `upstream="refrate.currency-api"` | the secondary feed that the divergence verdict is drawn against |
| `upstream="refrate.floatrates"` | the third feed, fetched only to attribute a `DISAGREE`, so its series is absent from a process that never disagreed |
| `upstream="checks"` | the counterparty-check client, which reaches Horizon and the anchor's own servers (stellar.toml, SEP-1/6/10/24/31 endpoints); no finer label because one client serves both and a check's findings already name what it probed |

The first four are the pricing path. `checks` is the audit of the counterparty, so
a rise in `kind="transport"` under `upstream="checks"` usually means an anchor's
domain moved or went down rather than that the DEX did.

`kind` on `wayfare_upstream_errors_total` — the HTTP-level shape of the failure:

| Value | Meaning |
|---|---|
| `kind="rate_limited"` | HTTP 429: the request was well-formed and the quota was not. Transient, and the only kind whose remedy is to wait. |
| `kind="client_error"` | any other 4xx. The upstream answered and refused this request specifically, which usually means the URL or the asset is wrong rather than the upstream being ill. |
| `kind="server_error"` | any 5xx. The upstream is unhealthy and no client change fixes it. |
| `kind="transport"` | no HTTP response at all: DNS failure, connection refused, TCP reset. |
| `kind="timeout"` | the exchange was cut off by a deadline, either the client's own timeout or the request context's measurement budget. |

`outcome` on `wayfare_live_request_duration_seconds` — how one request that asked
for a measurement ended. It labels the histogram rather than sitting in a separate
counter so that a 40s timeout and a 0.4s success do not share a bucket:

| Value | Status | Meaning |
|---|---|---|
| `outcome="measured"` | `200` | the ladder priced and the body is that measurement. The duration includes the counterparty checks, because the reader waited through both. |
| `outcome="stale"` | `200` | the measurement failed and the response is the most recent stored run inside a `stale` envelope. The reader got a number; it is not current. |
| `outcome="timeout"` | `504` | the attempt ran out of the budget its rung count was given (10s plus 6s per rung, capped at `-timeout`) with no history to fall back on. |
| `outcome="error"` | `502` | the measurement failed for another reason with no history behind it, so the body is an error envelope. A reader that hangs up mid-request lands here too: the client's cancellation is not an upstream timeout, and on a public endpoint the two have entirely different remedies. |

A request answered before it reaches the engine is not a live request. A
history-first read served from the store, a rejected query parameter and a
refused request never appear in this histogram. On an instance started without
`-history-first`, every `GET /api/corridor` is a live one.

### Histogram boundaries

`wayfare_upstream_request_duration_seconds` covers one HTTP exchange, so its top
bucket sits at the longest timeout a client is given (20s for Horizon, 10s for a
reference feed): `0.05`, `0.1`, `0.25`, `0.5`, `1`, `2.5`, `5`, `10`, `20`, then
`+Inf`. A Horizon path search that takes 8s is normal; one that regularly takes
18s is the story these buckets exist to tell.

`wayfare_live_request_duration_seconds` covers a whole measurement — twelve rungs
at the default sizes, up to the twenty-four `sizes=` accepts, plus the
reference-rate fetch and the checks, all inside the request budget. Its top
bucket is the 90s `-timeout` default, and the sub-second buckets exist because a
corridor served over a warm connection is that fast: `0.25`, `0.5`, `1`, `2.5`,
`5`, `10`, `20`, `40`, `60`, `90`, then `+Inf`.

Both are cumulative, as the format requires, and `le="+Inf"` always equals
`_count`. Durations are in seconds and `_sum` keeps the fraction the example needs:
three Horizon exchanges of 410ms, 95ms and 20s sum to `20.505`.

### What these numbers do not say

- **They reset.** A counter is this process's total since it started, not the
  deployment's. Alert on `rate()` over a window; the raw value makes every
  restart look like a fall to zero.
- **Absence is not zero.** A labelled series that has never been observed is
  missing from the body, which is the same line this project draws between
  unknown and nil. The one exception is `wayfare_rate_limit_rejections_total`,
  which always prints, because an absent line there would be indistinguishable
  from an endpoint that stopped working.
- **A response that arrives and cannot be parsed is invisible here.** Counting
  happens at the HTTP transport, the one layer where every exchange is visible
  with its status and duration intact without touching the pricing path. A body
  that decodes badly fails one layer up and reads as a clean `200` at the
  transport, so it surfaces as `outcome="error"` on a live request whose rungs
  never priced and as `outcome="failed"` in a sweep, and in the log — never as an
  upstream error.
- **Each redirect hop is its own exchange.** The histogram counts HTTP exchanges,
  so a provider that answers 301 and then 200 contributes two observations, and a
  failing hop contributes one `wayfare_upstream_errors_total`.
- **The sweep series need a scheduler.** `-schedule=0` serves HTTP without
  sweeping, and `-once` exits after its sweep without starting a listener, so
  `wayfare_sweep_outcomes_total` only exists in the process that runs the monitor.
- **A scrape costs a request.** `/metrics` passes through the same per-client
  limiter as every other route (see [Rate limiting](#rate-limiting)), so a
  scraper hitting a 4-request/second, burst-10 endpoint from one address will
  eventually be refused, and the refusal is counted in
  `wayfare_rate_limit_rejections_total`. Scrape no faster than the sweep
  interval. With `-rate-limit=0` the limiter is still mounted and the counter
  still prints, but nothing is refused, so a constant `0` there means the limit
  is off rather than that it is never reached.

### Not this endpoint

`docs/metrics.md` and `docs/adding-a-metric.md` describe corridor metrics —
spread, depth, price impact, concentration — which are measurements of a market
and live in the API response and the run store. This endpoint is the operator's
view of the process. They share a word and nothing else.

## `GET /readyz`

Readiness, not liveness. `/healthz` answers "is this process up and how old is
its data". `/readyz` answers the question a platform acts on: **should this
process receive traffic at all?**

A Wayfare process is ready when two things are true:

1. **The history it serves verified at startup.** Opening the run store walks
   every chain and checks each record's seal against the previous one. When the
   binary's embedded history will not verify, the process logs the failure and
   keeps serving from an empty store — which is the right call for liveness and
   the wrong one for traffic routing, because the readings it publishes are no
   longer ones this project will stand behind.
2. **The store answers a read now.** `/readyz` lists the store's corridors on
   every request, so a volume that unmounted or a chain file that became
   unreadable takes the instance out of rotation while it is still serving.

Ready returns `200`, not ready returns `503`, and both are the same document —
a probe asked a valid question and got a complete answer either way. The two
gates are reported separately because they fail for different reasons and a
single boolean would hide which.

**cURL:**
```bash
curl -s https://wayfare-cdb9.onrender.com/readyz
```

**Example Response (200 OK):**

```json
{
  "status": "ready",
  "code_commit": "c7436e2257ae391e6177e063a9012e631540e1d7",
  "data_commit": "05b446626f6f169b927b18822bd08c700f4f80de",
  "checks": {
    "verification": {
      "state": "verified",
      "note": "embedded history verified at open across 4 corridors"
    },
    "store": {
      "state": "readable",
      "corridors": 4
    }
  }
}
```

**Example Response (503 Service Unavailable):**

```json
{
  "status": "not_ready",
  "code_commit": "c7436e2257ae391e6177e063a9012e631540e1d7",
  "data_commit": "05b446626f6f169b927b18822bd08c700f4f80de",
  "checks": {
    "verification": {
      "state": "unverified",
      "note": "embedded history did not open or verify: runstore: USDC-NGNC: runstore: record seq 1 has hash 424b33fcf120… but its contents hash to 3c4afa7aa3ef…; it was modified after it was written"
    },
    "store": {
      "state": "readable",
      "corridors": 0
    }
  }
}
```

What the `checks` block reports in both responses is what a real binary
returned: the 200 from a build of this tree, the 503 after appending text to
the `finding` field of the first record of `data/USDC-NGNC.ndjson` and
rebuilding, so the embedded chain no longer seals. `/healthz` on that same
503-returning process still answered `200`. The two commit fields are the
illustrative provenance used throughout this document.

### Check states

`checks.verification.state`:

| State | Meaning |
|---|---|
| `verified` | Every chain loaded at startup passed verification. |
| `unverified` | Verification was attempted and did not pass, or there was nothing to verify. The process is serving history it cannot vouch for. |
| `unrecorded` | No verdict was recorded, because nothing opened a store. Distinct from `unverified`: "nobody checked" is not "the check failed", and neither is a pass. |

`checks.store.state`:

| State | Meaning |
|---|---|
| `readable` | The store answered `Corridors`, and `corridors` is how many it named. |
| `unreadable` | The read failed; `error` carries why and `corridors` is `null`. |
| `absent` | No run store is configured, so there is nothing to read; `corridors` is `null`. |

`corridors` is `null` rather than `0` whenever the count is unknown, the same
rule `/healthz` applies to its freshness block: an unreadable store is not a
report of an empty one. `code_commit` and `data_commit` are the build
provenance fields described under [`GET /healthz`](#build-provenance-fields).

### What readiness does not check

`/readyz` makes no upstream call and re-walks no chain. A platform polls a
readiness probe hard, and a probe that depends on Horizon goes red when the
network does — which is a different fact about a different thing. Verification
costs a full read of the published history, so it is recorded once at startup
and reported from there; whether the store still answers is the part asked
live.

Point a platform's **readiness** check at `/readyz` and leave its **liveness**
check on `/healthz`. Restarting an instance that has refused traffic tells you
nothing you did not already know from the body, and a chain that fails
verification fails the same way every time the process starts.

## Rate limiting

Every route — including `/healthz` and the UI — passes through a per-client
limiter, because the thing being bounded is the service's cost (upstream
calls, CPU), and that cost is real on every path.

- **Sustained rate:** 4 requests/second per client (`-rate-limit`)
- **Burst:** 10 requests (`-rate-burst`)
- **Client identity:** the connecting peer's host, or the client named in
  `X-Forwarded-For` when the request arrives from loopback or a private-range
  address (the reverse-proxy shape of the hosted deployment). On Render, every
  request arrives from a private-range proxy peer; the key is the address the
  proxy appended to the right. Leftmost entries are client-controlled and
  ignored so spoofed headers cannot bypass the limiter. A directly connected
  public peer cannot choose its own bucket by setting that header.
- **Off switch:** `-rate-limit=0` disables limiting entirely — for an
  operator who already has a limiter in front.

A refused request answers:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1

{"error": "rate limit exceeded; ...", "code": "rate_limited"}
```

`Retry-After` is rounded up to whole seconds so a client that honours it is
never refused twice for the same bucket. The limit is a fairness mechanism,
not a security boundary: it bounds what one client can cost, and does not
authenticate anyone.

## Freshness and provenance

`live` is not a verdict. It describes where the response came from. A response with `live: false` is historical, and its `stale` envelope is authoritative for age. Consumers must not infer freshness from deployment time, request time, or the absence of an error.

Reference rates are never averaged. The response identifies the provider and, when available, the second provider and divergence. On a `DISAGREE` it may also carry `reference_diagnostic`, which names an independent third provider and which of the two primaries is farther from it; that attribution never changes the scored mid. `NO-MARKET` means no path was returned; it is not the same claim as a priced route with an `UNUSABLE` verdict.

## API deprecation policy

Breaking changes to wire shapes without notice break consumers that depend on published measurements. As established in the consumer spike (`docs/spike-api-consumers.md`), the contract's difficulty is not the schema, it is the discipline. This policy defines how endpoints and response fields are phased out.

### Scope of changes

- **Non-breaking (additive):** Introducing optional query parameters or new JSON response fields. Clients must ignore unknown JSON keys.
- **Breaking:** Renaming or removing endpoints, query parameters, or response fields; changing field types; or modifying error code formats. Any breaking change must follow the full deprecation lifecycle below.

### Deprecation lifecycle

1. **Announcement:**
   - Documented in the release notes of the version introducing the deprecation.
   - A dedicated GitHub tracking issue opened under `area:api`.
   - Updated status and timeline recorded in `docs/api.md`.

2. **In-band signalling (`Deprecation` and `Sunset` headers):**
   - Deprecated endpoints emit the standard `Deprecation` response header with the date or Unix timestamp representing when deprecation was announced:
     ```http
     Deprecation: @1760000000
     ```
   - When a definite retirement date is determined, responses also emit the `Sunset` header (RFC 8594) indicating the cutoff date after which the endpoint will be shut down:
     ```http
     Sunset: Wed, 11 Nov 2026 00:00:00 GMT
     ```
   - A `Link` header with `rel="deprecation"` points consumers to migration guidance:
     ```http
     Link: <https://github.com/Wayfare-labs/wayfare/blob/main/docs/api.md>; rel="deprecation"
     ```

3. **Minimum notice window:**
   - **Endpoints and fields have a minimum deprecation window of 90 days** (or at least two minor release cycles, whichever is longer) before removal.
   - During the notice window, the endpoint or field remains fully functional and continues returning real measurements. A deprecated field is never filled with fabricated or estimated numbers: unknown remains reported as unknown.

4. **Removal:**
   - At the sunset date, the deprecated endpoint or field is permanently removed.
   - Requests to removed endpoints return HTTP `410 Gone` with a structured JSON error body:
     ```json
     {
       "error": "this endpoint has been retired; refer to docs/api.md for current endpoints",
       "code": "endpoint_deprecated"
     }
     ```
   - Field removals drop the key from the JSON payload.

## Related contracts

- [Error responses](errors.md) — the machine-readable `code` on every failure
  path, and the shape a client can rely on
- [Cost decomposition](cost-decomposition.md) — the rung `cost` block's
  components, and which are undetermined
- [Reading the API correctly](api-consumer.md) — a worked consumer that
  respects `live`, `scored` and a null `recommended`, with its offline tests
- [Run store](run-store.md) — stored record and hash-chain format
- [Snapshot format](snapshot-format.md) — recorded upstream bytes
- [Checks](checks.md) — tri-state counterparty findings and metrics
- [Contributing](../CONTRIBUTING.md) — invariants for changes
