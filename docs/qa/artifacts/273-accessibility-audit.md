# #273 — WCAG 2.2 AA audit of the corridor UI

**Backlog entry H3 (#214).** This records how the shipped single-page UI
measures against WCAG 2.2 Level AA: which success criteria it meets, which it
fails, and which could not be judged from this environment. It is an **audit
artifact, not a fix.** Per the issue, every failure below is a finding to be
filed separately with reproduction steps; nothing here is corrected in this
change, and no finding is asserted as passing without being observed.

- **Run:** 2026-09-26, contrast computed offline from the source stylesheet.
- **Subject:** `server/index.html`, SHA-256
  `d6f53f6506fe10603ab19232a6e00f8a24e354c23af0f211bbc09bc447988cb8`
  (1,224 lines, 55,624 bytes) at commit `1992ecc`, served at `GET /` by
  `uiHandler` (`server/ui.go`).
- **Method for colour:** `node docs/qa/browser/contrast.mjs` reads the actual
  custom-property values out of the stylesheet, resolves every `var()` chain to
  a literal hex (including the layered primitives the token refactor introduced),
  composites each translucent fill over its real background, and applies the
  WCAG relative-luminance formula. Thresholds: **4.5:1 normal text** (SC 1.4.3),
  **3:1 non-text** (SC 1.4.11). Raw output: `docs/qa/browser/results/contrast.json`.
- **Method for semantics:** source inspection of the same file (grep for the
  ARIA/feature under test), not a running browser.
- **Not run here:** the Playwright engine harness and any assistive
  technology. See *What this audit could not observe* — those criteria are
  marked **not observed**, never **pass**.

## Findings by success criterion

### Passed

| SC | Criterion | Evidence |
|:---|:---|:---|
| 3.1.1 | Language of Page | `<html lang="en">` is present. |
| 1.3.1 | Info and Relationships | Measurements are a real `<table>` with `<th>` headers; both SVG charts carry `role="img"` and an `aria-label` (`server/index.html:680`, `:1152`). |
| 1.4.1 | Use of Color | Integrity states carry a distinct glyph *and* the word (`◆ DIRECT`, `↪ DERIVATIVE`, `∅ NO-MARKET`); check findings carry `✓`/`×`/`?` beside PASS/FAIL/UNKNOWN. Colour is reinforcement, never the sole signal. |
| 4.1.2 | Name, Role, Value | The corridor `<select>` and both `<button>`s are native elements with accessible names (`server/index.html:325`, `:329`, `:330`). |
| 1.4.10 | Reflow — *not asserted* | See *could not observe*; the page is single-column with `max-width: 900px` and scrolls tables horizontally, which is consistent with reflow but was not measured at 320px here. |

Text contrast that **passes** at 4.5:1 in both schemes (light / dark): body ink
17.43 / 13.53; muted (`.sub`, `.meta`, `th`, `.axis`) 5.55 / 5.57; ok-on-panel
`.v-good`/`.v-fair`/`.rec-some` 5.91 / 7.20; bad-on-panel `.err`/`.rec-none`/
`.v-unusable` 6.51 / 6.30; `✓`/`×` finding states on their soft fills 5.24/5.67
(light) and 5.64/5.35 (dark); `?`/UNDETERMINED neutral 4.76 / 7.04; the new
empty-state glyph (non-text, needs 3:1) 5.38 / 8.25.

### Failed — findings

**F-1 · SC 1.4.3 Contrast (Minimum) — light scheme, warn-coloured text.**
Three elements set body-weight text in `--warn` (`#96712a`) on a light
background and land under 4.5:1:

| Element | Foreground on background | Light | Dark |
|:---|:---|:---|:---|
| `.v-poor`, `.cs-derivative` | `--warn` on `--panel` `#ffffff` | **4.48 FAIL** | 8.03 pass |
| `.b-derivative` badge text | `--warn` on 12% warn composited over panel | **3.87 FAIL** | 6.36 pass |
| `.provenance` RECORDED banner text | `--warn` on 10% warn composited over `--bg` | **3.83 FAIL** | 7.37 pass |

The composited background for the tint fills was computed by alpha-over-sRGB,
not read off a fixed hex, so these are the values a browser paints. **Reproduce:**
`cd docs/qa/browser && node contrast.mjs`. This is a light-scheme-only failure;
the same hues clear 7:1 in dark. *File separately: deepen the light-scheme
`--warn` primitive (e.g. toward `#7a5c1f`) until the banner clears 4.5:1, then
re-run the script; the DERIVATIVE/`poor` roles inherit the fix.*

**F-2 · SC 4.1.3 Status Messages (WCAG 2.2, new) — none of the async output is
announced.** `grep -c 'aria-live'` and `grep -c 'role="status"'` on the subject
both return **0**. The in-flight status line (`$('status').textContent =
'measuring live…'`, `server/index.html:547`), the results injected into
`<div id="out">` (`:334`), and the error panels all change the DOM with no live
region, so a screen-reader user gets no notice that a measurement started,
finished, or failed. This is the same gap documented with full reproduction
steps in **`artifacts/274-screen-reader.md`**; it is listed here because 4.1.3 is
a WCAG 2.2 AA criterion the rollup must record, not as an independent
observation. *File separately: already covered by #274.*

**F-3 · SC 2.4.7 Focus Visible — at risk, not confirmed.** `grep -c ':focus'`
returns **0**: the stylesheet never styles focus. Native `<button>`/`<select>`
fall back to the engine default ring, which *usually* satisfies 2.4.7, but the
UI also paints its own borders and background on those controls and uses
`color-mix` tints, so whether a clearly visible indicator survives in each
engine and each scheme is a browser property, not a source property. It is
recorded as **at risk / not observed** (see below), not as a pass and not as a
hard fail. *File separately once the harness captures focus in Chromium,
Firefox and WebKit in both schemes.*

**F-4 · SC 2.2.2 / 2.3.3 motion — informational, below the AA bar.** The
cold-start spinner is `animation: pulse … infinite` (`server/index.html:267`)
and `grep -c 'prefers-reduced-motion'` returns **0**. It is not an AA failure:
2.2.2 excludes motion shorter than five seconds and this spinner only runs
during a cold load, and reduced-motion is a AAA (2.3.3) concern rather than AA.
Flagged because the absence of the query is real and the fix is trivial.

**F-5 · SC 1.4.11 Non-text Contrast — panel border, documented as exempt.** The
`.panel` boundary (`--border`/`--line-1` `#e4e2dd` on white) measures **1.29:1**,
under 3:1. Reported for completeness but **not** counted a violation: 1.4.11
applies to *active* UI-component boundaries and to graphics needed to understand
content, and the card border is a decorative grouping separator — removing it
changes no meaning or operability. Recorded so the number is not mistaken for an
overlooked failure.

## What this audit could not observe

These AA criteria are properties of a running browser and assistive technology.
This environment has no network and no installed engine binaries, so the
harness was not run; each is listed as **not observed** and must be confirmed or
filed against `docs/qa/browser/`:

- **2.4.7 Focus Visible** (F-3) and **2.4.11 Focus Not Obscured** — need a real
  engine to see whether the default ring renders against these backgrounds and
  is not clipped by the sticky-less layout.
- **1.4.10 Reflow** at 320 CSS px and **1.4.12 Text Spacing** — need the
  rendered page under a narrowed viewport.
- **2.5.8 Target Size (Minimum)** (WCAG 2.2) and **2.5.5** — the control height
  must be measured in the browser; the source `padding: .5rem .8rem` is
  indicative only. Partially covered for touch by `artifacts/270-mobile.md`.
- **4.1.3** announcement behaviour itself (F-2) — #274 observed a silent update;
  confirming a *fix* needs a screen reader (VoiceOver/NVDA), not a grep.
- **4.1.2** for the two dynamically built SVG charts and the composite integrity
  card — the accessible name exists in source; how it is read aloud was not run.

The cross-engine and cross-scheme coverage this rollup assumes is recorded in
the sibling artifacts `269-cross-browser.md`, `270-mobile.md`, and
`272-color-schemes.md`; the pre-token-refactor resolved values are in 272.

## Out of scope by project invariant

No finding here proposes changing *when* a state fires. Integrity
(DIRECT/DERIVATIVE/NO-MARKET), severity, check result (PASS/FAIL/UNDETERMINED),
provenance (LIVE/RECORDED) and freshness are maintainer-owned definitions; an
accessibility fix may change only how they *look* — a hue, a live region, a
focus style — never their meaning or thresholds. F-1's colour deepening is
chosen so UNDETERMINED stays visually distinct from a failure and NO-MARKET is
not drawn in the `--bad` severity role.
