# Good first issue audit

**Issue:** [#329 — A "good first issue" audit](https://github.com/Wayfare-labs/wayfare/issues/329). **Checked:** 2026-09-26.
**Question:** Can a new contributor orient themselves from the README and
CONTRIBUTING, understand the task boundary from its issue, and make progress
without first needing a maintainer to define the approach?

## Scope correction

Issue #329 says seven open issues carried the `good first issue` label. The
upstream metadata does not reproduce that count: the open-issues API query
returned **21** on 2026-09-26. The individual issue event histories show that
20 of those currently open issues had received the label before #329 was
opened at 2026-08-24T20:57:05Z; #329 itself received the label at
20:57:07Z. The seven-item subset intended by the issue is not identified, so
this audit assesses the complete current set rather than guessing. The count
is a dated tracker observation, not a repository capability claim.

**Source:** GitHub REST API,
[`repos/Wayfare-labs/wayfare/issues?state=open&labels=good%20first%20issue&per_page=100`](https://api.github.com/repos/Wayfare-labs/wayfare/issues?state=open&labels=good%20first%20issue&per_page=100), checked 2026-09-26; issue [#329](https://github.com/Wayfare-labs/wayfare/issues/329), opened 2026-08-24.
The issue event history for [#329](https://api.github.com/repos/Wayfare-labs/wayfare/issues/329/events?per_page=100) confirms its label was added two seconds after opening; the event histories for the other listed issues were checked on the same date.

## Findings

“Suitable” means the issue gives a bounded outcome and an entry point, with
project invariants discoverable in the README and CONTRIBUTING. It does not
mean the work needs no repository reading or review. “Not yet” means the label
currently promises more than the issue state allows; it is an issue-tracker
follow-up, not a judgment about the contributor.

| GitHub issue | Finding | Audit |
|:---|:---|:---|
| [#6 — Verify the USDC issuer against circle.com's stellar.toml](https://github.com/Wayfare-labs/wayfare/issues/6) | Not yet | This identity underlies every published corridor. The issue is understandable, but a mistaken “correction” would change the asset being measured. Keep maintainer review on the verification and any registry edit; not a safe unsupervised first contribution. |
| [#14 — Make integrity states visually distinct in the UI](https://github.com/Wayfare-labs/wayfare/issues/14) | Suitable, bounded | The README explains why integrity is independent of the verdict; CONTRIBUTING reserves the taxonomy, not its presentation. Keep the task to rendering/copy and do not change integrity semantics. |
| [#167 — Expected failure cost must stay undetermined, and say why](https://github.com/Wayfare-labs/wayfare/issues/167) | Suitable, test-only | The intended guard is explicit: prevent an undetermined cost from becoming zero. A focused offline test can protect the existing rule without changing the costing implementation. |
| [#171 — Count distinct paths per rung as a published measurement](https://github.com/Wayfare-labs/wayfare/issues/171) | Not yet | The issue is explicitly blocked on #99. A label must not invite work before its data/contract dependency exists; remove the newcomer label or clear the blocker first. |
| [#172 — Never average two provider mids, and test that we do not](https://github.com/Wayfare-labs/wayfare/issues/172) | Suitable, test-only | CONTRIBUTING states that reference rates must be attributable and the README describes independent references. The issue asks for an adversarial regression test, not a change to pricing or the reconciliation rule. |
| [#233 — Document `ladder -replay`](https://github.com/Wayfare-labs/wayfare/issues/233) | Suitable | This is a bounded documentation gap around an existing command. A contributor can verify the flags and fixture workflow in the source/docs, then add a usage example without changing behavior. |
| [#276 — Audit every recorded snapshot for provenance](https://github.com/Wayfare-labs/wayfare/issues/276) | Suitable with an evidence boundary | The deliverable is an inventory of existing fixtures and their recorded provenance. Report missing metadata as missing; do not infer an upstream source or claim a fixture is exhaustive. The offline-test guidance explains the fixture model. |
| [#277 — Verify the committed chain independently](https://github.com/Wayfare-labs/wayfare/issues/277) | Suitable | CONTRIBUTING requires Go 1.22+ and the README points to the project; `docs/verify-store.md` documents a read-only verification command and its output. The issue is reproducible without contacting an upstream service. |
| [#282 — A typographic scale](https://github.com/Wayfare-labs/wayfare/issues/282) | Suitable, UI-only | The issue lists the ad-hoc sizes it targets. A newcomer can consolidate typography while preserving semantic colors and not introducing a frontend build system. |
| [#283 — A spacing and radius scale](https://github.com/Wayfare-labs/wayfare/issues/283) | Suitable, UI-only | The desired change is localized to CSS tokens. Keep it presentational; the issue does not license changing states, pricing, or the single-file UI architecture. |
| [#289 — Persist the theme choice](https://github.com/Wayfare-labs/wayfare/issues/289) | Not yet | The issue is marked blocked on #287. Do not advertise a blocked follow-up as ready until the theme toggle contract exists. |
| [#290 — Drive the corridor selector from /api/assets](https://github.com/Wayfare-labs/wayfare/issues/290) | Suitable | The API and selector are identifiable entry points. The contributor can consume the existing verified-assets response and preserve the current default and error behavior. |
| [#292 — Make the corridor state URL-addressable](https://github.com/Wayfare-labs/wayfare/issues/292) | Suitable with a test/review note | The desired URL state is concrete. The PR should state precedence for URL parameters versus current form defaults and cover reload/back behavior; it must not alter which measurement the server returns. |
| [#293 — Preserve selection and input across an error](https://github.com/Wayfare-labs/wayfare/issues/293) | Suitable | The failure and expected user-visible outcome are stated. A contributor can keep the current controls intact while updating the result panel, without changing server or measurement semantics. |
| [#305 — Give the loss curve a text alternative](https://github.com/Wayfare-labs/wayfare/issues/305) | Suitable with accessibility review | The issue identifies the SVG and corresponding table. Associate the chart with a textual summary or data table; do not imply interpolation or values that were not measured. |
| [#306 — Touch targets and hit areas](https://github.com/Wayfare-labs/wayfare/issues/306) | Suitable, UI-only | The issue is presentation work and CONTRIBUTING directs browser QA for UI changes. Keep target-size adjustments from obscuring state labels or changing interactions. |
| [#315 — Cache Go modules consistently across jobs](https://github.com/Wayfare-labs/wayfare/issues/315) | Suitable with CI review | The discrepancy is stated in the workflow. A small, reviewable workflow change is possible; preserve the offline-test job's deliberate pre-download and network-isolation order. |
| [#317 — Pin and document the dependency surface](https://github.com/Wayfare-labs/wayfare/issues/317) | Not yet | “Pin and document” does not settle the policy, update cadence, or enforcement mechanism. Agree those choices first; otherwise a newcomer could produce a brittle pinning scheme. |
| [#327 — Seed Discussions with the questions contributors actually ask](https://github.com/Wayfare-labs/wayfare/issues/327) | Not yet | The scope depends on choosing and publishing prompts in GitHub Discussions, but gives no agreed questions or owner for ongoing moderation. Define the seed set and maintenance expectation before treating it as a self-contained first task. |
| [#328 — Issue and pull-request templates](https://github.com/Wayfare-labs/wayfare/issues/328) | Suitable | The missing artifact and repository location are explicit. Templates can mirror CONTRIBUTING's existing constraints without introducing a new code path or dependency. |
| [#329 — A "good first issue" audit](https://github.com/Wayfare-labs/wayfare/issues/329) | Suitable after this scope correction | The artifact is now linked from the contributor entry points. Its original count was stale by the date checked; this document records the discrepancy and audits the observed set rather than retroactively inventing the historical seven. |

## What a new contributor can rely on

The README explains the product, current limits, architecture and documented
entry points. CONTRIBUTING explains setup, invariants, maintainer-owned areas,
code conventions and the expected verification/reporting discipline. Those are
sufficient orientation for the **suitable** items above when read alongside the
specific issue description. The unsuitable items are not evidence that a new
contributor lacks ability: their dependencies or decision boundaries are not
ready to be delegated as-is.

The audit does not certify that a future pull request will be accepted, nor
that “easy” means no project reading. Each change remains subject to the same
review and CI requirements as any other contribution.

## Recommended label hygiene

1. Remove `good first issue` from #171 and #289 while their blockers remain.
2. Have a maintainer settle scope/policy on #6, #317 and #327 before inviting an
   unsupervised implementation.
3. Keep the UI issues explicitly presentational and the test-only issues
   regression-focused; none should change pricing, verdict thresholds,
   integrity semantics, check composition or the hash-pinned run-record layout.
4. Refresh this audit whenever the open label set changes materially. Preserve
   a dated list in future audits so the originally reported count can be
   reconstructed.

## Sources checked 2026-09-26

- [README.md](../README.md) and [CONTRIBUTING.md](../CONTRIBUTING.md): newcomer orientation, product/architecture boundaries, setup, invariants and review requirements.
- [docs/offline-testing.md](offline-testing.md): fixture and network requirements for tests.
- [docs/verify-store.md](verify-store.md): chain-verification command and expected outputs.
- [docs/backlog.md](backlog.md): linked backlog summaries and explicit blockers where present.
- Upstream issue descriptions linked in the table and the GitHub open-label query above.
- Current UI, server, test, workflow and runstore implementations were checked only to confirm the scope boundaries stated here; no behavior is claimed beyond the code present at this checkout.
