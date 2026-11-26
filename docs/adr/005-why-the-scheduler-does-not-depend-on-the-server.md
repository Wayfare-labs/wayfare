# ADR 005: Why the scheduler does not depend on the server

**Status:** Accepted

## Context

Wayfare has two main runtime components: a scheduler that measures corridors
on a fixed interval, and an HTTP server that serves those measurements. A
natural coupling would be to have the scheduler depend on the server — to
measure when a request arrives, or to write results through the server's
handler.

This would create a failure mode where the monitor only runs while somebody
is watching. A page that nobody has open would produce no measurements, and
the history would have gaps exactly where nobody was looking.

## Decision

`monitor` imports nothing from `server`. The scheduler runs independently:
`wayfared -serve=false` measures with no HTTP at all. The server reads from
`runstore`, which is the shared boundary between the two.

The dependency graph is:

```
.                       →
analysis                → runstore
anchor                  → asset
api                     →
asset                   →
automerge               →
buildinfo               →
checks                  → anchor, asset, dex, refrate
dex                     → asset, transport
failover                → transport
internal/tools/newcheck →
measure                 →
metrics                 →
monitor                 → asset, checks, failover, metrics, route, runstore, snapshot
refrate                 → transport
route                   → asset, checks, dex, refrate, sep38
runstore                → asset, checks, route, snapshot
sep38                   → asset
server                  → analysis, asset, buildinfo, checks, failover, metrics, refrate, route, runstore, transport
snapshot                →
transport               →
```

`.` is the repository root package. `cmd/*` and `examples/*` are leaves: they
may import from the graph above, and nothing in it may import them.

Neither `monitor` nor `server` imports the other. Measurements pass between
them only through the hash-chained history on disk. When both run in one
process they also share a `*metrics.Collector`: the scheduler writes sweep
counters into it and the server serves them at `/metrics`.

## Consequences

- The monitor runs regardless of whether anyone is viewing the UI.
- A server failure does not stop measurements.
- A measurement failure does not crash the server.
- The history records every scheduled sweep, not just the ones that happened
  to coincide with a page view.
- The two can be deployed, scaled, and failed independently.

## Enforcement

The graph above is not documentation that happens to be true. It is the
allow-list `TestPackageImportGraph` in `importgraph_test.go` reads when the
tests run: an import that is not a row here fails the test, and a row here that the
code does not use fails it too. Adding an edge is a change to this ADR and to
the code in the same commit, which is the point — the rule is enforced, not
remembered.

`cmd/*` and `examples/*` are checked as leaves by the same test.

## Evidence

- `monitor/monitor.go` — the scheduler, imports nothing from `server`
- `server/api.go` — the server, imports nothing from `monitor`
- `importgraph_test.go` — the test that enforces this graph
- `README.md` — "The scheduler does not depend on the server" section

| `client` | `github.com/shopspring/decimal` |
