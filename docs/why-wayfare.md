# Why Wayfare?

Why a quoted rate is not a rate you can get, why only executable value can be
graded, and why **"do not send this"** is sometimes the correct answer rather
than a failure of the tool.

This is the product story, and every figure in it is a recorded measurement
with its date and source. Where the repository does not support a claim, the
claim is not made.

**Checked against the code and the recorded measurements at commit `c9bfb75`,
2026-09-24.**

---

## The one-paragraph version

A rate is a number, and a number that looks reasonable is the most expensive
thing this project could publish. Wayfare prices a stablecoin → fiat-token
corridor at a range of trade sizes through the same pathfinder that would
execute the payment, scores the result against an independent mid-market rate,
and — when no size delivers value worth having — says so and recommends
nothing. The recommendation is withheld on every corridor measured here, and
that is the point: a ranking implies its winner is worth taking, and on these
corridors that implication is false at every size tested.

---

## 1. A quoted rate is not an executable rate

Three separate things sit between the rate on a screen and what arrives at the
other end, and each one is a decision about what to believe.

**An order book is not the market.** Stellar settles path payments against
order book offers *and* AMM liquidity pools together, while the order book
endpoint reports only the offers. A router that walked bids would therefore
both misprice the route and fail to see liquidity that genuinely exists. The
discrepancy is recorded in the `dex` package doc from a live query on
2026-08-04: the USDC/NGNC order book showed a best bid of 333.33 NGNC per USDC
with 2,184.54 units of depth, while Horizon's own strict-send pathfinder,
asked to price 100 USDC over the same market at the same moment, returned
21,785.78 NGNC — a figure that cannot be reconstructed from those order book
levels under either reading of the amount field (`dex/dex.go`).

So Wayfare does not walk the book. It asks `/paths/strict-send`, the engine
that would actually execute the payment, and takes the answer as the price.
The order book is still fetched, but only as a market-health diagnostic, where
its limitations cannot move a verdict.

**Price is size-dependent.** A single quote is a single point on a curve, and
the shape of that curve is the finding. The USDC → NGNC ladder measured
2026-08-08 prices twelve sizes from 0.1 to 5,000 USDC
([docs/corridor-measurements.md](corridor-measurements.md)):

| Send (USDC) | Receive (NGNC) | Effective rate | Loss vs mid | Verdict |
|---:|---:|---:|---:|:---|
| 0.1 | 102.78 | 1,027.84 | 24.65% | UNUSABLE |
| 100 | 62,890.83 | 628.91 | 53.89% | UNUSABLE |
| 1,000 | 140,903.54 | 140.90 | 89.67% | UNUSABLE |
| 5,000 | 158,365.19 | 31.67 | 97.68% | UNUSABLE |

Reference mid 1364.0070 USD/NGN. The curve has no local minimum, no plateau,
and no band where it dips back toward usable: every step up in size is strictly
worse than the one below it.

**"What arrives" is a token, not money in an account.** The on-chain leg ends
in NGNC, and someone must still redeem that for naira — a separate step with
its own cost, which the figure does not include. Every quote this project
produces carries that warning (`route/route.go`). A comparison that ignored it
would be comparing two different products.

---

## 2. Why executable value is the only thing worth grading

Once you price executable value, a surprising thing happens: the useful answer
is often not a number at all.

### The loss has a structural floor, not a depth problem

