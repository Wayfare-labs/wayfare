# When a live measurement fails locally

Both binaries measure against live upstreams. There is no offline mode and no
cached figure to fall back on — that is a design decision, not a missing
feature ([CONTRIBUTING.md](../CONTRIBUTING.md#invariants): *never display a
rate that did not come from a live source*). The first time a measurement fails
it is easy to read the failure as a broken checkout. This document says how to
tell which upstream refused, and what to do next.

**Checked against the code at commit `c9bfb75`, 2026-09-24.** Every error string
below is quoted from the code that produces it, with the file named, rather than
paraphrased.

---

## Why there is nothing to fall back on

A measurement needs two independent things:

1. **Horizon**, for `/paths/strict-send` — the on-chain price. `dex` delegates
   to the same pathfinder that will execute the payment, because an order-book
   walk would both misprice the route and miss AMM liquidity
   (`dex/dex.go`, package doc).
2. **A reference mid**, for the comparison that produces a verdict
   (`route/route.go`). Without it the engine can rank routes but cannot tell a
   good deal from a disaster, so it treats a missing reference as a hard
   failure rather than degrading to "no spread shown".

`cmd/ladder` uses one provider chosen by `-ref`; `cmd/wayfared` uses two and
cross-checks them (`refrate.Cross`). None of them answers from a fixture, and a
failed fetch is never rewritten into a plausible number.

---

## The failure modes, and what each one prints

### 1. No network at all — the reference provider is unreachable

Every size prints an error and the run ends with nothing priced:

```
corridor USDC -> NGNC, benchmarked against USD/NGN
run at 2026-09-24T...Z

SEND            RECEIVE         RATE      LOSS% VERDICT    INTEGRITY   PATH
0.1       ERROR: route: reference rate unavailable: refrate: exchangerate-api unavailable: Get "https://open.er-api.com/v6/latest/USD": dial tcp: lookup open.er-api.com: no such host
...

no size could be priced for USDC -> NGNC
```

What to read from it:

- `ERROR:` on a size line comes from `Rung.Err` (`cmd/ladder/main.go`,
  `printTable`).
- `route: reference rate unavailable:` is `route.Engine.Quote` wrapping the
  provider's own error (`route/route.go`). The provider is named inside it.
- `unavailable` is `refrate.ErrUnavailable`, which covers transport failures
  (DNS, refused, timeout) and non-2xx HTTP responses
  (`refrate/refrate.go`).

A rung that errors is an absence of information, not a finding about the
corridor. `cmd/ladder` distinguishes the two: it prints the error, still exits
`1` because nothing is recommendable, and refuses to record a snapshot from a
run with holes in it.

### 2. Both reference providers are down (the server's two-provider path)

`cmd/wayfared` uses `refrate.Cross` with two providers. Both failing is an
error that names both, and the `... was unavailable (...)` / `... returned an
unparseable response` phrasing comes from `refrate.classifyError`:

```
refrate: no reference rate for USD/NGN: exchangerate-api was unavailable (refrate: exchangerate-api unavailable: ...); currency-api was unavailable (refrate: currency-api unavailable: ...)
```

One provider failing is **not** a failure. The survivor is returned and labelled
`SINGLE`, with a note naming the one that did not answer — uncorroborated, and
saying so(`refrate/cross.go`, `singleSource`). The run continues.

### 3. A provider rate-limits you

A free-tier feed returning HTTP 429 is reported as itself, not flattened into a
generic outage, because the remedy is different:

```
refrate: exchangerate-api rate-limited this request; retry after 1m0s
```

Through the cache (`cmd/wayfared`), it reads:

```
refrate: exchangerate-api is rate-limited and no cached rate is within the age bound; a stale rate will not be presented as current: refrate: exchangerate-api rate-limited this request; retry after 1m0s
```

`retry after ...` is only present when the provider actually sent a
`Retry-After` header. When it did not, the message says nothing about timing
rather than inventing a backoff (`transport/retryafter.go`).

**A failed fetch is not cached** (`refrate/cached.go`). Caching a failure would
turn one transient outage into a TTL-long refusal, and a later reader could not
tell a provider that is down *now* from one that was down an hour ago. The
practical consequence is that repeated attempts hit the provider again — which
is what you want when it was transient, and what you do not want when it was a
rate limit.

### 4. `make run` can spend more requests than you expect

This one is worth knowing before you conclude a provider is broken:

- **`cmd/ladder` does not cache the reference rate.** It wires the provider
  through `refrate.Checked` with no `MaxAge` and no cache
  (`cmd/ladder/main.go`). Each priced rung calls it, and `printTable` calls it
  once more for the "reference mid" line — so one `make run` can make up to
  thirteen reference requests against a free tier.
- **`cmd/wayfared` does cache.** Its providers are wrapped in
  `refrate.Cached`, TTL one hour (`refrate.DefaultCacheTTL`), which collapses
  a burst of identical requests into one upstream call.

So: a ladder run can trip a quota that a deployed server would not, and running
`make run` in a tight loop is the likeliest way to rate-limit yourself. Waiting
out the interval in the message is the fix.

### 5. Horizon is unreachable or rate-limits, but the reference is fine

The reference resolved, so the rungs are measured — and each one learns only
that the DEX leg could not be priced. The table shows `UNKNOWN` integrity with
the reason in the last column, and the run ends:

```
no size could be priced for USDC -> NGNC
```

The note on the wire names the cause, e.g.
`DEX route unavailable: dex: querying horizon: ...` (`route/route.go`), or from
Horizon itself:

```
dex: horizon rate-limited /paths/strict-send; retry after 30s
dex: horizon returned HTTP 503 for /paths/strict-send
```

`UNKNOWN` here is exactly the state the taxonomy exists for: nothing was learned
about the corridor's structure, which is a different result from `NO-MARKET`
(where Horizon answered and said no path exists). The two are easy to conflate
because both produce zero-valued figures — the difference is the integrity
state, not the error. See [docs/glossary.md](glossary.md#integrity).

### 6. The benchmark itself is unusable

When the two providers disagree by more than 10%, `refrate` classifies the pair
as `MALFUNCTION` and Wayfare issues **no verdict at all**: no verdict derived
from a benchmark that is not measuring the same thing would be a measurement.
The run still reports what it found about the corridor's structure, and the wire
marks it `scored: false` with the reason attached. On the ladder this surfaces
as a run with no recommendation. See
[docs/glossary.md](glossary.md#reference-agreement).

### 7. It only failed at one or two sizes

A partially-failed ladder is a real measurement with a qualification, not a
break. `LadderResult` keeps every figure describing only the sizes that were
measured, and the `Finding` states the gap in prose: *"N of 12 sizes could not
be measured (...) so every figure here describes only the sizes that were."*
(`route/ladder.go`, `partialQualification`).

Read the qualification rather than the headline figure. A floor established at
three of twelve sizes is a weaker claim than the same number on a full curve.

### 8. The counterparty checks failed

They are supposed to be able to. Every check produces *determined + passed*,
*determined + failed*, or **not determined**, and the third is not a failure: an
anchor that publishes no SEP-10 endpoint is a different fact from one whose
endpoint is dead. A check that cannot establish anything carries a reason and
does not stop the measurement (`checks/runner.go`, `ForAsset`).

Checks qualify the headline and never move it. If you want to remove the extra
latency while diagnosing a measurement problem:

```bash
go run ./cmd/ladder -checks=false
```

The JSON then carries **no** findings block — "not checked", which is a
different claim from "checked, nothing found".

### 9. `-record` refused

Two distinct refusals, both deliberate
([docs/snapshot-record-replay.md](snapshot-record-replay.md)):

- A modified working tree is refused (`refusing to record a snapshot from a
  modified working tree.`), because `git_revision` would then name a tree that
  did not produce the bytes. Commit or stash, or pass `-allow-dirty` and record
  a marked-approximate fixture.
- A run with any errored rung is refused (*"not recording a snapshot: N of M
  sizes failed to reach an upstream"*), because replaying it would render the
  corridor as partly unpriced when the market was fine and the network was not.

---

## Telling the failures apart

| What you see | What refused | Is the corridor measurable? |
|:---|:---|:---|
| `refrate: <provider> unavailable: ...` on every size | the reference provider | Unknown — nothing was learned |
| `refrate: no reference rate for <pair>: ...; ...` | both reference providers | Unknown |
| `refrate: <provider> rate-limited ...` | the reference provider's quota | Unknown; retry later |
| `refrate: <provider> ... unparseable` | the provider answered with something unreadable | Unknown; the provider is broken, retrying will not help |
| `DEX route unavailable: dex: ...horizon...` | Horizon | Unknown |
| `INTEGRITY` reads `NO-MARKET` on every size, then `no size could be priced` | nothing — Horizon answered | **Yes**: a finding (`NO-MARKET`), not an outage |
| Rows priced, every one graded `UNUSABLE`, exit 1 | nothing | **Yes**: the corridor is broken, by measurement |

The distinction that matters most is the last two rows. `NO-MARKET` means the
request succeeded and Horizon answered that no path exists — the absence of a
price. A request that never landed means nothing was learned. Both produce
identical zero-valued figures, so reading the error rather than the numbers is
the only way to tell them apart.

### Reachability, checked by hand

The errors name their upstream, so the quickest next step is to ask that
upstream directly. These are the same hosts the code fetches (`refrate`
`DefaultExchangeRateAPI`, `DefaultCurrencyAPI`; `dex.DefaultHorizonURL`):

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://open.er-api.com/v6/latest/USD
curl -sS -o /dev/null -w '%{http_code}\n' https://latest.currency-api.pages.dev/v1/currencies/usd.json
curl -sS -o /dev/null -w '%{http_code}\n' https://horizon.stellar.org/
```

`000` means the connection never happened (DNS or routing). `200` on a provider
that the measurement reported as unavailable points at a transient failure, not
a permanent one — re-run. `429` is the quota case in section 3.

`cmd/ladder` also lets you isolate the reference from the DEX without a
separate tool: if the error is `route: reference rate unavailable`, the DEX was
never reached (`Engine.Quote` returns before pricing); if the table priced rows
and named a `DEX route unavailable` note, the reference worked and Horizon did
not.

---

## Exit codes

`cmd/ladder` carries the answer in its exit code so a script never has to parse
prose (`cmd/ladder/main.go`):

| Code | Meaning |
|:---:|:---|
| `0` | A size was recommendable — `result.Viable()` is true. |
| `1` | Ran, and no size was recommendable. **This is the expected result on the default corridor** ([docs/corridor-measurements.md](corridor-measurements.md)), and it is also what a network failure ends in. |
| `1` | A measurement that could not run at all — e.g. `measuring corridor: context deadline exceeded`. `cmd/ladder` bounds the whole run at five minutes. |
| `2` | Bad input: `unknown destination`, no valid `-sizes`, or `-record` refusing. Nothing was measured. |

Because `1` covers both "the corridor is broken" and "the network was", the
**error text is the discriminator, not the exit code**. A run with `ERROR:` or
`Could not price` lines failed to measure; a run with a full price table and no
recommendation measured a broken corridor.

`go run` reports the program's status on stderr (`exit status 1`); `make run`
propagates it.

### The deployed instance

Same failure, different surface. `cmd/wayfared` bounds each measurement with
`-timeout` (default 90s) and answers:

| Condition | Response |
|:---|:---|
| Live measurement succeeds | `200`, `live: true` |
| Live measurement fails, history exists | `200` with the newest stored run, `live: false` and a `stale` block carrying its age |
| Live measurement fails, no history | `502` `measurement_failed`, or `504` `upstream_timeout` when the cause was the deadline |
| Every request fails to reach an upstream | Treated as the failure it is, not as a corridor that priced at nothing (`server/api.go`) |

Nothing is ever synthesised to fill the gap, so read `live` before reading any
figure. The public instance also sleeps when idle and its served history is
older than its cadence implies; both are documented in
[docs/cold-start-reliability.md](cold-start-reliability.md) and
[docs/embedded-history.md](embedded-history.md).

---

## Checklist

1. Re-run once. A large share of these are transient.
2. Read the error text and name the upstream from it — `exchangerate-api`,
   `currency-api`, or `horizon`.
3. If it is a rate limit, wait out the interval in the message. Do not loop.
4. If it is `unavailable`, check that host with `curl` above.
5. If the reference resolved and Horizon did not, the corridor was not measured;
   the run is inconclusive, not negative. Do not report it as a finding about
   the corridor.
6. If rows priced and none is recommendable, that **is** a result. Publish it,
   with the timestamp, as [CONTRIBUTING.md](../CONTRIBUTING.md#reporting-a-corridor)
   describes.
7. If `make test` fails but `make run` is the thing you were changing, the two
   are unrelated: the suite never reaches the network. See
   [docs/offline-testing.md](offline-testing.md).

---

## Scope boundary

This document describes the failure behaviour of the live measurement paths as
checked against the repository on 2026-09-24. It changes no verdict threshold,
no integrity semantic, no check composition rule and no run-record field. Where
it states something the repository does not currently do — a cache in
`cmd/ladder`, a fallback figure, a retry — it says so rather than describing it
as available.

---

## Related documents

- [docs/development-loop.md](development-loop.md) — the Makefile loop, and why
  `make run` exits 1 on the default corridor
- [docs/offline-testing.md](offline-testing.md) — why the test suite never has
  this problem, and how CI proves it
- [docs/glossary.md](glossary.md) — every state a reader can meet: `NO-MARKET`
  against `UNKNOWN`, the agreement bands, and what *not determined* means
- [docs/embedded-history.md](embedded-history.md) — how the deployed instance
  serves stored runs when a live measurement fails
- [docs/verify-store.md](verify-store.md) — the other way a run can look wrong:
  a chain that will not verify
- [docs/backlog.md](backlog.md) — entry #169 (issue
  [#229](https://github.com/Wayfare-labs/wayfare/issues/229)) is this document
