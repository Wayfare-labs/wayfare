# #272 — QA both colour schemes

**Backlog entry H3 (#214).** The stylesheet has a `@media (prefers-color-scheme:
dark)` block that had never been systematically reviewed. This records what the
browser actually resolves in each scheme, and what that means for contrast.

This is an **audit artifact, not a fix.** Per the issue, anything broken found
here is filed separately with reproduction steps and is not corrected in the
same change. Every finding below says so explicitly.

- **Run:** 2026-09-23T21:45:18Z. Raw result set: `docs/qa/browser/results/color-schemes.json`.
- **Engine:** Chromium 153.0.8010.12 (the only engine installed here).
- **Target:** `http://127.0.0.1:8099/`, built from the commit under test with `-history-first`.
- **Subject:** `server/index.html`, SHA-256 `ec3b0a9186b5dbcd7bbfa0b1fe89f4660a186b05d4862447ee69cb8b729e14a7` (1,020 lines, 46,837 bytes), served at `GET /` by `uiHandler` (`server/ui.go`).
- **Procedure:** `docs/qa/README.md`, then
  `cd docs/qa/browser && ./servers.sh start && node run-color-schemes.mjs`.

## What was measured, and how

Two passes, one per scheme, with `prefers-color-scheme` emulated. For each pass
the harness records the **resolved** value of every colour custom property
(engine-resolved, not the source hex), the computed colour of every element
whose contrast is asserted below, and screenshots of the live and trend views.

Contrast ratios are the WCAG 2.2 relative-luminance formula applied to those
resolved values. Thresholds: **4.5:1 normal text** (SC 1.4.3), **3:1 non-text**
(SC 1.4.11). Where an element paints over a translucent fill — `.provenance`,
`.b-derivative`, and the trend integrity strip — the effective background is
composited before measuring, and for `.provenance` the composited background is
read from the browser rather than inferred.

Token resolution was identical in both passes to the last digit, which is what a
shared CSS variable block predicts. The resolved values match the source hex
exactly, so nothing below depends on a colour that failed to resolve.

## The dark block is a straight palette swap

Fourteen tokens are redefined and nothing else changes. That is the structural
observation the whole review rests on, and it is confirmed by the resolved
values rather than by reading the file:

| Token | Light | Dark |
|:---|:---|:---|
| `--bg` | `#fbfbfa` | `#161815` |
| `--panel` | `#ffffff` | `#1e211d` |
| `--border` | `#e4e2dd` | `#33372f` |
| `--ink` | `#1a1a18` | `#eceae4` |
| `--muted` | `#6b6862` | `#9a978e` |
| `--accent` | `#2f6f5e` | `#7fb8a4` |
| `--bad` | `#a03e2f` | `#e08b78` |
| `--warn` | `#96712a` | `#d9b06a` |
| `--ok` | `#2f6f5e` | `#7fb8a4` |
| `--ok-soft` | `#e9f4ef` | `#20372f` |
| `--bad-soft` | `#fbeceb` | `#422522` |
| `--unknown` | `#666b70` | `#b7b9b8` |
| `--unknown-soft` | `#f0f1f2` | `#2a2d2d` |
| `--grid` | `#ecebe7` | `#2a2e27` |

`--ok` and `--accent` are aliased in **both** schemes (same value), so every
statement about one is a statement about the other.

## Recorded results

Ratios from the resolved tokens. **Bold** is below the threshold for that role.

