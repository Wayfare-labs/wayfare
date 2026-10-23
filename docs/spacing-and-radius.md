# Spacing and radius scale

The interface's spacing and corner radii, specified concretely enough to
implement without guessing (issue #283, backlog #225).

**Status: implemented.** The canonical tokens live in `server/index.html` in
the `:root` block. This document is the spec those tokens implement; when the
two disagree, the code is wrong.

---

## The boundary

The issue sits under the same constraint as every other design-system entry:
**how a state looks is design; when it fires is not.** Spacing and radius are
pure presentation. Nothing here changes verdict thresholds, integrity
semantics, check composition or the run-record layout — those are the
measurement engine's, not the stylesheet's, and a change that needed one would
be a different issue with a different review bar.

The single-file rule also holds: `server/index.html` is one file embedded in
the binary. The scale is expressed as CSS custom properties in the existing
`:root` block, with no build step.

## The spacing scale

One 4px-based ladder, referenced by every `padding`, `margin` and `gap` in the
stylesheet. The interface is compact, so the ladder starts at 4px rather than
16px; a zero gap is spelled `0`, not a step.

| Token | Value | px | Typical use |
|:---|:---|:---|:---|
| `--space-1` | `.25rem` | 4 | chip/state padding, hairline offsets |
| `--space-2` | `.5rem` | 8 | tight gaps, table cell padding |
| `--space-3` | `.75rem` | 12 | control padding, row gaps |
| `--space-4` | `1rem` | 16 | default block spacing, panel gaps |
| `--space-5` | `1.25rem` | 20 | panel padding and heading rhythm |
| `--space-6` | `1.5rem` | 24 | section spacing, loading panel |
| `--space-7` | `1.75rem` | 28 | header block rhythm |
| `--space-8` | `2rem` | 32 | footer separation |
| `--space-10` | `2.5rem` | 40 | page top padding |
| `--space-12` | `3rem` | 48 | reserved for larger surfaces |
| `--space-20` | `5rem` | 80 | page bottom padding |

Values that used to sit between steps were snapped to the nearest step. Where
that moved a size, the intent was preserved rather than the pixel value: a
tight gap stays the tightest step, a panel padding stays on the panel step.

## The radius scale

Four steps, plus a deliberate circle.

| Token | Value | Use |
|:---|:---|:---|
| `--radius-xs` | `4px` | chips, badges-as-state, focus outlines, legend swatches |
| `--radius-sm` | `6px` | controls and callouts (recommendation, provenance) |
| `--radius-md` | `10px` | panels and the loading panel |
| `--radius-lg` | `14px` | reserved for the larger surfaces |

`border-radius: 50%` (the loading dot) is a shape, not a step, and stays
literal. A circle has no corner to round.

## Both colour schemes

Spacing and radius carry no colour, so the scale is defined once in the light
`:root` and needs no dark override. The dark block overrides only colour
primitives; the scale resolves identically in both schemes. This is the same
"one contract, rendered twice" rule the interactive states use.

## Breakpoints

The scale is size-independent: the same steps apply at every defined breakpoint
(640px, 520px) and at full width. Breakpoints move where elements sit, not how
far apart they sit.

## Testing this

`server/ui_spacing_test.go` pins the scale as source-text assertions in
`go test` (the approach the other UI contracts use — no build step, no JS test
runner in CI):

- every spacing and radius token is declared;
- spacing and radius tokens are actually referenced, not merely declared;
- no `padding`, `margin` or `gap` carries a literal `rem` length;
- every `border-radius` is a token, except a deliberate `50%` circle.

Rendering cannot be proven from Go: `docs/qa/README.md` is the harness that
drives the real binary in Chromium, Firefox and WebKit, and a visual pass
there is the follow-up.

## Related

- [docs/ui-states.md](ui-states.md) — the sibling state contract
- [docs/qa/README.md](qa/README.md) — browser harness for visual confirmation
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the invariants this stays behind
