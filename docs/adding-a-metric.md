# Adding a metric

[checks/metric.go](../checks/metric.go) documents the interface.
[checks/metric_spread.go](../checks/metric_spread.go) is the smallest
implementation, and this is the walkthrough that sits beside them: the same
path [`docs/adding-a-check.md`](adding-a-check.md) takes for a fact, walked once
for a quantity, end to end.

**Checked against the code at commit `35d7d8d`, 2026-09-25.** Every claim about
the code below was read from the tree at that commit. One thing it says is
unflattering and is stated plainly rather than skipped: **a metric added today
is compiled, validated, tested and callable, and does not appear in any
response.** [Step 6](#step-6--registering-there-is-nowhere-to-put-it-yet) is
that finding, and CONTRIBUTING.md's rule that a negative finding is a valid
result applies to this document as much as to a corridor.

---

## What you are adding

One quantity about a subject. Not a pass/fail verdict, not a score.

| | A metric answers | A check answers |
|:---|:---|:---|
| Shape | what is this quantity? | does this fact hold? |
| Result | a `decimal.Decimal` and a unit, or **not determined** | `determined` + `passed`, or **not determined** |
| Example | "the bid/ask spread is 128.8% of mid" | "the declared SEP-10 endpoint returns a challenge" |

The two shapes share one machinery — `Observation` and `Evidence` are identical
— and they are separate interfaces because forcing a quantity through pass/fail
discards the number that carries the meaning. A spread of 2% and a spread of
200% are both valid metric results. Deciding which of them is unacceptable is a
threshold, that is the same class of decision as the verdict bands, and it is
**maintainer-owned**: a contributor measures correctly without holding any
authority over what the measurement means.

If your contribution is a yes/no fact, read
[adding-a-check.md](adding-a-check.md) instead.

---

## Where the contract lives

| Piece | File | What it gives you |
|:---|:---|:---|
| `Metric` | `checks/metric.go` | `Describe() Descriptor` and `Run(ctx, Subject) MetricResult` |
| `RunMetric` | `checks/metric.go` | the panic guard, `ValidateAsMetric`, the context check, and evidence validation |
| `MetricValue`, `MetricUndetermined` | `checks/metric.go` | the two result constructors |
| `MetricResult` | `checks/checks.go` | `Value`, `Unit`, `Venue`, `Summary`, and an embedded `Observation` |
| `Descriptor.ValidateAsMetric` | `checks/checks.go` | the metric-only rules: the venue must be declared for a corridor metric, and must be a known one |
| `Unit`, `Venue` | `checks/checks.go` | the vocabulary |
| `structuralUndetermined`, `bookPair`, `bookSource` | `checks/metric_structural.go` | why a book metric cannot fetch, which pair it actually measured, and how to say so in evidence |
| Helpers | `checks/checks_test.go` | `recordedTransport`, `ctx()` and the fixture patterns tests use |

**Always run through `RunMetric`, never call `Run` directly.** `RunMetric`
installs its panic guard before `Describe` is even called, validates the
descriptor, honours a cancelled context, and replaces a determined result whose
evidence is incomplete (`Source` or `Observed` blank, `ObservedAt` zero) with an
undetermined one. A metric that panics becomes an undetermined result; it does
not take down a sweep.

---

## Step 1 — Name it, and choose a unit and a venue

IDs are `<source>.<fact>`, lower-case and hyphenated, matching the metric
implementation written as `<Source>Metric`. Five metric types are in the tree
today, declaring seven IDs between them — the worked-example library:

| ID | File | Scope | Cost | Venue | Unit |
|:---|:---|:---|:---|:---|:---|
| `spread.bid-ask` | `metric_spread.go` | corridor | one-request | order-book | percent |
| `depth.observed-executable` | `metric_depth.go` | corridor | expensive | order-book* | count / amount |
| `depth.observed` | `metric_depth.go` | corridor | one-request | order-book | count |
| `depth.executable` | `metric_depth.go` | corridor | expensive | pathfinding | amount |
| `price-impact.size` | `metric_price_impact.go` | corridor | expensive | pathfinding | percent |
| `concentration.liquidity` | `metric_concentration.go` | corridor | one-request | order-book | ratio |
| `deviation.book-vs-reference` | `metric_deviation.go` | corridor | one-request | order-book | percent |

\* `DepthMetric.Describe` declares `VenueOrderBook`; its `RunExecutable`
measurement declares `VenuePathfinding`. Both are in the tree, and the
distinction is the point of the field.

`DepthMetric` is also the model for a type that publishes two related
quantities: `Describe()` returns one descriptor while `RunObserved` and
`RunExecutable` build results with `depth.observed` and `depth.executable`. Read
it before inventing a second type.

**`Unit` is a closed set** — `percent`, `ratio`, `count`, `amount`, `seconds`
(`checks/checks.go`). Pick the one that describes what the number *is*, not what
it was derived from.

**`Venue` is required for a corridor metric and forbidden elsewhere.**
`ValidateAsMetric` rejects an unknown venue string, rejects a corridor metric
with no venue, and rejects a venue on an anchor or asset metric. The reason is
in the type's own doc comment: Horizon's `/order_book` reports offers only,
while `/paths/strict-send` prices through offers *and* AMM liquidity pools, so
two metrics over one corridor can describe different markets and cannot be
reconciled by arithmetic. A consumer that knows the venue can refuse to
reconcile them by machine; prose in `CannotDetermine` cannot be acted on.

**The ID is a key, not a label.** `route/health_score.go` names the IDs it
consumes — `spread.bid-ask`, `depth.observed-executable`, `price-impact.size` and
`concentration.liquidity`, plus a synthetic `cost.total_loss_pct` that comes from
`route.Decompose` rather than from a `checks.Metric` — and matches them by
string. Renaming an ID silently unhooks that metric from the health score rather
than failing to compile. The health score is itself merged and unreachable today
— see step 6 — so this is a trap for the future, which is exactly when it will be
forgotten.

---

## Step 2 — Decide what the subject must contain

`Runner.ForAsset` never populates `Subject.Send` or `Subject.Receive`, because
the runner has no metric path (step 6). A corridor metric is therefore written
against a subject a future caller supplies, and it must behave when the fields
it needs are absent.

The fields a corridor metric reads, and what each means when it is empty:

| Field | Meaning | When empty |
|:---|:---|:---|
| `Send`, `Receive` | the corridor's pair | undetermined — there is no pair to measure |
| `Integrity` | `DIRECT`, `DERIVATIVE`, `NO-MARKET`, or `""` | empty is **unknown**, not `DIRECT`; proceed and let the fetch decide |
| `Underlying` | the pair a `DERIVATIVE` corridor actually traverses | undetermined for `DERIVATIVE`, unless you are explicitly substituting |
| `ReferenceAgreement` | `MALFUNCTION` when the two reference mids diverged past tolerance | empty is unknown, and unknown is **not** malfunction |
| `DEX` | the Horizon client on your metric struct | undetermined — no client, no measurement |

`checks/metric_structural.go` exists so this reasoning is written once. Call
`structuralUndetermined(d, s)` **before any network call**: it returns a result
for `NO-MARKET` (no pair exists by construction) and for `DERIVATIVE` with no
`Underlying`, and a `false` that means "proceed". If you do substitute the
underlying pair, `bookPair` tells you which pair you are really measuring and
`bookSource` renders the evidence source in a form that names the substitution
— **the substitution is never silent.**

An unscorable reference is a harder rule than it looks. `DeviationMetric`
returns undetermined rather than a number when `ReferenceAgreement` is
`MALFUNCTION`, and `PriceImpactMetric` short-circuits *before* its network calls
for the same reason (`TestPriceImpactMetricMalfunctionShortCircuitsBeforeNetwork`).
If your metric is derived against a benchmark, apply the same discipline.

---

## Step 3 — Write `Describe()`

```go
func (MyMetric) Describe() Descriptor {
    return Descriptor{
        ID:    "source.fact",
        Scope: ScopeCorridor,
        Cost:  CostOneRequest,
        Venue: VenueOrderBook,
        Title: "One line naming what is measured",

        CanDetermine: "...",

        CannotDetermine: "...",
    }
}
```

A metric descriptor has no `Severity`: a quantity is not a problem, and severity
is the check vocabulary. `ValidateAsMetric` will reject a descriptor missing
`ID`, `Title`, `CanDetermine` or `CannotDetermine`, one with an unknown venue,
and a corridor metric with no venue — with an undetermined result, not a panic.

**`CannotDetermine` is where the honesty lives.** Write it by answering: *what
would a reader wrongly conclude from this number?* For the spread metric the
answer is that the figure may reflect only the top of book rather than
executable depth, and that the venue is order-book, so AMM liquidity is not in
it. For depth it is that the counted levels may be stale offers rather than
live liquidity.

---

## Step 4 — Write `Run()`

```go
func (m MyMetric) Run(ctx context.Context, s Subject) MetricResult {
    d := m.Describe()
    at := time.Now().UTC()

    if s.Send.Code == "" || s.Receive.Code == "" {
        return MetricUndetermined(d, s, "no send or receive asset specified")
    }
    if res, structural := structuralUndetermined(d, s); structural {
        return res
    }
    if m.DEX == nil {
        return MetricUndetermined(d, s, "no DEX client available to fetch the order book")
    }

    h, err := m.DEX.OrderBook(ctx, s.Send, s.Receive)
    if err != nil {
        return MetricUndetermined(d, s, fmt.Sprintf("order book fetch failed: %v", err))
    }

    ... // decide, then:
    return MetricValue(d, s, value, UnitPercent, summary, evidence)
}
```

Five rules.

**Unknown is never zero.** An unmeasurable result carries `Determined: false`,
never `Value: 0` — a zero is a plausible-looking number, and "a spread of
nothing" is a different fact from "a spread that could not be read". This has a
sharp edge in practice: `dex.BookHealth.Mid` is **zero when the book has no
two-sided mid** (`dex/health.go`), which is a sentinel in a value slot. A metric
that published it would put a plausible zero on the wire for a market that has
no mid at all. Translate the sentinel into `MetricUndetermined` explicitly, and
say which book shape produced it — one-sided, empty, or unreadable are three
different facts, and `SpreadMetric.Run` names all three.

Zero is still a valid *measurement* when it was measured. `DepthMetric` returns
a determined zero for a book that was read and is exhausted
(`TestDepthExhaustedProducesDeterminedZero`): the difference is that the zero is
`Determined: true`, with the book as its evidence. The rule is about unknown,
not about zero.

**Every determined result carries evidence.** `Source`, `Observed` and a
non-zero `ObservedAt`, with the value seen verbatim where practical — the
spread metric records bid, ask, mid, level counts and dust count in one
`Observed` string. `RunMetric` validates all three and replaces an incomplete
result with an undetermined one, so a missing `ObservedAt` loses your
observation rather than being tidied up later.

**Money and quantities are `decimal.Decimal`. Never `float64`.** It is a hard
constraint in CONTRIBUTING.md, and a metric whose entire purpose is measuring
small differences is where rounding drift does the most damage.

**Never panic, and honour the context.** `RunMetric` recovers a panic, but a
recovered panic is a lost measurement. `Run` receives a context and is expected
to honour it; a metric that can hang holds the whole sweep open, which is why
the runner bounds the sweep with its own timeout.

**Bound anything you read whole.** `maxErrorBody` (`checks/transport.go`) and
Horizon's `limit=200` are the existing examples: an upstream response is
payer-controlled data and this is a public service.

---

## Step 5 — Test it offline

CI runs the whole suite with no route out, and `snapshot.Replayer` returns
`ErrNotRecorded` rather than falling through to the network, so a test that
reaches out fails rather than passing intermittently.

| What to test | Existing example |
|:---|:---|
| A determined value from a recorded book | `TestSpreadMetricFromRecordedBook`, `TestConcentrationMetricFromRecordedBook` |
| An empty book | `TestSpreadMetricEmptyBook` |
| A one-sided book | `TestSpreadMetricOneSidedBook`, `TestDeviationMetricOneSidedBook` |
| No client at all | `TestSpreadMetricNilDEX`, `TestPriceImpactMetricNilDEX` |
| An empty subject | `TestSpreadMetricEmptySubject`, `TestPriceImpactCurveEmptySubject` |
| Structural versus market versus availability | `TestBookMetricsDistinguishNoMarketFromEmptyBook`, `TestBookMetricsDistinguishDerivativeFromEmptyBook` |
| The substitution being named in evidence | `TestBookMetricSubstitutesUnderlyingPairExplicitly` |
| The descriptor validating | `TestSpreadMetricDescriptorIsValid`, and `TestEveryCorridorMetricDeclaresAVenue` for the venue rule |
| The evidence rules themselves | `TestRunMetricBlankSourceProducesUndetermined`, `TestRunMetricZeroObservedAtProducesUndetermined` |
| Malfunction reference short-circuiting before the network | `TestPriceImpactMetricMalfunctionShortCircuitsBeforeNetwork` |

For a new metric, the cases worth a table test are:

```
send or receive asset missing        -> undetermined
NO-MARKET / DERIVATIVE with no pair  -> undetermined, and the reason says "by construction"
book fetched, two-sided             -> determined, with a decimal value
book fetched, one-sided or empty     -> undetermined, and the reason says which shape
fetch failed                         -> undetermined, reason names the failure
sentinel zero in the source type     -> undetermined, never Value: 0
a determined zero that was measured  -> determined, Value: 0
```

```bash
go test ./checks/ -run TestMyMetric -count=1
make fmt vet test race
```

---

## Step 6 — Registering: there is nowhere to put it yet

This is the step the check guide could take and this one cannot, and it is worth
stating with the code that shows it rather than as a caveat.

```
  checks.Runner                    checks/runner.go
    Metrics []Metric               <- does not exist
    ForAsset()                     -> RunAll(ctx, r.checks(), subject): checks only
    Subject.Send / Subject.Receive <- never populated by ForAsset
        │
        ╳  NOT REACHABLE
           RunMetric has no non-test caller.
           Findings.AddMetric has no non-test caller.
           FindingsJSON.Metrics has no non-test producer.

  route/health_score.go            consumes metric IDs by string
        │
        ╳  NOT REACHABLE — no non-test caller anywhere in the tree
```

`checks/wire.go` already serialises metrics (`MetricJSON`, and `Metrics` inside
`FindingsJSON` with `omitempty`), and `route.WithFindings` will attach whatever
is in `Findings.Metrics`. So the wire is ready and empty: on the live
deployment, `GET /api/corridor?to=NGNC` returns no `findings.metrics` key at
all, which is the correct output for "none were run" and would be
indistinguishable from "none were found" if the field were always present.

What this means for a contributed metric:

- **Write it, test it, and say what it is.** A metric is compiled, validated by
  `ValidateAsMetric`, callable through `RunMetric`, and covered by offline tests
  today. That is real work that will not need rewriting.
- **Do not invent a caller to reach it.** Adding `Metrics` to the runner means
  deciding how many upstream requests a sweep is allowed to spend, which is a
  budget question with a public-Horizon blast radius, and it is already scoped
  as [#91](https://github.com/Wayfare-labs/wayfare/issues/91) (backlog `#49`,
  labelled `needs-maintainer-review`). A PR that wires metrics by another route
  is a PR that changes how every corridor response is produced.
- **Do not describe it as published.** In the PR, in a doc, or in a comment:
  the honest sentence is *"implemented and tested; not yet reachable from
  `/api/corridor`"*. The README's own capability table uses that phrasing for
  exactly this situation, and a metric described as though it were on the wire
  is the failure mode this project exists to catch.
- **A threshold derived from your metric is a separate contribution.** "Spread
  above X is a failure" is a check built on a metric, and it belongs to the same
  maintainer-owned class as the verdict bands.

---

## The worked example: `book.mid`

A complete metric, written the way the repository writes them. It is an
illustration: nothing below is registered or run, and the file it would live in
is `checks/metric_book_mid.go`.

**The fact.** `SpreadMetric` computes `(ask - bid) / mid` and retains the mid
only as a diagnostic inside its evidence string. The mid is independently
useful — it is the number to compare a reference rate against, without
re-fetching the same order book — and publishing it as a quantity rather than
prose is what a metric is for.

```go
package checks

import (
    "context"
    "fmt"
    "time"

    "github.com/Wayfare-labs/wayfare/dex"
)

// BookMidMetric reports the mid-market rate implied by the corridor's direct
// order book.
//
// This is a metric, not a check: it produces a quantity in receive-asset units
// per send-asset unit, and it does not say whether that quantity is good. The
// mid is not a tradable price either — crossing the spread is what execution
// costs — which is why the descriptor says so rather than leaving it implied.
type BookMidMetric struct {
    // DEX is the Horizon client used to fetch the order book.
    DEX *dex.Client
}

// Describe implements Metric.
func (BookMidMetric) Describe() Descriptor {
    return Descriptor{
        ID:    "book.mid",
        Scope: ScopeCorridor,
        Cost:  CostOneRequest,
        Venue: VenueOrderBook,
        Title: "Mid-market rate implied by the direct order book",
        CanDetermine: "The mid-market rate implied by the best non-dust bid and ask " +
            "on the corridor's direct order book, in receive-asset units per " +
            "send-asset unit, with the levels it came from recorded as evidence.",
        CannotDetermine: "Whether that rate is executable: the mid is not a price " +
            "anyone offers, and crossing the spread costs the difference. Nor " +
            "does it account for AMM liquidity — the venue is order-book, which " +
            "reports offers only, while the ladder prices through pathfinding " +
            "and so includes pools. See docs/liquidity-venues.md.",
    }
}

// Run implements Metric.
//
// The order book's Mid is zero when the book has no two-sided mid at all,
// which is a sentinel rather than a measurement — dex.BookHealth documents it.
// It is translated into an undetermined result here so that a market with no
// mid cannot publish as a market whose mid is zero.
func (m BookMidMetric) Run(ctx context.Context, s Subject) MetricResult {
    d := m.Describe()
    at := time.Now().UTC()

    if s.Send.Code == "" || s.Receive.Code == "" {
        return MetricUndetermined(d, s, "no send or receive asset specified")
    }
    if res, structural := structuralUndetermined(d, s); structural {
        return res
    }
    if m.DEX == nil {
        return MetricUndetermined(d, s, "no DEX client available to fetch the order book")
    }

    sell, buy, substituted := bookPair(s)
    h, err := m.DEX.OrderBook(ctx, sell, buy)
    if err != nil {
        return MetricUndetermined(d, s, fmt.Sprintf("order book fetch failed: %v", err))
    }

    evidence := Evidence{
        Source: bookSource("/order_book", s, sell, buy, substituted),
        Observed: fmt.Sprintf("mid=%s, bid=%s, ask=%s, bids=%d, asks=%d, dust=%d",
            h.Mid, h.BestBid, h.BestAsk, h.BidLevels, h.AskLevels, h.DustLevels),
        ObservedAt: at,
    }

    switch {
    case h.BidLevels == 0 && h.AskLevels == 0:
        return MetricUndetermined(d, s,
            "order book is empty: no bids and no asks, so no mid can be implied", evidence)
    case h.BidLevels == 0:
        return MetricUndetermined(d, s,
            "one-sided market: no bids, so nothing to average the ask against", evidence)
    case h.AskLevels == 0:
        return MetricUndetermined(d, s,
            "one-sided market: no asks, so nothing to average the bid against", evidence)
    case h.Mid.IsZero():
        // Belt and braces: the levels are non-empty but no mid was set, which
        // means the source type could not form one. Publishing a zero here
        // would be the default-to-zero failure in a new place.
        return MetricUndetermined(d, s,
            "the book has levels on both sides but yielded no mid", evidence)
    }

    summary := fmt.Sprintf("mid %s from best bid %s and best ask %s (%d bid levels, %d ask levels)",
        h.Mid, h.BestBid, h.BestAsk, h.BidLevels, h.AskLevels)

    return MetricValue(d, s, h.Mid, UnitAmount, summary, evidence)
}
```

If it were registered, its wire form would be one entry in
`findings.metrics`:

```json
{
  "id": "book.mid",
  "scope": "corridor",
  "determined": true,
  "value": "1348.5",
  "unit": "amount",
  "venue": "order-book",
  "summary": "mid 1348.5 from best bid 1340 and best ask 1357 (3 bid levels, 2 ask levels)",
  "evidence": [ ... ],
  "observed_at": "2026-09-25T09:00:00Z"
}
```

Note what is absent: no verdict, no severity, no threshold. A consumer that
wants to grade this mid against the reference mid has to do so explicitly, in
the open, which is the separation the whole contract exists to keep.

**What it would report today: nothing.** `Runner.ForAsset` does not run metrics,
so this metric would compile, pass its tests, and appear in no response. That is
not a defect in the metric; it is the state of the plumbing, scoped as
[#91](https://github.com/Wayfare-labs/wayfare/issues/91).

---

## Checklist

- [ ] The quantity is one quantity, the ID is `<source>.<fact>`, and the ID is
      not already consumed by `route/health_score.go`
- [ ] `Unit` is one of the declared constants and describes what the number is
- [ ] `Venue` is declared for a corridor metric and empty elsewhere
- [ ] `CanDetermine` and `CannotDetermine` both say something a reader can act on
- [ ] `structuralUndetermined` is consulted before any network call
- [ ] Missing input produces `MetricUndetermined` with a reason, never a zero
- [ ] A sentinel zero in the source type is translated, never published
- [ ] Every determined result carries `Source`, `Observed` and a non-zero
      `ObservedAt`
- [ ] No new dependency; no `float64`; no unbounded read of an upstream body
- [ ] Tests cover determined, empty, one-sided, nil-client and structural
      cases, offline, through `RunMetric`
- [ ] The PR says plainly that the metric is implemented and tested but **not
      reachable** from `/api/corridor` yet
- [ ] `make fmt vet test race` is clean

## Related

- [checks/metric.go](../checks/metric.go) — the interface and its doc comment
- [checks/metric_structural.go](../checks/metric_structural.go) — structural,
  market and availability reasons, in one place
- [docs/metrics.md](metrics.md) — the methodology page for every metric; add
  yours there when it lands
- [docs/checks.md](checks.md) — the two-shape contract, and why they are
  separate
- [docs/adding-a-check.md](adding-a-check.md) — the other shape, for facts
- [docs/liquidity-venues.md](liquidity-venues.md) — why two venues cannot be
  reconciled by arithmetic
- [docs/maintainer-owned-areas.md](maintainer-owned-areas.md) — why the engine
  is maintainer-owned while individual metrics are open contribution
