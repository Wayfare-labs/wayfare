# Reading the API correctly

**Status: implemented, current as of 2026-09-25.** The program is
[`examples/api-consumer/main.go`](../examples/api-consumer/main.go); this
document is the reading rules it encodes and why each one exists.

The API is small, public and keyless. The difficulty is not the schema — it is
the **discipline**: a client that ignores `scored`, or renders a stored reading
as current, is using Wayfare incorrectly in a way the API cannot prevent. This
is the reference implementation of not doing that.

```bash
go run ./examples/api-consumer                            # USDC -> NGNC, from the live instance
go run ./examples/api-consumer -to GHSC                   # another corridor
go run ./examples/api-consumer -live                      # measure now instead of serving history
go run ./examples/api-consumer -base http://localhost:8080
```

---

## The four refusals, and the reasoning behind each

| Field | What it means | What this program does |
|:---|:---|:---|
| `live` / `stale` | `false` means the reading came from history because a live measurement failed or was not asked for. `stale` carries `recorded_at`, `age_seconds` and `age_human`. | Prints the reading as a **stored reading**, with its age, and never as something measured now. |
| `scored` | `false` means the two reference providers diverged past tolerance, so **no verdict may be issued** against either mid. The loss figures may still be present in the body. | Prints the agreement and the note, and renders **no loss figure and no verdict at all** — not even the ones the body carries. |
| `recommended` | `null` means **nothing is worth taking** — not an oversight, not missing data. | Says so, and does not substitute the best-scoring rung. |
| `integrity` | `NO-MARKET` is the absence of a price, not a bad one; `DERIVATIVE` means every path runs through another fiat token. | Names the state, and names the dependency for a derivative corridor. |

A fifth refusal is defensive, and it is the one this program adds beyond the
contract:

| Guard | Why |
|:---|:---|
| A `recommended` quote whose `verdict` is `UNUSABLE` is withheld | A recommendation implies its winner is worth taking. `route.Ladder` only recommends a `POOR`-or-better quote, so the API does not produce this combination today — but a client that rendered it blindly is the one publishing the claim. |

Two more rules hold throughout:

**Money is read as a string, never a number.** On the wire, amounts, rates and
percentages are decimal strings, never JSON numbers
([ADR 006](adr/006-why-money-crosses-the-wire-as-decimal-strings.md)). The
consumer declares every money field as a `string`, so a JSON number in one of
them is a **decode error** — loud, rather than a figure quietly rounded through
a `float64`. This is not an implementation detail to copy or skip: it is the
reason the wire format is what it is.

**An unpriced rung is never rendered as a zero.** A size that was not priced and
a size that priced at nothing are different facts, and only one of them has a
number. The rung's `error` is printed instead.

---

## What it prints

Captured on 2026-09-25 against the deployed instance
(`https://wayfare-cdb9.onrender.com`), which runs with `-history-first`, so an
unadorned request is answered from the embedded history:

```
$ go run ./examples/api-consumer -to NGNC
USDC (GA5Z…) -> NGNC (GASB…)
measured_at 2026-08-22T12:09:59Z
live        false — STORED READING, not a live measurement
stale       recorded 2026-08-22T12:09:59Z, 33d ago (2933894s)
integrity   DIRECT
reference   USD/NGN 1349.669672 (exchangerate-api)
scored      true (agreement AGREE)
floor       27.15% at 0.1
worst       97.52% at 5000
recommended none — no size produced a verdict of POOR or better

rungs
  0.1          27.15%  UNUSABLE  USDC -> BLND -> XLM -> NGNC
  1            27.99%  UNUSABLE  USDC -> BLND -> XLM -> NGNC
  5            31.54%  UNUSABLE  USDC -> BLND -> XLM -> NGNC
  10           33.32%  UNUSABLE  USDC -> XLM -> NGNC
  25           38.13%  UNUSABLE  USDC -> XLM -> NGNC
  50           44.77%  UNUSABLE  USDC -> XLM -> NGNC
  100          54.53%  UNUSABLE  USDC -> XLM -> NGNC
  250          70.28%  UNUSABLE  USDC -> XLM -> NGNC
  500          81.16%  UNUSABLE  USDC -> XLM -> NGNC
  1000         89.12%  UNUSABLE  USDC -> XLM -> NGNC
  2500         95.20%  UNUSABLE  USDC -> XLM -> NGNC
  5000         97.52%  UNUSABLE  USDC -> XLM -> NGNC
```

