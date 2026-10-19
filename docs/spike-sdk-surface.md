# Spike: what a Wayfare SDK would need to expose

Issue [\#221](https://github.com/Wayfare-labs/wayfare/issues/221), backlog `#161`.

**Status: completed, design-only.** The SDK's shape is a design question that
can be answered now against the wire contract as it exists; whether to *build*
it stays gated on [\#217](https://github.com/Wayfare-labs/wayfare/issues/217)
("Spike: who would consume this API"), which is still OPEN (state checked
2026-09-23). This document specifies what a client library for this API would
have to expose and how, so that if \#217 returns consumers the SDK is a wire
binding rather than a redesign. No code was written and none is proposed to be
written by this issue.

---

## Why this matters

Backlog `#157` / issue \#217 states the intended consumers plainly: "Wallets,
PSPs and anchors are named as intended users; none has been asked" (issue \#217
"Spike: who would consume this API, and what shape do they need", opened from
`docs/backlog.md:1056-1058`; the same consumer set is independently named in
backlog `#273`/issue `#326`, `docs/backlog.md:1695-1698`). Both checked
2026-09-23.
Backlog `#161` pins the open question: *who* would consume it is settled only
by asking, but *what* a client needs from it is a design question answerable
now. An SDK built before the consumers are named would bake in a shape nobody
asked for; naming the shape before the consumers are found lets \#217
interview against something concrete.

---

## What the wire contract already guarantees

Checked 2026-09-23 against `docs/api.md`, `server/api.go`, `server/trend.go`,
`route/route.go`, `checks/`. The API surface is small and stable:

| Endpoint | Notes | Source |
|---|---|---|
| `GET /api/corridor` | `from`, `to`, `sizes` (≤24), `live=1`; measures or returns latest stored run | `docs/api.md:19-192` |
| `GET /api/corridor/trend` | `from`, `to`, `limit` (default 100, capped 500); reads history only, never measures | `docs/api.md:194-241`, `server/trend.go:28-33` |
| `GET /api/assets` | verified assets, `can_be_destination` | `docs/api.md:243-283` |
| `GET /healthz` | liveness, no upstream validation | `docs/api.md:285-301` |
| `GET /` | embedded single-file UI | `server/api.go:86` |

Guarantees an SDK can rely on:

- **Money is a decimal string, never a JSON number or float.** "Amounts, rates,
  percentages, and sizes are decimal strings, not JSON numbers"
  (`docs/api.md:28`); run records repeat "All money is a decimal string. Never a
  JSON number, never a `float64`." (`docs/run-store.md:114`). An SDK must parse
  with a decimal type, never a binary float; `route/money.go` pins that
  wire-boundary rule by walking every money string a client can receive, and
  the project's arithmetic type is `decimal.Decimal` (shopspring, used as
  `route/route.go` and `analysis/analysis.go` do).
- **The result has two independent dimensions: integrity and verdict.**
  `integrity` is `DIRECT`/`DERIVATIVE`/`NO-MARKET`/`UNKNOWN` and is independent
  of `verdict` (`GOOD`/`FAIR`/`POOR`/`UNUSABLE`/`UNKNOWN`)
  (`route/route.go:111-168`, `docs/api.md:33`). No-Market ≠ unusable:
  "`NO-MARKET` means no path was returned; it is not the same claim as a priced
  route with an `UNUSABLE` verdict" (`docs/api.md:306`).
- **No verdict means no score.** `scored: false` when the reference could not
  be trusted; verdicts, loss figures and recommendation are then withheld
  (`route/route.go:430-475`). An SDK must model the unscored state rather than
  treating missing loss as zero.
- **Live vs outdated is explicit, never inferred.** A `stale` envelope with
  `recorded_at`/`age_seconds`/`age_human` appears exactly when `live` is false;
  consumers must not infer freshness from deployment time or absence of error
  (`docs/freshness.md`, `docs/api.md:302-306`, `server/api.go:470-478`).
- **Counterparty findings are three-valued.** Each check is
  `{determined, passed}` with `severity` (`critical`/`warning`/`notice`/`info`)
  and `worst_severity` rolled up; `determined: false` is not a failure — the
  tri-state is preserved through storage (`docs/checks.md`, `docs/run-store.md:107-112`).
  Findings qualify the headline and never change integrity or verdict
  (`docs/api.md:46`).
- **Trend is a read; the store's minimums travel with it.** `trend` returns a
  `DivergenceStats` block whose `determined` is false below
  `analysis.MinSampleSizeForMeanStdDev` (30) with a `reason`, and whose trend
  fields appear only with 60+ observations (`server/trend.go:60-86`,
  `analysis/analysis.go:39-48`). An SDK must surface undetermined-with-reason,
  not a plausible-looking zero.
- **Errors carry both a message and a machine code.** "Clients should switch on
  code, not on the human-readable message" (`server/api.go:343-357`).

