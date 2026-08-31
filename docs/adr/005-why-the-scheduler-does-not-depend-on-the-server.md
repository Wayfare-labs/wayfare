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
monitor → route, refrate, dex, asset, checks, runstore
server  → route, runstore, asset, checks
```

Neither imports the other. They communicate only through the hash-chained
history on disk.

## Consequences

- The monitor runs regardless of whether anyone is viewing the UI.
- A server failure does not stop measurements.
- A measurement failure does not crash the server.
- The history records every scheduled sweep, not just the ones that happened
  to coincide with a page view.
- The two can be deployed, scaled, and failed independently.

## Evidence

- `monitor/monitor.go` — the scheduler, imports nothing from `server`
- `server/api.go` — the server, imports nothing from `monitor`
- `README.md` — "The scheduler does not depend on the server" section