Every one of those lines is a fact about the deployed instance on that date, not
a product demonstration: the served history is older than the six-hour cadence
implies because the measure workflow cannot push
([#63](https://github.com/Wayfare-labs/wayfare/issues/63)), which is exactly
what `live: false` and the age are there to make visible. The finding — 27.15%
at the dust size, 97.52% at 5000 — is the corridor, and the recommendation is
`none` because no size clears `POOR`.

### Exit codes

| Code | Meaning |
|:---|:---|
| `0` | A recommendation was rendered |
| `2` | The response was read, and there is nothing worth taking |
| `1` | Nothing could be read: transport, HTTP or decode failure |

**`go run` reports a non-zero exit status itself**, so `go run
./examples/api-consumer` on a corridor with nothing recommendable prints
`exit status 2` and finishes with `go run`'s own exit status 1. That is the
program's contract working, not a broken checkout — the same thing
[docs/development-loop.md](development-loop.md) explains about `make run`.

---

## The reading rules in full

### 1. `live` is not a verdict; `stale` is authoritative for age

`live: false` means the reading came from the run store rather than from a
measurement taken for this request. It is present on every response and never
omitted, so its absence cannot be read as "live". Freshness must not be inferred
from the request time, the deployment time, or the absence of an error.

A client rendering a stored reading as current is making a claim the response
does not support. This is the one place a continuous monitor can quietly betray
its own data.
[Freshness and its failure modes](freshness.md) ·
[cold start, and why the first request may fail](cold-start-reliability.md).

### 2. `scored` gates every loss figure and every verdict

Two independent reference providers are queried per measurement and are
**never averaged**. When they diverge past tolerance the agreement is
`MALFUNCTION`, `scored` is `false`, and no verdict is issued — because beyond
that point the feeds are not disagreeing about the rate, they are measuring
different things, and a verdict would be an artefact of which provider was
believed.

The loss figures may still be present in the body. A client that renders them
anyway presents an artefact as a measurement.

| Agreement | State | Scored against |
|:---|:---|:---|
| ≤ 2% | `AGREE` | the primary |
| 2–10% | `DISAGREE` | the **more conservative** mid — the one producing the higher loss |
| > 10% | `MALFUNCTION` | **nothing.** No verdict is issued |
| — | `STALE` | the fresher feed, when the two describe different moments |
| — | `SINGLE` | the one that answered; uncorroborated, and says so |

### 3. `recommended: null` means "nothing is worth taking"

Not "the best of a bad set", and not "no data". The engine recommends a quote
only when some size grades `POOR` or better; below that the monitor recommends
nothing at all, because a ranking carries a hidden assumption — that its winner
is worth taking — and on a broken corridor that assumption is expensive.

On the wire, `recommended` is always present and `null` in that case, never
omitted, so a client cannot read its absence as an oversight.

### 4. `integrity` is structure, and it is not the verdict

`NO-MARKET` is the absence of a price, which is not the same claim as a priced
route graded `UNUSABLE`; `DERIVATIVE` means no path reaches the destination
without traversing another fiat-pegged token, and `depends_on` names it. A UI
that shows only a loss percentage has discarded the reason a corridor failed.
[Integrity states](glossary.md) are documented with the check contract.

### 5. Findings qualify the headline; they never move it

`findings.checks` and `findings.metrics` are observations about the
counterparties a corridor depends on. No result, at any severity, changes
`integrity` or a verdict — that is enforced in code, not by convention
([docs/checks.md](checks.md)). A consumer may render them; it must not treat
them as a grade.

Two current limits worth knowing before reading `findings`:

- **Checks run on every sweep**; the seven in `checks.Runner.Default()`.
- **Metrics run nowhere.** `findings.metrics` has no producer today: a
  contributed metric is validated and testable but not reachable from
  `/api/corridor` ([#91](https://github.com/Wayfare-labs/wayfare/issues/91)).
  A response with no `metrics` key means *none were run*, which is a different
  claim from *none were found* — the field is `omitempty` for exactly that
  reason.

### 6. An error body is not a measurement

Errors have the shape `{"error": "...", "code": "..."}`. The code is what a
program branches on (`measurement_failed`, `upstream_timeout`,
`unknown_receive_asset`, `no_fiat_peg`, `invalid_query`, …); the message is what
a human reads. Nothing is ever synthesised to fill a gap: when a live
measurement fails and no stored run exists, the request errors rather than
returning a plausible number.

---

## What this program deliberately does not do

- **No retry, no caching, no credential.** The API is public, keyless and
  read-only (CORS is `*`, so a browser consumer needs no proxy), and a retry
  policy is a decision for the caller.
- **No `live=1` by default.** A default request is answered from history in
  milliseconds; `-live` prices a full ladder against Horizon and takes tens of
  seconds.
- **It does not reconcile venues.** A metric's `venue` says which market it
  measured — `order-book` excludes AMM liquidity, `pathfinding` includes it —
  and two figures from different venues must not be reconciled by arithmetic
  ([docs/liquidity-venues.md](liquidity-venues.md)).
- **It does not turn a `trend` into a series.** `/api/corridor/trend` returns
  irregular snapshots, and a line drawn between two of them is an inference.

## Related

- [docs/api.md](api.md) — the endpoint reference, with example responses
- [examples/api-consumer](../examples/api-consumer) — the program itself, and
  its offline tests
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the invariants this document is a
  client-side restatement of
- [docs/checks.md](checks.md) — findings, and why they never move the headline
- [docs/adding-a-check.md](adding-a-check.md) — how the findings this program
  reads are written
- [docs/spike-api-consumers.md](spike-api-consumers.md) — what is known, and
  not known, about who consumes this API
