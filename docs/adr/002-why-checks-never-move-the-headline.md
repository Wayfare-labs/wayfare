# ADR 002: Why checks never move the headline

**Status:** Accepted

## Context

Wayfare runs counterparty checks (e.g., does the issuer have the right auth
flags? does the SEP-10 endpoint respond?) alongside every measurement. A
natural temptation is to let a failed check downgrade a verdict — for
example, marking a corridor as worse because its issuer flags changed.

This would make the headline unfalsifiable: a reader could no longer tell
whether a corridor was downgraded because its liquidity moved (a real market
fact) or because someone added a check (an implementation choice). The
integrity state and the verdicts would become functions of which checks
happen to be installed, not of the corridor's actual behaviour.

## Decision

Checks **qualify** the headline. They **never move** it.

No check result, at any severity, may change `integrity` or a verdict. Those
are derived from pathfinding and a reference rate; letting observations about
third parties rewrite them would break the project's central epistemic rule:
a layer can never be more certain than the layer beneath it.

This is enforced in code, not just documented: `route.WithFindings` branches
on nothing — it attaches check results to the response without inspecting
them.

## Consequences

- The headline (integrity + verdicts) is always reproducible from pathfinding
  data and a reference rate, regardless of which checks are installed.
- A reader can always distinguish "the corridor's liquidity moved" from "a
  new check was added."
- Checks add context (issuer health, endpoint availability) without altering
  the measurement itself.
- The separation is enforced in code, making accidental composition
  impossible.

## Evidence

- `route/wire.go` — `WithFindings` branches on nothing
- `checks/runner.go` — check execution
- `TestFindingsDoNotMoveTheHeadline` — the test that defends this invariant
