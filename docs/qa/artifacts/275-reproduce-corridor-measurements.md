# Reproducing docs/corridor-measurements.md — Issue #275

**Method under test:** `docs/corridor-measurements.md`
**Tested by:** Buffy (autonomous agent), on the repository working tree at
commit `7b53469` (branch `main`), 2026-09-27
**Timestamp:** measurements 2026-09-27T21:20:27Z – 2026-09-27T21:22:20Z; TOML
and reference checks 2026-09-27T21:19Z – 21:26Z
**Endpoints used:** Horizon mainnet `https://horizon.stellar.org`
(`/paths/strict-send` via `dex.Client`), reference rate
`https://open.er-api.com/v6/latest/USD` (`refrate` provider
`exchangerate-api`), `https://ngnc.online/.well-known/stellar.toml`,
`https://cowrie.exchange/.well-known/stellar.toml`

## What the issue asks

The document carries timestamps and raw output; whether a stranger can
reproduce **the method** is the claim that matters. This artifact walks the
document's own "Reproducing" section against each figure, records what matched
and what did not, and keeps every observed number with its timestamp and
endpoint. Per the issue constraints, nothing found was fixed here; the
contradictions are listed as separate findings with reproduction steps.

## Procedure (repeatable by anyone)

```bash
go run ./cmd/ladder -json > ngnc.json
go run ./cmd/ladder -to GHSC -json > ghsc.json
go run ./cmd/ladder -to KESC -json > kesc.json
go run ./cmd/ladder -to NGNT -json > ngnt.json   # the document's NGNT section
```

Requires live network access to Horizon and the reference provider, exactly as
the document states. Derived figures below (loss vs mid, marginal rates,
depth-at-mid) were recomputed independently from the raw `receive_amount` and
`reference_mid` values in each JSON document using decimal arithmetic, not
copied from the engine, so the check covers the document's arithmetic as well
as its measurements. Exit codes were recorded: NGNC 0, NGNT 0 (both corridors
now have at least one POOR-or-better size); GHSC 1 and KESC 1 (nothing
recommendable — the documented behaviour for a broken corridor).

Reference mids for USD/NGN, USD/GHS and USD/KES were also fetched directly
from the provider's own endpoint and matched the engine's `reference_mid` in
every case; the provider's `time_last_update_utc` (2026-09-27 00:02:31 +0000)
matches the `reference_as_of` stamp on every response.

## Raw results — USDC → NGNC, 2026-09-27T21:20:27Z

Reference mid **1328.77796** USD/NGN via exchangerate-api, as of
2026-09-27T00:02:31Z. Loss recomputed from raw receive amounts; the engine's
published full-precision `loss_pct` matched the recomputation at every rung
(differences below 1e-9), which independently reproduces the wire-arithmetic
contract ("the published number always matches the grade").

| Send (USDC) | Receive (NGNC) | Rate | Loss vs mid (recomputed) | Verdict (engine) | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | 116.8985457 | 1168.985457 | 12.03% | POOR | USDC → yUSDC → AQUA → NGNC |
| 1 | 1159.9332978 | 1159.9332978 | 12.71% | POOR | USDC → yUSDC → AQUA → NGNC |
| 5 | 5659.8534902 | 1131.97069804 | 14.81% | POOR | USDC → yXLM → XLM → NGNC |
| 10 | 11012.2250446 | 1101.22250446 | 17.13% | POOR | USDC → XLM → NGNC |
| 25 | 25458.6396552 | 1018.345586208 | 23.36% | UNUSABLE | USDC → BTC → XLM → NGNC |
| 50 | 45233.56345 | 904.671269 | 31.92% | UNUSABLE | USDC → XLM → NGNC |
| 100 | 73965.0915547 | 739.650915547 | 44.34% | UNUSABLE | USDC → XLM → NGNC |
| 250 | 119512.3021689 | 478.0492086756 | 64.02% | UNUSABLE | USDC → XLM → NGNC |
| 500 | 150379.9616819 | 300.7599233638 | 77.37% | UNUSABLE | USDC → XLM → NGNC |
| 1000 | 172679.8540002 | 172.6798540002 | 87.00% | UNUSABLE | USDC → XLM → NGNC |
| 2500 | 189541.8694988 | 75.81674779952 | 94.29% | UNUSABLE | USDC → XLM → NGNC |
| 5000 | 195919.4275122 | 39.18388550244 | 97.05% | UNUSABLE | USDC → XLM → NGNC |

