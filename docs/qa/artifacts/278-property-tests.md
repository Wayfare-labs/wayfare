# #278 — Property-based tests for money arithmetic

**Backlog entry H4 (#220).** Rate, loss and percentage conversions must hold
for arbitrary decimals, not only the values someone thought to write down.

## What was added

Four hand-rolled property suites — the repo carries no third-party test
dependencies (CONTRIBUTING), so generation uses only `math/rand` and
`shopspring/decimal`:

| Suite | Under test | Properties |
|:---|:---|:---|
| `route/props_test.go` | `(*Quote).score`, `verdictFor` | rate round-trips (`send × EffectiveRate ≈ receive`); loss never negative; zero loss exactly when the route pays ≥ mid (modulo 16-place division); `LossPct` and `LossAmount` describe the same shortfall; verdict matches the documented 3/8/20 thresholds including boundaries; more loss never buys a better verdict; zero send or zero mid yields UNKNOWN, never a computed verdict |
| `refrate/props_test.go` | `reconcile` | divergence never negative; divergence symmetric in which provider is primary; `divergence × lo ≈ hi − lo`; identical mids cannot diverge; band placement follows the 2%/10% thresholds; DISAGREE always scores against the **larger** mid (the conservative one); AGREE never replaces the primary; a zero mid from either feed is MALFUNCTION with a stated reason; STALE still scores, against the fresher rate |
| `sep38/props_test.go` | `(*Quote).normalize` | `GrossBuyAmount × Price ≈ SellAmount`; derived fee equals the fee charged; `TotalPrice ≥ Price` (the all-in rate never beats the advertised one); `TotalPrice × BuyAmount ≈ SellAmount`; zero-fee quotes derive exactly zero fee and `TotalPrice ≈ Price`; non-positive prices, negative amounts, and buy-above-gross quotes are all refused |
| `dex/props_test.go` | `Path.Rate` | `Rate × SourceAmount ≈ DestAmount` within one ulp; uniform scaling of both sides leaves the rate unmoved; a zero source amount yields zero without attempting the division |

## How to run, and reproducibility

```bash
go test ./route ./refrate ./sep38 ./dex -run TestProps -v
```

Generation is deterministic: each suite uses a fixed seed (`20260926`) and
every failure message carries the seed and case index, so a counterexample
is reproducible by anyone without the original run. Case counts per run:
route 1400, refrate 650, sep38 800, dex 500 — roughly 3,300 random
money-shaped inputs (mantissa 1..999 × 10^e, e ∈ [−7, 7]) per `go test`.

Tolerances: `shopspring/decimal` division rounds at `DivisionPrecision = 16`
decimal **places**, so round-trip identities are bounded absolutely (one ulp
of the quotient times the multiplier) rather than relatively — a relative
tolerance would be wrong for quotients below 1, which the generators do
produce.

## Result

All properties hold for every case run. Nothing found that contradicts the
documentation, with one exception:

## Finding — filed separately, not fixed here

`sep38.(*Quote).normalize` **accepts a zero `buy_amount`** and leaves
`TotalPrice` at its zero value. A quote of `price=1500, sell_amount=150,
buy_amount=0` normalises without error and reports `total_price` of `0` —
a figure the anchor never produced, presented as the all-in rate the user
"experiences". This is the shape the project's own hard constraint forbids
("an unavailable quantity is unknown, never zero and never a default"; the
division `sell/buy` is undefined here, and the honest outcomes are refusal
or an explicit unknown).

Reproduction:

```go
q := sep38.Quote{Price: decimal.NewFromInt(1500),
	SellAmount: decimal.NewFromInt(150), BuyAmount: decimal.Zero}
err := q.normalize() // err == nil, q.TotalPrice == 0
```

Per the issue's constraints this is recorded here, with the repro above, for
filing as its own issue; no fix is attempted in this PR.
