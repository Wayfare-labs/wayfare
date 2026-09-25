# Troubleshooting

Issue [#230](https://github.com/Wayfare-labs/wayfare/issues/230), backlog
`#170`.

The four stumbles people actually hit, in the order of frequency. Each entry
is a symptom → diagnosis → fix. The line references were checked against the
tree at `c9bfb75` on 2026-09-24; a figure from the deployed instance is only
as current as its build (see §2).

---

## 1. "I'm getting rate-limited everywhere"

### Symptom

Log lines naming `ErrRateLimited` from `dex` (Horizon) or `refrate` (a
reference provider), each with a `retry after ...` suffix:

```
ERROR ... dex: Horizon refused USDC -> NGNC strict-send on 1000: rate limited; retry after 1m0s
ERROR ... refrate: exchangerate-api refused: rate limited; retry after 3600s
```

or, over the API, errors with a `source` of `horizon` / a provider name.

### Diagnosis

A 429 is a rate budget, not a failure. The code says so at the source: "under
a monitoring schedule a 429 is routine and transient — ask again"
(`dex/dex.go:68-72`), and every rate-limit error carries the
`Retry-After`-asked-for duration when the server sent one
(`dex/dex.go:76-89`, `refrate/refrate.go:140-158`). Providers have small,
shared budgets: mark and measure every corridor in one run (the four-registered
corridor set) touches each reference provider at most a handful of times per
run, and a `wayfared` server plus a manual `cmd/ladder -live` in the same
hour can exceed a free provider's daily allotment on their own.

### Fix

1. **Read `RetryAfter` and wait it out.** The duration in the line is the
   server's own request. A few minutes of decoupling is the intended recovery
   (`refrate/refrate.go:147-149`, `dex/dex.go:79-81`).
2. **Don't run the server and a local ladder at the same time.** A live server
   request (`?live=1`) prices a full ladder (README, `?live=1` note), and so
   does `go run ./cmd/ladder`; two ladders in the same minute exhaust
   Horizon's budget twice over.
3. **For CI/automation, pace against the schedule:** Wayfare itself measures
   once per six hours (`.github/workflows/measure.yml` cron) because that is
   the sustainable cadence for the providers in use, not because more would be
   better (`README.md` "Freshness depends on the measure workflow").
4. If a *provider*, not Horizon, is the one 429ing, the reference feeds are
   named per corridor in the run store record — check
   `docs/monitor.md` for how each provider's budget is budgeted per run.

A 429 that never clears (same duration on every retry, hours apart) is
different: that is an exhausted daily allotment, which no retry shortens. Wait
for the provider's daily reset, then look at §2 if the deployment is what is
producing the traffic.

---

## 2. "The deployment looks asleep / gives me an empty or stale answer"

### Symptom

- The first request after a quiet period fails outright or takes several
  seconds (`README.md:41-45`).
- A response succeeds but carries `live: false` and a `stale` block — or no
  figure at all with an error.

### Diagnosis

The public instance sleeps after fifteen minutes without traffic, so the first
request must wake it, which can fail before the instance answers
(`README.md:41-45`). Separately, the instance serves *recorded* history, not
live numbers: it runs with `-schedule=0 -history-first` and answers from the
hash-chained history embedded in the binary at build time
(`README.md:27-31`). The measure workflow writing new records is
[#63](https://github.com/Wayfare-labs/wayfare/issues/63) — currently unable to
push — so "down" and "up-to-date" are different complaints with different
fixes. `/healthz` answers **process** liveness (`status`) and
**data** age (`data.age_human`) separately, and `status: "ok"` does **not**
mean a live measurement succeeded (`README.md:461-463`).

### Fix

- **Wake it, then retry once.** That is the documented, observed boundary:
  manual retry is expected behaviour for the free tier's cold start, and the
  checked observation lives in
  [docs/cold-start-reliability.md](cold-start-reliability.md).
- **Read `stale.age_human`, don't assume.** The served history is as current
  as the last successful measure-and-push redeploy; freshness advances by
  redeploy, not by scheduler (`README.md:33-39`).
- **If you specifically need a fresh number, ask for a live measurement** with
  `?live=1` — that prices a full ladder against Horizon and takes tens of
  seconds (README, `?live=1` note).
- **If /healthz disagrees with your watch** — a monitoring badge flagging the
  instance "down" while `/healthz` says `ok` is the `status` vs `data.age`
  distinction, not a contradiction.

---

## 3. "The stellar.toml will not resolve"

### Symptom

A corridor's checks report an anchor step as `undetermined` — typically the
`toml-url` / `well-known` check — with `found: false` and a note about the
URL, while every other check passed.

### Diagnosis

The URL Wayfare follows is the canonical SEP-1 location constructed from the
issuer's own declared domain: `https://{domain}/.well-known/stellar.toml`
(`anchor/anchor.go:55-57`, `anchor.TOMLURL` at `anchor/anchor.go:263-266`).
A check that cannot establish a fact **is not a failure**: it reports
`determined: false` with a reason, and a reader is told the claim could not be
verified — never that it is false (`checks/checks.go:13-20`,
`checks/checks.go:430-447`). So "will not resolve" is almost always one of:

1. **The domain is wrong or HTTP-only.** SEP-1 is HTTPS and the well-known
   path is exact — a `www.` vs bare domain, a trailing-slash, or
   `stellar.toml` served at the domain root instead of `.well-known/` leaves
   the file unreachable at the canonical URL even though the domain loads in a
   browser.
2. **The file rejects HTTP checks.** A redirect from
   `https://{domain}/.well-known/stellar.toml` to a differently-cased path or a
   port the probing server does not follow yields `undetermined`, and resolved
   addresses on private/loopback ranges are refused by design
   (`checks/transport.go:44-119`).
3. **The issuer declared a domain that serves nothing.** That is an issuer
   problem, correctly surfaced as `undetermined` rather than papered over.

### Fix

- **Check the canonical URL yourself, with the exact path:**
  `curl -I https://{domain}/.well-known/stellar.toml`. Note: SEP-1 also
  requires the `CURRENCIES` array for an asset, and Wayfare reads `currency`
  fields from the *registered* entry it was verified against, not just from
  whatever the TOML now says (`asset/known.go:161-198`).
- **Confirm the case is 1 by toggling the declared domain.** Fixing the
  issuer's `domain` in `asset/known.go` (or the issuer's redirect) is the
  fix; a check can only follow what the registry records
  (`asset/known.go:371-378`, `server/api.go:139-148`).
- **If the domain is correct and reachable, the file is genuinely missing on
  the issuer's side** — that is a finding about the issuer, and the correct
  outcome is the undetermined check, so the corridor keeps its other checks
  (SEP-10, SEP-24) instead of being exempted.

---

## 4. "My chain will not verify"

### Symptom

`go run ./cmd/wayfared -verify-store -data ./data` prints a `FAIL` line
naming a corridor, or exits `1` after `N of M chains failed verification` —
or `-verify-store` reports no history at all where your data holding was
expected to have it.

### Diagnosis

`-verify-store` walks every corridor's hash chain and proves no record was
edited after it was written; it is run deliberately after a deploy and after a
restore from backup (`cmd/wayfared/main.go:239-273`). Each record's `hash` is
the SHA-256 of the record with the hash field omitted, and the chain fails the
moment one link disagrees (`docs/verify-store.md`, `docs/run-store.md`). In
this repository the chain is verified *before* committing, never after:
`.github/workflows/measure.yml` runs the measure sweep, then runs
`-verify-store` and only commits when every chain passes again
(`.github/workflows/measure.yml:46-56`). So a failed verify in CI means the
data commit was edited between sweep and verify; a failed verify in a working
tree means the data *file* was edited after it was recorded.

The usual causes:

1. **The data dir was hand-edited** (a `recorded_at`, a `hash`, a `prev_hash`,
   whitespace) — the cheapest and most common cause.
2. **The wrong commit is being verified.** The chain verifies against the
   bytes it was recorded under; a record written by a different revision can
   fail if the record's `git_revision` no longer matches the tree's, and a
   snapshot replayer additionally refuses a record version it does not know
   (`docs/snapshot-record-replay.md`, `cmd/ladder/main.go:346-370`).
3. **`data/` was copied across machines mid-record** — a record appended on
   one host and re-ordered on another breaks `prev_hash` linkage
   (`docs/run-store.md`).

### Fix

- **Re-clone the data dir from the verified commit.** `data/` is committed
  history; the tree's own `data/` verifies clean by construction
  (CI checks it before merge). If your *working* `data/` fails, restore it
  from the commit — the byte drift is what failed, not the record.
- **Verify against the commit the record ran on**: check the record's
  `git_revision` field against `git log` before blaming the store
  (`cmd/ladder/main.go:328-370` records the revision and refuses a dirty tree,
  so a mismatch means the bytes were written by an older checkout).
- **If CI now fails on a previously-green chain**, a human edited the data
  files on the way in — the measure workflow's pre-commit verify guard
  (`measure.yml` §"Before committing, not after") is doing exactly its job.

---

## If your symptom is not in this list

- `make test` failing offline is environmental: check
  [contributor-faq.md](contributor-faq.md) and [offline-testing.md](offline-testing.md)
  — the suite provably touches no network, so a "network needed" failure is
  the signal to check your Go version.
- "It says UNUSABLE everywhere": that is the product, not a fault — a corridor
  with no recommendable size says so by design
  ([README](https://github.com/Wayfare-labs/wayfare) "why a monitor and not a
  router"; [non-goals.md](non-goals.md) §6).
- Anything else: file an issue with the endpoint, the `stale.age_human`
  reading, and the exact line from the log — [backlog.md](backlog.md) is where
  evidence-based gaps are tracked.

## Related

- [cold-start-reliability.md](cold-start-reliability.md) — the sleeping-instance
  observation and the manual-retry boundary (§2)
- [verify-store.md](verify-store.md), [run-store.md](run-store.md) — chain
  invariants and the pre-commit verify guard (§4)
- [embedded-history.md](embedded-history.md) — how the deployed instance
  serves history and why `stale` is a normal state (§2)
- [monitor.md](monitor.md) — the six-hour cadence and provider budgets (§1)
- [non-goals.md](non-goals.md) — why "not determined" never becomes a false
  (§3)