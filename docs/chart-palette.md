# The chart palette

The colours the two line charts draw with, specified concretely enough to
implement without guessing (issue #284, backlog #226).

**Status: implemented.** The canonical tokens live in `server/index.html` in
the `:root` blocks. This document is the spec those tokens implement; when the
two disagree, the code is wrong.

---

## The problem

`curve()` drew the line, the dots **and** the threshold all in `var(--bad)`.
The chart said "danger" before it said anything, because the only colour on it
was the severity colour. The fix is not to remove colour but to give the chart
its own palette: a neutral role for the data, and a colour reserved for the one
thing on the chart that is genuinely a boundary.

## The boundary

As with every design issue, **how a state looks is design; when it fires is
not.** This change touches only how the charts paint. It changes no verdict
threshold, no integrity rule, no check composition and no run-record field, and
it stays inside the single-file rule (CSS custom properties + inline SVG
attributes; no build step, no charting library).

## The tokens

| Token | Light | Dark | Painted on |
|:---|:---|:---|:---|
| `--chart-grid` | `#CBD5E1` | `#334155` | gridlines and axis rules |
| `--chart-line` | `var(--brand-slate)` | `#E2E8F0` | the measured-loss polyline and trend segments |
| `--chart-point` | `var(--brand-navy)` | `#F8FAFC` | the per-size loss dots |
| `--chart-threshold` | `var(--critical)` | `#FCA5A5` | the Unusable threshold marker and its label |

The threshold is the one genuine severity a chart carries — it marks where the
engine refuses a size — so it keeps a red role, but a *chart* one. The data
never borrows it.

## Structural integrity states

The trend chart colours each run by its integrity state. DIRECT, DERIVATIVE and
NO-MARKET are **structural states, not severities** (docs/state-vocabulary.md),
so they get structural roles rather than `--warn` / `--bad`:

| Token | Light | Dark | State |
|:---|:---|:---|:---|
| `--chart-direct` | `var(--brand-navy)` | `#F8FAFC` | DIRECT |
| `--chart-derivative` | `var(--brand-slate)` | `#CBD5E1` | DERIVATIVE |
| `--chart-nomarket` | `var(--brand-coral)` | `#FDBA74` | NO-MARKET |

These are the same brand primitives the integrity badges use, so a state reads
the same colour in the badge and on the chart. Crucially, NO-MARKET is coral,
not red: a corridor with no market has a structural absence, not a failure.
Anything the engine did not determine — including an UNDETERMINED state — stays
`--muted`, a neutral grey, and so cannot read as failure either.

The legend beneath the trend chart draws its three swatches from the same
tokens, so the legend and the strip can never disagree.

## Both schemes and every breakpoint

Every chart token is declared in the light `:root` and overridden in the dark
block, so both schemes render the palette with intent rather than by inversion.
The palette is size-independent: the same colours apply at every defined
breakpoint and at full width, because breakpoints move where a chart sits, not
what it is painted with.

## Testing this

`server/ui_chart_palette_test.go` pins the palette as source-text assertions in
`go test`:

- every chart token is declared, and the integrity roles are declared in the
  dark scheme too;
- neither `curve()` nor `trendChart()` references `--ok`, `--warn`, `--bad`,
  `--critical`, `--warning` or `--success`;
- both renderers actually use a `--chart-*` token;
- no threshold marker still paints with `var(--bad)`.

Rendering cannot be proven from Go: `docs/qa/README.md` is the harness that
drives the real binary in Chromium, Firefox and WebKit, and a chart pass there
is the follow-up for visual confirmation.

## Related

- [docs/state-vocabulary.md](state-vocabulary.md) — why structural states are not severities
- [docs/ui-states.md](ui-states.md) — the sibling design-system contract
- [docs/qa/README.md](qa/README.md) — browser harness for visual confirmation
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the invariants this stays behind
