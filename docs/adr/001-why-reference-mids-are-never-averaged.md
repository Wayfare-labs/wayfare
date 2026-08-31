# ADR 001: Why reference mids are never averaged

**Status:** Accepted

## Context

Wayfare queries two reference-rate providers per measurement. A natural
temptation is to average the two mids into a blended rate. This would produce
a number that looks more "robust" but would actually destroy the ability to
verify any published figure.

Every loss percentage, every verdict, and every recommendation is scored
against a specific mid from a specific provider. A reader can check that
figure by querying the same provider at the same timestamp. An averaged mid
names no provider — it is an artefact of the implementation, not a fact about
the market — and a reader cannot reproduce it.

## Decision

Rates are **never averaged**. When two providers agree (divergence ≤ 2%), the
primary mid is used. When they disagree (2–10%), the more conservative mid
(the one producing the higher loss) is used. When they malfunction (> 10%),
no verdict is issued at all.

The `scored_against` field in every response names the exact mid that
produced the verdicts. A reader can always trace a published number back to
its source.

## Consequences

- Every figure is independently reproducible from recorded bytes and a
  checkable source.
- A reader never has to wonder "which provider's rate produced this number?"
- The conservative selection in the DISAGREE range means published losses are
  never understated.
- Beyond 10% divergence, the system correctly refuses to issue a verdict
  rather than publishing an artefact.

## Evidence

- `refrate/cross.go` — the cross-check implementation
- `README.md` — "Reference agreement" section
