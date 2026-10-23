# Spike: the failure modes of publishing a wrong prediction

Issue [\#205](https://github.com/Wayfare-labs/wayfare/issues/205), backlog `#144`
(Initiative E2 — Predictive intelligence, V5).

**Status: completed, taxonomy only.** This document writes down what can go
wrong if a wrong prediction is ever published, before any model exists. It
proposes no implementation, defines no thresholds, and designs no wire shape
(checked 2026-09-24: nothing in this document is implemented, and no issue in
E2 asks for it yet).

Two findings are themselves negative or inconclusive, and are reported as
such:

- **The prior-art survey was inconclusive.** Nothing found in a bounded
  search generalises cleanly to this project's constraints — not because the
  well is dry, but because the closest analogues all lean on either volume or
  authority, and this project can rely on neither. What was found is
  catalogued below with what is and is not adoptable.
- **No failure mode can be demonstrated or validated against any model,
  ever, until a layer-3 model exists to be wrong.** The validation column of
  the taxonomy is therefore necessarily a list of *design-time checks a
  future spike would have to satisfy*, not a test anyone can run today. This
  is the correct shape for V5 work at this stage of the roadmap; it is a
  finding about the sequencing, not a gap in the deliverable.

---

## Why this matters, in the project's own terms

From `README.md`: *"A layer 4 output publishing a layer 3 estimate as fact is
the specific failure this project exists to avoid"* (checked 2026-09-24,
`README.md` Architecture section). The project's founding finding is that a
figure can be accurate, useful-looking, and still cost the sender more than
half of what they sent (`README.md`, "Why a monitor and not a router": 100
USDC → ~62,900 NGNC against a mid near 1,364).

A prediction is a figure of exactly that kind, with one property measured
figures do not have: **its wrongness is not observable at publish time.** A
measured loss of 27.15% is anchored to recorded bytes (`data/USDC-NGNC.ndjson`
record 1, `floor_loss_pct: "27.15"`, recorded 2026-08-22T12:09:59Z) and a
reader can recompute it. A "this corridor will likely fail" statement is
anchored to nothing a reader can recompute — the future has not happened. The
question this spike answers is not *how to predict well*. It is *what happens
to a reader, and to this project, when the prediction is wrong* — because on
a corridor whose best route already loses 27% at the dust size, wrongness is
not an edge case to be minimised but the ordinary condition to be designed
around.

The gate this spike must respect, from `docs/backlog.md` (Initiative E
preamble): *a layer can never be more certain than the layer beneath it.*
Most of what follows is that rule restated at the point of harm: the reader.

---

## What the code already says, with sources

All checked 2026-09-24 against the tree as it stands.

| Fact | Values / rule | Source |
|---|---|---|
| Verdict bands | GOOD ≤3%, FAIR ≤8%, POOR ≤20%, UNUSABLE >20% loss vs mid | `route/route.go:71-75` (vars), `route/route.go:97-111` (`verdictFor`) |
| Bands are judgement calls | "These are judgement calls, not measurements… anchored to what the incumbent market actually achieves" | `route/route.go:65-70` |
| Corridor under measurement today | floor 27.15% at 0.1 USDC → worst 97.52% at 5000; `recommended: null` | `data/USDC-NGNC.ndjson`, record 1 (recorded 2026-08-22T12:09:59Z); the README table is a different run — see its [measurements doc](corridor-measurements.md) |
| No layer-3 model exists | Layers 3 and 4 "no packages and no stubs, deliberately" | `README.md` Architecture; ADR 003 (`docs/adr/003-why-layers-3-and-4-have-no-packages.md`, Accepted) |
| What today's history could support | one record per corridor file in `data/`; headline figures only (`floor_loss_pct`, `worst_loss_pct`, `recommended`, `reference`) | `data/USDC-NGNC.ndjson` (1 line, read 2026-09-24); `docs/run-store.md`, "The record" |
| Reference states | SINGLE / AGREE / DISAGREE / STALE / MALFUNCTION; >10% divergence refuses to score at all | `refrate/cross.go:20-55` (states), `refrate/cross.go:56-79` (2%/10% rationale) |
| Refusal discipline | "it refuses to score, and says why" — no verdict when neither mid is defensible | `refrate/cross.go:79` |
| Statistical gates already in code | mean/std-dev need ≥30 observations (≈12.5 days), trend needs ≥60 (≈25 days); below that, UNDETERMINED | `analysis/analysis.go:39-48` |
| Undetermined is not a failure | tri-state `{determined, passed}`; every undetermined result must carry a `Reason` | `docs/checks.md:36-47`, `docs/run-store.md` ("checks and metrics") |
| Unknown is never zero | an undetermined metric carries no number at all; omitted, not `0` | `docs/checks.md:234`; `route/wire.go:36-53` (`CostPartJSON`) |
| Trend output is a description, not a forecast | "using it as a forecast is a category error" | `docs/spike-alerting-semantics.md` (never-alert list, item 4); `analysis/analysis.go:24-31` |
| Checks qualify, never move, the headline | `route.WithFindings` branches on nothing | ADR 002 (`docs/adr/002-why-checks-never-move-the-headline.md`, Accepted) |
| A mis-scaled label is withheld from the wire entirely | `DivergenceStats` computes a regime internally but never renders it: "publishing a regime label computed against the wrong scale would read as a verdict this project never issued" | `server/trend.go:88-94` |
| Benchmark vs corridor movement is separable | both reference mids recorded per run for exactly this purpose | `docs/run-store.md` ("reference carries both mids, always"); `server/trend.go:49-56` |