---

## What an SDK for this contract would need to expose

The useful list is short because the wire contract already did the design work.
A client library should be a *typed, idiomatic binding of the wire shape* plus
the following client-side behaviours, not a reimplementation of measurement:

1. **A decimal type** (string-preserving, arithmetic-capable) for every amount
   and percentage field — the single non-negotiable, since every downstream bug
   class in this project traces to float handling (`route/route.go` uses
   rounded decimal everywhere; tests pin decimal-string boundaries).
2. **A typed result model** covering the four states a corridor can be in, so
   a caller cannot silently conflate them:

   | State | Wire markers |
   |---|---|
   | priced corridor | `scored: true`, integrity `DIRECT`/`DERIVATIVE`, rungs with verdicts |
   | unscored corridor | `scored: false`, verdict absent/UNKNOWN, `finding` explains |
   | no-market corridor | integrity `NO-MARKET` |
   | unknown corridor | integrity `UNKNOWN` |

3. **Freshness handling**: a `Live?`/`measured_at`/`age` decision at the SDK
   level so the consumer defaults to shown-history and opts into `live=1`
   deliberately. The project's own UI rule is that a stored reading must be
   labelled as such; an SDK that cached or hid the flag would recreate exactly
   the misread `docs/freshness.md` warns about.
4. **Tri-state check results** surfaced as three values
   (passed / failed / undetermined), plus `worst_severity`, never collapsed to
   a boolean.
5. **Reference attribution**, carried through so a verdict never loses which
   provider and which as-of it was scored against (`reference_source`, `as_of`,
   `secondary_*`, `divergence_pct`, `scored_against` — wire fields in
   `docs/api.md:35-37` and record fields `docs/run-store.md:46-55`).
6. **Error typing by `code`** (e.g. `upstream_timeout`, `no_fiat_peg`,
   `unknown_send_asset`, `invalid_sizes` — `server/api.go:347-357`), not by
   message text, so retry/backoff decisions are mechanical.
7. **Trend pagination awareness**: `limit` clamps to 500, not errors
   (`server/trend.go:309-330`); note that with no offset/cursor parameter on the
   wire today, an SDK that must walk *all* history pages by successive `limit`
   calls is a design gap to flag, not paper over.

---

## What an SDK must *not* expose

The public contract is explicit and the wire reflects it: the service is
read-only, holds no funds, and executes no payments ("All endpoints are
read-only. The service does not hold funds, issue tokens, sign transactions, or
execute payments." — `docs/api.md:5`). An SDK therefore must expose **no write,
transfer, signing, or account primitives**, and must not pretend to: it reads a
monitor. Client libraries in this space also must not reach into the internal
packages (`route`, `runstore`, `refrate`) directly — those are not a public
contract; only the wire shape and [api.md](api.md) are.

---

## Gaps and open questions before building

- **Consumer identity is unresolved.** Until \#217 interviews wallets/PSPs/
  anchors, "SDK in which language / runtime" is a guess. This spec is
  language-agnostic by construction (it binds wire JSON).
- **Error-code taxonomy is inconsistent across endpoints.** `server/api.go`
  uses `snake_case` codes (`method_not_allowed`, `no_fiat_peg` — api.go:347-357)
  while `server/trend.go` uses `SCREAMING_SNAKE` (`METHOD_NOT_ALLOWED`,
  `UNKNOWN_ASSET`, `BAD_LIMIT` — trend.go:230-266). An SDK must handle both
  spellings; harmonising them is a small, separate issue and is **out of scope**
  here (this spike makes no code change).
- **Trend has no cursor.** Consumers wanting the entire history of a long-lived
  corridor can fetch at most 500 runs per request (`server/trend.go:32-33`).
  Whether that is a product gap is a \#217 consumer question.

---

## Verdict

**A consumer-facing SDK is a typed binding of an already-stable wire contract,
not a design problem — with one named precondition and two named gaps.** The
precondition is \#217's consumers; absent them the SDK has no audience and no
language. The gaps (error-code case split, trend lacks a cursor) are small
and would be fixed at build time, not by this spike. **Negative/inconclusive
finding, stated plainly:** nothing in the tree currently proves any external
consumer needs an SDK, so building one today would be speculative; the value of
this issue was to fix the *shape* against the real wire so the build is
mechanical if the audience arrives.

## Related

- [\#217](https://github.com/Wayfare-labs/wayfare/issues/217) — consumer
  discovery, the gating issue
- [api.md](api.md), [freshness.md](freshness.md), [checks.md](checks.md),
  [run-store.md](run-store.md) — the contract the SDK binds
- `server/api.go`, `server/trend.go` — wire implementation and the two error-code
  spellings
- [spike-alerting-semantics.md](spike-alerting-semantics.md) — what a client
  would be notified *about* (future capability)