At 0.1 USDC the trade routes direct, `USDC → NGNC`, with no bridge hop, and
price impact is negligible. It still loses **24.65%** against mid. That floor is
not liquidity — it is the price at which NGNC trades against its own peg, and it
already exceeds the 20% `UNUSABLE` threshold on its own. **No trade size can be
acceptable, because the corridor's zero-size limit is already unacceptable**
([docs/corridor-measurements.md](corridor-measurements.md#1-there-is-a-structural-floor-of-roughly-2465-before-any-slippage)).

That is the single most important consequence of measuring at several sizes. A
one-size quote at 5,000 USDC would show 97.68% and look like a depth problem
that better liquidity could fix. The dust rung is what proves it is not: the
corridor is broken before depth enters the picture at all.

### The measurement is stable, not a bad snapshot

The same corridor measured 2026-08-04 and again 2026-08-08
([docs/corridor-measurements.md](corridor-measurements.md#comparison-with-the-2026-08-04-run)):

| | 2026-08-04 | 2026-08-08 |
|:---|---:|---:|
| 100 USDC receives | 65,150.86 NGNC | 62,890.83 NGNC |
| Effective rate | 651.51 | 628.91 |
| Reference mid | 1364.77 | 1364.0070 |
| Loss | 52.3% | 53.89% |

Four days apart, and slightly worse. This is not a transient outage or a single
unlucky reading, and saying so requires the dates.

### And it is not universal: the tool grades what it finds

The same tool, on a second issuer's naira token, measured 2026-08-27:

| Send (USDC) | Receive (NGNT) | Loss vs mid | Verdict |
|---:|---:|---:|:---|
| 0.1 | 1,276.60 | 0.00% | GOOD |
| 100 | 513,603.08 | 0.00% | GOOD |
| 1,000 | 1,249,352.68 | 7.15% | FAIR |
| 2,500 | 2,028,044.86 | 39.71% | UNUSABLE |
| 5,000 | 2,936,669.79 | 56.35% | UNUSABLE |

Reference mid 1345.6228 USD/NGN. Here nine of twelve rungs grade `GOOD`, the
1,000 rung is `FAIR`, and the corridor only becomes unusable once size exhausts
liquidity ([docs/corridor-measurements.md](corridor-measurements.md#cowrie-ngnt-corridor-run-of-2026-08-27)).

This is in the product story deliberately. A tool that reported only bad news
would be indistinguishable from one that reported nothing, and the
`docs/backlog.md` rule applies to this document as much as to a measurement: an
unflattering finding and a flattering one get the same treatment, and a
negative or inconclusive result is a valid result.

---

## 3. Why the comparison needs an independent rate

The `refrate` package exists because of a measurement, not a feature request.
On 2026-08-04 the best on-chain route for USDC → NGNC returned roughly 651 NGN
per USD while the real-world rate sat near 1,500. Presented on its own, "651"
looks like a perfectly reasonable number. It is only recognisable as a
catastrophic loss when compared against an outside source (`refrate/refrate.go`).

So the reference mid is a **required dependency, not an optional enrichment**.
Without it the engine can rank, but it cannot tell a good deal from a disaster,
and `route.Engine` refuses to price at all rather than degrading to "no spread
shown" (`route/route.go`).

Three properties of that benchmark are load-bearing:

**Two providers, never averaged.** A blended mid names no provider, and every
figure published has to be traceable to a source a reader can check. One mid is
chosen and the record says which. When the providers genuinely disagree, the
one producing the **higher** loss is chosen — the more conservative reading —
because the failure this project most needs to avoid is flattering a corridor.
Beyond a 10% gap neither is used and no verdict is issued, because at that
distance they are not disagreeing about the rate, they are measuring different
things ([docs/glossary.md](glossary.md#reference-agreement)).

**The benchmark is the charitable one.** The reference is the official /
interbank USD/NGN rate. If the rate people actually transact at is weaker, every
loss figure here *understates* the real cost, because the same NGNC output is
being measured against a larger fair value. Choosing the official rate does not
flatter the corridor — it is the most generous benchmark available, and the
corridor fails against it at every size
([docs/fair-value-ngn.md](fair-value-ngn.md),
[docs/parallel-rate-research.md](parallel-rate-research.md)).

**Divergence between the providers is itself a measurement.** It is reported
alongside the corridor and never feeds back into a verdict: a benchmark whose
providers increasingly disagree is a fact about the benchmark
([docs/glossary.md](glossary.md#divergence-trend)).

---

## 4. Why "do not send this" can be the correct answer

> **When no size produces a verdict of `POOR` or better, the monitor recommends
> nothing.**

Not the best of a bad set. Nothing. This is the product thesis, so it is stated
as a contract in [README.md](../README.md) rather than left in a doc comment,
and it is enforced in code: `Result.Recommended` stays `nil`, `LadderResult`
summarises a `Recommended` of `nil`, and `cmd/ladder` exits non-zero so a script
can detect it without parsing prose (`route/route.go`, `route/ladder.go`,
`cmd/ladder/main.go`).

Three reasons the alternative is worse than saying nothing:

**A ranking carries a hidden assumption.** That its winner is worth taking. On
a broken corridor that assumption is false, and the cost of the implication is
not borne by the tool. A tool that displayed *"best route: 62,900 NGNC"* would
be accurate, useful-looking, and would have cost the sender more than half of
what they sent.

**Absence has to be unambiguous on the wire.** `recommended` is **always
present and `null`** in that case — never omitted — so a client cannot read its
absence as an oversight and substitute the best-scoring quote of its own
accord.

**A plausible number is the specific failure mode.** An unavailable fact is
reported as unknown, never as a default, a zero, or a fallback to a
plausible-looking constant. The same discipline governs the reference rate, the
metrics, and the check results: an anchor that publishes no SEP-38 endpoint is
reported as such, never estimated.

---

## 5. Why one grade is not enough

Three corridors from one issuer, measured in a single 60-second window on
2026-08-08, fail in three structurally different ways
([docs/corridor-measurements.md](corridor-measurements.md#what-the-sweep-shows)):

| Mode | Corridor | Observation |
|:---|:---|:---|
| **Live, value-destroying** | USDC → NGNC | The issuer declares `status="live"`. A market exists and prices continuously. It loses 24.65% at 0.1 USDC and 97.68% at 5,000. |
| **Derivative** | USDC → GHSC | No independent market: every path at every size traverses NGNC, so the corridor carries NGNC's loss plus its own. It floors at 74.14%. |
| **No-market** | USDC → KESC | Horizon returns zero paths at every size tested. Not a poor price — no price. |

Reporting all three as "Unusable" would be accurate and would discard the reason
each is unusable, and the reason is the useful part. So the monitor publishes an
**integrity state** alongside the verdict rather than folding one into the
other — `DIRECT`, `DERIVATIVE`, `NO-MARKET`, `UNKNOWN`
([docs/glossary.md](glossary.md#integrity)).

`UNKNOWN` earns its place for the same reason: it separates "nothing was
learned" from "nothing exists". Both produce identical zero-valued figures, and
a caller that conflated them would publish "0.00% floor loss" as a measurement
of the corridor when it was a measurement of the network.

Total on-chain liquidity reachable from USDC across all three of that issuer's
tokens, valued at the reference mid, was approximately **$142.88**. The pattern
held across the whole issuer set: not one of the twenty-four priced points
reached `POOR`, let alone `FAIR` or `GOOD`, and the engine recommended nothing
anywhere.

---

## 6. What Wayfare is not

These keep the project shippable and legal for a small team, and they are
refusals rather than gaps:

- **Not a router.** The project began as one. Live mainnet measurement killed
  that thesis — which is why the measurement, not the interface, is the reason
  the tool exists in its current form.
- **Not an anchor.** Never issues tokens or holds reserves.
- **Not custodial.** Never takes possession of funds.
- **Not a money transmitter.** No custody, so no licensing surface.
- **Not a KYC provider.** Delegated to anchors via SEP-12.

Settlement primitives — escrow, custody, payment execution — are **explicitly
not planned**. Wayfare analyses corridors; it does not move money through them.

---

## 7. What this story does not claim

The version of this argument that is dishonest is the one that leaves the
bounded parts out. As of the date at the top of this document:

- **Only the DEX rail is measured.** Every quote in every response is
  `kind: "dex"`. No measured corridor has an anchor that publishes
  `ANCHOR_QUOTE_SERVER`, so no corridor's own rails can be priced, and a
  comparison against an anchor's SEP-38 quote has not been performed for any
  corridor. (A live SEP-38 round-trip *has* been performed against the
  `testanchor.stellar.org` sandbox on 2026-08-28 and is recorded in
  `sep38/testdata/live/` — a sandbox anchor, not a corridor.)
- **Market-quality metrics are merged but unreachable.** Spread, depth, price
  impact and concentration are implemented and tested, but `checks.Runner` has
  no way to run a `Metric`, so none has ever appeared in a response, been
  recorded, or been rendered. The published comparison today is loss against
  mid, not the full cost decomposition. See
  [docs/metrics.md](metrics.md).
- **The corridor health score is implemented and not reachable.** A 0–100 blend
  of five metric inputs exists in `route/health_score.go` with tests, and has no
  non-test caller; its `Value` is deliberately absent from the wire
  (`json:"-"`). It is also undetermined whenever any input is undetermined, so
  it would report nothing today. The weights are maintainer-owned and
  provisional.
- **The history is short and currently frozen.** `runstore` has been collecting
  since August 2026, and the scheduled measure workflow cannot push
  ([#63](https://github.com/Wayfare-labs/wayfare/issues/63)), so the served
  history is older than its six-hour cadence implies. Nothing in this document
  depends on longitudinal analysis, because there is not enough history to do
  any.
- **Layers 3 and 4 are not built and have no stubs.** Predictive intelligence
  needs observed failures that do not exist yet, and attestations would
  introduce a publisher-trust assumption the project does not currently have.

The governing rule for all of it is stated in [README.md](../README.md) and
applies to the argument above as much as to a response body:

> **A layer can never be more certain than the layer beneath it.**

---

## Related documents

- [docs/corridor-measurements.md](corridor-measurements.md) — the raw figures
  behind sections 2 and 5, with timestamps and the endpoint each came from
- [docs/glossary.md](glossary.md) — verdicts, integrity states, agreement
  bands, and what *not determined* means
- [docs/fair-value-ngn.md](fair-value-ngn.md) — what "fair value" means here and
  why the official mid is the charitable benchmark
- [docs/parallel-rate-research.md](parallel-rate-research.md) — whether a
  defensible parallel-rate source exists (no usable source was found)
- [README.md](../README.md) — the shared contracts: thresholds, the
  recommendation rule, integrity states, and the layer model
- [docs/backlog.md](backlog.md) — entry #191 (issue
  [#251](https://github.com/Wayfare-labs/wayfare/issues/251)) is this document
