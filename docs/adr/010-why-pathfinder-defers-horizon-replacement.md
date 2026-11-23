# ADR 010: Keep Horizon behind a Pathfinder seam until a trigger appears

**Status:** Accepted

## Context

Horizon provides Stellar pathfinding, which Stellar RPC does not. Replacing
Horizon therefore means rebuilding the pathfinding behavior Wayfare measures,
including the full path set and AMM routes. The evidence, alternatives, costs,
and current implementation are documented in the
[Horizon-to-RPC migration research](../horizon-rpc-migration.md).

## Decision

Use Option E: keep Horizon as the active pathfinding provider and depend on the
`route.Pathfinder` interface rather than the concrete Horizon client. Do not
build a replacement without a concrete reason to migrate.

Reopen this decision if:

1. SDF announces a Horizon retirement date.

Watch Horizon's protocol-compatibility updates as the leading indicator for a
retirement announcement. If updates stop, re-check the facts and the retirement
risk; do not treat that signal alone as an automatic migration decision.

When the retirement trigger fires, choose an implementation that preserves the
full path set, including AMM routes. A third-party Horizon-compatible provider
is an acceptable short-term stopgap; it is not, by itself, the long-term
migration.

## Consequences

- The existing `Pathfinder` seam keeps `route` swappable without paying the
  cost of a second pathfinding implementation now.
- Horizon remains a runtime dependency, and the project must watch the two
  trigger signals above.
- Any eventual replacement must satisfy the same route-coverage and measurement
  requirements; an RPC endpoint without pathfinding is not a drop-in
  replacement.

## Related

- [Horizon-to-RPC migration research](../horizon-rpc-migration.md)
- [`route.Pathfinder`](../../route/pathfinder.go)
