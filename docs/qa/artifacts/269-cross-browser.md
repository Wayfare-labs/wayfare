# #269 — Cross-browser QA matrix

**Backlog entry H3 (#211).** Recorded results, not impressions.

- **Run:** 2026-09-23T17:34:47Z. Raw result set: `docs/qa/browser/results/cross-browser.json`.
- **Target:** `http://127.0.0.1:8099/`, the branch under test with `-history-first`.
- **Procedure:** `docs/qa/README.md`. Re-run with
  `cd docs/qa/browser && ./servers.sh start && node run-cross-browser.mjs`.

## The engines, and what they stand in for

The issue asks for Chrome, Firefox, Safari and Edge, current and one prior
major. What this environment can actually run is four engines short of that, and
the gap is recorded rather than glossed over.

| Ran | Version | Stands in for | Honest caveat |
|:---|:---|:---|:---|
| Chromium | 153.0.8010.12 | Google Chrome, Microsoft Edge | Chromium is Chrome's and Edge's engine. Edge adds its own UI chrome (title bar, menu, default fonts) but renders this page with the same engine. **No Edge binary was available and no Edge result is claimed.** |
| Firefox | 155.0 | Mozilla Firefox | Directly the engine in question. |
| WebKit | 26.6 | Apple Safari | WebKit is Safari's engine, but Playwright's WebKit build is not Safari and does not run on macOS. Safari-specific behaviour — font rendering, scrollbar styling, system-link handling — **cannot be concluded from these results**. |

**Not tested at all:** Microsoft Edge, Apple Safari on macOS, and any *prior*
major release of any engine (no older builds were installed). Those cells of the
matrix are empty, not passing.

Chrome's own version cannot be stated separately: the run is Chromium 153, and
the corresponding Chrome stable channel was not checked.

## Results

Seven checks per engine. Same result everywhere:

| Check | Chromium 153 | Firefox 155 | WebKit 26.6 |
|:---|:---|:---|:---|
| Renders a corridor on load | PASS | PASS | PASS |
| Corridor selector populated | **FAIL** | **FAIL** | **FAIL** |
| Integrity badge with state + colour | PASS | PASS | PASS |
| Measurements table with rows and headers | PASS | PASS | PASS |
| Page does not scroll horizontally | PASS | PASS | PASS |
| Findings panel matches the response contract | PASS | PASS | PASS |
| Trend view renders chart + stored-runs table | PASS | PASS | PASS |
| **Total** | 6/7 | 6/7 | 6/7 |

The single failure is identical on all three engines and is a code defect, not a
rendering difference: `/api/assets` is never requested, so the selector never
leaves its placeholder (`Loading corridors…`). See `266-error-paths.md`, finding 2.

## Where the engines do differ

One real difference was observed, on the network-failure error path. The
message shown to the user is the browser's own `TypeError` text, so the same
cause reads differently depending on the browser:

| Engine | Rendered text for a network failure on "Measure live" |
|:---|:---|
| Chromium | `Could not measure: Failed to fetch` |
| Firefox | `Could not measure: NetworkError when attempting to fetch resource.` |
| WebKit | `Could not measure: Load failed` |

The unknown-asset (application-generated) message is byte-identical across all
three, which is the expected behaviour: only the browser-authored text varies.
Screenshots: `docs/qa/browser/results/screenshots/chromium-cross-browser.png`,
`firefox-cross-browser.png`, `webkit-cross-browser.png`.

## Dark colour scheme

`prefers-color-scheme: dark` was emulated and the computed colours recorded. All
three engines resolved the same values — identical to the last digit, which is
what the shared CSS variable block predicts:

| Property | Chromium | Firefox | WebKit |
|:---|:---|:---|:---|
| `body` background | `rgb(22, 24, 21)` | `rgb(22, 24, 21)` | `rgb(22, 24, 21)` |
| `body` colour | `rgb(236, 234, 228)` | `rgb(236, 234, 228)` | `rgb(236, 234, 228)` |
| Badge colour | `rgb(127, 184, 164)` | `rgb(127, 184, 164)` | `rgb(127, 184, 164)` |

Screenshot: `docs/qa/browser/results/screenshots/dark-mode.png` (Chromium).

## Console noise present in every engine

Every page load in every engine logs errors that are not request failures. In
Chromium the count was 8, in WebKit 7, in Firefox 3. The material one:

```
/cdn-cgi/challenge-platform/scripts/jsd/main.js  →  404
Refused to execute script ... because its MIME type ('text/plain') is not executable
```

The trailing script that requests it is committed in `server/index.html`
(line 1019). Filed separately.

## Reproducing

```bash
cd docs/qa/browser && ./servers.sh start && node run-cross-browser.mjs
# or one engine:
node run-cross-browser.mjs --engines=webkit
```

The script prints a `PASS`/`FAIL` line per check per engine and writes the full
observations — including the raw DOM text, the computed colours, and each
engine's console output — to `docs/qa/browser/results/cross-browser.json`.
