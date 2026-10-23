# Session audit: 2026-09-28 — Issue #48

> **This is an audit, not a feature request.** It re-verifies the state the
> [2026-08-23 audit](https://github.com/Wayfare-labs/wayfare/issues/48)
> recorded, checks what has since changed, and states the current state with
> evidence for every claim.

**Target repo:** `Wayfare-labs/wayfare`
**Deployed target:** https://wayfare-cdb9.onrender.com/
**Tested by:** Ryzenthefirst, branch `feat/wave-batch`
**Timestamp:** 2026-09-28 (all times UTC)
**Main at audit time:** `0fb02e2` — *feat: compare corridors on shared fields (#499)*, committed `2026-09-28T15:55:48Z`

---

## Summary

| Component | 2026-08-23 finding | 2026-09-28 finding | Δ |
|:---|:---|:---|:---:|
| Required CI checks on main | ✅ green | ✅ green (all 4) | = |
| Render `/healthz` | ❌ returns 404 | ✅ returns 200, `status:ok`, freshness block | ✅ fixed |
| Measure workflow | ❌ push to main rejected by ruleset | ❌ still failing — now at PR creation (Actions PR permission) | ↔ moved |
| Data freshness | ❌ stale (~14h) | ❌ stale **~37 days** (2026-08-22T12:10Z) | ↓ worse |
| Corridors measured | 1 (USDC-NGNC) | 3 (USDC-GHSC, USDC-KESC, USDC-NGNC) | ✅ expanded |

---

## Required status checks on main — all green

Check runs on `0fb02e2` (HEAD of main):

| Check | Conclusion |
|:---|:---|
| `build and test` | success |
| `golangci-lint` | success |
| `tests run with no network` | success |
| `container image builds` | success |

Evidence: `gh api repos/Wayfare-labs/wayfare/commits/main/check-runs`.

---

## Render `/healthz` — resolved (was 404, now 200)

The 2026-08-23 audit found `/healthz` returning HTTP 404. It now returns
**HTTP 200** with a well-formed JSON freshness block:

```json
{"data":{"USDC-GHSC":{"recorded_at":"2026-08-22T12:10:05Z","age_human":"37d ago"},
"USDC-KESC":{"recorded_at":"2026-08-22T12:10:09Z","age_human":"37d ago"},
"USDC-NGNC":{"recorded_at":"2026-08-22T12:09:59Z","age_human":"37d ago"}},
"freshness":{"chain_head":null,"newest_record_at":"2026-08-22T12:10:09Z","record_count":3},
"status":"ok"}
```

Observed `2026-09-28T20:35Z` via `GET /healthz`. The first request hit a Render
free-tier cold-start splash ("service waking up"); the endpoint answered on the
next request. This matches [`262-healthz-cold-start.md`](262-healthz-cold-start.md).

**Note (design decision):** `status` is `ok` even though every corridor is 37
days stale. Freshness is *reported* in the body but does not influence the
health verdict.

---

## Critical finding: the measure workflow is still broken

`.github/workflows/measure.yml` was rewritten since the last audit: it no
longer pushes directly to main (the old ruleset-rejection finding is gone).
It now measures, rotates, verifies, and opens a PR via
`peter-evans/create-pull-request@v7`.

That final step **fails on every scheduled run**:

```
##[error]GitHub Actions is not permitted to create or approve pull requests.
  — https://docs.github.com/rest/pulls/pulls#create-a-pull-request
```

- Failing step: **Open measurement pull request** (measure/rotate/verify all succeed).
- Five consecutive scheduled failures observed: `2026-09-27T10:56Z`, `15:55Z`,
  `20:32Z`, `2026-09-28T02:17Z`, `12:12Z`. Latest run: `36420332422`.
- No `automation/measure` PR has ever been opened
  (`gh pr list --head automation/measure --state all` is empty).

**Root cause:** the repository (or org) has *Allow GitHub Actions to create and
approve pull requests* disabled, so the default `GITHUB_TOKEN` cannot open the
measurement PR. This is a settings/permissions gate, not a code defect.

---

## Data freshness — stale ~37 days (consequence of the above)

The committed store has not advanced since the last successful measurement:

| Corridor | Newest `recorded_at` (committed) |
|:---|:---|
| `data/USDC-GHSC.ndjson` | `2026-08-22T12:10:05Z` |
| `data/USDC-KESC.ndjson` | `2026-08-22T12:10:09Z` |
| `data/USDC-NGNC.ndjson` | `2026-08-22T12:09:59Z` |

That is ~37 days before this audit. The deployed instance serves the same
stale window (`/healthz` above). The measurement *job* itself succeeds every
run — only the PR that would land the data fails — so the fix is entirely in
how the data reaches main, not in the measurement.

---

## Recommended actions (maintainer decision required)

1. **Unblock the measure workflow.** Either enable *Settings → Actions →
   General → Workflow permissions → "Allow GitHub Actions to create and approve
   pull requests"*, or give the workflow a token that may open PRs (a PAT or a
   GitHub App installation token) in place of the default `GITHUB_TOKEN`. This
   is the sole blocker keeping the data 37 days stale.
2. **Decide whether `/healthz` `status` should degrade on staleness.** It
   currently reports `ok` at 37 days; the freshness data is present but unused
   by the verdict.

---

## What was NOT checked

- **Fly.io deployment** — `fly.toml` exists but was not exercised.
- **Horizon / Stellar RPC availability** — the measure job succeeds, so upstream
  data sources are reachable; not probed directly.
- **refrate providers** — not independently verified this session.
- **Repository settings** — a contributor cannot read or change the Actions
  PR-creation setting; the root cause above is inferred from the exact API
  error the workflow logs.
