# Token contrast audit - issue #286

Checked 2026-10-22 against `server/index.html` at
`9ac0de4e2e5b33157e90326d19c9eb2e22bc31bc`. Backlog **#228**; prerequisite #281
landed through #511. This is an audit and implementable design handoff. It does
not change the production palette or certify the whole application as WCAG AA.

## Reproduce

```sh
cd docs/qa/browser
npm ci --ignore-scripts
node --check token-contrast.mjs
node token-contrast.mjs --screenshots --check
# Current source: exit 1, intentionally exposes known failures.
node token-contrast.mjs --candidate --screenshots --check
# Proposed design: exit 0.
```

Use `--executable=/path/to/chrome` for an installed browser. The recorded browser
was Chromium **153.0.8010.12**, Windows, Playwright from the existing lockfile.
No new dependencies or product build step were added. Browser requests are
aborted, including fonts and scripts. The labelled specimens contain state
names and explanatory text, never invented corridor figures or API responses.

Raw [baseline](../browser/results/token-contrast.json) checked at
`2026-10-22T18:52:31.525Z` and [candidate](../browser/results/token-contrast-candidate.json)
checked at `2026-10-22T18:52:35.218Z` include timestamps, source commit, source-byte
SHA-256, browser version, resolved foregrounds/backgrounds, font sizes and every
result. Source SHA is checkout-byte specific (LF/CRLF changes it). The candidate
also records its proposal stylesheet hash.

## Coverage and method

- Two OS preferences x Auto/Light/Dark x eight viewport widths = **48** cases.
- Widths **320, 519, 520, 521, 639, 640, 641, 900** exercise both sides and the
  exact boundary of all width queries currently defined (520px and 640px).
- **29 text specimens / 1,392 measurements** per run: page/panel/alternate-panel
  ink, smallest metric units, provenance, structural badges and selector states,
  verdict text, links/disclosures, nested finding rows, standalone legend chips,
  UNKNOWN/UNDETERMINED and a button label.
- The real embedded stylesheet is resolved by the browser. Actual `.f-state`
  classes and `.finding-row:has(...)` ancestors exercise double translucent
  backgrounds; standalone legend chips are measured separately.
- WCAG 2.x sRGB relative luminance, normal text **4.5:1** for every sample;
  no large-text exemption. Canvas resolves colours into 8-bit sRGB/RGBA, and
  transparent backgrounds are composited through ancestors before calculating
  luminance. Pass/fail uses the unrounded ratio; display rounds to three decimals.
- Theme assertions verify resolved `--panel` against the requested scheme,
  independently of the contrast result. Reduced motion avoids transition frames.

This is an **offline CSS specimen audit**, unlike the real-server suites in this
folder. It does not exercise product scripts, selector persistence, CSP, live
API rendering, hover/focus/disabled states, gradients/images, SVG strokes or
text, print, screen readers, physical devices or other browser engines. It
establishes text-colour contrast in these contexts, not layout fitness or
complete accessibility. Full-page checks remain necessary after implementation.

## Observed results

Baseline: **288 failing text samples**, across 11 specimen/context names;
**8 theme assertion failures** (explicit Dark with a light OS, at every width).
The 288 repeats are not 288 distinct defects. Muted panel text, including the
11.52px `.m-unit`, passes in both correctly selected schemes. UNKNOWN and
UNDETERMINED also pass and retain neutral styling.

The table reports Auto under each OS, so -dark- really is the dark palette.
A light OS with explicit Dark currently has the light column's colours.
All text below requires 4.5:1; the raw files include the remaining specimens.

| Text/context | Baseline light | Baseline dark | Candidate light | Candidate dark |
| --- | ---: | ---: | ---: | ---: |
| muted on panel | 7.578 | 12.024 | 7.578 | 12.024 |
| small metric unit | 7.578 | 12.024 | 7.578 | 12.024 |
| recorded provenance | 4.126 | 11.708 | 5.643 | 11.708 |
| DERIVATIVE | 4.421 | 9.007 | 8.399 | 8.891 |
| NO-MARKET | 2.476 | 5.488 | 8.399 | 8.891 |
| table link | 2.537 | 7.038 | 5.484 | 11.712 |
| cost disclosure | 2.537 | 7.038 | 5.484 | 11.712 |
| pass chip on row | 4.826 | 1.800 | 4.826 | 6.465 |
| fail chip on row | 5.296 | 2.278 | 5.296 | 7.766 |
| pass legend chip | 4.826 | 2.444 | 4.826 | 8.776 |
| fail legend chip | 5.296 | 2.550 | 5.296 | 8.693 |
| UNDETERMINED | 8.399 | 8.891 | 8.399 | 8.891 |

