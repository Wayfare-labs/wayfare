# How Wayfare works, for someone who has never shipped an exchange rate

This is the plain-language version of what you are looking at. Every claim in
it was checked against the code on **2026-09-29** (the files are named as they
go). If a sentence here and a line of code disagree, the code wins — please
open an issue.

## The one-paragraph version

Wayfare prices one currency corridor at a time: how much of your money would
survive a trip from one token (say USDC) to another (say the Nigerian NGNC).
It does not route your money anywhere — it is read-only, non-custodial, and
holds no funds or keys. What it does do is refuse to be quiet when the honest
answer is "don't send this."

## The four ideas, in order

### 1. The reference rate: the benchmark your money is judged against

Any exchange gives you *a* rate. The interesting question is how far that rate
sits from a fair one. Wayfare fetches an independent USD→NGN (or BRL→USD, and
so on) mid-market rate — separate from the venues being measured — and calls
it the **reference rate**. Each rung's `loss_pct` is simply: how far below
that reference mid did the corridor leave you?

Two details that make this trustworthy rather than decorative:

- **Two independent providers, cross-checked.** The engine consults two rate
  providers and compares them. If they disagree past a documented tolerance,
  the measurement is marked `scored: false` — no loss figures and no verdicts
  are published, because a percentage scored against a disputed benchmark
  would be an artefact of which provider was believed (`refrate/`,
  `route/route.go`, `route/unscored_test.go`).
- **The mid is never averaged away.** When the two providers agree, the
  primary mid is used; when they diverge, the more conservative mid is
  scored against. Either way the response's `scored_against` field names the
  exact mid that was used, so every figure can be checked against the
  provider it came from — averaging would manufacture a number no provider
  actually stood behind
  (`docs/adr/001-why-reference-mids-are-never-averaged.md`, `refrate/`).

The verdicts are plain-language grades of that loss: `GOOD` at 3% loss or
less, `FAIR` at 8% or less, `POOR` at 20% or less, `UNUSABLE` above 20%
(README "Verdict thresholds", `route/route.go`). They are anchored to what
incumbent remittance services actually cost — not to what blockchains
normally charge — so "GOOD" means "as good as the banks," not "good for a
DEX." Changing those thresholds is treated as a breaking change in the
project's own documentation, and so is anything that lets a bad route look
better than it is (`SECURITY.md`).

### 2. The ladder: one trade size is a guess; twelve is a curve

Corridors fail differently at different sizes — cheap at $10, ruinous at
$1000 — so Wayfare prices each corridor across a fixed set of send amounts
called the **ladder**: 0.1, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500 and
5000, all in USDC (`dex/sizes.go` `DefaultSizes`, explained rung by rung in
`docs/ladder-sizes.md`).

The extremes are diagnostic. At the bottom rung (0.1) price impact is
negligible, so whatever loss remains is the corridor's *spread* — its cost
with depth effects removed. At the top rung (5000) you see the corridor
*exhausted* — how badly it degrades past realistic amounts. Together they
tell you not just *that* a corridor is bad but *why* (spread, or depth), and
that difference decides what, if anything, to do about it.

If a rung could not be measured, its record says so — `priced: false`, or the
upstream error, or (when nothing priced at all) `integrity: NO-MARKET`. An
absence of a price is never printed as a zero (`route/wire.go`,
`route/ladder.go`; `server/index.html` renders unpriced rungs as "not
priced").

### 3. The check contract: a verdict can be published only if a benchmark existed

Everything above feeds one rule: **when no size produces a verdict of POOR or
better, Wayfare recommends nothing.** On the wire, `recommended` is present
and `null` — not omitted — so no client can mistake an empty slot for an
oversight and fill it with the best-of-a-bad-set (README "The recommendation
rule").

The check behind that rule is structural: the engine never publishes a loss
figure it could not score against the reference rate, never renders an
unmeasured rung as a number, and never reports a corridor as more direct than
it is. If every path to the destination runs through another fiat token, the
corridor is marked `DERIVATIVE`; if no path was returned at any size, it is
marked `NO-MARKET`. Those are structural facts about the market, not
severities, so they get their own colour family in the UI and are never
rendered as failures (`route/route.go`, `docs/state-vocabulary.md`,
`server/index.html` — the `--chart-*` structural roles in the stylesheet).

A worked example of a client obeying the same contract lives in
`examples/api-consumer`, and the reading rules it encodes are written out in
`docs/api-consumer.md`.

### 4. The verdict: what the monitor is for

Put together, one response tells you:

- the **integrity state** — did an independent market exist at all (`DIRECT`,
  `DERIVATIVE`, `NO-MARKET`);
- the **loss curve** — every rung's loss against the reference mid, with the
  floor (best small size) and the worst (largest size) called out;
- the **recommendation** — the quote at the best rung that reached POOR or
  better, or `null` when none did;
- **freshness** — whether it was measured now (`live: true`) or replayed from
  a stored history (`live: false`, with a `stale` block carrying the reading's
  age). Stored readings are labelled, never presented as current
  (`route/wire.go`, `docs/embedded-history.md`).

One recent, real example from the project's own published measurements
(`docs/corridor-measurements.md`, measured 2026-08-08): sending USDC→NGNC on
the best available route returns roughly 46% of fair value at 100 USDC —
about 25% loss even at the 0.1 dust rung, degrading to ~98% loss at 5000. The
monitor's finding is not "best route: 62,900 NGNC"; it is "no size is worth
taking," which is the difference between a router and a monitor.

## Where the engine stops and the inference starts

Wayfare is explicit about its layers: observed facts (Horizon pathfinding,
order books, issuer flags), deterministic calculations on top of them
(rates, losses, integrity), then — not yet built — probabilistic inference
and attestations. A layer can never be more certain than the one beneath it:
anything the engine could not determine is `UNDETERMINED`, which is a
first-class state in the UI, never styled as a failure (README "Architecture",
`docs/glossary.md`, `docs/state-vocabulary.md`).

Layer 3 (failure probability, slippage prediction, VaR) is marked in the code
as **not built** — it is blocked on months of stored history that do not
exist yet. Nothing in this repository predicts; everything in it measures,
labels, and refuses to fill gaps with plausible numbers (README "Architecture"
diagram, `docs/non-goals.md`).

## If you read one response, read this

Open the live instance, pick a corridor, and press **Measure live**. The page
is a single embedded file (`server/index.html`) — the same figures the API
publishes, with the labels, the reference source, and the "measured at" stamp
attached. Every figure on the page traces to a source named on the page;
nothing there is a Wayfare estimate.

## Where to go next

- `docs/about.md` — the one-page story of the project.
- `docs/corridor-measurements.md` — the real measurements behind the example above.
- `docs/ladder-sizes.md` — why the ladder is the sizes it is.
- `docs/api.md` — the HTTP contract, field by field.
- `docs/glossary.md` — every state a reader can meet.
- `docs/non-goals.md` — what this project refuses to build, and why.
