# Browser QA

Reusable browser QA for the Wayfare UI. Backlog section **H — Cross-cutting: QA
and reproducibility**: issues
[#266](https://github.com/Wayfare-labs/wayfare/issues/266) (every error path),
[#269](https://github.com/Wayfare-labs/wayfare/issues/269) (cross-browser),
[#270](https://github.com/Wayfare-labs/wayfare/issues/270) (mobile and touch)
and [#272](https://github.com/Wayfare-labs/wayfare/issues/272) (both colour
schemes).

Everything here drives the **real server binary** and a **real browser engine**.
Nothing is a mock of the product: the responses the UI renders are the bytes
`server/api.go` actually produced, and the rendering is a browser's.

Recorded results live in [`browser/results/`](browser/results/), and the written
findings in:

| Issue | Written artifact |
|:---|:---|
| #266 error paths in the browser | [`artifacts/266-error-paths.md`](artifacts/266-error-paths.md) |
| #269 cross-browser matrix | [`artifacts/269-cross-browser.md`](artifacts/269-cross-browser.md) |
| #270 mobile device QA | [`artifacts/270-mobile.md`](artifacts/270-mobile.md) |
| #272 QA both colour schemes | [`artifacts/272-color-schemes.md`](artifacts/272-color-schemes.md) |

## Setup

Go 1.22+, Node 18+ and the ability to download browser binaries.

```bash
cd docs/qa/browser
npm install
npx playwright install --with-deps chromium firefox webkit
```

`npx playwright install --with-deps` needs root (or `sudo`) because it installs
the system libraries each engine links against. On a machine that already has
them, plain `npx playwright install chromium firefox webkit` is enough.

## Run

Three server instances are started, because the causes the UI has to display
come from the server's own behaviour rather than from a stand-in:

| Port | Role | What it produces |
|:---|:---|:---|
| 8099 | `-history-first` over the embedded history | serves the UI and the deterministic 400s, with no network access |
| 8098 | empty store, `-horizon` pointed at a closed port | a real `502 measurement_failed` |
| 8097 | empty store, `-horizon` black-holed, `-timeout=2s` | a real `504 upstream_timeout` |

```bash
cd docs/qa/browser
./servers.sh start
node run-error-paths.mjs --engine=chromium,firefox,webkit   # #266
node run-cross-browser.mjs                                  # #269
node run-mobile.mjs                                         # #270
node run-color-schemes.mjs                                  # #272
./servers.sh stop
```

Each script writes `results/<name>.json` and screenshots into
`results/screenshots/`. The scripts do not fail the shell on a failed check —
they record it, because a recorded failure is the deliverable. Read the summary
each one prints.

Flags: `--base=URL`, `--upstream-down=URL`, `--upstream-slow=URL`,
`--engine=`/`--engines=` (comma separated), `--devices=` (mobile only).

## Scope limits, stated up front

These are properties of the environment, not of the harness, and the artifacts
repeat them where they apply:

- **Chromium is not Microsoft Edge and WebKit is not Safari.** They share
  rendering engines, which is what matters for a static page, but no Edge binary
  and  no macOS Safari were available. See `artifacts/269-cross-browser.md`.
- **No physical mobile device was available.** Every mobile result is device
  emulation, which can speak to layout and to whether a touch gesture reaches the
  right element, and cannot speak to momentum, overscroll or real font metrics.
  See `artifacts/270-mobile.md`.
- **`display: none` is not exercised.** The UI has no such states today.
- **#272 runs Chromium only.** The colour variables are engine-independent CSS
  values, but only one engine was measured; see `artifacts/272-color-schemes.md`.
- The harness never writes to the repository under test; it only reads the UI and
  the API.
