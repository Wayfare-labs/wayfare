# #266 — QA every error path in the browser

**Backlog entry H2 (#208).** Recorded by driving the UI in a real browser
against a real server, not by reading the source.

- **Run:** 2026-09-23T17:34:08Z. Raw result set: `docs/qa/browser/results/error-paths.json`.
- **Browser:** Chromium 153.0.8010.12 (Playwright), headless.
- **Servers:** three local instances of the branch under test —
  `127.0.0.1:8099` (`-history-first`, embedded history),
  `127.0.0.1:8098` (empty store, Horizon at a closed port),
  `127.0.0.1:8097` (empty store, Horizon black-holed, `-timeout=2s`).
- **Procedure:** `docs/qa/README.md`. Re-run with
  `cd docs/qa/browser && ./servers.sh start && node run-error-paths.mjs`.

Fifteen scenarios were run, each in a fresh page, and each one's rendered DOM and
the exact HTTP response the browser received were recorded.

## What the UI actually does

The UI has four places that can show an error, and they are not one panel:

| Surface | Trigger | Prefix rendered |
|:---|:---|:---|
| Initial load | `boot()` fetches `/api/corridor?to=<selected>` | `Could not load corridor data: …` |
| Measure live | `#run` fetches `/api/corridor?to=…&live=1` | `Could not measure: …` |
| Show trend | `#trend` fetches `/api/corridor/trend?to=…` | `Could not load history: …` |
| Corridor selector | `loadAssets()` would fetch `/api/assets` | — never reached, see below |

Every error on every surface is rendered as **the same element**: a `div` with
class `panel err`, containing the prefix plus the server's `error` prose. Across
the fifteen scenarios the harness observed **one** distinct panel class
(`panel err`) and eleven distinct prose strings.

## The matrix

| Cause | Surface | HTTP | Rendered text (verbatim) |
|:---|:---|:---|:---|
| network failure | initial load | — (aborted) | `Could not load corridor data: Failed to fetch. If the instance is still waking up, wait a moment and reload.` |
| upstream failure | initial load | 502 | `Could not load corridor data: measuring corridor: no size could be measured; every request failed to reach an upstream. If the instance is still waking up, wait a moment and reload.` |
| upstream timeout | initial load | 504 | `Could not load corridor data: measuring corridor: context deadline exceeded. If the instance is still waking up, wait a moment and reload.` |
| network failure | Measure live | — (aborted) | `Could not measure: Failed to fetch` |
| upstream failure | Measure live | 502 | `Could not measure: measuring corridor: no size could be measured; every request failed to reach an upstream` |
| upstream timeout | Measure live | 504 | `Could not measure: measuring corridor: context deadline exceeded` |
| unknown asset | Measure live | 400 | `Could not measure: unknown receive asset "SCAMC"; verified assets are EURMTL, GHSC, KESC, NGNC, NGNT, PYUSD, USDC, USDZ, ZARZ` |
| malformed size | Measure live | 400 | `Could not measure: bad size "abc": not a number` |
| unknown query parameter | Measure live | 400 | `Could not measure: unknown query parameter(s): "tp"; supported parameters are from, to, sizes, live, pretty` |
| server error | Measure live | 500 | `Could not measure: listing stored corridors: read /data: input/output error` |
| network failure | Show trend | — (aborted) | `Could not load history: Failed to fetch` |
| no stored runs | Show trend | 200 | *not an error* — the "no stored runs" panel renders |
| server error | corridor selector | 500 | **not rendered** — see below |
| network failure | corridor selector | — (aborted) | **not rendered** — see below |

How each row was produced is in `docs/qa/browser/results/error-paths.json` under
each run's
`how`, `injected` and `api` fields. The 502 and 504 rows are real responses from
the two degraded instances; only the abort rows are injected, because a browser
cannot be made to fetch from an origin that is not listening.

## Findings

**1. The machine-readable `code` never reaches the screen.** `server/api.go`
publishes `{"error": "…", "code": "unknown_receive_asset"}` and the UI discards
the code. In the recorded DOM, no error panel mentions it. This is the finding
the issue predicted: the causes are distinguished only by English prose, so a
user cannot tell "the corridor does not exist" from "Horizon is down" without
reading it.

**2. Two of the four error surfaces are unreachable, because
`loadAssets()` is never called.** `server/index.html` defines `loadAssets()`
(line 316), `buildSelector()` (331) and `updateCorridorDetail()` (347) but never
invokes `loadAssets`, and adds no `DOMContentLoaded` hook that would. In the
recorded run, **`/api/assets` was never requested by any page load** — the
browser recorded zero calls to it. Consequences, all observed:

- the corridor selector is stuck on its placeholder (`Loading corridors…`) in
  every scenario and on every engine;
- the whole `/api/assets` error path — including the `Could not load corridors`
  fallback — is dead code;
- `boot()` requests `/api/corridor?to=` with an empty `to`, so the page measures
  the server's default corridor regardless.

Filed separately, with reproduction steps.

**3. The boot error tells the user to do something that cannot help.** All three
boot failures append *"If the instance is still waking up, wait a moment and
reload."* That advice is written for a sleeping free-tier instance. It is shown
identically for a 502, a 504 and a transport failure, where reloading is not the
remedy. The recorded text is identical apart from the cause clause.

**4. No JavaScript error occurs on any error path.** `pageErrors` was empty in
all fifteen scenarios. The error handling itself is sound: it always reaches a
panel and never leaves the loading state spinning.

**5. Every page load logs a browser-console error unrelated to the request.**
`/cdn-cgi/challenge-platform/scripts/jsd/main.js` is requested by the trailing
script embedded in `server/index.html` (line 1019, carried over from the
deployed page). It 404s on any instance that is not behind Cloudflare, and the
browser refuses it:

```
Refused to execute script from 'http://127.0.0.1:8099/cdn-cgi/challenge-platform/scripts/jsd/main.js'
because its MIME type ('text/plain') is not executable, and strict MIME type checking is enabled.
```

Filed separately.

## Reproducing a row by hand

```bash
cd docs/qa/browser && ./servers.sh start

# A real 502 and a real 504, no browser needed:
curl -s "http://127.0.0.1:8098/api/corridor?to=NGNC&live=1"; echo   # 502 measurement_failed
curl -s "http://127.0.0.1:8097/api/corridor?to=NGNC&live=1"; echo   # 504 upstream_timeout

# The 400s, from the normal instance:
curl -s "http://127.0.0.1:8099/api/corridor?to=SCAMC"              # 400 unknown_receive_asset
curl -s "http://127.0.0.1:8099/api/corridor?to=NGNC&sizes=abc"     # 400 invalid_sizes
curl -s "http://127.0.0.1:8099/api/corridor?tp=NGNC"               # 400 invalid_query
```

Then open `http://127.0.0.1:8099/` in any browser for the network-failure row:
stop the server and reload, or throttle/block the request in the devtools network
panel. The panel text is the first row of the matrix.

## What this artifact does not claim

- Only Chromium was used for the error-path pass. Whether the same panels render
  in Firefox and WebKit is `269-cross-browser.md`.
- This is the branch under test, not the deployed instance. The deployed
  instance is an older build: its errors carry no `code` field at all and it
  ignores unknown query parameters, so rows 9 and 10 of the matrix cannot occur
  there. See [`docs/api.md`](../api.md).