**The engine recommended a route** (`recommended_size` "0.1"; the run exited 0).
This contradicts the document on the headline finding — see Findings F1.

## Raw results — USDC → GHSC, 2026-09-27T21:21:08Z

Reference mid **11.632012** USD/GHS, as of 2026-09-27T00:02:31Z. Integrity
**DERIVATIVE**, `depends_on` = NGNC — as documented.

| Send (USDC) | Receive (GHSC) | Loss vs mid (recomputed) | Best path |
|---:|---:|---:|:---|
| 0.1 | 0.3330066 | 71.37% | USDC → AQUA → NGNC → GHSC |
| 1 | 3.2949302 | 71.67% | USDC → AQUA → NGNC → GHSC |
| 5 | 15.8655323 | 72.72% | USDC → XLM → NGNC → GHSC |
| 10 | 30.3773648 | 73.88% | USDC → XLM → NGNC → GHSC |
| 25 | 67.3266167 | 76.85% | USDC → XLM → NGNC → GHSC |
| 50 | 113.2390864 | 80.53% | USDC → XLM → NGNC → GHSC |
| 100 | 171.8264356 | 85.23% | USDC → XLM → NGNC → GHSC |
| 250 | 249.168893 | 91.43% | USDC → XLM → NGNC → GHSC |
| 500 | 293.1701229 | 94.96% | USDC → XLM → NGNC → GHSC |
| 1000 | 321.5551713 | 97.24% | USDC → XLM → NGNC → GHSC |
| 2500 | 341.3842666 | 98.83% | USDC → XLM → NGNC → GHSC |
| 5000 | 348.5512985 | 99.40% | USDC → XLM → NGNC → GHSC |

Every best path traverses NGNC at every size — the document's central DERIVATIVE
claim reproduces exactly. Losses are UNUSABLE at all twelve sizes (exit 1), as
documented.

## Raw results — USDC → KESC, 2026-09-27T21:21:27Z

Reference mid **129.660848** USD/KES, as of 2026-09-27T00:02:31Z. Integrity
**NO-MARKET**: all twelve rungs `priced: false`, no path at any size, exit 1.
This reproduces the document's "NO ROUTE at every size tested" exactly.

## Raw results — USDC → NGNT, 2026-09-27T21:22:20Z

Reference mid **1328.77796** USD/NGN, as of 2026-09-27T00:02:31Z. Integrity
**DIRECT**. The verdict ladder reproduces the document's shape rung for rung:
GOOD ×9 (0.1 → 500), FAIR at 1000 (5.48%), UNUSABLE at 2500 (42.39%) and 5000
(56.26%) — the document has 7.15%, 39.71% and 56.35% at those rungs. The
document's best paths named EURC/TFT/LIBRE bridges; today's named PYUSD and
plain XLM — same family of two-hop routes, different third-party tokens, which
is drift in the market, not in the method. A separate table-mode run
(21:24:15Z) produced the same verdict ladder with slightly different receive
amounts and at 0.1–25 USDC chose `USDC → XLM → NGNT` / `USDC → NGNT` directly,
consistent with minute-scale drift at small sizes.

## Verdict-by-figure against the document