Every rule a prediction layer would have to respect is already encoded
somewhere in the tree. None of it was designed for prediction. That is the
gap this document fills: the code disciplines *publishing*, not *predicting*,
and the failure modes live precisely where those two differ.

---

## Assumptions this taxonomy is scoped to

1. **V5 work starts after V3/V4 have accumulated months of per-run records**
   (`README.md` roadmap, v4 "NOT YET", blocked on history and on record
   content). Nothing here applies to a model trained on the current
   one-record-per-corridor store.
2. **A wrong prediction can only mislead through publication.** An internal
   estimate that never reaches the wire cannot cost a reader money; it can
   only cost maintainers time, which is out of scope.
3. **The reader set is the one named in the README and backlog:** a person
   or organisation deciding whether to move value through a corridor, reading
   the public deployment.

---

## The taxonomy

Eight failure modes. The order is by proximity to the reader, not by
likelihood: the first four cost the reader directly, the last four cost the
project's ability to keep being trusted. Each entry names the project rule
that already guards against it and what is missing — because the finding is
that **the guards cover the measured layers only**.

### F1 — A probabilistic figure is read as a measured one

The reader sees "failure probability: 12%" next to a measured "loss_pct:
27.15" and treats both as the same kind of fact. This is the exact failure
the four-layer model names as the project's reason to exist, and it needs no
exaggeration to be lethal: the measured figure beside it is the one the
project defends with decimal strings and reproducible preimages, so the
probability figure inherits credibility from its neighbour rather than from
its own evidence.

Present guard: none at layer 3. The undetermined-not-zero discipline
(`route/wire.go`, `CostPartJSON`; `docs/checks.md:234`) governs figures the
code chose not to compute — it cannot mark a number that *was* computed as
being of a different kind.