| Element / pairing | Light | Dark | Need |
|:---|---:|---:|---:|
| Body copy, `.case strong`, `td.num` — `--ink` on `--panel` | 17.43 | 13.53 | 4.5 |
| `.sub`, `.meta`, `.case`, `th`, `footer`, `.axis`, `.integrity-help`, `.cs-info`, `.m-unit`, `.trend-legend` — `--muted` on `--panel` | 5.55 | 5.57 | 4.5 |
| `.err`, `.rec-none`, `.v-unusable`, `.b-nomarket` — `--bad` on `--panel` | 6.51 | 6.30 | 4.5 |
| `.v-poor`, `.b-derivative` text, `.integrity-dependency`, `.cs-derivative` — `--warn` on `--panel` | **4.48** | 8.03 | 4.5 |
| `.v-good`, `.v-fair`, `.rec-some` — `--ok` on `--panel` | 5.91 | 7.20 | 4.5 |
| `.provenance` (stale banner) — `--warn` on `color-mix(--warn 10%, transparent)` over `--bg` | **3.83** | 7.37 | 4.5 |
| `.b-derivative` badge — `--warn` on `color-mix(--warn 12%, --panel)` | **3.87** | 6.36 | 4.5 |
| `.f-pass` / `.b-direct` / `.provenance-live` — `--ok` on `--ok-soft` | 5.24 | 5.64 | 4.5 |
| `.f-fail` / `.b-nomarket` — `--bad` on `--bad-soft` | 5.67 | 5.35 | 4.5 |
| `.f-unknown` / `.m-state` — `--unknown` on `--unknown-soft` | 4.76 | 7.04 | 4.5 |
| `.f-summary` on a PASS row — `--ink` on `--ok-soft` | 15.48 | 10.59 | 4.5 |
| `.f-id` / `.f-limit` / `.f-evidence` on a PASS row — `--muted` on `--ok-soft` | 4.93 | **4.36** | 4.5 |
| `.f-id` / `.f-limit` / `.f-evidence` on a FAIL row — `--muted` on `--bad-soft` | 4.84 | 4.73 | 4.5 |
| `.f-id` / `.f-limit` / `.f-evidence` on an UNKNOWN row — `--muted` on `--unknown-soft` | 4.91 | 4.76 | 4.5 |
| Panel border, `.scroll` frame — `--border` vs `--panel` | 1.29 | 1.34 | 3 |
| Table / finding-row separators — `--grid` vs `--panel` | 1.19 | 1.18 | 3 |
| Panel against page — `--panel` vs `--bg` | 1.04 | 1.10 | — |
| Trend integrity strip label — `--muted` on the 75 %-opacity band | 1.44–1.92 | 1.09–1.48 | 4.5 |

### Trend integrity strip

The strip is the one place a colour is **composited** rather than painted: each
band is a `<rect opacity="0.75">`, and its label is a `<text class="axis">`
(fill `--muted`) drawn after it, so the label sits on its own tinted band. Only
the DIRECT state was reachable in this run (the embedded history holds a single
DIRECT run), and it was confirmed from the browser: the band `fill` and the
label `fill` are **the same colour** — `rgb(154, 151, 142)` in dark,
`rgb(107, 104, 98)` in light — differing only by the band's 0.75 opacity.

| Band state | Light | Dark |
|:---|---:|---:|
| DIRECT (measured) | 1.70 | 1.48 |
| DERIVATIVE (from resolved tokens) | 1.92 | 1.09 |
| NO-MARKET (from resolved tokens) | 1.44 | 1.35 |

### What was on screen, and what was not

The embedded history predates recorded counterparty checks, so no findings block
renders and these elements were **absent**, recorded as `null` rather than
silently skipped: `.panel.err`, `.provenance-live`, `.m-value`, `.m-unit`,
`.m-state`, `.integrity-dependency`, `.b-derivative`, `.b-nomarket`,
`.finding-row`, `.f-id`, `.f-limit`, `.f-evidence`, `.f-summary`, `.rec-some`,
`.trend-legend`.

Every row of the results table that depends on one of those elements is
therefore computed from **resolved tokens**, not measured from a live element.
That is a real limit of this run and it is the reason F1 could not be confirmed
on screen here. The `.f-state` **chips** did render (they appear in the verdict
legend), which is why the chip row is live-measured.

## Findings

Nothing below is fixed in this change.

### C1 — Light scheme: the `--warn` family fails AA, on screen

- **Observed.** `--warn` on `--panel` = **4.48:1** (`.v-poor` verdict cells at
  `.88rem`, `.cs-derivative` at `.72rem`). On its own tints it is worse:
  `--warn` on `color-mix(--warn 10%, transparent)` over `--bg` = **3.83:1** —
  the **stale/provenance banner**, which is on screen in this run and whose
  composited background the browser resolved to
  `color(srgb 0.588235 0.443137 0.164706 / 0.1)` — and on
  `color-mix(--warn 12%, --panel)` = **3.87:1** (`.b-derivative`).
- **Expected.** ≥ 4.5:1 for normal text.
- **Note.** The dark scheme passes all three (8.03 / 7.37 / 6.36). The light
  scheme is the one that fails, which is the opposite of what "review the dark
  block" would have caught.
- **Reproduce.** `cd docs/qa/browser && ./servers.sh start && node run-color-schemes.mjs`,
  then measure `--warn` against the two `color-mix()` backgrounds in the
  `light` pass. Or open the page in a light scheme and read the yellow verdict
  cells and the yellow provenance banner.
- **Do not fix here.** Maintainer-owned palette and token decision; overlaps
  roadmap #228 (contrast audit) and #223 (design tokens).

