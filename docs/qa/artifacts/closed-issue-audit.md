# Closed issue audit — 2026-11-23

> **Status: partial; the requested V1/V2 audit is not complete.** This report
> records the named issues checked against `main` and the evidence found. It
> does not claim that every closed issue in either milestone was reviewed.

**Repository:** [Wayfare-labs/wayfare](https://github.com/Wayfare-labs/wayfare)
**Audited commit:** `e5148a474002c1fa160965413a7435e793c7055c`
**Audit date:** 2026-11-23

## Scope and limitation

At audit time, V1 contained 159 closed issues and V2 contained 69. The three
issues called out in #651 — #91, #92 and #177 — were checked against their
issue descriptions, implementation and available test/operational evidence.
The three cited historical merge-repair issues (#469, #480 and #493) were also
checked for current build/CI status.

The other **225 closed V1/V2 issues were not individually checked**. The full
acceptance criterion in #651 therefore remains outstanding; this artifact is
not evidence that the whole milestone tracker is accurate.

## Findings

| Issue | Verdict on audited `main` | Evidence |
|:---|:---|:---|
| [#91 — Metrics runner](https://github.com/Wayfare-labs/wayfare/issues/91) | **Partially verified** | `checks.Runner.ForCorridor` supplies both assets and runs registered metrics; `TestRunnerForCorridorRunsMetrics` covers that wiring. Recorded-fixture tests cover spread, observed/executable depth, concentration and price impact in `checks/checks_test.go`. The default metric set intentionally excludes price impact and executable depth because they repeat pathfinding. The issue's whole-sweep request-bound criterion was not independently verified in this audit, so the issue's full acceptance criteria are not marked met. |
| [#92 — Metrics on `/api/corridor`](https://github.com/Wayfare-labs/wayfare/issues/92) | **Met in code and tests** | `server/api.go` calls `ForCorridor` for live responses. `TestFindingsMetricsSchema` pins determined/undetermined metric JSON, omission of unrun metrics, and decimal-string values; `TestLiveCorridorCarriesMetrics` checks a live response contains metrics. The audited main CI run `45179479645` passed. |
| [#177 — Issuer drift](https://github.com/Wayfare-labs/wayfare/issues/177) | **Implemented; scheduled reporting currently unverified** | `checks/issuer_drift.go` compares issuer observations; the server and scheduler load the preceding record as a baseline (`monitor/monitor.go`, `server/api.go`). `monitor/monitor_test.go` covers baseline loading and scheduled-run recording. However, the five latest scheduled measurement runs observed on 2026-11-23 all failed at **Open or update the measurement pull request** after measurement and chain verification succeeded. The deployed `/healthz` response reported the oldest three corridor records at 93 days and NGNT at 36 days. Schedule execution exists, but fresh reports are not landing on `main`; file a follow-up or reopen #177 to track operational delivery. |
| [#469 — Batch merge repair](https://github.com/Wayfare-labs/wayfare/issues/469) | **Historical repair; current CI passes** | The issue describes a broken tree from the earlier unverified merges. On audited commit `e5148a4`, workflow run `45179479645` completed successfully, including build/test, no-network tests, lint, reproducible build and container build. |
| [#480 — Restore build](https://github.com/Wayfare-labs/wayfare/issues/480) | **Historical repair; current CI passes** | The issue describes duplicate declarations and an unused import. The audited commit's build/test and lint checks passed in workflow run `45179479645`. |
| [#493 — Restore CI workflow parsing](https://github.com/Wayfare-labs/wayfare/issues/493) | **Historical repair; current CI passes** | The audited commit's workflow ran its required build/test, no-network, lint and container checks successfully in run `45179479645`. The separate scheduled measurement failure is recorded under #177. |

The CI evidence is the completed GitHub Actions run:
[run 45179479645](https://github.com/Wayfare-labs/wayfare/actions/runs/45179479645).
The live freshness evidence was retrieved from `GET /healthz` on 2026-11-23:
three records dated 2026-08-22 (93 days old), plus one dated 2026-10-17
(36 days old). The latest scheduled measurement run was
[45111968765](https://github.com/Wayfare-labs/wayfare/actions/runs/45111968765);
measurement and chain verification succeeded, while opening the update PR
failed.

## README correction

The layer overview in `README.md` said spread, depth, price impact and
concentration were "implemented, not yet reachable." That status was stale:
metrics run through `checks.Runner.ForCorridor`, and the README roadmap already
describes their live-response path. The label now says they are reachable
through corridor sweeps. This does not imply every metric is in the default
set; expensive pathfinding metrics remain opt-in.

## Follow-up still required

- Complete the criteria-by-criteria review of the remaining 225 closed V1/V2
  issues and add their verdicts and evidence here.
- Track the scheduled measurement PR failure so new issuer-drift observations
  and other scheduled measurements reach `main`. The automated follow-up is
  open as [#782](https://github.com/Wayfare-labs/wayfare/issues/782).
- Confirm the request-count bound criterion from #91 against its documented
  budget and tests. The related duplicate-order-book-request follow-up is open
  as [#887](https://github.com/Wayfare-labs/wayfare/issues/887).

The target repository was read-only to the audit account, so #177 could not be
reopened. The existing operational follow-up #782 is open; no new issue was
filed.
