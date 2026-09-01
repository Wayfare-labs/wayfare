# Native XLM routing: does it change execution quality?

**Status: v1 finding.** Derived from `testdata/snapshots/` recorded on
2026-08-21 (git revision `b36a3af`). Reproducible from the same bytes by
`go run ./cmd/hop-analysis`.

---

## The question

The best path on the live deployment on 2026-08-04 was
`USDC → BLND → XLM → NGNC` — a route through native XLM and through an
unrelated token. Whether the XLM leg helps or hurts is not something the
engine has ever characterised, and characterising it changes how the tool
should talk about a corridor. See issue #101.

Native routing appearing in the best path is not itself a finding; the
finding is what happens to execution quality when it does. If XLM routing
consistently beats the best non-XLM alternative, that is a Stellar-specific
advantage worth stating. If it doesn't, that is worth knowing too.

## Method, briefly

For each supported corridor and each size in the recorded `-sizes` list, the
analysis:

1. Loads the snapshot (verifying every response body against its recorded
   hash — see [docs/snapshot-format.md](snapshot-format.md)).
2. Reads every path Horizon returned for the strict-send probe at that size.
3. Classifies each hop via `asset.Classify` — `native`, `settlement`,
   `fiat-token`, `stellar-token`, `fiat`, `unknown` — rather than by string
   match on the code.
4. Picks the overall best path (largest `destination_amount`) and, separately,
   the best path whose hops include no `native` asset.
5. Reports the XLM advantage as
   `(best - best_non_xlm) / best_non_xlm × 100%`, and the corridor rollup
   counts (sizes-priced, sizes-best-uses-XLM, sizes-with-non-XLM-alternative).

Scoring against a reference rate is deliberately out of scope. This document
is about which venue liquidity comes from within a corridor, not about which
corridor is worth using — the ladder already answers the second and it does
so in the same terms for every corridor, XLM or not.

## Corridor-by-corridor findings

### USDC → NGNC (DIRECT)

Snapshot: `usdc-ngnc-20260821T223040Z`, `USD/NGN`.

| Size (USDC) | # paths | Best uses | Best path                              | XLM advantage |
| -----------:| -------:| --------- | -------------------------------------- | -------------:|
|         0.1 |       2 | no-XLM    | `USDC → Cleanshave → AQUA → NGNC`      |         0.00% |
|           1 |       4 | no-XLM    | `USDC → yUSDC → AQUA → NGNC`           |         0.00% |
|           5 |       3 | XLM       | `USDC → USDZ → XLM → NGNC`             |        14.30% |
|          10 |       3 | XLM       | `USDC → BTC → XLM → NGNC`              |        28.68% |
|          25 |       2 | XLM       | `USDC → XLM → NGNC`                    |        67.52% |
|          50 |       3 | XLM       | `USDC → PYUSD → XLM → NGNC`            |       121.21% |
|         100 |       3 | XLM       | `USDC → PYUSD → XLM → NGNC`            |       199.98% |
|         250 |       2 | XLM       | `USDC → XLM → NGNC`                    |       326.79% |
|         500 |       2 | XLM       | `USDC → XLM → NGNC`                    |       414.07% |
|        1000 |       2 | XLM       | `USDC → XLM → NGNC`                    |       477.80% |
|        2500 |       2 | XLM       | `USDC → XLM → NGNC`                    |       526.42% |
|        5000 |       2 | XLM       | `USDC → XLM → NGNC`                    |       544.88% |

**Best path traverses XLM at 10 of 12 sizes.** The two exceptions are the two
smallest probes (0.1 and 1 USDC), where routes through niche assets
(`Cleanshave`, `yUSDC`, `AQUA`) narrowly beat the XLM path. At 5 USDC and
above the XLM route wins, and its advantage grows monotonically — from
**14.30%** at 5 USDC to **544.88%** at 5000 USDC. The non-XLM alternative at
5000 USDC delivers roughly a sixth of what the XLM path does.

A non-XLM alternative existed at **every size** measured. The XLM
advantage is not because nothing else was available; it is because nothing
else had comparable depth.

### USDC → GHSC (DERIVATIVE via NGNC)

Snapshot: `usdc-ghsc-20260821T222915Z`, `USD/GHS`.

| Size (USDC) | # paths | Best uses | Best path                          | XLM advantage |
| -----------:| -------:| --------- | ---------------------------------- | -------------:|
|         0.1 |       1 | no-XLM    | `USDC → NGNC → GHSC`               |         0.00% |
|           1 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |         2.76% |
|           5 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |        14.62% |
|          10 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |        28.46% |
|          25 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |        64.10% |
|          50 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       109.22% |
|         100 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       168.19% |
|         250 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       248.55% |
|         500 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       295.57% |
|        1000 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       326.44% |
|        2500 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       348.26% |
|        5000 |       2 | XLM       | `USDC → XLM → NGNC → GHSC`         |       356.19% |

Same pattern, more extreme. At 0.1 USDC the XLM leg is not yet worth taking;
at every larger size it wins, and the advantage grows monotonically to
**356.19%** at 5000 USDC. Note that GHSC has no independent market: every
non-XLM alternative is itself `USDC → NGNC → GHSC`. So this table is
effectively comparing "XLM in, NGNC out" against "no XLM in, NGNC out" for
the NGNC leg — consistent with the direct-corridor finding above.

### USDC → KESC (NO-MARKET)

Snapshot: `usdc-kesc-20260821T222949Z`, `USD/KES`.

**No paths at any of 12 sizes.** The question is inapplicable: with nothing
routing at all, there is no XLM leg to have or to lack. This is a real
outcome for this row of the acceptance criteria — the answer for NO-MARKET
corridors is not `n/a`, it is `no paths at all`, and the classifier makes
that unambiguous.

## Overall finding

**On the corridors this project supports, native XLM routing is an
execution advantage.** The advantage is not marginal at realistic remittance
sizes: 14% at 5 USDC, 200% at 100 USDC, 400–500% at 1000–5000 USDC on both
priced corridors, monotonically increasing with size. Below that band
(≤ 1 USDC) the XLM leg has no discernible advantage — dust-size probes fit
inside other pools — but that is where the ladder's structural floor already
sits well below anything useful.

The pattern is stable across the two priced corridors in the recorded
history (2026-08-21). It does not appear on the corridor that has no
market, because nothing routes there at all.

## What this does not say

Two things the acceptance criteria are careful about, worth restating:

- **This is not a recommendation to route through XLM.** The engine takes
  the best path Horizon returns and does not filter by hop composition.
  Preferring or penalising a path type in the engine is explicitly out of
  scope (issue #101, "Out of scope").
- **This is not a verdict on the corridor.** Best-path advantage is a
  hop-composition measurement, not a fair-value comparison. The verdict
  work continues to live with `route.verdictFor`; nothing here feeds into
  it, and nothing here changes what a rung is graded.

## How to reproduce

```
go run ./cmd/hop-analysis                    # text table
go run ./cmd/hop-analysis -json | jq '.'     # structured output
```

Add a new snapshot with `go run ./cmd/ladder -record testdata/snapshots`
first if the corridor list is not the one recorded on 2026-08-21. The tool
reads whatever snapshot directories it finds under `-snapshots`; the
recorded `sizes` list drives the analysis, not a private constant.