### C2 — Dark scheme only: `.f-id` / `.f-limit` / `.f-evidence` on a PASS row (4.36:1)

- **Observed.** Dark `--muted` (`#9a978e`) on dark `--ok-soft` (`#20372f`) =
  **4.36:1**, for `.76rem`, `.8rem` and `.74rem` text. The same pairings pass
  in light (4.93 / 4.84 / 4.91).
- **Why dark only.** The dark `--muted` was tuned against `--panel`, where it
  measures 5.57. The tinted finding-row backgrounds were added later
  (`332c6fe`, "Improve finding state visual hierarchy") without re-checking
  `--muted` against them. Light did not regress because its soft fills are very
  pale.
- **Reproduce.** Resolved-token arithmetic (the element is not reachable in this
  run): pair dark `--muted` with dark `--ok-soft`. To see it, a corridor with a
  PASS finding is needed.
- **Do not fix here.** Overlaps #228 and #230 (design dark mode deliberately).

### C3 — Both schemes: the trend integrity-strip label is unreadable

- **Observed.** 1.70 (light) / 1.48 (dark) in the one state that was on screen,
  and 1.09–1.92 across all three states from the resolved tokens. In the DIRECT
  state the label and its band are literally the same colour.
- **Expected.** ≥ 4.5:1, or a label colour chosen against the band rather than
  the page.
- **Reproduce.** Load the page with at least one stored run, press *Show trend*,
  and read the state word on the band under the chart axis — in either scheme.
- **Do not fix here.** Present in both schemes, so it is not a dark-block
  defect. UI-owned drawing change; deserves its own issue.

### C4 — Both schemes: separators are below the 3:1 non-text threshold

- **Observed.** `--border` vs `--panel` = 1.34 / 1.29; `--grid` vs `--panel` =
  1.18 / 1.19; `--panel` vs `--bg` = 1.10 / 1.04. Cards are delineated almost
  entirely by the border.
- **Expected.** SC 1.4.11 asks 3:1 for graphical objects *required to
  understand the content*. Whether table separators and card outlines clear
  that bar is a judgement call. **Recorded as an observation, not asserted as a
  defect.**
- **Do not fix here.** Belongs with #223 and #228.

### Adjacent — already filed

The run reproduced the console noise `269-cross-browser.md` reports: a `404`
and a MIME-type refusal for
`/cdn-cgi/challenge-platform/scripts/jsd/main.js`, requested by a Cloudflare
loader committed into `server/index.html`. That finding is already recorded and
"filed separately" in `artifacts/269-cross-browser.md`; it is repeated here only
because this run observed it independently. **Not re-filed, not fixed.**

## Where the two schemes disagree, in one line

Every light-scheme failure is on `--warn` and its tints; every dark-scheme
failure is `--muted` over `--ok-soft`. A review of one scheme alone would have
missed the other scheme's only failure — which is the argument for the issue's
title.

## Scope limits, stated up front

- **Chromium only.** Firefox and WebKit were not installed in this environment.
  The values are engine-independent CSS custom properties and are not expected
  to differ, but that is not measured here.
- **Static contrast only.** Nothing here speaks to overlap, clipping, sub-pixel
  rendering, motion, or legibility at other zoom levels.
- **No `forced-colors` / Windows High Contrast, no print, no `prefers-contrast`.**
  These need their own pass.
- **The findings-block states were not reachable** (see above), so C2 rests on
  resolved tokens rather than a live element.
- **This is not the WCAG 2.2 AA audit** (roadmap #215); contrast is one
  criterion among many.

## Screenshots

| View | Light | Dark |
|:---|:---|:---|
| Live measurement | `results/screenshots/color-schemes-light-live.png` | `results/screenshots/color-schemes-dark-live.png` |
| Trend | `results/screenshots/color-schemes-light-trend.png` | `results/screenshots/color-schemes-dark-trend.png` |

## Artefact checklist

- [x] Procedure written so a third party can repeat it without the original run
- [x] Subject pinned by SHA-256; run timestamped; endpoint named
- [x] Every figure is a computed ratio with its threshold stated
- [x] Colours read from the browser, not from the source file
- [x] Findings carry reproduction steps and are not fixed in this change
- [x] Screenshots for both schemes, live and trend
- [ ] Firefox and WebKit — not installed here
- [ ] Findings-block states on screen — require a corridor with recorded checks
- [ ] Follow-up issues filed for C1–C4 — **to do after this lands**