The selector's DERIVATIVE and NO-MARKET have the same baseline ratios as their
badge equivalents. All measured contexts and all theme assertions pass with
the candidate: **0 / 1,392 text failures**, **0 / 48 theme failures**. No width
changed the measured contrast.

## Concrete implementation handoff

The exact proposed declarations are in
[`token-contrast-candidate.css`](../browser/token-contrast-candidate.css).
They are loaded only with `--candidate`; they are not shipped to the product.
Implement these presentation-only changes in the embedded stylesheet:

1. Move the existing `:root[data-theme="dark"]` block outside the
   `prefers-color-scheme: dark` query. Keep its tokens and add the same
   `--focus-outline: #5EEAD4` as automatic dark. Keep explicit Light, Auto,
   persistence and measurement logic unchanged. The harness lifts the existing
   dark declarations into a separate specimen sheet to validate that relocation.
2. Add `--link-ink: #047857` in light and `#6EE7B7` in both dark selectors.
   Apply it to `a.to-table` and `details.cost-decomp summary`; retain underlines.
   Leave the decorative brand/accent token alone. Links communicate interaction,
   not a price verdict.
3. Add `--provenance-ink: #854D0E` in light and `#FCD34D` in dark. Use it for
   non-live `.provenance` text, border and its existing 10% tint. Preserve the
   RECORDED/CACHED wording and timestamps; `.provenance-live` remains distinct.
   Provenance is age/source information, never financial severity.
4. Add `--structural-ink: #334155` in light and `#CBD5E1` in dark. Apply it with
   `--neutral-soft` to all `.b-direct`, `.b-derivative`, `.b-nomarket` and the
   three `.cs-info .cs-state` variants. Preserve each existing state word and
   badge glyph. Their shared neutral colour communicates **structural state**;
   neither amber warning nor brand coral should imply a severity. Do not change
   when a state is emitted or the existing chart encoding in this text audit.
5. Change `.f-pass` text to `var(--ok)` and `.f-fail` to `var(--bad)`, replacing
   fixed light-palette primitives. Preserve background tints, borders, glyphs,
   labels and row rules. Leave `.f-unknown` and `.m-state` neutral. Verdict
   thresholds, check composition and money arithmetic are outside this work.

These declarations supply exact values and selectors for both schemes; no new
API fields or financial meaning are required. Candidate CSS keeps existing
breakpoint rules and adds no sizing/layout changes. After applying the design,
run the baseline command without `--candidate`: it must then return exit 0
with 0 text and theme failures. Rerun real-page browser and non-text accessibility
checks before claiming application-wide conformance.

## Existing stylesheet risks outside the text candidate

At this source revision, `.trend-legend` opens twice without closing the first
rule (`server/index.html`, around lines 534-535). Chromium consequently parses
later styles as descendants of the first rule, including later media rules.
Appending proposals to the same source sheet would inherit that malformed
scope; the candidate deliberately uses a **separate** style element. During
implementation, repair the missing closing brace and rerun the real-page,
responsive and print suites; candidate text passes do not validate those nested
rules. An earlier generic `:focus-visible` rule also uses `--accent`, so the
focus-outline token alone is not a complete focus-contrast fix. Non-text and
keyboard-state validation remain explicit follow-up work.

## Visual evidence

These are labelled CSS specimens, not screenshots of measured corridors:

| | Current source | Proposed design |
| --- | --- | --- |
| Light | [baseline](../browser/results/token-contrast-light.png) | [candidate](../browser/results/token-contrast-candidate-light.png) |
| Dark | [baseline](../browser/results/token-contrast-dark.png) | [candidate](../browser/results/token-contrast-candidate-dark.png) |
