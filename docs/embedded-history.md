# Embedded history

Why the deployed instance's data is only as fresh as the last deploy — and how
to tell how old it actually is.

The public deployment ([Render](https://render.com), `render.yaml`) runs with
`-schedule=0 -history-first` and **no `WAYFARE_DATA_DIR`**. It never measures on
a schedule, and its history is whatever was committed to `data/` when its image
was built. This page records how that works and what it implies for readers and
operators.

---

## The mechanism

Three files cooperate to make the binary self-contained:

- **`embed.go`** — `//go:embed all:data` bakes the committed run store into the
  binary at build time. The embedded copy is what a deployment serves when no
  writable data directory is configured.
- **`Dockerfile`** — `COPY . .` then `go build` with `CGO_ENABLED=0`. `data/`
  is in the build context (`.dockerignore` excludes `testdata/`, docs and git
  metadata, not `data/`), so the image embeds exactly what was committed when
  it was built. The image's default command is
  `-addr 0.0.0.0:8080 -schedule=0 -history-first`.
- **`cmd/wayfared/main.go`** — with an empty `-data` flag,
  [`openStore`](../cmd/wayfared/main.go) opens `runstore.OpenFS` over the
  embedded `data/` instead of a writable directory. That store is read-only:
  `Append` always fails, and a live measurement taken on the instance is
  reported and then discarded, never silently appearing to be recorded.

The chain is **verified at load**. `runstore.OpenFS` walks and verifies every
corridor's chain before the store is returned, so a deployment refuses to serve
a chain that does not verify — and `embed_test.go` (`TestEmbeddedHistoryVerifies`)
asserts in CI that the embedded history loads and verifies.

## What the deployed instance does and does not do

- **Serves the embedded chain.** With `-history-first`, a request answers from
  the stored run unless the caller asks for `?live=1`. Every response carries
  `live: false` and a `stale` block with the reading's age.
- **Does not measure on a schedule.** `-schedule=0` disables the scheduler. The
  binary says so at startup: *"scheduler disabled; history will only grow if
  another instance is writing."* The instance is a reader, not a clock.
- **Can measure on demand.** `?live=1` prices a full ladder against Horizon and
  takes tens of seconds — but on the embedded read-only store the result is
  served and discarded, not recorded.

## Freshness depends on redeploys, not on the scheduler

The clock is `.github/workflows/measure.yml`: every six hours it runs
`wayfared -once -data ./data`, verifies the chain, and commits new records back
to `data/` in the repository. That is where new measurements go.

They reach the deployed instance **only when a new image is built and deployed**
from a repository whose `data/` contains them. Between redeploys the instance
serves the history embedded in the image it is running, however old that is.

Consequences:

- A successful sweep does not freshen the deployed data. A deploy does.
- Two instances built from the same commit serve identical history; freshness
  is a property of the image, not of the running process.
- The scheduler's cadence and the served data's age are unrelated on this
  deployment. Read `stale.age_human` rather than assuming six hours.

## How to tell how old the served history is

Every `/api/corridor` response carries its provenance:

```json
{ "live": false,
  "stale": { "recorded_at": "2026-08-22T12:09:59Z",
              "age_seconds": 437154,
              "age_human": "5d ago" } }
```

(Values as served 2026-08-27T13:35Z: 5 days 1 hour 25 minutes after the
record.)

- `live: false` — this is a stored reading, not a current measurement.
- `stale.age_human` / `stale.age_seconds` — how long ago the reading was taken.
- `/api/corridor/trend` serves the same stored runs, oldest first, so the age
  of the whole series is visible there too.

To get a figure measured now, ask for `?live=1`.

## Verification

- The embedded chain is verified when the store opens at startup, and again by
  `TestEmbeddedHistoryVerifies` in CI on every build — a tampered or truncated
  record refuses to load rather than being served.
- To check the committed chain locally, mirroring what the next image will
  embed: `go run ./cmd/wayfared -verify-store -data ./data`.

As checked 2026-08-27 against the tree, the embedded `data/` holds one run
per corridor (USDC-NGNC recorded 2026-08-22T12:09:59Z, USDC-GHSC recorded
2026-08-22T12:10:05Z, USDC-KESC recorded 2026-08-22T12:10:09Z). The example
above is the USDC-NGNC record as it would be served that day. The measure
workflow's push failure ([#63](https://github.com/Wayfare-labs/wayfare/issues/63))
means the served history has not advanced since then — see the README's
"Try Wayfare" section for the live status.

## Related

- [deployment.md](deployment.md) — running it continuously, costs, backups
- [run-store.md](run-store.md) — the chain, and what verification proves
- [README.md](../README.md) — what the deployed instance serves, user-facing
