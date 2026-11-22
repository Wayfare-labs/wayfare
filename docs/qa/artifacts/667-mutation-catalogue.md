# Mutation catalogue for the pricing path (#667)

**Measured:** 2026-11-22, on top of `d48ff57`, Go 1.27.4, against `go test ./...`
(the whole module, so a mutant is attributed to every test that notices it,
not only to tests in its own package).

Boundary tests exist for the four places that decide what Wayfare publishes —
`verdictFor`, `classify`, the strict-send refusals and the conservative-mid
selection — but a test that *executes* a line is not a test that would *notice*
the line changing. Coverage cannot tell the two apart
([docs/coverage-floors.md](../../coverage-floors.md) says so explicitly). This
catalogue measures the second thing directly: each entry is a hand-written
change a plausible refactor or a careless fix could make, applied on its own,
with the full suite run against it.

## Procedure

```bash
go run scripts/mutation/main.go                  # all 24, ~8 minutes
go run scripts/mutation/main.go -run 'M1[3-8]'   # a subset by id
go run scripts/mutation/main.go -json out.json   # also write raw results
```

- The catalogue is the `mutations` table in `scripts/mutation/main.go`. Each
  entry is an exact text substitution that must match its file **exactly
  once**; if the code moves, the tool stops with "anchor found N times" rather
  than silently mutating nothing.
- The tool refuses to start if a target file has uncommitted changes, restores
  the file after every mutant (and on interrupt), and builds with
  `//go:build ignore`, so it is not part of the module.
- A mutant is **killed** when at least one test fails, **survived** when the
  whole suite passes, and **invalid** when it does not compile (none here: an
  invalid mutant measures nothing and should be rewritten).
- The command exits non-zero if any outcome differs from the expected one, so
  re-running it after a change to the pricing path is a pass/fail check.

**When to re-run:** after any change to `route/route.go` (`verdictFor`,
`Quote.score`, `classify`), `dex/dex.go` (`parsePathRecords`) or
`refrate/cross.go` (`reconcile`), and after deleting or rewriting a test that
the table below lists as a mutant's only killer. Add a mutation when a new
refusal or band is added; keep ids stable so this table can be diffed.

## Baseline

