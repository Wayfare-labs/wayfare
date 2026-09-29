# #304 — Every state, legible without colour

**Issue #304.** Recorded by driving the UI in a real browser against real
servers, not by reading the source.

- **Run:** 2026-09-26T23:23Z (see `ranAt` in the result set). Raw result set:
  `docs/qa/browser/results/state-legibility.json`.
- **Browser:** Chromium 148.0.7778.179, headless — an installed Google Chrome,
  named with `--executable`, because this machine could not fetch Playwright's
  own browser download. The runner is the same one; only the binary differs.
- **Servers:** two local instances of the branch under test, built from the tree
  this branch is on — a normal one (`-schedule=0 -history-first`, serving the
  UI and the embedded history) and an upstream-down one (empty store, Horizon
  at a closed port, so a live measurement fails with a real `502`). This run
  used ports 8139 and 8108; `servers.sh start` provides the same pair on 8099
  and 8098, which are the defaults.
- **Procedure:** `docs/qa/README.md`. Re-run with
  `cd docs/qa/browser && ./servers.sh start && node run-state-legibility.mjs`.

The contract under test: **no state on the page may be carried by hue alone.**
A verdict was a `.v-good` / `.v-poor` / `.v-unusable` colour class and nothing
else, so the grade went wherever colour went — a greyscale print, a
forced-colors mode, a reader who does not separate those hues. Every grade now
carries a mark, every integrity state a shape, the provenance banner a border
style, and both scrolling tables a way in by keyboard.

## What was measured, and how

`run-state-legibility.mjs` reads `#out` the way a reader without colour would:
for every state element it records the words, marks and shapes next to the
colour the browser resolved, then asserts on the non-colour channels. The
strongest evidence is the forced-colors pass, where **the browser itself drops
every author colour** — anything still told apart there cannot be a hue.

Four corridors were chosen because the states they render are disjoint, and
every one of them is a real response from the running server:

| Corridor | Integrity | What it renders | Also on screen |
|:---|:---|:---|:---|
| NGNC | `DIRECT` | graded rungs | an undetermined check |
| NGNT | `DIRECT` | graded rungs | an undetermined check |
| GHSC | `DERIVATIVE` | every rung `UNUSABLE` | no recommendation is issued |
| KESC | `NO-MARKET` | no rung priced at all | the threshold legend alone |
| all four, cold start | — | — | the `RECORDED … NOT CURRENT MARKET DATA` banner, dashed |

Which of the four grades a rung earns follows the market and moves between
runs, so the runs do not depend on it: the threshold legend prints **all four
grades on every scored corridor**, which is why the four marks are on screen in
every pass whatever the market does. What is fixed per corridor is the
integrity state, the presence of the undetermined check, and GHSC's every-rung
unusable ladder.

## Results

**445 checks across 21 passes. 441 passed.** Both schemes, all five widths
(320 / 375 / 768 / 1024 / 1440).

- Every verdict on screen carries a mark, and the four marks
  (`✓ GOOD`, `≈ FAIR`, `! POOR`, `✕ UNUSABLE`) are distinct — a shared mark
  would make two grades one grade without colour, so that is asserted, not
  just "a mark exists".
- Forced colors: one colour left on the page, four verdicts still told apart by
  their marks and words. The premise of the pass is recorded per render rather
  than assumed.
- Check states `✓ PASS` / `× FAIL` / `? UNKNOWN` and the `? UNDETERMINED`
  metric chip each keep their own glyph; the three integrity states are
  `◆ DIRECT` / `↪ DERIVATIVE` / `∅ NO-MARKET`.
- Provenance: `border-top-style` measures `solid` on a live reading and
  `dashed` on a recorded one, so "is this current?" is answerable from the
  shape of the box before the words are read.
- The trend key draws the plot's own shapes and is announced as a named group
  (`role="group"`, `aria-label="Integrity state key"`) — a label on a plain
  `div` is inert, so the key names itself in the source *and* has a role that
  can hold the name.
- Geometry: the mark does not overlap the grade, sits within 5px of the same
  line, and stays inside its cell, at every width. The first run failed this at
  320 and 375 — the verdict column was 86px wide there, so the mark and the
  grade wrapped onto separate lines a line apart — which is why the cell now
  holds them together with `white-space: nowrap`. The table already scrolls
  sideways at those widths; it now scrolls 24px further.
- Keyboard: both tables are `tabindex="0" role="region"` with names, and Tab
  from the first control reaches the measurements table in 3 stops, after which
  the arrow keys scroll it 80px of its 258px at 320px. At 320px the verdict
  column is past the right edge, so this is the difference between a grade a
  keyboard can read and one it cannot.
- The error banner is exercised against the real 502 from the upstream-down
  instance: one `role="alert"` naming the failure in words.

Screenshots are in `docs/qa/browser/results/screenshots/` as
`304-<corridor>-<scheme>-1024-{recorded,live,trend}.png`,
`304-widths-<scheme>-<width>-{recorded,trend}.png` and
`304-forced-<corridor>-1024-live.png`.

## The four failures

All four are the same pre-existing check — *the page does not scroll sideways* —
at 320px and 375px, in both schemes. The cause is the finding-state legend,
which keeps three columns at those widths, so `Could not determine` overhangs
the viewport by 35px and 17px. **It is not caused by this change**: a build of
the commit this branch starts from, run the same way over the same embedded
history, overflows by the same 35px and 17px and names the same element.
`results/state-legibility.json` records the offending element in
`overflowOffenders` on each of the four passes. Recorded as a finding rather
than fixed: a responsive `.legend-grid` is its own change, and a harness that
quietly stops asserting this would stop being evidence.

## Scope limits

- **Chromium only.** No Firefox or WebKit binary was available on this machine.
  The marks are text and the shapes are SVG geometry, both engine-independent,
  but that is reasoning, not a measurement.
- **The metrics panel was not exercised**, and could not be. No code path wires
  a `DepthMetric` or `DeviationMetric` into a corridor response today, so the
  `UNDETERMINED` metric state is unreachable from any API response. The `?` was
  added to the metric-state chip and `server/ui_test.go` pins it; the runner
  asserts metric states only when it finds them, and found none.
- **Print stylesheets and `prefers-contrast` are not exercised** here.
  `272-color-schemes.md` already records those as open (C1–C4).
- `DERIVATIVE` was verified on a plotted dot, on GHSC. `NO-MARKET` could not be:
  KESC has no priced rungs, so its plot has nothing to mark. Its square is
  verified where it does appear — the key draws all three shapes on every
  corridor, and `server/ui_test.go` pins both branches of `dotMark`.
- Console errors recorded on every pass are the Cloudflare challenge-platform
  404 that `269-cross-browser.md` already filed as an adjacent finding, plus
  the expected 502s in the error-banner pass. Nothing from this change.