What a future V5 issue would have to answer first: is a layer-3 figure
permitted on the wire at all, and if so, in what field, under what key, with
what marker that survives a plain-text reading? That question isalready open as backlog entry #143 (tracked as
[#340](https://github.com/Wayfare-labs/wayfare/issues/340)) — "what would make
a prediction publishable under
this project's rules" — and this spike does not answer it, only records that
F1 is the failure that makes #340 a *safety* question and not a formatting
one.

### F2 — A confidence interval is presented without its width

"12% ± 11%" publishes as "12%" the moment any serializer, UI summary, or
third-party aggregator strips the interval. The failure is not that the
interval is unknown — it is that **everything downstream of the publisher
has an economic incentive to quote the bare point**, and the project controls
exactly one link of that chain.

Present guard: none. Nothing in the tree has ever carried an interval; the
open research question of how a measured-with-interval figure would even be
represented is backlog entry #140 (tracked as
[#338](https://github.com/Wayfare-labs/wayfare/issues/338)). This spike's contribution is the reason
that representation matters: the interval is not decoration on a layer-3
figure, it is the figure. A point without its width is not a less precise
prediction, it is a different, stronger claim than the model earned.

### F3 — Wrong about failure: the cry-wolf collapse

A model that predicts corridor failure where none occurs gets corrected by
reality, publicly, every time. This is the *loud* failure and, perversely,
the cheaper one: reality is a correction service that requires no maintainer
and does not accept appeals. But the cost is not zero, because the project's
warnings are already load-bearing — the founding finding is that the correct
advice on USDC→NGNC is "don't send" (`README.md`, "Why a monitor and not a
router"; `recommended: null` in the recorded runs — one record per corridor
file, read 2026-09-24). A reader who has seen a false "this corridor will fail"
prediction has been given a discount on every future warning, including the
measured ones that were right.

Present guard: partial, and instructive. `runstore/transition.go:98-104`
already refuses to alert on DIRECT → UNKNOWN with the comment that *"a
monitor that cries wolf gets muted"* — the project has already written down,
in code, the cheapest version of this failure. What is missing is any notion
of a prediction's own precision being tracked over time; nothing in the
store today could even record "predicted X on date D, observed Y on date D+k"
(checked 2026-09-24: `runstore.Record` has no prediction fields; see
`docs/run-store.md`).

### F4 — Wrong about safety: the silent harm

A model that predicts a corridor is fine, and the corridor eats the reader's
money. This is the *quiet* failure, and it is the one this project exists to
prevent: the README's founding scenario is precisely a display that was
accurate and useful-looking while the sender lost half of what they sent. A
false "all clear" is worse than a false alarm in both detection (nobody
complains — the reader does not know they were misadvised) and correction
(the loss surfaces as an unexplained bad day, not as a wrong forecast).

Present guard: the strongest one in the tree, and the reason this spike is
optimistic overall. `refrate/cross.go:65-79` refuses to score when the two
providers diverge more than 10% — the code deliberately *prefers refusing an
answer over risking a confident wrong one*. `docs/checks.md` extends the
same posture: undetermined is not a failure, and every refusal carries a
reason. The finding for V5 is that this asymmetry — bias every published
statement toward "do not rely on this" — is the single most transferable
discipline the measured layers already encode.

### F5 — Wrong in the denominator: survivorship in the training data

The store will record what was measured. It cannot record what was never
measured. Corridor sweeps cost upstream calls (`docs/qa/artifacts/261-live-ladder-timeout.md`
times a live ladder in the tens of seconds), so every historical record is a
survivor of a selection process — corridors that were chosen, sizes that were
priced, times the scheduler was awake (the measure workflow's inability to
push, #63, has already produced a multi-day gap per `docs/backlog.md`, entry
#117 — "gaps in history must be visible, not smoothed"). A model trained
on that record learns the selection as faithfully as the market.

Present guard: none, and part of the gap is structural. What *can* be seen
today: `data/` holds one record per corridor file (read 2026-09-24), the
schedule is every 6h (`docs/backlog.md` Architecture snapshot), and gap
visibility is itself still an open issue (backlog entry #117, tracked as
[#188](https://github.com/Wayfare-labs/wayfare/issues/188) — "gaps in
history must be visible, not smoothed"). Until gaps are first-class, a
history cannot even declare where its own holes are, which is the minimum
input any survivorship correction would need.

### F6 — The benchmark moves and the prediction is scored against the wrong world

Predictions about a corridor priced against an official-rate reference inherit
every caveat the measured layer already documents: under exchange controls the
transacted rate may differ, so every figure understates the loss (`README.md`,  "The benchmark is the charitable one"; `docs/fair-value-ngn.md` — backlog
  #90, tracked as #51).
A prediction trained through a period when the official and transacted rates
diverge is a prediction about the wrong quantity, and no amount of internal
validation detects it, because the model's own history is internally
consistent.

Present guard: structural, and already built. `runstore` records both
reference mids per run deliberately, so that "the corridor moved" and "the
benchmark moved" stay distinguishable afterwards (`docs/run-store.md`,
"reference carries both mids, always"; `server/trend.go:49-56`). The
prediction layer inherits this for free — but only if its training windows
are labelled with the same provenance. Nothing today would enforce that
(checked 2026-09-24: no model, no trainer, nothing to enforce).

### F7 — Confidence becomes a ranking, and a ranking becomes a recommendation

The project's own origin story is this failure mode, executed once already.
`route/route.go:5-15` ("Why a verdict, and not just a ranking"):
*"A pure ranking has a hidden assumption: that the best option is a good
option… A tool that displayed 'best route: 65,100 NGNC' would be accurate,
useful looking, and would have cost the user more than half of what they
sent."* A predicted failure probability is precisely the kind of figure that
sorts well, and sorting is what readers do with confidence numbers regardless
of what the publisher intended. Backlog entry #149 (tracked as
[#209](https://github.com/Wayfare-labs/wayfare/issues/209) — "what a
single number would destroy") records the same concern for composite scores.
For predictions the sequence ends one step worse: probability is the most
rankable figure the project could publish, and ADR 002's whole argument is
that derived judgements must never rewrite the measurement.

Present guard: two precedents rather than one mechanism. ADR 002 (checks
qualify the headline, never move it) and route's verdict-not-ranking contract
(`route/route.go:1-20`) both name the same ratchet. What is missing is the
insight this spike adds: the ratchet does not need an editor or a bug to
engage. It engages at *composition time* — the moment a prediction is
rendered beside a measured figure, a reader will rank by it — so the guard
that matters is the wire shape (again backlog #143/#340), not model quality.

### F8 — Reproducibility dies, and the trust model changes shape

Today every published figure is independently recomputable from recorded
upstream bytes (`snapshot/record.go`, `snapshot/replay.go`; CI runs the
suite network-isolated, `docs/offline-testing.md`). A model output is not:
reproducing it needs the exact training snapshot, the exact code, the exact
seed. This is the V6 tradeoff (`docs/backlog.md` E4 — "an oracle asks readers
to trust the publisher instead") arriving *early*, through the V5 door: a
published prediction makes the project a publisher of claims its own evidence
model cannot support, before any attestation is signed.

Present guard: the evidence standard itself, and the honest finding is that
it is a guard the project must *choose* to extend, not one that extends
automatically. What would have to be true before a prediction is published,
extracted from what the tree already does for measured figures: training data
pinned and hash-addressed (the runstore preimage rule,
`docs/run-store.md`), the model version recorded per prediction, and the
prediction logged at publish time so later reality can be scored against it.
None of these mechanisms exists (checked 2026-09-24), and none should be
built by this spike's issue — each is a layout or process decision with a
different review bar.

---

## What the tree already forbids (the project's own prior art, best available)

The survey below is deliberately *internal first*: the repository encodes a
coherent refusal discipline, and any external borrowing must not weaken it.

| Prior art in the tree | What it guards | Transferable to a prediction layer? |
|---|---|---|
| `refrate/cross.go` MALFUNCTION refusal (scores nothing past 10% divergence) | Confident output from a broken input | **Yes, directly** — F4's asymmetry, already in code |
| `runstore/transition.go` UNKNOWN exclusion (no alerts on unknown states) | Crying wolf on measurement noise (F3) | **Yes, directly** — the "never publish on an undetermined base" rule |
| `analysis/analysis.go` minimum sample sizes (30 / 60) | Precision that looks real and is not | **Yes** — V5 will need its own, larger gates; this is the shape |
| `docs/checks.md` tri-state + mandatory `Reason` | Undetermined masquerading as failure (or as pass) | **Yes** — a prediction needs a fourth state: *stated with what evidence* |
| `route/wire.go` unknown-is-never-zero (`omitempty` on undetermined numbers) | The default-to-zero failure | **Yes** — the interval (F2) must be on the wire or the point must not be |
| ADR 002 (checks never move the headline) | Derived judgement rewriting measurement | **Yes** — a prediction must never change `integrity` or a verdict |
| `server/trend.go` refuses to render a regime label at the wrong scale | A figure that reads as a verdict the project never issued | **Yes** — publishing nothing beats publishing a number the evidence cannot scale to |
| `docs/spike-alerting-semantics.md` never-alert list, item 4 | Trend described as forecast | **Already the same line this spike defends** |

### External prior art: inconclusive

A bounded survey (standard web sources; not a literature review) found the
obvious families — weather forecasting's probabilistic-communication
standards, credit-risk model governance, per-metric alerting practice in
SRE, forecast-resolution scoring (Brier and descendants) — and none could be
adopted wholesale:

- **Meteorology** communicates probabilities calibrated over *decades* of
  dense, cheap, homogeneous observations. This project would have *months* of
  sparse, expensive, heterogeneous records (six-hour cadence, three corridors,
  selection effects per F5). The communication conventions transfer; the
  calibration basis does not.
- **Credit-risk model governance** assumes an authority relationship — a
  regulated publisher, an audited model, a defined harmed class — that a
  pre-MVP open-source monitor does not have, and its controls (back-testing
  regimes, model inventories) presuppose institutional machinery out of scope
  here.
- **SRE alerting practice** solves the *operator's* problem (alert precision
  against page fatigue), which is F3-shaped; the reader-protection problem
  (F1, F2, F4) is out of its scope.
- **Forecast-resolution scoring** (Brier score and relatives) is the right
  *vocabulary* for F3/F4 accountability, but requires a stream of resolved
  predictions and outcomes. This project has neither (checked 2026-09-24: no
  prediction record, no outcome record, and whether "a corridor failure" is
  even observable from pathfinding data is itself the open definitional
  question in backlog #141/#339).

**Inconclusive verdict, stated as one:** external prior art offers vocabulary
and warnings, not a copyable policy. The closest adoptable statement is the
one the project already enforces for measured figures: publish the evidence
with the claim, or do not publish the claim. Where external practice and
project rules conflict — e.g. any convention that tolerates bare point
estimates — the project rule wins, and this document is the argument for why.

---

## Concrete harm scenarios (one paragraph each, deliberately concrete)

**Scenario A — F1 + F4.** A V5 model, trained on six weeks of records that
predate an NGN regime change, publishes "USDC→NGNC failure risk: low" beside
the measured loss figures. A PSP integrating the API reads the low risk, and
the measured UNUSABLE verdicts beside it, and ships the corridor because the
risk figure sorted best in their dashboard comparison. The corridor eats a
weekend of transfers at the structural floor before anyone re-reads the
measured layer. Nobody is lying anywhere in this chain; the probability
figure simply outranked the measurement in a consumer the project never saw.

**Scenario B — F3.** The model predicts "GHSC will lose availability this
week" twice; both times NGNC's book deepens and GHSC routes fine. The third
warning — the measured, correct finding that GHSC is DERIVATIVE and bounded
by NGNC — is discounted by the same readers, because the project's name is
now attached to two misses and one hit, indistinguishable from outside.

**Scenario C — F2.** "Expected slippage at 1000 USDC: 4.1%" reaches a
comparison site via the API. The site strips the interval because its schema
has no field for it (the project published the interval; the *consumer*
dropped it). A sender trades at 9% and the project's public record says it
said 4.1%. The failure was complete before the second hop.

The scenarios share one property worth naming: **in none of them is the
model unusually bad.** Each is an ordinary model, wrong at its ordinary rate,
meeting a publication discipline that did not anticipate wrongness.

---

## Design implications for whichever spike comes next

Constraints that any V5 publication design inherits from this taxonomy,
stated as implications rather than as new rules (the rules themselves are
the tree's; see table above):

1. **A prediction cannot be a field on the existing measurement shape.**
   F1 and F7 make the co-located-magnitude problem structural: any surface
   where a probability sits beside a loss percentage, the probability wins
   the reader. Whatever shape backlog #143 (#340) lands on must answer
   "where does this live such that it cannot be ranked against a measured
   figure?" before it answers anything about schema.
2. **The interval is mandatory and first-class (F2).** If a future wire type
   for layer-3 figures cannot carry width, the design is wrong, whatever its
   other virtues. This is the same discipline as `CostPartJSON` omitting
   undetermined amounts rather than emitting zero — extended from *presence*
   to *precision*.
3. **Every published prediction must be scoreable later (F3, F4, F8).**
   Prediction, outcome, and the corridor state that falsified or confirmed —
   recorded, hash-chained, reproducible. The project already proves it can do
   this for measurements; a prediction is not publishable under this
   project's rules until its misses are as public as its hits. (The general
   publication bar is backlog #143/#340's question; this is the part of it
   that F3/F4/F8 force.)
4. **Sample gates larger than `analysis`'s, and stated per corridor (F5).**
   A model that cannot say how much of its training window was holes cannot
   publish, which makes backlog entry #117 ([#188](https://github.com/Wayfare-labs/wayfare/issues/188),
   "gaps visible, not smoothed") a *prerequisite* for V5 publication, not a
   V3 nicety.
5. **The refusal posture is the product (F4).** The MALFUNCTION precedent —
   refuse the answer when the input cannot support it — is the most valuable
   thing the measured layers can hand to a prediction layer. The design
   question for V5 is not "how do we predict well" but "what is the
   MALFUNCTION-equivalent state for a prediction, and how often does it
   fire". If the honest answer is "most of the time, for years", that is a
   valid finding, and it should not be padded into a launch.
6. **A held-out negative result remains possible for all of V5 (backlog
   entry #146, tracked as [#207](https://github.com/Wayfare-labs/wayfare/issues/207)).** Nothing in this taxonomy assumes a model is worth having.
   If F5 + F6 + the sample gates make every prediction layer permanently
   UNDETERMINED on this project's data budget, the correct outcome is to
   record that and close the initiative — the measured layers were the
   product all along.

---

## Verdict

**The tree already contains the disciplines that make measured figures safe
to publish, and none of them were designed for figures whose wrongness is
invisible at publish time.** The eight failure modes above are the delta.
Three of them (F1, F2, F7) are settled by the same future decision — what
the published shape of a prediction is, which is backlog #143/#340's open
question; this document's contribution is establishing that #143 is a
reader-safety question, not a schema question. Two (F3, F4) are accountable
only if predictions are recorded and scored after the fact, which no current
mechanism does. Two (F5, F6) are data-quality properties that make the
gap-visibility issue ([#188](https://github.com/Wayfare-labs/wayfare/issues/188)) a
prerequisite for V5 rather than a V3 nicety. One (F8) is the V6
trust tradeoff arriving early, and is the strongest single argument that V5
publication and V6 attestation are one decision, not two.

The prior-art survey was inconclusive, and the honest summary is narrow:
*external practice offers vocabulary, not policy; the project's own refusal
discipline is the best available template; and the cheapest way to be right
about every failure mode above is to keep layer 3 unpublished until its
publication design can survive its own wrongness.*

No implementation is attempted, and none is implied: nothing in this
document changes verdict thresholds, integrity semantics, the check
composition rule, or the run-record layout.

---

## Related

- Issue [\#205](https://github.com/Wayfare-labs/wayfare/issues/205) — this spike
- [backlog entry #143, tracked as #340](https://github.com/Wayfare-labs/wayfare/issues/340) — what
  would make a prediction publishable under this project's rules (the open
  question F1/F2/F7 defer to)
- [backlog entry #146, tracked as #207](https://github.com/Wayfare-labs/wayfare/issues/207) — would a
  model add anything over the deterministic measurements? (the held-out
  negative result)
- [backlog entry #141, tracked as #339](https://github.com/Wayfare-labs/wayfare/issues/339) — what a
  route failure actually is, observationally (the definitional prerequisite)
- [backlog entry #140, tracked as #338](https://github.com/Wayfare-labs/wayfare/issues/338) — how to
  express uncertainty in a published figure (the F2 representation question)
- [ADR 003](adr/003-why-layers-3-and-4-have-no-packages.md) — why layers 3
  and 4 have no packages (why this document contains no design either)
- [ADR 002](adr/002-why-checks-never-move-the-headline.md) — derived
  judgement never rewriting measurement (the F7 precedent)
- [spike-alerting-semantics.md](spike-alerting-semantics.md) — the
  compare-two-recorded-runs primitive and the trend-is-not-a-forecast rule
- [checks.md](checks.md) — undetermined is not a failure; unknown is never zero
- [run-store.md](run-store.md) — the preimage rule, both-mids recording, and
  what today's records could and could not support
