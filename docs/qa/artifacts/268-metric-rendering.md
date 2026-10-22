# #268 — Metric rendering, including undetermined metrics

## Outcome

The isolated metric renderer reports an undetermined metric as **UNDETERMINED**
with its reason, separately from a failed check's **FAIL** badge. Determined
metrics retain their value and unit, use neutral ink, and expose their evidence
source and observation timestamp. Empty and omitted metric arrays produce no
metrics panel. These observations passed in Chromium at 1440px and 375px, in
light and dark colour schemes.

**Normal UI navigation did not initialize.** This is a negative QA finding,
not a claim that the application passed end-to-end. Both defects were filed
separately, with reproduction steps, and neither is fixed by this artifact:

- [#831 — CRLF HTML causes CSP to reject both inline scripts](https://github.com/Wayfare-labs/wayfare/issues/831).
- [#832 — Missing theme element aborts corridor initialization](https://github.com/Wayfare-labs/wayfare/issues/832).

## Evidence and limits

- Assessed product source: `24f3cc04bb9b3c52067b378907cf89ef9c1dd691`.
- Runner: [`../browser/run-metric-rendering.mjs`](../browser/run-metric-rendering.mjs).
- Canonical LF run: [`../browser/results/metric-rendering.json`](../browser/results/metric-rendering.json).
- Windows CRLF run: [`../browser/results/metric-rendering-crlf.json`](../browser/results/metric-rendering-crlf.json).
- Each result records its UTC `ranAt`, browser version, harness SHA-256, served
  HTML SHA-256, response CSP, console errors, per-check outcomes, and screenshots.
- Environment: Windows, Go 1.27.3, Node 24.21.0, Chromium 153.0.8010.12 using the
  repository's pinned browser-QA dependency. Only Chromium was exercised; no
  physical phone or assistive-technology validation is claimed.
- Local read-only servers: `http://127.0.0.1:8268/` for the CRLF build and
  `http://127.0.0.1:8269/` for the canonical LF build. Both use
  `-schedule=0 -history-first`; no live-market request was made.
- The prerequisite [#94](https://github.com/Wayfare-labs/wayfare/issues/94) is
  closed; the production renderer exists. The embedded history examined here
  contains no metrics. That limits what the recorded API can establish.

The runner **does not inject metric data into an API response or a run record**.
It first requests actual recorded corridor responses, then calls the existing
`findings()` function against their findings block, using the production
caller's absent-findings guard. Because startup failed, this is a renderer
observation, not evidence that selector-driven navigation worked.

The other passes replace only the browser's `#out` contents with the production
renderer output for named schema fixtures. No product file is patched. The
determined/undetermined entries are copied from
`server/api_test.go:metricsSchemaFixture`; the failed comparison check is
explicitly synthetic. The fixture's `0.0004 ratio`, `5000 USDC` reason, source
label `https://horizon.stellar.org/order_book`, and
`2026-08-21T12:00:00Z` timestamp are **test data, not a market observation**.
That Horizon endpoint was not contacted. The API observation timestamp and
endpoint for each real response are recorded independently in the JSON.

## Recorded results

| Coverage | Observed result |
| --- | --- |
| Five fixture states × two schemes × two widths | 20 renderer-only passes; 148 checks passed |
| Four recorded API responses | HTTP 200; no metrics; eight response/absence checks passed |
| LF application bootstrap | Renderer declaration available, selector empty; one of two checks failed; one uncaught missing-theme exception |
| CRLF application bootstrap | Neither renderer nor selector initialized; two checks failed; two CSP console errors; metric cases not run |
| LF total | 157 passed, one failed, one page error; **not** a green end-to-end result |

The fixture states are metrics omitted, an empty metrics array, determined
only, undetermined only, and a mixed list next to a failed check. The mixed
pass verifies that the unknown metric differs from failure in both its word
and computed colour. Each metric is counted once. Measured values preserve
their source strings and units (CSS displays `ratio` as `RATIO`), have no
pass/fail/verdict classes, and use the neutral ink colour. Undetermined entries
have no fabricated numeric value or unit. Identifier/timestamp, evidence
source/timestamp where supplied, and viewport fit are also checked.

Settled screenshots (the runner waits for the 240ms panel entrance animation):

- [Light, 1440px](../browser/results/screenshots/268-metrics-light-1440.png)
- [Light, 375px](../browser/results/screenshots/268-metrics-light-375.png)
- [Dark, 1440px](../browser/results/screenshots/268-metrics-dark-1440.png)
- [Dark, 375px](../browser/results/screenshots/268-metrics-dark-375.png)
- [LF startup failure](../browser/results/screenshots/268-bootstrap-lf.png)
- [CRLF startup failure](../browser/results/screenshots/268-bootstrap-crlf.png)

## Repeat the procedure

Use a checkout with canonical LF `server/index.html` for the main matrix.
Go 1.27.3 is required by the current `go.mod`; the older Go requirement in the
general QA setup page does not describe this revision. On a Windows clone,
`git ls-files --eol server/index.html` distinguishes `w/lf` from `w/crlf`.
The LF control build used the exact bytes from
`git show HEAD:server/index.html`, verified before replacement; it introduces
no committed product change.

For a POSIX environment with the required browser system libraries:

```sh
(cd docs/qa/browser && npm ci --ignore-scripts && npx playwright install chromium)
GOMAXPROCS=2 go build -p 1 -o /tmp/wayfare-268 ./cmd/wayfared
/tmp/wayfare-268 -schedule=0 -history-first -addr=127.0.0.1:8269 > /tmp/wayfare-268.log 2>&1 &
task_server_pid=$!
trap 'kill "$task_server_pid"' EXIT
node docs/qa/browser/run-metric-rendering.mjs --base=http://127.0.0.1:8269
```

Wait for `/healthz` to return 200 before running the browser command. On Windows,
build an `.exe` and start it in a second terminal with the same flags; the Node
runner is platform-independent. For the second result, build a CRLF checkout
as described in #831, serve it on port 8268, and run:

```sh
node docs/qa/browser/run-metric-rendering.mjs --base=http://127.0.0.1:8268 --result=metric-rendering-crlf.json
```

The runner records failures rather than failing the shell, consistent with the
existing QA harness. Read `summary`, `bootstrap`, `errors`, `executionError`
(when present), and every false check. Operational failures must not be read
as a completed matrix. Screenshots and JSON are regenerated by the runner;
source/harness hashes make the revision behind a rerun explicit.

## Other verification

Both server builds and `node --check docs/qa/browser/run-metric-rendering.mjs`
passed. The initial parallel Go build exhausted Windows paging space; the
bounded `GOMAXPROCS=2 go build -p 1` succeeded. An attempted
`GOMAXPROCS=2 go test -p 1 ./...` could not complete because the disk filled.
Only this task's failed temporary build directory was removed. No passing Go
test-suite result is claimed. Race, lint, and network-namespace offline checks
were not completed in this environment. Runtime QA used only loopback traffic;
that is not equivalent to a full test suite under a network blackout.
