# QA the API from a consumer's perspective — Issue #260

**Target:** https://wayfare-cdb9.onrender.com/ (Render free instance)
**Tested by:** chiprime, branch `verify-claims`
**Timestamp:** 2026-09-26T01:26:56Z – 2026-09-26T01:27:05Z (all UTC)
**Endpoints:** every endpoint and parameter in [`docs/api.md`](../../api.md):
`GET /`, `GET /healthz`, `GET /api/assets`, `GET /api/corridor`,
`GET /api/corridor/trend`

## What the issue asks

Every documented endpoint and parameter, with the responses recorded as
fixtures for later comparison.

## Method (repeatable)

```bash
node docs/qa/api/run-endpoints.mjs
```

The script issues 37 cases — every endpoint and parameter `docs/api.md`
documents, plus the method and error paths a consumer hits — and records each
response's status, headers, wall time and raw body in
[`../api/results/endpoints.json`](../api/results/endpoints.json). `expected` in
that file is what `docs/api.md` leads a caller to expect; a case that returns
something else is recorded as a mismatch and printed at the end. No fixture was
hand-edited.

## Result

**34 of 37 cases matched the documented expectation.** The three mismatches are
all the same finding: `/api/assets` and `/healthz` do not reject non-GET
methods.

| Case | Method + path | Expected | Observed |
|:---|:---|:---|:---|
| `post-assets` | `POST /api/assets` | 405 | **200** |
| `post-healthz` | `POST /healthz` | 405 | **200** |
| `put-healthz` | `PUT /healthz` | 405 | **200** |

Every documented positive path, every documented error path, and the strict
query handling all behaved as documented. The complete per-case fixtures are in
the result file; the raw bodies are quoted in the findings below.

## Finding 1 (mismatch): `/assets` and `/healthz` accept any HTTP method

`docs/api.md` says, under "Endpoints": *"Unsupported methods return `405`."*
Two endpoints do not enforce that.

**Reproduce:**

```bash
BASE=https://wayfare-cdb9.onrender.com
curl -s -o /dev/null -w "POST /api/assets -> %{http_code}\n" -X POST "$BASE/api/assets"
curl -s -o /dev/null -w "POST /healthz   -> %{http_code}\n" -X POST "$BASE/healthz"
curl -s -o /dev/null -w "PUT  /healthz   -> %{http_code}\n" -X PUT  "$BASE/healthz"
```

Observed at 2026-09-26T01:26Z: `200`, `200`, `200`, each with a full JSON body.

**Why it happens:** `handleAssets` and `handleHealth` (`server/api.go`) call
`checkParams` but never test `r.Method`, while `handleCorridor` and
`handleTrend` both return 405 first. The service is read-only, so a `POST` does
not mutate anything today — but the documented contract and the implementation
disagree, and a client that probes method support gets the wrong answer. Per
the issue constraints this is **not fixed here**; see "Filed separately".

## Finding 2: error `code` casing differs between the two JSON data endpoints

`docs/api.md` says errors have the shape `{ "error": "..." }` and does not list
the machine-readable `code` values; the code comments in `server/api.go` say
clients should switch on `code`. They cannot switch on one spelling:

| Endpoint | Condition | `code` observed |
|:---|:---|:---|
| `/api/corridor` | unknown receive asset | `unknown_receive_asset` |
| `/api/corridor/trend` | unknown receive asset | `UNKNOWN_ASSET` |
| `/api/corridor` | bad size | `invalid_sizes` |
| `/api/corridor/trend` | bad limit | `BAD_LIMIT` |
| `/api/corridor` | unknown query parameter | `invalid_query` |
| `/api/corridor/trend` | unknown query parameter | `INVALID_QUERY_PARAM` |
| `/api/corridor` | unsupported method | `method_not_allowed` |
| `/api/corridor/trend` | unsupported method | `METHOD_NOT_ALLOWED` |

**Reproduce:**

```bash
curl -s "$BASE/api/corridor?to=NOPE" | python3 -c 'import sys,json;print(json.load(sys.stdin)["code"])'
curl -s "$BASE/api/corridor/trend?to=NOPE" | python3 -m json.tool
```

Observed at 2026-09-26T01:26Z: `unknown_receive_asset` vs `UNKNOWN_ASSET`; the
same paragraph in `server/api.go` and `server/trend.go` produces the two
spellings. `handleTrend` writes uppercase literals (`server/trend.go`), while
`handleCorridor` uses the lowercase `code*` constants.

## Finding 3 (minor): undocumented behaviour a consumer meets

These did not fail a case because they are additive, but they are absent from
`docs/api.md` and a consumer will meet them:

- **`pretty` is implemented on every JSON endpoint** and changes the body to
  indented JSON; it is not documented. Cases `corridor-pretty`,
  `assets-pretty`, `healthz-pretty` returned indented bodies.
- **`/healthz` returns a `data` block**, one entry per stored corridor with
  `recorded_at`, `age_seconds` and `age_human`; `docs/api.md` shows the body as
  `{ "status": "ok" }` only. Observed at 2026-09-26T01:26Z:
  `{"data":{"USDC-GHSC":{"recorded_at":"2026-08-22T12:10:05Z",...}},"status":"ok"}`.
- **CORS preflight is implemented** (`OPTIONS /api/corridor` → `204` with
  `Access-Control-Allow-Methods: GET, HEAD, OPTIONS`); `docs/api.md` does not
  mention it.
- **In history-first mode `sizes` has no effect.** `GET
  /api/corridor?to=NGNC&sizes=10,100` returned the stored 12-rung run in 28ms,
  not a two-size measurement. That is the documented history-first behaviour
  ("answer from the covered history"), but `sizes` is documented without a note
  that stored history ignores it, and a consumer could read the returned rungs
  as the sizes they asked for.

## Anything broken

No functional breakage beyond Finding 1's documentation/implementation
mismatch. Every endpoint returned well-formed JSON, every error carried a
`code`, and no request errored unexpectedly. The two findings above change no
verdict threshold, integrity semantics, check composition or run-record layout.

## Filed separately

Per the issue constraints, the findings are not fixed here. Ready-to-file issue
bodies, with the reproduction steps above, are reproduced in the pull request
that lands this artifact; the method-handling mismatch (Finding 1) and the code
casing mismatch (Finding 2) are the two candidates.

## Timestamps and endpoints

| Item | Timestamp (UTC) | Endpoint / command |
|:---|:---|:---|
| Run started | 2026-09-26T01:26:56Z | `node docs/qa/api/run-endpoints.mjs` |
| `POST /api/assets` → 200 | 2026-09-26T01:27Z | `POST /api/assets` |
| `POST /healthz` → 200 | 2026-09-26T01:27Z | `POST /healthz` |
| `PUT /healthz` → 200 | 2026-09-26T01:27Z | `PUT /healthz` |
| Code-casing pair | 2026-09-26T01:26Z | `GET /api/corridor?to=NOPE`, `GET /api/corridor/trend?to=NOPE` |
| Full recorded set | 2026-09-26T01:27:05Z | `docs/qa/api/results/endpoints.json` |