| # | Document claim | Observed 2026-09-27 | Verdict |
|:---|:---|:---|:---|
| 1 | NGNC: all 12 sizes UNUSABLE; engine recommends nothing; ~24.65–25.02% structural floor | 4 sizes POOR (floor now **12.03%** at 0.1 USDC); engine recommends 0.1 USDC; run exits 0 | ❌ **CONTRADICTED** (F1) |
| 2 | NGNC: loss climbs monotonically 24.65% → 97.68% | Monotonically 12.03% → 97.05%; same shape, different floor | ✅ shape reproduces (floor moved, see F1) |
| 3 | NGNC: liquidity exhausted ≈160,000 NGNC; marginal rate 1.90 NGN/USD at 2500→5000 | Asymptote now ≈195,919 NGNC; marginal rate 2.55 at 2500→5000 (44.60 / 11.24 / 2.55 over the three documented steps) | ✅ pattern reproduces; figures moved with the market |
| 4 | NGNC 100 USDC ≈ 62,890.83 NGNC (53.89% loss) | 73,965.09 NGNC (44.34% loss) | ❌ superseded (F1) |
| 5 | GHSC: every path routes through NGNC; DERIVATIVE; ~74.14% floor; unusable at every size | DERIVATIVE, all 12 paths through NGNC; floor 71.37%; UNUSABLE at every size | ✅ reproduces (floor figure drifted, expected) |
| 6 | KESC: no route at any size; NO-MARKET; "the absence of a market" | Identical. All 12 rungs unpriced | ✅ reproduces exactly |
| 7 | NGNT: 9×GOOD → FAIR@1000 → UNUSABLE×2, DIRECT | Identical ladder shape (5.48% / 42.39% / 56.26% at the graded rungs) | ✅ reproduces |
| 8 | NGNT: Cowrie SEP-1 declares NGNT live, fiat-pegged, **no ANCHOR_QUOTE_SERVER** | `status='live'`, `anchor_asset='NGN'`, `is_asset_anchored=true`; no `ANCHOR_QUOTE_SERVER` anywhere in the document (fetched 21:25Z) | ✅ reproduces |
| 9 | Issuer's TOML: NGNC `live`, GHSC and KESC `pending` | Same, from `https://ngnc.online/.well-known/stellar.toml` (21:20Z) | ✅ reproduces |
| 10 | TOML defect: KESC sets `anchor_asset="KESC"` not `KES` | Line 64 of the fetched document: `anchor_asset="KESC"`. The ISO-4217 check fails for KESC (`toml.anchor-asset-iso4217` determined=false/failed) and passes for NGNC/GHSC | ✅ reproduces |
| 11 | TOML defect: invalid TOML — a stray `s` follows the quoted KESC `image` URL; a conforming parser rejects the file | **Not reproduced.** The KESC image URL now ends `…_KESc.png` (one stray **c**, lowercase c — the file-name quirk remains) but inside the quotes, closing cleanly with `"\n`. Parsed with the project's own TOML library (`BurntSushi/toml` v1.6.0): **PARSED OK** | ❌ **NOT REPRODUCED — the file has been repaired since 2026-08-08** (F2) |
| 12 | Reference rate is exchangerate-api, official/interbank; the charitable benchmark | Engine and a direct `open.er-api.com` fetch agree on all three mids; `reference_as_of` matches the provider's own `time_last_update_utc` | ✅ reproduces |
| 13 | "Not one of the twenty-four priced points reached Poor" | NGNC alone has 4 POOR rungs today | ❌ **CONTRADICTED** (F1) |
| 14 | Total depth across the three corridors ≈ $143 | NGNC $147.44 + GHSC $29.96 + KESC $0 = **$177.40** at today's mids | ✅ same order of magnitude and same method; figures moved with the market |
| 15 | Comparability note: a 2026-08-04 vs 08-08 "stable and slightly worse" comparison | Not re-run (would require reproducing 2026-08-04 live state). Noted as method-only | ➖ not re-runnable |

## Findings — what contradicts the document, with reproduction steps

Per the issue constraints these are reported, not fixed, here.

### F1 — The NGNC case study no longer describes the corridor it names ❌

**What the document says:** every size UNUSABLE, a ~24.65–25.02% structural
floor at dust size, the engine recommends nothing, "not one of the twenty-four
priced points reached Poor".

**What was observed (2026-09-27T21:20:27Z, Horizon mainnet):** the floor is now
**12.03%** at 0.1 USDC; sizes 0.1–10 grade POOR; the engine publishes
`recommended` for the first time on this corridor (`recommended_size: "0.1"`).
Best paths at small sizes run through **yUSDC and yXLM** (yield-bearing
tokens) and AQUA — tokens absent from every historical run in the document.
The loss curve is still monotonic and still hits 97.05% at 5000 USDC, and the
corridor is still expensive; but the specific, headline "all twelve sizes are
Unusable — no trade size can be acceptable" claim is false today.

**Reproduction:**

```bash
go run ./cmd/ladder -json | python3 -c \
  'import json,sys; d=json.load(sys.stdin); print(d["recommended_size"], d["rungs"][0]["quote"]["verdict"], d["rungs"][0]["quote"]["loss_pct"])'
```

Observe `0.1 POOR <loss < 20%>` and exit code 0, where the document and the
stored `data/USDC-NGNC.ndjson` chain both predict `UNUSABLE` and exit 1: the
newest committed record (2026-08-22T12:09:59Z) carries `floor_loss_pct: 27.15`,
`worst_loss_pct: 97.52`, `recommended: null`, and all twelve rungs `UNUSABLE`.

**Why it matters:** the README, the docs, and the product thesis all cite this
corridor as the founding case study. The market changed under the document —
plausibly the arrival of the yUSDC/yXLM bridge markets — and the document has
no mechanism to say so. This is exactly the gap the corridor-research template
and the trend endpoint exist to close.

### F2 — The invalid-TOML defect in the issuer's stellar.toml has been repaired ❌

