# Spike: survey how other market-quality tools express confidence

Issue [#206](https://github.com/Wayfare-labs/wayfare/issues/206), backlog
`#145` ([docs/backlog.md](backlog.md)).

**Status: completed. A survey, not a design.** Five external tools that
publish judgement-bearing market or quality figures were surveyed for how
they express confidence. Every tool was read from its own documentation, and
each entry below separates what Wayfare could adopt from what it could not.
The headline result is stated plainly at the end: **the repository already
expresses confidence more conservatively than every tool surveyed** — by
gating and abstention rather than by publishing a confidence number — and the
one thing a V5 confidence vocabulary would need (a measured frequency basis)
does not exist in the tree. No code was written and none is proposed.

Checked against the tree at `a89d985` on 2026-09-25. External sources checked
2026-09-25 unless noted.

---

## Why this matters now, and why it could not be answered from the tree alone

Backlog `#145` asks for "prior art, cited, with what is adoptable and what is
not" ([docs/backlog.md:992-994](backlog.md), checked 2026-09-25). V5
(predictive intelligence) is where a confidence vocabulary would first be
needed — a failure probability or an expected slippage is worthless without a
published sense of how much to trust it — and the four-layer model forbids
publishing a layer-3 estimate as if it were a layer-1 fact. The vocabulary
should be chosen before any model exists, and it should be chosen from prior
art rather than invented.

## What the tree already expresses, and how

Before surveying others: the repository's own confidence surfaces, checked
2026-09-25. The pattern is consistent — **confidence is expressed by
qualification and refusal, never by a number claiming certainty**:

| Surface | Expression | Where |
|:---|:---|:---|
| Verdict | Ordinal bands anchored to incumbent-market behaviour, full-precision reconciliation | `route/route.go` (`ThresholdGood`/`Fair`/`Poor`, `verdictFor`) |
| Corridor structure | Carried alongside the verdict, never folded into it | `route/route.go`, `Integrity` doc comment |
| Benchmark trust | SINGLE / AGREE / DISAGREE / STALE / MALFUNCTION; MALFUNCTION scores nothing (`Scorable() == false`) | `refrate/cross.go:35-37`, `refrate/refrate.go:74` |
| Counterparty checks | Tri-state `{determined, passed}` — unknown is expressible and never a false | `checks/checks.go:508-520` |
| Statistics | Minimum samples before mean (30) or trend (60) is computed; below them, undetermined with a reason | `analysis/analysis.go:34-44` |
| Freshness | `live: false` + a `stale` age block; nothing synthesised to fill a gap | `server/api.go` (`staleJSON`) |
| Recommendation | `recommended: null` — the refusal to name a winner is itself published | `route/route.go` (`Result.Recommended`) |

None of these publishes a probability. That is the baseline the survey is
measured against.

## The survey

### 1. Pyth — confidence interval beside every price

Pyth publishes a price **and** a confidence interval from its first-party
providers; both share a fixed-point exponent, and the SDKs ship a default
staleness check so an old price fails loudly rather than reading as current
([Pyth best practices](https://docs.pyth.network/price-feeds/core/best-practices),
checked 2026-09-25). A Pyth essay describes the confidence as the providers'
own estimate of uncertainty, streamed with the price
([What is confidence](https://www.pyth.network/blog/what-is-confidence),
checked 2026-09-25).

- **Adoptable:** the *shape* — uncertainty travels as a published magnitude
  beside the figure, not as prose; and staleness gating that refuses to
  answer rather than answering with an old number (the tree's `stale` block
  is the same idea, already implemented).
- **Not adoptable:** the content. Pyth's interval is a producer's estimate of
  its own quoting precision. Wayfare is not a producer; its uncertainty is
  about coverage and sample (did the corridor answer at all; do the two
  reference feeds agree; how many observations back a statistic) — different
  quantities that would be fabricated, not measured, if borrowed. Pyth's
  advice to widen spreads with confidence also does not transfer: Wayfare
  publishes, it does not quote.

### 2. Chainlink — freshness and quorum as the confidence story

Chainlink's aggregation model expresses trust structurally rather than as a
published uncertainty: an update lands only when a deviation threshold or a
heartbeat triggers it, and only when a minimum number of operators answered —
otherwise "the latest answer will not be updated"
([Chainlink decentralized data model](https://docs.chain.link/architecture-overview/architecture-decentralized-model),
checked 2026-09-25).

- **Adoptable:** freshness-as-trigger (already present: the six-hour cadence,
  the `stale` block, `reference_fetched_at`); and the idea that *no answer is
  a defined state* — which the tree already implements more explicitly via
  MALFUNCTION → unscored.
- **Not adoptable:** median-of-many aggregation. It needs a population of
  independent reporters; Wayfare has exactly two reference providers and
  refuses to average even those two (ADR: reference mids are never averaged,
  `docs/adr-reference-mids.md`). A median of two is either one of them or a
  blend the repository forbids.

### 3. Lighthouse — the single weighted number, and its own caveat

Lighthouse computes Performance as a weighted average of metric scores over
log-normal curves derived from HTTP Archive data, with weights that changed
between versions (v8 → v10 rebalanced and removed metrics) — and its own
documentation warns that "it might be more useful to think of your site
performance as a distribution of scores, rather than a single number"
([Lighthouse performance scoring](https://developer.chrome.com/docs/lighthouse/performance/performance-scoring),
checked 2026-09-25).

- **Adoptable (as counter-evidence):** the two lessons Wayfare should take
  before ever publishing a composite — that weights and curve shapes are
  editorial choices which churn over time, and that even the tool whose
  product *is* the single number tells readers to prefer the distribution.
  Both feed directly into [the single-number spike](spike-single-number-cost.md)
  and [the undetermined-composition spike](spike-composite-undetermined.md).
- **Not adoptable:** the composite itself; and population-relative curves
  ("25th percentile of HTTP Archive becomes a 50") — Wayfare has no population
  of scored corridors to curve against, and one corridor is not a distribution.

### 4. Moody's — ordinal grades with a carried-alongside modifier

Moody's publishes ordinal letter grades with numeric modifiers (Aa2 vs Aa3),
and carries a separate outlook dimension — Positive, Negative, Stable,
Developing — alongside the rating rather than folded into it
([Moody's FAQ](https://www.moodys.com/web/en/us/help-support/faq.html);
[Moody's Ratings, overview of modifiers](https://en.wikipedia.org/wiki/Moody%27s_Ratings),
both checked 2026-09-25). Unrated (NR) is a real published state.

- **Adoptable:** three things Wayfare already does and this validates — an
  ordinal scale anchored to external reference behaviour (the verdict bands
  are anchored to the 3–8% incumbent remittance cost, `route/route.go`), a
  second qualitative dimension carried alongside (integrity beside verdict;
  outlook beside rating), and an explicit not-rated state (undetermined with
  a reason).
- **Not adoptable:** the mechanism. Moody's grades are committee judgement —
  unfalsifiable under Wayfare's rules, where every figure must be
  reproducible from recorded bytes.

### 5. CoinGecko — Trust Score, with a "None" state

Per trading pair, CoinGecko shows Trust Score as Green/Yellow/Red **or
"None"**, computed from listed components (orderbook spread, ±2% depth,
volume, trade frequency, outlier checks); per exchange it is a 1–10 composite
over broader categories, and its 2026 "Basilisk" update grades exchanges on a
curve relative to peers
([CoinGecko methodology](https://www.coingecko.com/en/methodology), checked
2026-09-25).

- **Adoptable:** "None" as a first-class published state rather than a low
  score; and the choice of *directly observable market facts* as inputs
  (spread, depth) — the same inputs Wayfare's implemented-but-unreachable
  metrics measure (`checks/metric_*.go`, no production runner as of
  2026-09-25).
- **Not adoptable:** web-traffic inputs (not reproducible from recorded
  bytes, so unverifiable under Wayfare's rules), curve-grading against peers
  (no population), and composite weights that the methodology page lists but
  does not publish as exact, testable numbers.

## Cross-cutting findings

1. **Nobody surveyed publishes a probability without a frequency basis
   behind it.** Pyth's interval comes from providers observing their own
   quoting; Chainlink's comes from quorum over many independent reporters;
   Lighthouse's curves come from HTTP Archive's population. Wayfare's tree
   has no observed failure frequencies to stand behind any such number —
   `runstore` records headline figures, not metrics
   ([docs/run-store.md:107](run-store.md), checked 2026-09-25) — so a
   V5 confidence number has nothing to be measured *from* yet. That is a
   data gap, not a vocabulary gap, and it is #91/#55 territory before it is
   V5's.
2. **The strongest shared pattern is abstention as a state.** Pyth's
   staleness refusal, Chainlink's quorum failure, Moody's NR, CoinGecko's
   None — every surveyed tool has one, and the tree already has the most
   explicit version of it (undetermined-with-reason, `recommended: null`,
   MALFUNCTION → no verdict).
3. **The second shared pattern is "carry the qualifier beside the figure".**
   Pyth's interval, Moody's outlook, Lighthouse's distribution caveat — the
   same shape as integrity-beside-verdict. On this, the surveyed tools and
   Wayfare agree.

## Verdict

**The repository's confidence expression — gating, abstention, and
qualifiers carried alongside — is ahead of the surveyed prior art, and the
survey found no pattern worth adopting that the tree does not already
implement in a stricter form.** The adoptable residue is small and future:
if V5 ever publishes an estimate, the shape to reach for is a measured
magnitude beside the figure (Pyth's interval pattern) gated on a measured
frequency basis that does not exist yet. Until that basis exists, any
confidence number would be the kind of fabrication the tree refuses, and
this spike records that as the finding rather than designing around it.

No implementation is attempted or proposed by this issue.

## Constraints check

- Assertions about the tree are checked against the code at `a89d985`, not
  against README prose (2026-09-25).
- Every external source carries its URL and the date checked (2026-09-25).
- Future capabilities (V5 confidence vocabulary, metric reachability) are
  marked future.
- The result is a mildly negative one — "nothing to adopt, ahead of prior
  art" — and is reported as such rather than padded into a design.
- No implementation is attempted.

## Sources

| Source | Checked |
|:---|:---|
| Tree at `a89d985`: `route/route.go`, `refrate/cross.go`, `refrate/refrate.go:74`, `checks/checks.go:508-520`, `checks/metric.go:38`, `analysis/analysis.go:34-44`, `server/api.go`, `route/health_score.go` | 2026-09-25 |
| [docs/run-store.md:107](run-store.md) (records carry headline figures, not metrics) | 2026-09-25 |
| [Pyth — Best Practices](https://docs.pyth.network/price-feeds/core/best-practices) | 2026-09-25 |
| [Pyth — What is confidence](https://www.pyth.network/blog/what-is-confidence) | 2026-09-25 |
| [Chainlink — Decentralized Data Model](https://docs.chain.link/architecture-overview/architecture-decentralized-model) | 2026-09-25 |
| [Lighthouse — Performance scoring](https://developer.chrome.com/docs/lighthouse/performance/performance-scoring) | 2026-09-25 |
| [Moody's — FAQ](https://www.moodys.com/web/en/us/help-support/faq.html) | 2026-09-25 |
| [Moody's Ratings — Wikipedia (modifiers)](https://en.wikipedia.org/wiki/Moody%27s_Ratings) | 2026-09-25 |
| [CoinGecko — Methodology](https://www.coingecko.com/en/methodology) | 2026-09-25 |

## Related

- [spike-single-number-cost.md](spike-single-number-cost.md) — what a
  composite score would destroy (#209)
- [spike-composite-undetermined.md](spike-composite-undetermined.md) — how a
  composite would handle undetermined inputs (#210)
- [docs/adr-reference-mids.md](adr-reference-mids.md) — why aggregation is
  refused even at two providers
- [docs/backlog.md](backlog.md) E2 — the V5 spike family this belongs to
