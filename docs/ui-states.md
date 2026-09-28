# Interactive states — focus, hover, active, disabled

The four interaction states of the interface's controls, specified concretely
enough to implement without guessing (issue #285, backlog #227).

**Status: implemented.** The canonical rules live in `server/index.html` in
the block titled *"Interactive state contract (issue #285)"*. This document is
the spec that block implements; when the two disagree, the code is wrong.

---

## The boundary

The issue's constraints are explicit, and they set the line this spec stays
behind:

- **How a state looks is design. When it fires is not.** Every rule below is
  a pure CSS pseudo-class on the browser's own semantics. There is no class
  toggling, no script-driven state, and no measurement in the UI that decides
  when a control looks pressed, focused or disabled. A control is disabled
  exactly when the markup says `disabled` — which is driven by the fetch
  lifecycle (`measure()` and `loadTrend()` disable their buttons while a
  request is in flight), never by style.
- **No brand colour carries financial meaning here.** The tokens these rules
  use — `--accent`, `--border`, `--panel`, `--panel-alt`, `--focus-ring`,
  `--press-shadow`, `--muted` — say "this control can be acted on" and
  nothing else. Verdict colours (`--ok`, `--warn`, `--bad`), the integrity
  palette and the UNDETERMINED styling are never referenced by a state rule,
  so no hover or focus could ever read as GOOD or UNUSABLE.
- **States never alter what the control does.** Focus rings do not change
  handlers; pressed styling does not fire events early. The measurement
  engine's verdicts and the check contract are untouched by this file.

## The four states

Every interactive control (`button`, `select`) is specified for five
positions — the four the issue names plus rest, so the transitions are
closed:

| State | Fires when | Looks like |
|:---|:---|:---|
| **Rest** | no interaction | `--panel` background, 1px `--border`, ink text |
| **Hover** | pointer over an enabled control | accent border, `--panel-alt` background. Guarded by `@media (hover: hover)` so touch devices never get a hover that sticks |
| **Focus** | keyboard focus (`:focus-visible`) | accent border + 3px `--focus-ring` halo; outline suppressed only because the ring replaces it. A mouse click does **not** trigger this |
| **Active** | the control is being pressed | `--panel-alt` background + inset `--press-shadow` depression. No translate/offset — moving the element under the finger shifts the press target mid-press |
| **Disabled** | `disabled` attribute set | opacity .55, `--muted` ink, `cursor: default`, and **no hover or active feedback** — an explicit cancel rule resets border, background and shadow so a pointer resting on a disabled control sees rest styling |

Tokens are defined per colour scheme in the `:root` blocks:

| Token | Light | Dark |
|:---|:---|:---|
| `--focus-ring` | `rgba(15, 118, 110, 0.28)` | `rgba(110, 231, 183, 0.35)` |
| `--press-shadow` | `inset 0 1px 2px rgba(15, 23, 42, 0.18)` | `inset 0 1px 2px rgba(0, 0, 0, 0.5)` |

Both schemes carry the states because the states are one contract rendered
twice; the dark values are tuned for the dark panel colours, not derived by
inversion.

## Where each state applies

- **Buttons** (`Measure live`, `Show trend`) — all four states. These are the
  only elements that can be `:active` in this interface.
- **Selects** (corridor selector, trend size) — hover, focus and disabled
  parity with buttons. A `select` never gets `:active` styling: the press
  opens the platform's own picker, whose chrome is the browser's, not this
  stylesheet's.
- **Links** — the reload link inside an error banner (issue #293 made it
  keyboard-focusable; this change makes the focus *visible*: 2px accent
  outline with 2px offset, so it reads on any background). Hover and active
  are left to the browser default because the link inherits its colour
  explicitly already.

## Motion

The state changes ride on the existing 150ms transition on border-colour,
background and box-shadow — short enough not to lag a fast tab-through, long
enough to read as a change rather than a flicker.

`prefers-reduced-motion: reduce` is honoured globally: transitions and
animations collapse to .01ms, and the loading dots freeze at a steady 70%
opacity instead of pulsing, so the cold-start state still reads. The state
*changes* remain under reduced motion — the feedback is the point; only the
travel between states is removed. This is the I5 (accessibility and motion)
half of the issue.

## Breakpoints

The states are size-independent: the same rules apply at every defined
breakpoint (640px, 520px) and at full width, because the states belong to
elements, not layouts. The only interaction with breakpoints is the existing
one — controls stretch full-width below 640px — which changes where the
target is, not how its states look.

## What was already there, and what this adds

The design-system pass had already landed a hover rule and a
`button:focus-visible`/`select:focus-visible` rule. This change completes the
contract from that base:

- **`:active`** — did not exist at all; added for buttons with the inset
  depression.
- **`select` parity** — hover and disabled previously applied to `button`
  only; a hovered or disabled select fell through to rest styling.
- **Disabled hover cancel** — previously a pointer resting on a disabled
  control kept the hover styling, implying the control was about to act.
- **Link focus visibility** — the banner reload link was focusable but its
  focus was the browser default outline, inconsistent with the controls.
- **Reduced-motion support** — did not exist; the loading animation ran for
  every reader regardless of the OS setting.
- **Touch hover guard** — `hover: hover` stops the sticky hover a first tap
  leaves on a touch screen.

## Testing this

`server/ui_states_test.go` pins the contract as source-text assertions in
`go test` (the same approach the other UI contracts use — the page is one
embedded file with no build step and no JS test runner in CI):

- all four states exist for the right selectors;
- hover and focus are scoped to enabled controls;
- disabled cancels hover feedback, and disabled applies to selects too;
- no state rule references a verdict/integrity/undetermined colour token;
- the focus ring and press tokens are defined in **both** schemes;
- the reduced-motion block exists and the dots freeze at non-zero opacity;
- the hover rule is inside a `(hover: hover)` guard;
- `:active` sets no transform (the press-target rule).

Rendering cannot be proven from Go: `docs/qa/README.md` is the harness that
drives the real binary in Chromium, Firefox and WebKit, and a states pass
there is the follow-up for visual confirmation.

## Related

- [docs/qa/README.md](qa/README.md) — browser harness for visual confirmation
- [docs/qa/artifacts/272-color-schemes.md](qa/artifacts/272-color-schemes.md) — the recorded palette both schemes implement
- [docs/checks.md](checks.md) — the check contract these styles never touch
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the invariants this stays behind
