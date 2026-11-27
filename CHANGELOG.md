# Changelog

Consumer-facing changes to Wayfare's wire contracts, run-store records,
thresholds, and deployment behavior. Dates identify when the changes appeared
on the main branch.

Deployment entries describe changes to when readers see data or identify the
running build, not every automatic deployment.

## Wire contract

- **2026-11-21** — Published `ledger_range` and the ledger observed for each
  rung. [Issue #720](https://github.com/Wayfare-labs/wayfare/issues/720),
  [PR #945](https://github.com/Wayfare-labs/wayfare/pull/945).
- **2026-11-20** — Added optional correction metadata to readings so consumers
  can identify a corrected record and its replacement.
  [Issue #729](https://github.com/Wayfare-labs/wayfare/issues/729).
- **2026-10-23** — Added `dependency_chain` for corridors whose assets depend
  on other fiat assets. [Issue #398](https://github.com/Wayfare-labs/wayfare/issues/398),
  [issue #22](https://github.com/Wayfare-labs/wayfare/issues/22).
- **2026-10-19** — Quote paths gained structured `hops` with asset identities;
  reference responses gained diagnostics for a third provider.
  [Issue #100](https://github.com/Wayfare-labs/wayfare/issues/100),
  [issue #790](https://github.com/Wayfare-labs/wayfare/issues/790).
- **2026-09-29** — Priced rungs can include `path_count`, the number of
  distinct paths. [Issue #534](https://github.com/Wayfare-labs/wayfare/issues/534).
- **2026-09-05** — Published the execution-rate `curve` and the scored and
  secondary reference timestamps (`reference_as_of`,
  `reference_secondary_as_of`).
  [Issue #434](https://github.com/Wayfare-labs/wayfare/issues/434),
  [issue #368](https://github.com/Wayfare-labs/wayfare/issues/368).
- **2026-08-31** — `loss_pct` now preserves full decimal precision instead
  of rounding for display, so it reconciles with the verdict. Added `kind` to
  distinguish DEX and anchor quotes.
  [Issue #425](https://github.com/Wayfare-labs/wayfare/issues/425),
  [issue #463](https://github.com/Wayfare-labs/wayfare/issues/463).
- **2026-08-30** — Published explicit asset identity in the wire form; an
  unidentifiable asset is not assigned a fabricated wire representation.
  [Issue #178](https://github.com/Wayfare-labs/wayfare/issues/178).
- **2026-08-27** — Rungs gained marginal execution-cost fields; reference
  fetch time is distinct from the provider's `as_of` time.
  [Issue #365](https://github.com/Wayfare-labs/wayfare/issues/365).
- **2026-08-25** — Empty `loss_amount` is omitted from quote JSON. Reference
  responses also began distinguishing parallel-rate data, and priced rungs
  gained cost decomposition.
  [Issue #111](https://github.com/Wayfare-labs/wayfare/issues/111),
  [issue #350](https://github.com/Wayfare-labs/wayfare/issues/350),
  [issue #95](https://github.com/Wayfare-labs/wayfare/issues/95).
- **2026-08-21** — Added reference agreement and secondary-provider details
  so responses distinguish corroborated rates from disagreement.

The API was versioned under `/api/v1` on 2026-11-21; the unversioned routes
remain deprecated aliases. [PR #933](https://github.com/Wayfare-labs/wayfare/pull/933).

## Run-store record versions

Records are append-only and hash-chained. New versions add optional fields
after the previous version's fields so old records retain their original
preimages and remain verifiable; versions are not rewritten in place.

- **2026-11-21 — Version 6:** Added `ledger_range` and per-rung `ledger` to
  show which ledger(s) a ladder observed. Unknown ledger values remain absent.
  [Issue #720](https://github.com/Wayfare-labs/wayfare/issues/720),
  [PR #945](https://github.com/Wayfare-labs/wayfare/pull/945).
- **2026-11-20 — Version 5:** Added an append-only `correction` record that
  identifies the corrected sequence and explains the reason.
  [Issue #729](https://github.com/Wayfare-labs/wayfare/issues/729).
- **2026-10-20 — Version 4:** Added the optional `snapshot` content hash,
  linking a recorded measurement to its exact upstream snapshot.
  [PR #810](https://github.com/Wayfare-labs/wayfare/pull/810).
- **2026-08-27 — Version 3:** Added optional `reference.fetched_at` so a
  reader can distinguish when a provider stamped a rate from when Wayfare
  fetched it. [Issue #365](https://github.com/Wayfare-labs/wayfare/issues/365).
- **2026-08-25 — Version 2:** Added optional `checks` and `metrics` blocks to
  each record. [PR #361](https://github.com/Wayfare-labs/wayfare/pull/361),
  [issue #93](https://github.com/Wayfare-labs/wayfare/issues/93).
- **2026-08-21 — Version 1:** Introduced the hash-chained corridor
  measurement record.

## Thresholds

- **2026-10-20** — Added `GET /api/thresholds`, which publishes the verdict
  and reference-divergence thresholds directly from the code constants.
  [PR #799](https://github.com/Wayfare-labs/wayfare/pull/799),
  [issue #626](https://github.com/Wayfare-labs/wayfare/issues/626).
- **2026-08-21** — Introduced reference-provider bands: `AGREE` through 2%
  divergence, `DISAGREE` above 2%, and `MALFUNCTION` above 10%. A difference
  in timestamps greater than 48 hours is classified as `STALE` unless the
  divergence already exceeds the malfunction threshold.
- **2026-08-04** — Set the route verdict bands at 3% (`GOOD`), 8% (`FAIR`),
  and 20% (`POOR`); greater losses are `UNUSABLE`. The boundaries are
  inclusive.

The thresholds endpoint is now also available at `/api/v1/thresholds`.
`/api/thresholds` remains as a deprecated alias.

## Deployment behavior

- **2026-10-20** — `/healthz` began reporting build provenance, including the
  code and data commits embedded in the running service.
  [PR #804](https://github.com/Wayfare-labs/wayfare/pull/804).
- **2026-10-18** — Measurement data updates began triggering a fresh hosted
  deployment so the embedded history served to readers advances with the
  committed data. [PR #770](https://github.com/Wayfare-labs/wayfare/pull/770).
- **2026-09-29** — Added a post-deployment smoke check of the running image's
  HTTP surface. [PR #516](https://github.com/Wayfare-labs/wayfare/pull/516).
- **2026-08-30** — Documented that hosted history is embedded at image build
  time: a scheduled measurement alone does not update the history served by
  the running deployment. [PR #421](https://github.com/Wayfare-labs/wayfare/pull/421),
  [issue #143](https://github.com/Wayfare-labs/wayfare/issues/143).