**What the document says:** defect 2 — "a stray `s` follows the quoted KESC
`image` URL. A conforming parser rejects the whole file, which is why
`anchor/salvage.go` exists."

**What was observed (2026-09-27T21:20Z):** the KESC image URL ends
`…65f06c75052c9d3cf7bed94b_KESc.png` — note the stray lowercase **c**, so the
typo's residue survives in the file name, but it sits inside the quoted string
and the line ends cleanly with `"\n`. The file parses with the project's own
TOML library:

```
$ go run /tmp/tomlcheck/main.go ngnc.toml     # BurntSushi/toml v1.6.0
PARSED OK
```

The defect is real history — it explains `anchor/salvage.go` and the 2026-08-08
observation — but a reader following the document today will not find it. The
document presents both TOML defects as current; only the `anchor_asset="KESC"`
one (defect 1, line 64) still is.

**Reproduction:**

```bash
curl -s https://ngnc.online/.well-known/stellar.toml | sed -n '64p'   # anchor_asset="KESC" — still broken
curl -s https://ngnc.online/.well-known/stellar.toml > /tmp/ngnc.toml
# parse with BurntSushi/toml v1.6.0 — PARSED OK, contradicting the document
```

### F3 — Figures drift between runs at small sizes on NGNT (documentation-only note) ⚠️

Two NGNT runs ~2 minutes apart (21:22:20Z JSON, 21:24:15Z table) agreed on the
verdict ladder and disagreed at the fourth decimal on receive amounts, and on
which bridge tokens appear at 0.1–25 USDC (EURC/TFT/LIBRE in the document,
PYUSD/XLM today, XLM in the second run). This is normal market drift and does
not undermine any claim, but it confirms the document's own caveat that exact
figures will differ: any future QA of this document should compare **verdict
ladders, integrity states, and curve shapes**, never specific receive amounts
or bridge-token names at small sizes.

### F4 — `toml.home-domain-roundtrip` is undetermined on the NGNT corridor ⚠️

The check engine reports `toml.home-domain-roundtrip` undetermined with reason
"the issuer home_domain document could not be fetched: anchor: fetching
stellar.toml for cowrie.excha…" on the 2026-09-27 NGNT run, although
`https://cowrie.exchange/.well-known/stellar.toml` fetches fine directly
(HTTP 200). Also `sep24.info-lists-asset` is undetermined because Cowrie
declares no `TRANSFER_SERVER_SEP0024` (it has `TRANSFER_SERVER`). This looks
like a check-level gap (home_domain resolution for the NGNT issuer), not a
document error — filed as a candidate issue, out of scope for this artifact.

## Method-level conclusion

**The method reproduces.** A stranger following the document's "Reproducing"
section with nothing but the repository gets: working commands, the same
endpoints, the same integrity taxonomy (DIRECT / DERIVATIVE / NO-MARKET), the
same verdict thresholds, the same two-reference-rate cross-check with matching
`as_of` stamps, and independently recomputable arithmetic that reconciles to
full precision at every rung. The structural findings that define the
document — GHSC's derivative dependence on NGNC, KESC's absent market, the
monotonic loss curve, liquidity exhaustion at the top of the ladder — all
reproduce.

**The figures do not, and should not.** Two things moved since 2026-08-08: the
market (NGNC's structural floor roughly halved and the corridor now has
recommendable sizes; bridge tokens rotate), and the issuer's own document (the
broken TOML was repaired). The document's claims are correctly dated, but two
of them are now false-as-of-now statements about a living market and one about
a live document, with no mechanism in the page to point a reader at the newer
state. That is finding F1 and it is the substantive result of this
reproduction.

## Honesty notes (scope limits)

- All measurements are single runs at the timestamps above, not averages; the
  document's own numbers are single runs too, so the comparison is like for
  like.
- The 2026-08-04 comparison table and the 12:53 vs 14:27 same-window comparison
  in the document are history and were not re-runnable on 2026-09-27; they are
  marked as such in the verdict table.
- The reference mid moves daily (`reference_as_of` 2026-09-27T00:02:31Z on the
  day of testing vs 2026-08-08T00:02:31Z in the document); every loss
  percentage carries that dependency, as the document itself states.
- No snapshot recording was taken for these runs: `-record` refuses a dirty
  tree, and this QA deliberately ran on a tree with the artifact in progress.
  The replayable-fixture side of reproducibility is covered by
  `testdata/snapshots/` (recorded 2026-08-21) and the offline-test contract.
