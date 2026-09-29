# #293 — An error must preserve the page

**Backlog entry H4 (#220) family; issue #293.** Recorded by driving the UI in
a real browser against real servers, not by reading the source.

- **Run:** 2026-09-26T11:58Z (see `ran_at` in the result set). Raw result set:
  `docs/qa/browser/results/error-preservation.json`.
- **Browser:** Chromium 153.0.8010.12 (Playwright), headless.
- **Servers:** two local instances of the branch under test —
  `127.0.0.1:8099` (`-history-first`, embedded history) and
  `127.0.0.1:8098` (empty store, Horizon at a closed port, so a live
  measurement fails with a real `502`).
- **Procedure:** `docs/qa/README.md`. Re-run with
  `cd docs/qa/browser && ./servers.sh start && node run-error-preservation.mjs`.

The contract under test (issue #293): a failed fetch must never empty the
page. Previously `measure()` and `loadTrend()` cleared `#out` *before* the
request, so an error left the reader on an empty page with nothing to
recover. Now the last successful render stays visible with an `role="alert"`
error banner above it, the corridor selection is untouched, and retrying
does not stack banners.

Seventy checks were run — ten scenarios (light and dark, at 320 / 375 / 768
/ 1024 / 1440 CSS px), each walking the same path:

1. **Cold start.** Boot with `/api/corridor` aborted (a browser cannot
   otherwise reach a dead origin): the landing panel must survive *under* the
   banner, and the recovery `<a>reload</a>` link must be keyboard focusable.
2. **Live failure over a good reading.** Reload so the recorded reading
   renders, then make `?live=1` return the **real 502 bytes** from the
   upstream-down instance and click "Measure live" twice: exactly one
   `Could not measure: …` banner above the retained `RECORDED` reading, the
   selection preserved, both buttons re-enabled, the status line cleared.
3. **Trend failure over a good trend.** "Show trend" renders history; abort
   the next trend request: exactly one `Could not load history: …` banner
   above the retained chart, whose size control is still live (the previous
   trend state is no longer dropped on failure).

## Results

All ten scenarios: **7/7 checks passed (70/70 overall)**, in both colour
schemes at every width. The error banner is a `div.panel.err` — the same
panel the rest of the page already uses — so it reflows with the existing
responsive rules; nothing in this change introduces a new layout. The
failure prose names the cause in words (`Could not measure: …`), so the
banner does not rely on its colour alone, and `role="alert"` announces it.

Screenshots of every scenario and state are in
`docs/qa/browser/results/screenshots/` as
`293-<engine>-<scheme>-<width>-{coldstart,measure,trend}.png`.

## Deviation notes

- The two failure causes are produced by a real server (the 502) and by
  `route.abort()` (network failure), the same split `266-error-paths.md`
  documents: only the network-failure class is injected, because a browser
  cannot fetch from a dead origin without aborting the request itself.
- Interceptors are switched by in-script flags rather than `unroute()`:
  Playwright cannot reliably remove a predicate-registered route, and a
  stale interceptor would silently poison later steps.
