# #270 — Mobile device QA on real hardware

**Backlog entry H3 (#212).** The issue's own framing is the constraint:
*"Emulator width is not touch behaviour; the `.scroll` table is the thing to
watch."* So this records two things separately — what emulation answered, and
what it cannot.

- **Run:** 2026-09-23T17:35:50Z. Raw result set: `docs/qa/browser/results/mobile.json`.
- **Target:** `http://127.0.0.1:8099/`, the branch under test with `-history-first`.
- **Procedure:** `docs/qa/README.md`. Re-run with
  `cd docs/qa/browser && ./servers.sh start && node run-mobile.mjs`.

## The headline: no real hardware was used

**No physical device was available in this environment.** Every figure below is
device emulation inside a desktop browser engine (Playwright device descriptors:
viewport size, `isMobile`, `hasTouch`, device pixel ratio and a mobile user
agent). That is enough to answer layout questions and to check whether a touch
gesture lands on the right element. It is **not** enough to answer how the page
feels or behaves on a phone, and this artifact does not claim to.

Specifically untested:

- momentum and inertia of a real finger swipe;
- iOS Safari rubber-band overscroll, and how it interacts with a horizontally
  scrolling table;
- Android's system back-gesture, which competes with horizontal scroll regions
  near the screen edge;
- real touch-target comfort and thumb reach;
- the device's actual fonts (iOS San Francisco, Android Roboto) and its text
  reflow;
- the on-screen keyboard covering the viewport.

## Device matrix

Chromium 153.0.8010.12, Firefox 155.0, WebKit 26.6. `scrollMax` is the
horizontal distance the `.scroll` container can move; `pageOverflow` is how much
wider the *document* is than its viewport.

| Engine | Device | Emulated viewport | `.scroll` client/scroll | `scrollMax` | Page overflow | Touch tap | Touch pan |
|:---|:---|:---|:---|:---|:---|:---|:---|
| Chromium | iPhone SE | 320×568 | 238 / 551 | 313 | **43 px** | works | **works** |
| Chromium | iPhone 13 | 390×844 | 308 / 551 | 243 | **20 px** | works | **works** |
| Chromium | Pixel 7 | 412×915 | 330 / 551 | 221 | **13 px** | works | **works** |
| Chromium | iPad Mini | 768×1024 | 686 / 686 | 0 | 0 px | works | n/a (no overflow) |
| Firefox | iPhone SE | 320×568 | 238 / 552 | 314 | **43 px** | works | works (wheel) |
| Firefox | iPhone 13 | 390×844 | 308 / 552 | 244 | **20 px** | works | works (wheel) |
| Firefox | Pixel 7 | 412×915 | 330 / 552 | 222 | **13 px** | works | works (wheel) |
| Firefox | iPad Mini | 768×1024 | 686 / 686 | 0 | 0 px | works | n/a |
| WebKit | iPhone SE | 320×568 | 238 / 551 | 313 | **43 px** | works | not performed |
| WebKit | iPhone 13 | 390×664 | 308 / 551 | 243 | **20 px** | works | not performed |
| WebKit | Pixel 7 | 412×839 | 331 / 551 | 220 | **13 px** | works | not performed |
| WebKit | iPad Mini | 768×1024 | 686 / 686 | 0 | 0 px | works | n/a |

The Emulated viewport column is the device descriptor's width and height. Note
that Chromium and Firefox report a *larger* `window.innerWidth` than the
descriptor (363, 410, 425 for the three phones) while WebKit reports the
descriptor's width exactly. The widened layout viewport is itself the symptom of
the overflow in finding 1.

## Finding 1 — the page scrolls sideways on every phone, and the table is not the cause

The wide table is correctly contained: `.scroll` clips it and owns the overflow,
and a touch drag pans the container without moving the document

```
Chromium, iPhone SE:   scrollLeft 0 -> 217,  documentScrolledInstead = false
Chromium, iPhone 13:   scrollLeft 0 -> 213,  documentScrolledInstead = false
Chromium, Pixel 7:     scrollLeft 0 -> 212,  documentScrolledInstead = false
```

The touch gesture was a real one: `Input.dispatchTouchEvent` over the table's
visible position, dispatched only after `scrollIntoViewIfNeeded()` brought the
table up from ~1,350 px below the fold. (An earlier attempt dispatched at the
element's layout position, which was off-screen, and wrongly looked like a
failure — recorded here because that is the kind of mistake this artifact exists
to prevent.)

What is **not** contained is the page. At every phone width the document is
wider than the viewport, by 43 px at 320, 20 px at 390 and 13 px at 412, so the
whole layout can be dragged sideways. At 768 px it is clean.

The cause is a single element with no clipping ancestor:

```json
{ "tag": "span", "classes": "", "width": 66, "right": 363, "text": "Could not determine" }
```

That span is the label of the third cell in the *Verdict thresholds* legend
(`.legend-grid`, three columns of `minmax(0, 1fr)`). Each cell holds an
`.f-state` badge with `min-width: 5.3rem` and `white-space: nowrap`, plus a
label that does not wrap. Three such cells do not fit in 320 px of content box,
and the third cell's label overflows the document. Both the grid and the label
are in `server/index.html`. Filed separately.

## Finding 2 — what works, confirmed by gesture rather than by inspection

- **The touch pan works.** A real touch drag over the table scrolls `.scroll` on
  every phone size in Chromium, and a wheel pan moves it in Firefox. The
  container is the scroll owner, and it scrolls to its end (212–217 of a
  221–313 px range in one gesture).
- **Touch tap works.** A `touchscreen.tap` on *Measure live* sets the status line
  to `measuring live — a full ladder is a dozen round trips…` and disables the
  button, on every device and every engine. The primary control is reachable
  with a finger.
- **The responsive breakpoint works.** `.integrity-card` computes to a single
  column below 520 px (`238px` at 320, `308px` at 390) and to two columns at
  768 px (`93.5px 576.5px`), matching the `@media (max-width: 520px)` rule.

## Not performed, and why

- **WebKit touch pan.** Playwright raises *"Mouse wheel is not supported in
  mobile WebKit"* and exports no touch-drag API, so no swipe could be performed.
  Recorded as `performed: false`, not as a pass or a failure.
- **Anything requiring real hardware.** See the headline.
- **Landscape orientation.** Not run; a rotation would change the overflow
  behaviour above and is not covered by these figures.

## Reproducing

```bash
cd docs/qa/browser && ./servers.sh start
node run-mobile.mjs                      # all engines, four devices
node run-mobile.mjs --devices="iPhone SE" --engines=chromium
```

Screenshots of the measurements table at each size are written to
`docs/qa/browser/results/screenshots/mobile-<engine>-<device>.png`. The touch-pan
coordinates, the `scrollLeft` before and after, and the overflow offenders are
all in `docs/qa/browser/results/mobile.json`.
