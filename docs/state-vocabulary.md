# State vocabulary for the interface

**Backlog entry I4 (#241).** The API can put a reader in one of twelve named
states at once — `UNDETERMINED`, `LIVE`, `RECORDED`, `STALE`, `UNAVAILABLE`,
`DIRECT`, `DERIVATIVE`, `NO-MARKET`, `GOOD`, `FAIR`, `POOR`, `UNUSABLE`. This
document is the **presentation** contract for those states: what each one
*looks* like, and the invariants that keep them distinguishable.

It is a companion to **[glossary.md](glossary.md)**, which is the **meaning**
contract: what each state answers and where in the code it is set. This file
never restates when a state fires. It only says how a fired state is rendered.

## The semantic boundary (why this is a design doc, not CSS trivia)

A mistake here is not cosmetic. Wayfare's whole claim is that it tells you the
truth about a corridor, including the truth that there is nothing worth taking.
Two confusions are expensive, and every rule below exists to prevent one of
them:

1. **A structural state must never read as a severity.** `NO-MARKET` (there is
   no price) and `UNUSABLE` (there is a price and it is terrible) are different
   facts about different things. If a reader can mistake one for the other, the
   tool has lied by omission.
2. **`UNDETERMINED` must never read as failure.** "This anchor publishes no
   SEP-10 endpoint" is not "this anchor's SEP-10 endpoint is broken." The
   check contract exists precisely to keep them apart; the UI is the last place
   that distinction can be thrown away.

So the palette is *role-scoped*. A brand green is never borrowed to say "this
route is cheap," and a warning amber is never reused to mean "unavailable." The
token names in `server/index.html` encode the five families; the mapping is
below.

## The five families

Every interface state belongs to exactly one family. The family answers one
question, and each family owns its own colour role. Two families may happen to
resolve to the same primitive hex, but they are named and styled separately so
they can diverge without one silently dragging the other.

| Family | Question | States | Token prefix |
|:---|:---|:---|:---|
| **Integrity** (structural) | Does this corridor have an independent market? | `DIRECT`, `DERIVATIVE`, `NO-MARKET` | `--structural-*` |
| **Severity** (pricing) | How far below fair value does this route fall? | `GOOD`, `FAIR`, `POOR`, `UNUSABLE` | `--severity-*` |
| **Check outcome** | Did a counterparty check establish its fact? | `PASS`, `FAIL`, `UNDETERMINED`(`UNKNOWN`) | `--check-*` |
| **Provenance** | Was this measured live or served from history? | `LIVE`, `RECORDED`, `STALE` | `--provenance-*` |
| **Availability** | Could we render this at all? | `UNAVAILABLE`, `UNDETERMINED` (metric) | `--availability-*` |

### Rendering per state

Colour is never the only carrier. Every state shows a **glyph or a word** as
well, so the distinction survives greyscale, colour-blindness, and a screen
reader that announces text and nothing else.

| State | Family | Where it appears | Word | Glyph | Colour role | Not-colour-alone carrier |
|:---|:---|:---|:---|:---|:---|:---|
| `DIRECT` | Integrity | integrity badge, corridor detail, trend strip | the literal word | `◆` | `--structural-direct` | glyph + uppercase word |
| `DERIVATIVE` | Integrity | integrity badge + `depends on …` line | the literal word | `↪` | `--structural-derivative` | glyph + word + dependency text |
| `NO-MARKET` | Integrity | integrity badge, empty recommendation | the literal word | `∅` | `--structural-nomarket` | glyph + word + "no path exists" copy |
| `GOOD` | Severity | loss table, legend | the literal word | — | `--severity-good` | the word is always present |
| `FAIR` | Severity | loss table, legend | the literal word | — | `--severity-fair` | the word is always present |
| `POOR` | Severity | loss table, legend | the literal word | — | `--severity-poor` | the word is always present |
| `UNUSABLE` | Severity | loss table, legend | the literal word | — | `--severity-unusable` | the word + "unusable above" chart line |
| `PASS` | Check | counterparty finding row | `PASS` | `✓` | `--check-pass` | glyph + word + row left-rule |
| `FAIL` | Check | counterparty finding row | `FAIL` | `×` | `--check-fail` | glyph + word + row left-rule |
| `UNDETERMINED` | Check / metric | finding row (`UNKNOWN`), metric value line | `UNKNOWN`/`UNDETERMINED` | `?` | `--check-undetermined` / `--availability-undetermined` | `?` glyph + word + reason text |
| `LIVE` | Provenance | provenance banner | `LIVE MEASUREMENT` | — | `--provenance-live` | the words + timestamp |
| `RECORDED` | Provenance | provenance banner | `RECORDED — NOT CURRENT` | — | `--provenance-recorded` | the words + recorded-at |
| `STALE` | Provenance | age in the RECORDED banner | `… ago` | — | (with `RECORDED`) | `age_human` text |
| `UNAVAILABLE` | Availability | error panel, cold start | plain-language cause | — | `--severity-unusable` (as an *absence*) | the sentence names the upstream |

## Invariants an implementation must hold

1. **Structural states are not severities.** `NO-MARKET`, `DERIVATIVE` and
   `DIRECT` never take a severity class and vice-versa. A `NO-MARKET` corridor
   renders no loss table at all — there is nothing to grade — rather than
   grading the absence as `UNUSABLE`.
2. **`UNDETERMINED` is neutral, not red.** It uses `--*-undetermined` /
   `--check-undetermined` (a grey), never `--severity-unusable` or
   `--check-fail`. A metric with no value shows its reason *in place of* the
   value and is styled as unknown, so an unrun metric cannot look like a failed
   one.
3. **No brand colour carries financial meaning.** `--brand-*` (the green) is the
   identity colour *and* the `GOOD`/`PASS`/`DIRECT` role by historical alias. A
   future contributor must not lean on `--accent` to mean "recommended"; the
   recommendation is a sentence, not a colour.
4. **`UNDETERMINED` and `UNAVAILABLE` are distinct.** Undetermined: we reached
   the source and could not establish the fact. Unavailable: we could not
   complete the request. The first is a grey with a reason; the second is an
   error naming the upstream that refused. See
   [live-measurement-failures.md](live-measurement-failures.md).
5. **Colour is never the only carrier.** Every row above pairs its colour role
   with a word and, for the check and integrity families, a distinct glyph.
   WCAG 2.2 SC 1.4.1 (Use of Colour) and 1.4.11 (Non-text Contrast) are the
   binding requirement; the glyph/word is how we satisfy them.

## Cross-scheme and breakpoint behaviour

All state colours are dark-aware because, since issue #281, `prefers-color-scheme:
dark` overrides only the palette primitives and the `--structural-*`,
`--severity-*`, `--check-*`, `--provenance-*` and `--availability-*` roles
resolve through `var()`. A state therefore keeps its *meaning* in both schemes;
only its hex moves. The measured contrast of each pairing against its background
is recorded per-iteration in
[`qa/artifacts/272-color-schemes.md`](qa/artifacts/272-color-schemes.md) and
audited against WCAG AA in
[`qa/artifacts/273-accessibility-audit.md`](qa/artifacts/273-accessibility-audit.md).

At ≤520px the integrity card collapses from a two-column badge-plus-copy grid to
a single column, and inside a table cell the badge shrinks its font and drops
the description to a `title`/`aria-label` so the row cannot overflow. None of
these change *which* state is shown — only where the words wrap. Verify at
320 / 375 / 768 / 1024 / 1440 per the QA harness (`docs/qa/README.md`).

## Out of scope

This document specifies appearance. The `GOOD ≤ 3%` / `FAIR ≤ 8%` / `POOR ≤ 20%`
thresholds, the integrity classification rule, and how checks compose into a
verdict are maintainer-owned (see `CONTRIBUTING.md` and the glossary sources in
`route/route.go` and `checks/checks.go`) and are **not** touched by any change
made under this issue.
