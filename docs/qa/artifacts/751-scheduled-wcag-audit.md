# #751 — Scheduled WCAG re-audit of the UI

**Backlog entry H3 (issues #273, #751).** This documents the automated, scheduled re-audit of the corridor UI against WCAG 2.2 Level AA, re-running the checks from the initial accessibility audit ([#273](273-accessibility-audit.md)) on a schedule with the browser harness and failing whenever new violations are detected.

## Purpose

The UI changes over time, while manual accessibility audits are point-in-time artifacts that silently drift. Automated checks continuously verify accessibility in CI so that accessibility regressions fail immediately rather than waiting for another manual audit pass.

## Architecture

The automated audit consists of three components:

1. **Axe Core Engine Integration (`docs/qa/browser/run-accessibility.mjs`):**
   Uses `@axe-core/playwright` to analyze the rendered DOM across live UI states (`USDC-NGNC` direct corridor, `USDC-KESC` derivative corridor, empty/initial state, and both light and dark color schemes).
2. **Baseline Regression Gate (`docs/qa/browser/results/accessibility-baseline.json`):**
   Encodes the known findings documented in #273 (F-1 light warn contrast, F-2 live region announcement, F-3 focus ring defaults, F-4 motion pulse).
   - Any violation matching a known baseline finding is cataloged without failing the run.
   - Any **new** violation that is not part of the documented baseline triggers an exit code `1` and fails the job.
3. **Nightly Scheduled Workflow (`.github/workflows/qa-accessibility.yml`):**
   Executes nightly at `03:00 UTC` and on `workflow_dispatch`. It builds `./cmd/wayfared`, launches test servers via `servers.sh`, and runs the accessibility and contrast audit suites against the replay-backed instance without external network access.

## Running Locally

To run the accessibility audit locally:

```bash
# 1. Start the local QA servers
./docs/qa/browser/servers.sh start

# 2. Run the automated WCAG audit
cd docs/qa/browser
npm run accessibility

# 3. Run the stylesheet contrast calculation
npm run contrast

# 4. Stop servers
cd ../../..
./docs/qa/browser/servers.sh stop
```

Exit codes:
- `0`: Clean run or only known baseline findings observed.
- `1`: New accessibility violations detected (regressions fail).
- `2`: Harness or network error.

Results are written to `docs/qa/browser/results/accessibility.json`.
