# QA Artifacts — Issues #257, #263, #264, #265

**Branch:** `qa/issues-257-263-264-265`
**Target:** https://wayfare-cdb9.onrender.com/
**Tested by:** opencode agent
**Timestamp:** 2026-09-23T17:23:52Z – 2026-09-23T17:24:07Z

## Summary

This branch contains end-to-end QA artifacts for four linked issues in the H2 — UI state matrix section of the backlog.

| Issue | Title | Artifact |
|:---|:---|:---|
| #257 | A production smoke-test checklist | [257-production-smoke-test.md](257-production-smoke-test.md) |
| #263 | Build the UI state matrix and test every cell | [263-ui-state-matrix.md](263-ui-state-matrix.md) |
| #264 | QA the NO-MARKET corridor end to end | [264-nomarket-corridor.md](264-nomarket-corridor.md) |
| #265 | QA the DERIVATIVE corridor end to end | [265-derivative-corridor.md](265-derivative-corridor.md) |

## Live Deployment Status at Test Time

- **URL:** https://wayfare-cdb9.onrender.com/
- **Deployment age:** 32 days (last run 2026-08-22T12:10:05Z)
- **Cold start:** ~58s on first request, ~0.6s thereafter
- **Healthz:** Returns `{"status":"ok"}` with data-age info but no prominent staleness warning

## Key Findings

1. **KESC (NO-MARKET)** degrades honestly — no empty table, all 12 rungs show clear error messages, integrity badge is prominent. ✅
2. **GHSC (DERIVATIVE)** shows dependency in prose and structure (`depends_on` array, per-rung warnings), but loss figures are not decomposed into GHSC-only vs NGNC-compounded components. ⚠️
3. **NGNC (DIRECT)** live data now shows a GOOD route at 0.1 USDC (0% loss), contradicting the stored finding that says "No usable size." The case study text is stale. ❌
4. **Stale banner** works structurally (`live: false`, `stale.age_human`) but does not prominently warn about 32-day-old data. ⚠️
5. **Error paths** return machine-readable codes and reject unknown parameters. ✅
6. **UI state matrix:** 3 of 12 cells were reachable on the live deployment. 9 cells are not reachable under normal operation. Documented in artifact #263.

## Separate Issues Filed

The following problems were identified but are not fixed in this branch (per constraints — they must be filed separately):

- Stale case study text (NGNC 25% claim contradicts live data)
- `floor_loss_pct: "0.00"` with `floor_size: "0"` for NO-MARKET corridors could mislead consumers
- Loss figures for DERIVATIVE corridors are not decomposed
- No prominent staleness warning on the page despite 32-day-old data
- Cold start takes ~58s with no prominent indicator
- Case study conflates NGNC and GHSC stories
