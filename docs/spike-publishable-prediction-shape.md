# Spike: what would make a prediction publishable under this project's rules

**Backlog:** #143 — Initiative E, E2 — Predictive intelligence (V5)
**Roadmap status:** V4+ — Future (not active)
**Type:** Design spike. No implementation is part of this document.
**Checked against the repository:** 2026-10-17

## Question

The four-layer model says a layer-3 estimate must never be published as fact.
What published shape would a prediction take, and what would have to be true
before one could be published at all?

## Method

Every claim below was checked against the code as it currently exists, not as
the backlog or roadmap describes it. Where a claim concerns the existing
design, the source file is cited. Where a claim concerns a capability that does
not exist, it is marked **future** and not described as built.

## Finding 1 — No predictive capability exists today

**Status: confirmed by inspection (2026-10-17).**

The repository publishes measurements (pathfinding results against a reference
rate), verdicts derived from those measurements, integrity states, and check
findings. It does not publish any forward-looking quantity. The regime
classification in `analysis.AnalyzeDecimal` (`analysis/analysis.go`) is a
descriptive label over stored history and is explicitly not served on the wire
(see `docs/regime-classification.md`). Nothing in the current response shapes
carries a forecast, projection, or probability about future liquidity.

Therefore a "prediction" is **future**: there is no existing predictive code
path to build on, and this spike does not assume one.

## Finding 2 — The rule that governs publication already exists

**Status: confirmed by inspection (2026-10-17).**

The layer rule is stated in the project's architecture and enforced in
design: a layer can never be more certain than the layer beneath it. The
checks-never-move-the-headline invariant (`docs/adr/002-why-checks-never-move-the-headline.md`,
`route/wire.go` `WithFindings`, `TestFindingsDoNotMoveTheHeadline`) shows the
project already separates observation from interpretation at the response
level. A prediction is a further layer above measurement and must inherit
that ordering: it may never alter `integrity`, a verdict, or a metric, and it
may never be presented in a field a reader would mistake for one of them.

## Finding 3 — Candidate published shapes

**Status: design proposal. Nothing here is built.**

The following are options a future implementation could take. None is
recommended for adoption by this spike; the spike's job is to record what
would have to be true for each.

**Option A — Separate, labelled prediction object (future).** A distinct
response field, clearly named as estimated, carrying the estimate, its basis
(which stored history it was computed from), the window, and an explicit
`layer: 3` marker. It must never share a field name with a measurement or
verdict. Precondition: a documented estimator and a backtest against stored
records showing its error is reported, not hidden.

**Option B — Descriptive regime only (current direction, not a prediction).**
Publish only the existing regime label with its thresholds and sample-size
gate. This is already a description of past behaviour, not a forecast, and is
the lowest-risk shape. Precondition: none beyond what `docs/regime-classification.md`
already requires.

**Option C — No published prediction.** Keep predictive work internal to
research notebooks and never serve it. Always available; the default if no
other option meets the preconditions.

## Finding 4 — Preconditions for any published prediction

**Status: derived from the layer rule and existing ADRs (2026-10-17).**

A prediction may be published only if all of the following hold. Failing any
one keeps it unpublished.

1. **Labelled as estimated, in its own field**, never merged into a measurement,
   verdict, or integrity state.
2. **Reproducible from stored records** with its basis and window reported.
3. **Backtested** against the stored history, with its error reported alongside
   the estimate rather than omitted.
4. **Insufficient data reported as unknown**, not defaulted, following the
   sample-size gate pattern in `analysis.MinSampleSizeForMeanStdDev`.
5. **Cannot move the headline**, enforced the same way ADR 002 enforces it for
   checks, with a test that pins the invariant.

## Negative result recorded

The spike does **not** find a publishable prediction shape ready to adopt.
No estimator has been defined, no backtest exists, and no error characteristic
has been measured against the stored history. Under the preconditions above,
the current publishable position is Option B or C. This is a valid,
inconclusive-for-prediction outcome and is reported as such.

## Out of scope (escalation boundary)

If a future predictive design requires changing verdict thresholds (3/8/20),
integrity semantics, the check composition rule, or the run-record layout, that
is a different issue with a different review bar and must be raised separately.
This spike proposes none of those changes.

## Related

- [regime-classification.md](regime-classification.md) — the descriptive label that is the nearest existing shape
- [adr/002-why-checks-never-move-the-headline.md](adr/002-why-checks-never-move-the-headline.md) — the ordering a prediction must respect
- [run-store.md](run-store.md) — the stored history any estimate would be computed from
- [spike-model-vs-deterministic.md](spike-model-vs-deterministic.md) — why a model-derived label is a description, not a prediction