| ID | Area | Mutation | Outcome | Killed by |
|:---|:---|:---|:---|:---|
| M01 | verdictFor | Good band excludes its 3% boundary | KILLED | `route.TestPropsVerdictThresholdsAndMonotonicity`, `route.TestVerdictThresholdBoundaries`, `route.TestVerdictThresholds` |
| M02 | verdictFor | Fair band excludes its 8% boundary | KILLED | `route.TestPropsVerdictThresholdsAndMonotonicity`, `route.TestVerdictThresholdBoundaries`, `route.TestVerdictThresholds` |
| M03 | verdictFor | Poor band excludes its 20% boundary | KILLED | `route.TestLadderRecommendationAtPoorBoundary`, `route.TestPropsVerdictThresholdsAndMonotonicity`, `route.TestVerdictThresholdBoundaries`, `route.TestVerdictThresholds` |
| M04 | verdictFor | Unusable band deleted: everything past Poor grades Poor | KILLED | `monitor.TestRunOnceWritesOneRecordPerCorridor`, `route.TestLadderRecommendationAtPoorBoundary`, `route.TestLiveCorridorIsRefused`, `route.TestPropsVerdictThresholdsAndMonotonicity`, `route.TestRecordedLadderFindingsMatchTheirState`, `route.TestRecordedNGNCIsDirect`, `route.TestVerdictThresholdBoundaries`, `route.TestVerdictThresholds`, `server.TestBrokenCorridorReturnsNullRecommendation`, `wayfare.TestCaseStudyReproducesFromTheCommittedSnapshot`, `wayfare.TestCorridorFiguresReproduce` |
| M05 | verdictFor | Good and Fair swapped | KILLED | `route.TestGoodRouteIsRecommended`, `route.TestPropsVerdictThresholdsAndMonotonicity`, `route.TestVerdictThresholdBoundaries`, `route.TestVerdictThresholds`, `server.TestGoodCorridorIsRecommended`, `wayfare.TestCorridorFiguresReproduce` |
| M06 | verdictFor | Negative-loss clamp deleted: a better-than-mid rate reports a negative cost | KILLED | `route.TestPropsScoreRateAndLoss`, `wayfare.TestCorridorFiguresReproduce` |
| M07 | classify | No paths classified DIRECT instead of NO-MARKET | KILLED | `route.TestChainDependencyHasNoMarket`, `route.TestNoPathsProducesNoQuotes` |
| M08 | classify | Destination-as-own-hop skip deleted | KILLED | `route.TestDestinationAsItsOwnHopIsNotADependency` |
| M09 | classify | Off by one: a path with one fiat hop counts as independent | KILLED | `monitor.TestDerivativeDependencyIsRecorded`, `monitor.TestPartialSweepRecordsSuccessfulCorridors`, `monitor.TestSweepLogsDependsOnChange`, `route.TestChainBackwardCompatible`, `route.TestChainCycleTerminates`, `route.TestChainDependencyHasNoMarket`, `route.TestChainDepthTwo`, `route.TestChainMeasuredDirect`, `route.TestChainSharedDependencyIsNotACycle`, `route.TestChainWireShape`, `route.TestDerivativeCorridorIsFlagged`, `route.TestRecordedGHSCIsDerivative`, `route.TestRecordedLadderFindingsMatchTheirState`, `wayfare.TestOfflineSweepRecordAndServe` |
| M10 | classify | A fiat hop is counted but not recorded as a dependency | KILLED | `monitor.TestDerivativeDependencyIsRecorded`, `monitor.TestPartialSweepRecordsSuccessfulCorridors`, `monitor.TestSweepLogsDependsOnChange`, `route.TestChainBackwardCompatible`, `route.TestChainCycleTerminates`, `route.TestChainDependencyHasNoMarket`, `route.TestChainDepthTwo`, `route.TestChainMeasuredDirect`, `route.TestChainSharedDependencyIsNotACycle`, `route.TestChainWireShape`, `route.TestDerivativeCorridorIsFlagged`, `route.TestRecordedGHSCIsDerivative`, `route.TestRecordedLadderFindingsMatchTheirState`, `wayfare.TestOfflineSweepRecordAndServe` |
| M11 | classify | An unregistered hop is treated as a fiat dependency | KILLED | `route.TestDestinationAsItsOwnHopIsNotADependency`, `route.TestUnknownOnlyPathIsTheDocumentedFalseNegative` |
| M12 | classify | Any independent path no longer makes the corridor DIRECT | KILLED | `ladder.TestReverseCorridorReplay`, `monitor.TestFailingSweepDoesNotBreakChain`, `monitor.TestPartialSweepRecordsSuccessfulCorridors`, `monitor.TestRunOnceWritesOneRecordPerCorridor`, `route.TestChainDepthTwo`, `route.TestChainMeasuredDirect`, `route.TestChainSharedDependencyIsNotACycle`, `route.TestChainWireShape`, `route.TestDestinationAsItsOwnHopIsNotADependency`, `route.TestDirectCorridorHasNoChain`, `route.TestDirectCorridorIsNotDerivative`, `route.TestEngineClassifiesFromTheCompletePathSet`, `route.TestMalfunctioningBenchmarkWithholdsVerdicts`, `route.TestOneIndependentPathDefeatsDerivative`, `route.TestPartialLadderMeasuresOnlyWhatAnswered`, `route.TestRecordedLadderFindingsMatchTheirState`, `route.TestRecordedNGNCIsDirect`, `route.TestRecordedNewHopsAreClassifiedFromSnapshots`, `route.TestUnknownOnlyPathIsTheDocumentedFalseNegative`, `server.TestCurveAlternativeReadsRealResponses`, `wayfare.TestCaseStudyReproducesFromTheCommittedSnapshot`, `wayfare.TestCorridorFiguresReproduce`, `wayfare.TestOfflineSweepRecordAndServe` |
| M13 | strict-send refusal | Null path record refusal deleted | KILLED | `route.TestRecordedMalformedPayloadsReportUnknown` |
| M14 | strict-send refusal | Zero computed amount accepted (only negative refused) | KILLED | `dex.FuzzStrictSendPaths`, `dex.TestStrictReceivePathsRejectsNonPositiveSourceAmount`, `route.TestRecordedMalformedPayloadsReportUnknown` |
| M15 | strict-send refusal | Decimal-places refusal off by one: 8 places accepted | KILLED | `dex.FuzzStrictSendPaths`, `route.TestRecordedMalformedPayloadsReportUnknown` |
| M16 | strict-send refusal | Destination exponent guard deleted | KILLED | `dex.TestPathRecordExponentGuards` |
| M17 | strict-send refusal | Unrecognised hop asset_type silently dropped instead of refused | KILLED | `route.TestRecordedMalformedPayloadsReportUnknown` |
| M18 | strict-send refusal | Strict-send validates source_amount instead of the computed destination_amount | KILLED | `dex.FuzzStrictSendPaths`, `dex.TestStrictReceivePathsRejectsNonPositiveSourceAmount`, `route.TestRecordedMalformedPayloadsReportUnknown` |
| M19 | conservative mid | Disagreeing providers scored against the smaller (flattering) mid | KILLED | `refrate.TestDiagnosticAttributesTheOutlierWithoutMovingTheScoredMid`, `refrate.TestDiagnosticAttributesTheSameOutlierRegardlessOfPrimaryOrder`, `refrate.TestDiagnosticTieNamesNoOutlier`, `refrate.TestDisagreeSelectsConservativeMidRegardlessOfOrder`, `refrate.TestDisagreementScoresConservatively`, `refrate.TestDivergenceBoundariesExact`, `refrate.TestPropsReconcileDivergence` |
| M20 | conservative mid | Disagree band includes its 2% boundary | KILLED | `refrate.TestBandBoundaries`, `refrate.TestDivergenceBoundariesExact` |
| M21 | conservative mid | Malfunction band includes its boundary | KILLED | `refrate.TestBandBoundaries`, `refrate.TestDivergenceBoundariesExact` |
| M22 | conservative mid | Divergence measured relative to the larger mid | KILLED | `refrate.TestBandBoundaries`, `refrate.TestDivergenceBoundariesExact`, `refrate.TestPropsReconcileDivergence` |
| M23 | conservative mid | Stale pair scored against the older mid | KILLED | `refrate.TestPropsReconcileStalePicksFresher`, `refrate.TestStaleBeatsConservativeSelection`, `refrate.TestStaleIsReportedDistinctly`, `refrate.TestStaleSelectsFresherFeedRegardlessOfOrder` |
| M24 | conservative mid | Zero-mid guard checks only the primary | KILLED | `refrate.TestZeroMidIsMalfunctionNotTotalDisagreement` |

