# ADR 004: Why a monitor and not a router

**Status:** Accepted

## Context

The project began as a router — find the cheapest path, rank the results. The
assumption was that on-chain DEX routes could be compared and the best one
surfaced to users. Live mainnet measurement killed that thesis.

Sending 100 USDC to NGNC returns roughly 62,900 NGNC against a USD/NGN mid
near 1,364. The best available route delivers about 46% of fair value. A tool
that displayed *"best route: 62,900 NGNC"* would be accurate, useful-looking,
and would have cost the sender more than half of what they sent.

## Decision

Wayfare is a **monitor**, not a router.

A ranking carries a hidden assumption — that its winner is worth taking — and
on the corridors measured so far that assumption is false at every size
tested. Every quote is scored against an independent reference rate, and a
corridor whose best option is unusable **says so** instead of presenting a
winner.

The reference rate is therefore a **required dependency**, not an optional
enrichment. Without it the engine can rank, but it cannot tell a good deal
from a disaster.

## Consequences

- The tool tells users when **not** to send, which is more valuable than
  telling them where to send on a broken corridor.
- Every published figure is traceable to an independent benchmark, making
  claims verifiable.
- The recommendation rule ("when no size produces a verdict of POOR or
  better, recommend nothing") is the product thesis, not an implementation
  detail.
- Wayfare never moves money. It analyses corridors. Settlement is explicitly
  out of scope.

## Evidence

- `README.md` — "Why a monitor and not a router" section
- `route/route.go` — the recommendation rule
- `docs/corridor-measurements.md` — the measurements that proved the thesis
