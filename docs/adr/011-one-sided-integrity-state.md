# ADR 011: ONE-SIDED Corridor Integrity State

**Status:** Approved

## Context

Research on Stellar fiat-pegged tokens (such as NGNC) demonstrated that corridors frequently have one-sided order books: bids are entirely absent while asks exist, allowing users to buy the fiat token but preventing them from unwinding positions back to crypto on the open decentralized exchange. Previously, these were grouped under DIRECT or unrepresented, obscuring the critical reality that market liquidity runs in only one direction.

## Decision

Introduce a dedicated **ONE-SIDED** integrity state alongside DIRECT, DERIVATIVE, NO-MARKET, and UNKNOWN to explicitly designate corridors that price on entry but have no executable exit bids.

## Consequences

- Readers and automated consumers can distinguish fully two-sided executable markets (DIRECT) from asymmetric ones (ONE-SIDED).
- Integrity snapshot tests, glossary, and schema representations updated accordingly.

## Evidence

- `docs/research/stellar-fiat-tokens-twosided-book-research.md` — empirical findings on NGNC and other fiat tokens showing complete absence of exit bids while entry asks remain active.
- `snapshot/` integrity test suite verifying one-sided classification.
