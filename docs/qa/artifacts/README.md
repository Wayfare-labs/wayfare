# QA Artifacts

Recorded, reusable QA results for the H — Cross-cutting / QA and reproducibility
backlog section. Each artifact states its own target, timestamps and endpoints.

| Issue | Title | Artifact | Bundle |
|:---|:---|:---|:---|
| #257 | A production smoke-test checklist | [257-production-smoke-test.md](257-production-smoke-test.md) | `qa/issues-257-263-264-265` |
| #263 | Build the UI state matrix and test every cell | [263-ui-state-matrix.md](263-ui-state-matrix.md) | `qa/issues-257-263-264-265` |
| #264 | QA the NO-MARKET corridor end to end | [264-nomarket-corridor.md](264-nomarket-corridor.md) | `qa/issues-257-263-264-265` |
| #265 | QA the DERIVATIVE corridor end to end | [265-derivative-corridor.md](265-derivative-corridor.md) | `qa/issues-257-263-264-265` |
| #261 | Time a full live ladder against the server timeout | [261-live-ladder-timeout.md](261-live-ladder-timeout.md) | `full-live-ladder` |
| #258 | Record the cold-start behaviour properly | [258-cold-start-distribution.md](258-cold-start-distribution.md) | `verify-claims` |
| #259 | Verify the deployed instance against the repository it claims to be | [259-served-history-vs-committed.md](259-served-history-vs-committed.md) | `verify-claims` |
| #260 | QA the API from a consumer's perspective, not the UI's | [260-api-consumer-qa.md](260-api-consumer-qa.md) | `verify-claims` |
| #262 | Verify /healthz behaviour during a cold start | [262-healthz-cold-start.md](262-healthz-cold-start.md) | `verify-claims` |

---

## Bundle: #258, #259, #260, #262

**Branch:** `verify-claims`
**Target:** https://wayfare-cdb9.onrender.com/
**Tested by:** chiprime
**Timestamp:** 2026-09-26T01:26Z – 2026-09-26T01:27Z (API and history), with a
separate cold-start measurement recorded in the artifacts.

Reusable harness: [`../api/`](../api/). Recorded results:
[`../api/results/`](../api/results/).

### Key findings

1. The served history matches the committed chain on every field the API
   exposes, and the committed chain verifies locally (#259). ✅
2. `/api/assets` and `/healthz` return 200 for `POST`/`PUT`, contradicting
   `docs/api.md`'s "unsupported methods return 405" (#260). ❌
3. Error `code` casing differs between `/api/corridor` (lowercase) and
   `/api/corridor/trend` (uppercase) (#260). ⚠️
4. `pretty`, the `/healthz` `data` block and CORS preflight are implemented but
   undocumented (#260). ⚠️

The method-handling and code-casing mismatches are the findings filed
separately, per the issue constraints.

---

## Bundle: #257, #263, #264, #265

**Branch:** `qa/issues-257-263-264-265`
**Target:** https://wayfare-cdb9.onrender.com/
**Tested by:** opencode agent
**Timestamp:** 2026-09-23T17:23:52Z – 2026-09-23T17:24:07Z

This bundle contains end-to-end QA artifacts for four linked issues in the H2 —
UI state matrix section of the backlog.

### Live Deployment Status at Test Time

- **URL:** https://wayfare-cdb9.onrender.com/
- **Deployment age:** 32 days (last run 2026-08-22T12:10:05Z)
- **Cold start:** ~58s on first request, ~0.6s thereafter
- **Healthz:** Returns `{"status":"ok"}` with data-age info but no prominent staleness warning

### Key Findings

1. **KESC (NO-MARKET)** degrades honestly — no empty table, all 12 rungs show clear error messages, integrity badge is prominent. ✅
2. **GHSC (DERIVATIVE)** shows dependency in prose and structure (`depends_on` array, per-rung warnings), but loss figures are not decomposed into GHSC-only vs NGNC-compounded components. ⚠️
3. **NGNC (DIRECT)** live data now shows a GOOD route at 0.1 USDC (0% loss), contradicting the stored finding that says "No usable size." The case study text is stale. ❌
4. **Stale banner** works structurally (`live: false`, `stale.age_human`) but does not prominently warn about 32-day-old data. ⚠️
5. **Error paths** return machine-readable codes and reject unknown parameters. ✅
6. **UI state matrix:** 3 of 12 cells were reachable on the live deployment. 9 cells are not reachable under normal operation. Documented in artifact #263.

### Separate Issues Filed

The following problems were identified but are not fixed in this branch (per constraints — they must be filed separately):

- Stale case study text (NGNC 25% claim contradicts live data)
- `floor_loss_pct: "0.00"` with `floor_size: "0"` for NO-MARKET corridors could mislead consumers
- Loss figures for DERIVATIVE corridors are not decomposed
- No prominent staleness warning on the page despite 32-day-old data
- Cold start takes ~58s with no prominent indicator
- Case study conflates NGNC and GHSC stories

---

## Bundle: #261

**Branch:** `full-live-ladder`
**Revisions:** `81da004` (base) measured 2026-09-24T05:13:25Z – 05:27:51Z
**Target:** local `wayfared` against live mainnet, and https://wayfare-cdb9.onrender.com/

Settles the issue's premise against observation. The 90s `Server.timeout()`
default holds with ~25× headroom (slowest ladder observed: 3.93s), but the
premise that "a ladder is a dozen Horizon round trips" is **wrong**: 28 calls
per market corridor, 12 for a NO-MARKET one. See the artifact for the
decomposition and the separate documentation discrepancy it also reports.