24 mutations, 24 killed, after the two fixes below. The first run killed 22.

## Survivors, and what was done

### M08 — the destination appearing as its own hop

`classify` skips a hop equal to the corridor's destination, so a legal path such
as `USDC → NGNC → XLM → NGNC` does not make NGNC depend on itself. Deleting the
skip survived the whole suite: no test, recorded or synthetic, contained such a
path. Without it, that corridor would be published as `DERIVATIVE` with NGNC
in its own `depends_on`.

**Fixed:** `route/classify_own_hop_test.go`
(`TestDestinationAsItsOwnHopIsNotADependency`) asserts `DIRECT` with no
dependencies for that path, and that the skip matches on issuer as well as
code — a same-code token from another issuer is still a hop to classify.

### M16 — the destination-amount exponent guard

`parsePathRecords` refuses an amount whose exponent no Stellar asset can carry
before any arithmetic touches it, on both the source and destination side. The
order-book decoder's equivalent guard was tested (#589); these two were not.
Deleting the destination guard survived, and without it a strict-send answer of
`1e40` passes every other refusal (it is positive, and the decimal-places check
only rejects *negative* exponents beyond seven) and is priced as a real receive
amount.

**Fixed:** `dex/path_exponent_test.go` (`TestPathRecordExponentGuards`) covers
both sides in both directions and checks that an in-range exponent still
prices.

The first version of that test used `1e999999999` and **still let M16
survive**. `shopspring/decimal` refuses exponents above 10,000 at parse time,
and its error also names the field and contains the word "exponent", so the
test passed for the wrong reason. The guard only has work to do between an
exponent of 31 and 10,000; the test now uses `1e40` and matches the guard's own
wording. The catalogue caught a test that asserted too loosely, which is the
failure mode it exists for.

## What the table also shows

- **The verdict bands are defended in depth.** Each boundary flip (M01–M03) is
  killed by three or four independent tests, and deleting the Unusable band
  (M04) by eleven across `route`, `server`, `monitor` and the documentation
  tests.
- **The published figures are load-bearing.** `TestCorridorFiguresReproduce`
  (#702), which recomputes every figure in `docs/corridor-measurements.md` and
  `docs/ngnc-structural-floor.md`, kills M04, M05, M06 and M12 on its own: a
  change to grading or integrity that moves a published number fails on the
  document as well as on the unit tests.
- **Several refusals each have one killer.** M11, M13, M17 and M24 are caught
  by a single test. That is enough for this baseline, but deleting or
  rewriting `route.TestRecordedMalformedPayloadsReportUnknown`,
  `route.TestUnknownOnlyPathIsTheDocumentedFalseNegative` or
  `refrate.TestZeroMidIsMalfunctionNotTotalDisagreement` would turn those back
  into survivors. Re-run the catalogue in the same pull request as any such
  change.

## What this does not claim

These are 24 hand-chosen mutants, not an exhaustive or tool-generated set. A
killed mutant shows that the suite notices *that* change, not that it would
notice every change to the same line. The catalogue is deliberately small
enough to read, so that each entry says what failure it represents.
