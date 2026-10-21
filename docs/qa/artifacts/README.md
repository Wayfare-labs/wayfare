# QA artifacts, and how they were produced

QA artifacts are dated, named records of a review or measurement that a reader
can check against the code. They are written up whether they pass, fail, or are
ambiguous, and the status is the claim — not the tone.

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
| #275 | Reproduce every figure in docs/corridor-measurements.md | [275-reproduce-corridor-measurements.md](275-reproduce-corridor-measurements.md) | — |

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

## What belongs here

- browser QA runs (colour, contrast, accessibility, screen-reader, cross-browser)
- smoke tests against a deployed instance
- reproducibility checks — can an independent run reproduce a claimed figure?
- documentation-vs-code audits — what the docs say versus what the code does
- offline / network isolation checks

## What does not

- a single figure pulled from a live run with no timestamp and no source

That belongs in `docs/corridor-measurements.md` or a corridor research doc, with
the timestamp and the raw output. An artifact can *cite* that doc; it is not a
substitute for it.

## Naming

`<run-id>-<one-line-subject>.md`

Examples in this tree: `272-color-schemes.md`, `259-served-history-vs-committed.md`,
`260-api-consumer-qa.md`.

## Checklist for a new artifact

- [ ] Title states what was inspected and on what date.
- [ ] The commit or revision checked is named.
- [ ] Every tool, browser, or environment that produced a result is named enough
      that a reader could repeat it.
- [ ] A failure is recorded as a failure, not as a question.
- [ ] A contradiction with the docs is recorded as a finding, not silently fixed
      in the text.
- [ ] Anything that depends on a live network record carries the timestamp and the
      source.

## Acceptance criteria

- [x] The directory and this note exist.
- [x] A new artifact added here is named and dated the same way as the existing
      ones.
