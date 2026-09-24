# Contributor FAQ

Questions a new contributor actually asks, answered against the code as it is in
this repository. Every claim below names the file it came from so it can be
checked rather than trusted. Where the answer is "no" or "not yet", it says so.

This is the written form of the seeded Q&A discussion. If your question is not
here, ask in
[Discussions → Q&A](https://github.com/Wayfare-labs/wayfare/discussions/categories/q-a)
— that is the fastest route to an answer, and a question that keeps coming up
should be folded back into this document.

> **Where things stand, as of 2026-09-23.** The V1 quote engine is implemented
> and tested. The V2 execution-economics layer is **partly merged but not
> reachable** (see "What is not built yet"). Nothing in V3–V6 is implemented. The
> repository's own [docs/backlog.md](backlog.md) is the map; it is larger and more
> current than this FAQ.

---

## Getting started

### What is Wayfare, in one sentence?

A corridor-integrity monitor for Stellar: it prices a stablecoin → fiat-token
corridor across trade sizes, scores every route against an independent
mid-market rate, and says plainly when none of them are worth taking
(`README.md`). Why that is the shape of the product rather than a ranking:
[docs/why-wayfare.md](why-wayfare.md).

### What do I need installed?

Go 1.22 or later, and nothing else for the test suite. `go.mod` pins
`go 1.22.2`; CI uses Go 1.22 (`.github/workflows/ci.yml`, `GO_VERSION`); the
image builds on `golang:1.22-alpine` (`Dockerfile`).

`golangci-lint` is needed only for `make lint` and is installed separately
(`Makefile`).

### How do I build and test it?

```bash
make test     # go test ./...
make race     # go test -race ./...
make vet      # go vet ./...
make fmt      # gofmt
make build    # build every binary into bin/
make run      # measure USDC -> NGNC against live mainnet
```

`make all` is `fmt vet test build`. CI additionally runs `gofmt -l`, `go vet`,
`go test -race`, `go build`, `golangci-lint` v2.1.6, a container build, and a
network-isolated test run. Each target's requirements, `make run`'s exit code,
and which check you cannot reproduce locally: [docs/development-loop.md](development-loop.md).

If a live measurement fails instead of a test — `make run` needs the network by
design — see [docs/live-measurement-failures.md](live-measurement-failures.md).

### Why does `make test` work with no network?

Because every test that would reach out replays recorded bytes instead.
`snapshot.Replayer` returns `ErrNotRecorded` for a request that was not
recorded, so a test that tries to go live fails loudly rather than passing
intermittently (`snapshot/replay.go`). CI proves this structurally: the
`offline-tests` job runs the suite inside a network namespace with no route out
(`.github/workflows/ci.yml`).

You can run the same thing locally with `make offline-test`, which uses
`unshare -rn`.

### Where do I look first in the code?

The flow is `refrate` → `route` → `checks` → `runstore` → `server`. The
architecture snapshot with the real package names and every arrow is section A
of [docs/backlog.md](backlog.md).

---

## Contributing

### How do I pick something to work on?

Browse issues by label. The label taxonomy is live in the repository
(`gh label list`) and means what [CONTRIBUTING.md](../CONTRIBUTING.md) says:

- **`good first issue`** — well-scoped, a few hours.
- **`difficulty:easy` / `medium` / `hard`** — honest sizing, with `hard` meaning
  "discuss before building".
- **`blocked`** — do not start; it waits on something that does not exist yet.
- **`needs-maintainer-review`** — the design is not settled and a PR may be
  rejected on approach rather than execution. Agree the shape first.
- **`area:*`** — `pricing`, `corridor`, `ui`, `tests`, `docs`, `data`,
  `design`, `devops`, `research`, `ecosystem`.

### Which parts are maintainer-owned?

Per [CONTRIBUTING.md](../CONTRIBUTING.md#maintainer-owned-areas): `dex` pricing
arithmetic, the verdict thresholds, the integrity taxonomy, SEP-38 fee handling,
the check engine and how results compose, and the corridor health score when it
exists. The reason is blast radius — an error in any of these invalidates
published measurements rather than breaking a feature. Individual checks and
metrics are exactly the contribution the project wants. Per-area detail:
[docs/maintainer-owned-areas.md](maintainer-owned-areas.md).

### What will get my PR rejected regardless of quality?

The invariants in [CONTRIBUTING.md](../CONTRIBUTING.md#invariants): moving
money or holding keys; recommending a route when every route is unusable;
displaying a rate that did not come from a live source; `float64` in any pricing
path (use `decimal.Decimal`); and filling in a SEP-38 rate for an anchor that
does not publish `ANCHOR_QUOTE_SERVER`.

### How do I add a corridor?

[docs/adding-a-corridor.md](adding-a-corridor.md). The rule throughout is **one
corridor at a time, and a negative finding is a result** — "this corridor cannot
be measured, and here is why" is an acceptable outcome. Five corridor research
issues exist as worked examples (#58 ZAR, #59 BRL, #60 PHP, #61 MXN, #62 INR),
and [docs/corridor-research-template.md](corridor-research-template.md) is the
form to fill in.

### How do I add a check?

[docs/checks.md](checks.md). A check reports a counterparty fact and **never
moves the headline**: integrity and every verdict are computed from pathfinding
and the reference rate alone. `route.WithFindings` is the only composition point
and branches on nothing.

### How do I add a metric?

See [docs/metrics.md](metrics.md). Be aware: as of this writing **no metric
runs in production**. `checks.Runner` has no `Metrics` field and `RunMetric` has
no non-test caller, so the four merged metrics are unreachable. Wiring them is
[#91](https://github.com/Wayfare-labs/wayfare/issues/91) and it is the critical
path for the whole V2 milestone.

### Do I need to change the wire format to add things?

Check [docs/api.md](api.md) and `route/wire.go` first. Money crosses the wire as
decimal strings, never JSON numbers (ADR
[006](adr/006-why-money-crosses-the-wire-as-decimal-strings.md)). There is a test
that walks every money field on every producer path — extend it rather than
working around it.

---

## Measurements

### Why does the deployed instance show old data?

Because the image embeds `data/` at build time and runs with
`-schedule=0 -history-first`, so freshness advances by redeploy, not by the
scheduler ([docs/embedded-history.md](embedded-history.md)). New records are
written by `.github/workflows/measure.yml`, which runs every six hours, verifies
the chain, and opens a pull request.

**Observed 2026-09-23T17:25:28Z:** `GET /healthz` on
`https://wayfare-cdb9.onrender.com` reported every corridor's newest record as
`recorded_at: 2026-08-22T12:09:59Z`, `age_human: "32d ago"`. A six-hourly
workflow and a 32-day-old reading do not agree, so the chain has not been
extending. `README.md` flags this in its verification table ("Continuous
measurement — **Not currently running**", [#63](https://github.com/Wayfare-labs/wayfare/issues/63))
and it is visible at runtime as `stale.age_human`. Read that rather than
assuming a cadence.

### Why does the API say `live: false`?

Because it served a stored reading. Every response carries `live` and, when
false, a `stale` block with the age. Ask for a live measurement with `?live=1`
— it prices a full ladder and takes seconds, not milliseconds. Nothing is ever
synthesised to fill a gap (`server/api.go`, `staleJSON`).

### How do I report a measurement, or a bug in one?

Open an issue with the send and receive assets with issuer accounts, the
reference pair and source, raw `cmd/ladder` output **with its timestamp**, and
the issuer's `stellar.toml` status ([CONTRIBUTING.md](../CONTRIBUTING.md#reporting-a-corridor)).
For a UI or API bug, include the exact request, the response, and the time —
that is what makes it reproducible.

### Can I reproduce a recorded measurement?

Yes, and it is the point. `./data/*.ndjson` is a hash-chained store; verify it
with:

```bash
go run ./cmd/wayfared -verify-store -data ./data
```

`-verify-store` names the first record that does not reconcile rather than
printing a boolean. See [docs/verify-store.md](verify-store.md) and
[docs/snapshot-record-replay.md](snapshot-record-replay.md).

### How do I know the reference rate is trustworthy?

It is not assumed to be. Two independent providers are fetched and
cross-checked, and the agreement band (AGREE / DISAGREE / STALE / MALFUNCTION /
SINGLE) is published alongside the mid. When they disagree beyond the thresholds
no verdict is issued at all. The mids are **never averaged** — a blended mid
names no provider (ADR [001](adr/001-why-reference-mids-are-never-averaged.md),
`refrate/cross.go`).

---

## What is not built yet

Answering these as "future" is deliberate: do not describe any of them as
available.

- **Execution metrics (spread, depth, price impact, concentration).** Code
  merged, **unreachable**. See "How do I add a metric?" above.
- **Historical analysis over the store** (means, trends, regimes). Not
  implemented. `runstore` holds records, but no analysis layer reads them and
  the records carry headline figures only.
- **Predictive intelligence** (failure probability, expected slippage).
  Not implemented, and deliberately so: it is blocked on history that does not
  yet exist.
- **Verifiable intelligence** (signed or on-chain attestations). Not
  implemented. Research spikes only.
- **A SEP-38 rate for any corridor Wayfare measures.** No corridor here has an
  anchor that publishes `ANCHOR_QUOTE_SERVER`, so no corridor's own rails can be
  priced. (A live SEP-38 round-trip *has* been performed against the
  `testanchor.stellar.org` sandbox: the response is recorded verbatim in
  `sep38/testdata/live/price-usd-srt.json`, 2026-08-28, and replayed by
  `sep38/live_roundtrip_test.go`. That is a sandbox anchor, not a corridor.)

`README.md` states the governing rule for all of this: a layer can never be
more certain than the layer beneath it, and an unavailable layer-1 fact is
*unknown*, never a default.

### The backlog says something that contradicts the code. What do I do?

Report it — the contradiction is the finding. Two known examples, both verified
against this tree on 2026-09-23:

- Section A of [docs/backlog.md](backlog.md) says `checks.Runner.ForAsset()` runs
  **three** checks. `checks/runner.go` `Default()` returns **seven**
  (`AnchorAssetISO4217`, `SEP10EndpointResponds`, `SEP24InfoListsAsset`,
  `SEP38QuoteServerPublished`, `IssuerAuthFlags`, `IssuerFlagImmutability`,
  `HomeDomainRoundTrip`).
- The same section says `route.Decompose` is unreachable dead code with no
  non-test caller. `route/ladder.go` calls it per priced rung and
  `route/wire.go` publishes the result as a `cost` block.
- Issue [#180](https://github.com/Wayfare-labs/wayfare/issues/180) says a live
  SEP-38 round-trip "has never been performed". One was performed against
  `testanchor.stellar.org` on 2026-08-28 and is committed as
  `sep38/testdata/live/price-usd-srt.json`.

The comment above `Runner.Default()` — "deliberately small … these three are the
worked examples" — is stale for the same reason.

---

## Etiquette

### Where do I ask questions?

[Discussions → Q&A](https://github.com/Wayfare-labs/wayfare/discussions/categories/q-a)
for questions, [Issues](https://github.com/Wayfare-labs/wayfare/issues) for
defects and scoped work. A question that gets asked twice belongs in this
document; a PR that answers one is welcome.

### How should I describe a change?

Explain what changed and why. If it touches pricing, say how you verified
correctness, and if you measured something live, include the raw figures and the
timestamp ([CONTRIBUTING.md](../CONTRIBUTING.md#before-opening-a-pull-request)).

### What if my measurement is unflattering to the project's own thesis?

Publish it unflattering. That is a documented rule, not a suggestion
([CONTRIBUTING.md](../CONTRIBUTING.md#measurement-discipline)).
