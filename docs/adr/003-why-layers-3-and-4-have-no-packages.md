# ADR 003: Why layers 3 and 4 have no packages

**Status:** Accepted

## Context

Wayfare's architecture is organised by epistemic status into four layers:

1. **Observable facts** (Horizon pathfinding, order books, issuer flags)
2. **Deterministic calculation** (effective rate, loss vs mid, integrity)
3. **Probabilistic intelligence** (failure probability, expected slippage)
4. **Verifiable output** (signed or on-chain corridor attestation)

Layers 1 and 2 are implemented. Layers 3 and 4 are not, and they have no
packages, no stubs, and no interfaces.

The temptation is to create empty packages or interface stubs for layers 3
and 4 — to "reserve the space" and make the architecture feel complete. This
is wrong.

## Decision

Layers 3 and 4 have **no packages and no stubs**, deliberately.

> "Speculative structure is worse than none: an empty package invites code
> that has no inputs yet."

An empty package is not a plan — it is a liability. It creates the
expectation of an interface that has not been designed, a contract that has
not been specified, and code that will be written to satisfy the stub rather
than the actual need. When the real inputs eventually arrive (months of
`runstore` history for layer 3, a trust model for layer 4), the stub will
almost certainly be wrong and will need to be deleted anyway.

The absence is itself a signal: it says "this capability does not exist yet"
more honestly than an empty package that says "this capability is here but
just empty."

## Consequences

- The codebase does not carry dead structure that misleads contributors.
- New packages are created only when real inputs exist to feed them.
- The roadmap clearly shows what is built, what is planned, and what does
  not exist yet — without stubs blurring the boundary.
- When layer 3 or 4 work begins, it starts from a clean design rather than
  from constraints imposed by a premature interface.

## Evidence

- `README.md` — Architecture section
- `docs/backlog.md` — "What does not exist" section
