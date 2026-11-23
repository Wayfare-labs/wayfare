# Wayfare's best route against Soroban router quotes, at the same moment

Research note for #682. Wayfare reports "none of these routes is worth taking"
from Horizon's strict-send pathfinder. If a Soroban DEX router routinely beat
that pathfinder, the verdict would be measuring one router's limitation rather
than the corridor. This captures both, back to back, at every ladder rung for
NGNC and NGNT, and compares them.

**Result in one line:** on 2026-11-23, **no Soroban quote beat Horizon at any
rung, on either corridor.**
- **NGNC:** the best Soroban quote delivered between 2.97% (0.1 USDC) and
  93.81% (5,000 USDC) *less* NGNC than Horizon's route.
- **NGNT:** there is no Soroban route at all, because NGNT has no Stellar
  Asset Contract on mainnet.

Splitting an NGNC trade between Horizon and Soroban helps by up to 2.65% and
changes no grade. The Horizon figures are not an artefact of the router.

Liquidity context, including why NGNT cannot be routed on Soroban, is in the
companion note [soroban-amm-liquidity-research.md](soroban-amm-liquidity-research.md)
(#681).

## Method

- **When:** 2026-11-23.
  - NGNC: Horizon ladder at 12:55:47Z, Soroban quotes 12:55:47Z–12:56:09Z,
    Horizon ladder again finishing 12:56:16Z.
  - NGNT: Horizon at 12:56:23Z, Soroban 12:56:23Z–12:56:43Z, Horizon again
    finishing 12:56:49Z.
  - Split probe: 12:57:37Z–12:57:52Z.
- **Horizon side: Wayfare's own measurement, unmodified.** Each corridor runs
  `ladder -to <T> -json -checks=false` once before the Soroban quotes and once
  after, so every Soroban quote is **bracketed** by two Horizon ladders. The
  drift between the two is reported per rung. It never exceeded 0.10%.
- **Soroban side, at every rung the ladder priced:**
  - **Soroswap router** (`CAG5LRYQ…JDDH`, from Soroswap's published
    `mainnet.contracts.json`): `router_get_amounts_out` on the paths
    USDC → T and USDC → XLM → T, simulated over Soroban RPC.
  - **Aquarius router:** Aquarius's own path-finding service (`find-path`). It
    searches every Aquarius pool, multi-hop included, and returns the amount its
    router contract would deliver.
  - **Phoenix:** no Phoenix pool holds NGNC or NGNT (#681), so there is
    nothing to quote.
  - **Best Soroban** is the larger of the two router quotes for that rung.
- **Which aggregator.** Soroswap's hosted aggregator API (`api.soroswap.finance`)
  answers `401 MISSING_CREDENTIAL` without an API key, so it was not used. Its
  on-chain aggregator contract (`CAYP3UWL…TXTO`; the wasm hash matches the
  published one) routes through exactly three adapters, read from its storage:
  Soroswap, Phoenix and Aquarius. The quotes above query those three venues
  directly through their own routers. A single-venue route through the
  aggregator therefore cannot beat the best quote shown here. The splitting the
  aggregator adds is bounded below.
- **Read-only:** simulations and a quote endpoint. No transaction was signed or
  submitted.
- **Loss and grade** are the ladder's own figures. `analyze.py` recomputes loss
  against the reference mid per [glossary.md](glossary.md), and the
  recomputation **matched the ladder's `loss_pct` at all 24 rungs** before
  being applied to any figure the ladder did not price.
- **Recorded:** everything is under `docs/research/soroban-liquidity/2026-11-23/`
  with `SHA256SUMS`:
  - both ladders per corridor (`*-horizon-before.json`, `*-horizon-after.json`);
  - every Soroban quote and error (`*-soroban.json`);
  - the split probe (`split-probe-NGNC.json`);
  - the derived figures (`analysis.json`).

  `run.sh` repeats the whole capture.
- **Reference mid:** 1,367.642612 NGN per USD, exchangerate-api.

## Per-rung comparison

Receive amounts are in the destination token. "Soroban vs Horizon" is
(best Soroban − Horizon before) / Horizon before.

### USDC → NGNC

| Send (USDC) | Horizon before | Horizon after | Drift | Best Soroban | Via | Soroban vs Horizon | Horizon grade (loss) |
|--:|--:|--:|--:|--:|:--|--:|:--|
| 0.1 | 163.87 | 163.98 | 0.07% | 159.00 | Aquarius USDC → NGNC | **-2.97%** | GOOD (0.00%) |
| 1 | 1,625.35 | 1,626.97 | 0.10% | 1,411.89 | Aquarius USDC → NGNC | **-13.13%** | GOOD (0.00%) |
| 5 | 7,854.00 | 7,861.56 | 0.10% | 4,712.93 | Aquarius USDC → NGNC | **-39.99%** | GOOD (0.00%) |
| 10 | 15,075.55 | 15,083.58 | 0.05% | 6,659.07 | Aquarius USDC → NGNC | **-55.83%** | GOOD (0.00%) |
| 25 | 33,627.13 | 33,642.95 | 0.05% | 8,852.34 | Aquarius USDC → NGNC | **-73.68%** | GOOD (1.65%) |
| 50 | 57,013.59 | 57,036.34 | 0.04% | 9,944.08 | Aquarius USDC → NGNC | **-82.56%** | POOR (16.63%) |
| 100 | 87,408.25 | 87,434.98 | 0.03% | 10,597.58 | Aquarius USDC → NGNC | **-87.88%** | UNUSABLE (36.09%) |
| 250 | 128,513.06 | 128,537.20 | 0.02% | 11,032.59 | Aquarius USDC → NGNC | **-91.42%** | UNUSABLE (62.41%) |
| 500 | 152,402.65 | 152,422.94 | 0.01% | 11,185.64 | Aquarius USDC → NGNC | **-92.66%** | UNUSABLE (77.71%) |
| 1000 | 168,023.83 | 168,036.59 | 0.01% | 11,263.77 | Aquarius USDC → NGNC | **-93.30%** | UNUSABLE (87.71%) |
| 2500 | 179,035.15 | 179,041.06 | 0.00% | 11,311.18 | Aquarius USDC → NGNC | **-93.68%** | UNUSABLE (94.76%) |
| 5000 | 183,033.63 | 183,036.71 | 0.00% | 11,327.07 | Aquarius USDC → NGNC | **-93.81%** | UNUSABLE (97.32%) |

- **Soroswap** was second at every rung. Its direct pair delivered 1,341.42 NGNC
  for 1 USDC.
- **Soroswap via XLM** never exceeded 284.75 NGNC at any size: that pool holds
  284.81 NGNC in total.
- **Why the gap widens with size:** the Soroban quotes flatten toward 11,343 NGNC,
  which is the reserve of the Aquarius USDC/NGNC pool. A 5,000 USDC order
  empties it. Horizon's route keeps finding depth well past that.

### USDC → NGNT

| Send (USDC) | Horizon before | Horizon after | Drift | Soroswap | Aquarius | Horizon grade (loss) |
|--:|--:|--:|--:|:--|:--|:--|
| 0.1 | 1,418.59 | 1,418.59 | 0.00% | no route | no route | GOOD (0.00%) |
| 1 | 13,951.66 | 13,951.66 | 0.00% | no route | no route | GOOD (0.00%) |
| 5 | 64,988.56 | 64,988.56 | 0.00% | no route | no route | GOOD (0.00%) |
| 10 | 119,699.57 | 119,698.63 | -0.00% | no route | no route | GOOD (0.00%) |
| 25 | 242,072.22 | 242,035.55 | -0.02% | no route | no route | GOOD (0.00%) |
| 50 | 367,232.36 | 367,187.41 | -0.01% | no route | no route | GOOD (0.00%) |
| 100 | 552,955.47 | 552,856.14 | -0.02% | no route | no route | GOOD (0.00%) |
| 250 | 876,333.78 | 876,181.30 | -0.02% | no route | no route | GOOD (0.00%) |
| 500 | 1,159,547.59 | 1,159,397.17 | -0.01% | no route | no route | GOOD (0.00%) |
| 1000 | 1,531,540.83 | 1,531,104.10 | -0.03% | no route | no route | GOOD (0.00%) |
| 2500 | 2,637,384.36 | 2,636,288.51 | -0.04% | no route | no route | UNUSABLE (22.86%) |
| 5000 | 3,027,898.22 | 3,027,346.76 | -0.02% | no route | no route | UNUSABLE (55.72%) |

"No route" is what the routers said, recorded verbatim in
`20261123T125616Z-NGNT-soroban.json`:
- **Soroswap:** both paths fail with `Error(Storage, MissingValue)`; no pair
  exists.
- **Aquarius:** `find-path` answers HTTP 400, `Object with address=CDCSVQEM…JXD7
  does not exist`.

That address is NGNT's Stellar Asset Contract ID, and `getLedgerEntries` finds
no instance for it (#681). An unrouteable corridor is reported as unrouteable,
not as zero loss and not as a skipped row.

## Does splitting across both venues change anything?

A router that sends part of an order through Horizon's route and part through
Soroban could beat both. No router examined does this:
- Horizon cannot see Soroban;
- the Soroswap aggregator's adapters are Soroban-only.

It is still the strongest form of the question #682 asks, so it was bounded
and then measured.

**The bound.** Both routes deliver more for more input. So no split of S
across them can deliver more than Horizon(S) + Soroban(S), the two full-size
quotes added together.
- **At most rungs this settles it.** From 100 USDC up, even that sum is still
  UNUSABLE, and up to 25 USDC Horizon alone is already GOOD.
- **At 50 USDC it does not.** The sum is graded GOOD (2.08% loss) against
  Horizon's POOR.

**The measurement.** For S = 25, 50 and 100 USDC, the ladder priced S − x
through Horizon (`ladder -sizes`) and the Soroban routers priced x, for
x = 1, 2, 5 and 10 USDC, in a single 15-second window:

| Rung | Soroban share | Horizon part | Soroban part | Split total | Horizon alone | Change | Loss alone → split |
|--:|--:|--:|--:|--:|--:|--:|:--|
| 25 | 1 | 32,548.67 | 1,411.89 | 33,960.56 | 33,661.02 | 0.89% | 1.55% → 0.67% (GOOD → GOOD) |
| 25 | 2 | 31,420.09 | 2,511.20 | 33,931.29 | 33,661.02 | 0.80% | 1.55% → 0.76% (GOOD → GOOD) |
| 25 | 5 | 27,926.71 | 4,712.93 | 32,639.64 | 33,661.02 | -3.03% | 1.55% → 4.54% (GOOD → FAIR) |
| 25 | 10 | 21,761.71 | 6,659.07 | 28,420.77 | 33,661.02 | -15.57% | 1.55% → 16.88% (GOOD → POOR) |
| 50 | 1 | 56,264.04 | 1,411.89 | 57,675.93 | 57,051.32 | 1.09% | 16.57% → 15.66% (POOR → POOR) |
| 50 | 2 | 55,455.92 | 2,511.20 | 57,967.13 | 57,051.32 | 1.61% | 16.57% → 15.23% (POOR → POOR) |
| 50 | 5 | 52,970.60 | 4,712.93 | 57,683.53 | 57,051.32 | 1.11% | 16.57% → 15.65% (POOR → POOR) |
| 50 | 10 | 48,613.27 | 6,659.07 | 55,272.34 | 57,051.32 | -3.12% | 16.57% → 19.17% (POOR → POOR) |
| 100 | 1 | 86,984.38 | 1,411.89 | 88,396.27 | 87,465.49 | 1.06% | 36.05% → 35.37% (UNUSABLE → UNUSABLE) |
| 100 | 2 | 86,511.77 | 2,511.20 | 89,022.97 | 87,465.49 | 1.78% | 36.05% → 34.91% (UNUSABLE → UNUSABLE) |
| 100 | 5 | 85,066.79 | 4,712.93 | 89,779.72 | 87,465.49 | 2.65% | 36.05% → 34.35% (UNUSABLE → UNUSABLE) |
| 100 | 10 | 82,564.10 | 6,659.07 | 89,223.16 | 87,465.49 | 2.01% | 36.05% → 34.76% (UNUSABLE → UNUSABLE) |

A small Soroban share does help, by 0.8–2.65%. At 50 USDC the best sampled
split still leaves a 15.23% loss, **POOR**, far from the 3% GOOD would need.
Nearly all of the bound's apparent headroom at 50 was the impossibility of
taking Soroban's full-size quote *and* Horizon's full-size quote at once.

## What this comparison does and does not show

**It shows:**
- Horizon's route was better than every Soroban router quote, at every rung,
  on both measured corridors.
- The pairs were captured within a minute of each other, with Horizon
  bracketing Soroban on both sides.
- Combining the venues, at the splits sampled, improves NGNC delivery a
  little and changes no grade.

**It does not show:**
- **Another day.** This is one window. Soroban NGNC liquidity is small enough
  that a single deposit could change it. Re-run `run.sh`.
- **The Soroswap aggregator's own optimiser.** Its hosted API needs a key.
  What was measured is every venue it can route through, quoted by that
  venue's own router. Its splits are among the splits bounded above, but its
  exact distribution was not observed.
- **An optimal split.** Twelve splits were sampled, not optimised. A finer grid
  could find a slightly better one. It cannot exceed the Horizon(S) + Soroban(S)
  bound, and at 50 USDC the sampled curve peaks well short of a grade change.
- **Execution.** These are quotes and simulations, not fills. Fees beyond those
  the routers include, slippage between quote and submission, and Soroban
  resource fees are not modelled.
- **Corridors other than NGNC and NGNT.** GHSC and KESC have no Soroban
  contract (#681). Other registered tokens were not examined.
