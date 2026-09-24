# Maintainer-owned areas, and why each is owned

[CONTRIBUTING.md](../CONTRIBUTING.md#maintainer-owned-areas) lists six areas and
gives one sentence of reasoning: *an error in any of them invalidates published
measurements rather than breaking a feature*. This document gives that
reasoning per area — what exactly is owned, what a mistake in it would publish,
and what defends it today.

**This is about blast radius, not gatekeeping.** The areas below are not closed
to contribution; they are places where an error does not fail a test suite, it
publishes a wrong number under the project's name. Discuss the approach first
and expect close review. The rest of the repository is open.

**Checked against the code at commit `c9bfb75`, 2026-09-24.** Every symbol,
file and test named below was read at that revision. Where a document says
something the code contradicts, it is reported as a finding rather than
repeated.

---

## The six areas at a glance

| Area | Where it lives | What a change moves | Defended by | Status |
|:---|:---|:---|:---|:---|
| [dex pricing arithmetic](#1-dex-pricing-arithmetic) | `dex/dex.go`, `route/route.go` (`quoteDEX`, `Quote.score`) | every rate, loss and verdict | `dex/dex_test.go`, `route/route_test.go`, `route/money.go` boundary walk | reachable — powers every response |
| [Verdict thresholds](#2-verdict-thresholds) | `route/route.go` | every verdict, and whether anything is recommended | `TestVerdictThresholds`, `TestVerdictThresholdBoundaries` | reachable |
| [Integrity taxonomy](#3-integrity-taxonomy) | `route/route.go` (`Integrity`, `classify`), `route/ladder.go` | the structural state on every response and every stored record | `route/integrity_snapshot_test.go` | reachable |
| [SEP-38 fee handling](#4-sep-38-fee-handling) | `sep38/sep38.go` (`normalize`) | the fee on any anchor-priced quote, in receive-asset units | `sep38/golden_test.go` against `testdata/golden/` | implemented, **unreachable** |
| [Check engine and composition](#5-check-engine-and-composition) | `checks/runner.go`, `checks/checks.go`, `route/wire.go` (`WithFindings`) | whether third-party observations can rewrite a headline | `TestFindingsDoNotMoveTheHeadline` | reachable |
| [Corridor health score](#6-corridor-health-score) | `route/health_score.go` | one published number blending five signals | `route/health_score_test.go` | implemented, **unreachable**, not on the wire |

---

## 1. `dex` pricing arithmetic

**What is owned.** The arithmetic that turns raw Horizon bytes into a rate and a
loss: parsing in `dex.StrictSendPaths`, the strict-send envelope and its
rejections, `Path.Rate`, `MeasureSlippage`, and the scoring path in
`route.quoteDEX` / `Quote.score` that produces `EffectiveRate`, `LossPct`,
`LossAmount` and the verdict.

**Why the blast radius is total.** Every published figure descends from it:
`floor`, `worst_loss`, each rung's `effective_rate`, `loss_pct` and `verdict`,
the recommendation, and every stored record's copy of all of them. There is no
downstream check that would notice a plausible-looking error — a rate off by a
decimal place is a rate, and it grades a corridor.

**What defends it.**

- **No binary floating point anywhere in the path.** Money is
  `decimal.Decimal` (CONTRIBUTING invariant). The upstream payload is decoded
  into `json.RawMessage` and only then parsed, so the provider's digits reach
  `decimal` exactly as published rather than through a `float64`
  (`refrate/exchangerate.go`, `refrate/currencyapi.go`, both with the same
  rationale in their doc comments).
- **Broken answers are refused, not priced.** A non-positive
  `destination_amount` is a corrupted response rather than a 100% loss; an
  amount with more than the 7 decimals a Stellar asset carries (`stellarMaxScale
  = -7`) is rejected rather than trimmed; a `null` path record is corruption
  rather than a zero-valued path (`dex/dex.go`).
- **Horizon's ordering is not trusted.** `quoteDEX` selects the maximum
  destination amount explicitly, because "best first" is not a documented
  guarantee (`route/route.go`).
- **The boundary is walked.** `route.MoneyStrings` enumerates every money-valued
  string in a corridor document in document order, including the cost blocks,
  and the boundary tests parse each one with `decimal.NewFromString` across all
  three producers of the shape (backlog #6). Adding a money field without
  teaching the walker about it is the failure that test exists to catch.

**Adjacent but not on the list.** `route.Decompose` (`route/cost.go`) produces
the per-rung `cost` block from the same price and is governed by the same
review expectation. It is not one of CONTRIBUTING's six, and it carries a TODO
for the anchor-fee component that is still unwired.

---

## 2. Verdict thresholds

**What is owned.** `ThresholdGood` (3), `ThresholdFair` (8), `ThresholdPoor`
(20), `verdictFor`, and `Verdict.Acceptable` (`route/route.go`).

**Why the blast radius is total, twice over.**

1. Every published `verdict` is a function of these three numbers.
2. Because `Acceptable` is what gates a recommendation — and because the
   recommendation rule says *recommend nothing when no size is `POOR` or
   better* — moving `ThresholdPoor` changes **whether the product recommends
   anything at all** on a corridor. A threshold change is also a product
   decision, not a tuning knob.

Stored records carry verdict strings, so a threshold change also changes how
old history reads.

**The anchoring is the point.** The bands are set where established remittance
corridors run — a total cost in the 3–8% band — so `GOOD` means "as good as what
already exists" rather than "good for a DEX". Benchmarks anchored to on-chain
norms instead would have graded this project's own findings as acceptable
(`route/route.go`, [README.md](../README.md#verdict-thresholds--breaking-if-altered)).

**What defends it.** `TestVerdictThresholds` covers the documented values and
`TestVerdictThresholdBoundaries` pins nine values immediately below, at, and
immediately above each threshold — `2.999 / 3.0 / 3.001`,
`7.999 / 8.0 / 8.001`, `19.999 / 20.0 / 20.001` — as decimal strings rather
than floats, asserting both the verdict and `Acceptable()`. The comment on that
test states its purpose: nothing else in the suite fails if a refactor quietly
swaps `LessThanOrEqual` for `LessThan`.

**A finding.** [README.md](../README.md#verdict-reconciliation--the-published-number-always-matches-the-grade)
and [docs/metrics.md](metrics.md) both cite a test named
`TestLossPctReconcilesWithVerdict`. **No test with that name exists in this
tree.** The threshold boundary discipline they describe is real and is enforced
by `TestVerdictThresholds` and `TestVerdictThresholdBoundaries` in
`route/route_test.go`. The claim about the behaviour holds; the citation is
stale.

---

## 3. Integrity taxonomy

**What is owned.** The four states and their meanings — `DIRECT`, `DERIVATIVE`,
`NO-MARKET`, `UNKNOWN` — the classification in `route.classify`, the ladder-level
roll-up in `LadderResult.summarise`, and the hop classification in
`asset/known.go` (`HopFiat` / `HopBridge`) that decides which intermediate
assets count as a fiat dependency.

**Why it is owned.** Integrity is carried **alongside** the verdict rather than
folded into it, and the reason is the useful part: a corridor that prices
continuously and prices badly, one that inherits another token's failure, and
one with no market at all are three different situations for anyone deciding
whether to build on a corridor
([docs/corridor-measurements.md](corridor-measurements.md#three-distinct-failure-modes-which-a-single-grade-cannot-express)).
Collapsing them discards that distinction, and the published `integrity` on
every response — and every stored record — would then mean something narrower
than it does today.

The load-bearing edge is `UNKNOWN` against `NO-MARKET`. Both produce identical
zero-valued figures; only the state separates "nothing was learned" from
"nothing exists". A classification change that merged them would let a network
outage be published as a finding about the corridor.

**What defends it.** `classify` examines **every** path, not the best one: a
single path avoiding fiat intermediaries is enough to disprove a `DERIVATIVE`
claim (`route/route.go`). An unregistered hop is treated as a bridge hop — a
documented, bounded false-negative in `asset/known.go` — and the coverage gap is
surfaced as a note rather than left silent. `route/integrity_snapshot_test.go`
pins classification against recorded bytes.

**Change cost.** This is a vocabulary that appears in stored history. Renaming or
redefining a state makes existing records read differently from the way they
read when they were written; treat it as a migration, not a rename.

---

## 4. SEP-38 fee handling

**What is owned.** `sep38.Quote.normalize` and the fee-denomination identity it
implements. SEP-38 returns a fee that may be denominated in **either** the sell
or the buy asset, and the naive handling — adding `fee.total` to `buy_amount` —
adds units of one asset to units of another. This package derives the
denomination-independent expression instead:

```
GrossBuyAmount = SellAmount / Price
FeeInBuyAsset  = GrossBuyAmount - BuyAmount
```

which needs no branch on `fee.asset` and yields the fee in the currency the
recipient is counting (`sep38/sep38.go`, package doc).

**Why it is owned.** The error is silent and directionally wrong. In SEP-0038's
own worked example — sell 542 BRL, buy 100 USDC, fee 42 denominated in BRL —
the correct answer is **8.4 USDC**, and the naive one reports **42 USDC**: the
fee overstated by the price factor. On a corridor whose output is a headline
loss percentage, that is the difference between a corridor and a fiction.

**What defends it.** `sep38/golden_test.go` pins the spec's own worked example
plus the cases a single example would not reach: a fee already denominated in
the buy asset (where `FeeInBuyAsset` must equal `fee.total` exactly, with no
conversion), both examples with the assets reversed, and a response with no fee
at all (where the identity must yield zero — not absent, not NaN). Fixtures live
in `sep38/testdata/golden/`. A quote whose own numbers imply a **negative**
converted fee is refused rather than presented as a bonus.

The fixtures are the spec's example rather than one derived from this code,
deliberately: a fixture you derived from your own implementation proves nothing
(CONTRIBUTING, *Code conventions*).

**Status: implemented, tested, unreachable.** Nothing outside the package's own
tests calls it — `route/cost.go` still carries the TODO for extracting an anchor
fee from a `sep38.Quote` "when a corridor with SEP-38 support is wired in". So
the blast radius today is prospective, which is exactly why the arithmetic is
owned now: the first corridor that does wire it in will publish on top of it.

---

## 5. Check engine and composition

**What is owned.** `checks.Runner` (`Default`, `ForAsset`, the resolve-once
structure), `checks.RunAll`, the three-valued result model
(`Determined` / `Passed` / `Reason`) with its severity ordering, and above all
`route.WithFindings` — the composition point.

**Why composition, specifically.** `WithFindings` is the only place check
results meet a corridor document, and it copies every headline field through
unchanged: there is no branch on severity, on failure counts, or on any check's
identity. The rule it enforces is *checks qualify the headline, they never move
it* — no result, at any severity, may change `integrity` or a verdict.

That rule exists because the alternative is unfalsifiable output. If a
third-party observation could rewrite a headline, a reader could no longer tell
whether a corridor was downgraded because its liquidity moved or because
somebody added a check.

**What defends it.** `TestFindingsDoNotMoveTheHeadline` (`route/findings_test.go`)
attacks the composition point directly. A second, quieter property is defended
in the same function: **absent and empty must not look the same.** A `nil`
findings block and a findings block with zero results are different claims —
"not checked" against "checked, nothing found" — so `WithFindings` returns the
document untouched rather than attaching an empty block.

**What is not owned, and is wanted.** *Individual* checks and metrics are
exactly the contribution this project asks for — see
[docs/checks.md](checks.md) and [docs/metrics.md](metrics.md). The engine and
the composition rule are owned; a new check that reports its own fact, and
carries a reason when it cannot determine one, is not.

**A finding.** The comment above `Runner.Default()` reads *"Deliberately small.
These three are the worked examples contributors copy."* The list it sits above
returns **seven** checks. The comment is stale; `docs/contributor-faq.md`
records the same contradiction.

---

## 6. Corridor health score

**What is owned.** `route/health_score.go`: the five weights
(`DefaultHealthScoreWeights`, currently 0.2 each), the normalisation ceilings
(`maxSpread` 0.5, `maxDepth` 50, `maxPriceImpact` 50, `maxConcentration` 1,
`maxCostLoss` 50), and the rule that the score is **undetermined whenever any
input is undetermined** rather than defaulting the gap to zero.

**Why it is owned.** Unlike a metric, which reports one quantity with a unit,
this is a single published number blending five signals — a judgement of the
same class as the verdict bands, and it would be read the way a grade is read.
The weights are labelled provisional in the code itself: *"they will be adjusted
once the five component metrics are reachable and producing real data"*.

**Status: implemented, tested, and unreachable.** There is no non-test caller of
`HealthScore` or `HealthScoreWeighted` in this tree, and `HealthScoreResult.Value`
is tagged `json:"-"`, so the blended number is deliberately absent from the wire
even if it were wired up. Its inputs are the four market-quality metrics — which
`checks.Runner` cannot run — plus `route.Decompose`'s total. Since the metrics
are unreachable, every input would be undetermined today, and the undetermined
rule would make the score report nothing at all.

**A finding.** The list in [CONTRIBUTING.md](../CONTRIBUTING.md#maintainer-owned-areas)
and the summary in [docs/contributor-faq.md](contributor-faq.md#which-parts-are-maintainer-owned)
both qualify this area with *"when it exists"*, and README described it as *"not
yet designed"*. It **does** exist in this tree, with tests, and it is unreachable
rather than undesigned. The distinction matters for contributors: the shape is
not open for redesign, and the numbers are the maintainer's. README's phrasing
was corrected with this document; the *"when it exists"* qualifier in the other
two remains and is now read as "when it is reachable".

---

## What is not owned

Everything else is open contribution, and adding a corridor is the
highest-value first contribution ([docs/adding-a-corridor.md](adding-a-corridor.md)):

- the UI (`server/index.html`) and the HTTP surface around the contracts
- the CLIs (`cmd/ladder`, `cmd/wayfared`, `cmd/hop-analysis`)
- documentation
- tests — including tests *of* the owned areas
- new corridors
- reference providers
- storage backends
- **individual checks and metrics**, which are the engine's inputs rather than
  the engine

---

## Proposing a change in an owned area

1. **Open an issue first** and say what would move and why. The reasoning above
   is what a reviewer will hold the change against, so the proposal should
   address it directly.
2. **Say how you verified the claim.** If the change rests on a measurement,
   include the raw figures and the timestamp, with its source
   ([CONTRIBUTING.md](../CONTRIBUTING.md#measurement-discipline)).
3. **Expect the change and the evidence to be reviewed together.** A correct
   implementation of a threshold nobody agreed on is still a rejected change.
4. **If the work turns out to need a threshold, an integrity semantic, the
   composition rule or the run-record layout changed, stop and flag it** — that
   is a different change with a different review bar, as the issue template for
   these areas says.

---

## Verification

Every claim above was checked at commit `c9bfb75` on 2026-09-24 by reading the
named file, not by trusting the document that mentions it:

- the owned symbols, defaults and thresholds were read from `route/route.go`,
  `route/health_score.go`, `checks/runner.go`, `route/wire.go`, `sep38/sep38.go`
  and `dex/dex.go`;
- reachability was established by searching the tree for non-test callers —
  `HealthScore`, `HealthScoreWeighted`, `RunMetric` and the `sep38` package have
  none outside their own tests, which is why they are marked unreachable;
- the defending tests were located by name, and the three stale citations are
  reported as findings rather than repeated.

No verdict threshold, integrity semantic, check composition rule or run-record
field is changed by this document, and none is proposed.

---

## Related documents

- [CONTRIBUTING.md](../CONTRIBUTING.md#maintainer-owned-areas) — the list this
  document expands, and the invariants a change must not break
- [README.md](../README.md#where-to-start) — the same list in the contributor
  orientation section, with the labelling convention
- [docs/checks.md](checks.md) and [docs/metrics.md](metrics.md) — the contracts
  for the individual checks and metrics that *are* open contribution
- [docs/glossary.md](glossary.md) — the verdict, integrity and agreement
  vocabularies in full
- [docs/backlog.md](backlog.md) — entry #190 (issue
  [#250](https://github.com/Wayfare-labs/wayfare/issues/250)) is this document
