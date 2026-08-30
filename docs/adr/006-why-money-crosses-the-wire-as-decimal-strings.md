# ADR 006: Why money crosses the wire as decimal strings

**Status:** Accepted

## Context

Wayfare publishes amounts, rates, and percentages in its API responses. A
common approach is to use JSON numbers, which most languages parse into
IEEE 754 double-precision floats (`float64` in Go, `number` in JavaScript).

This reintroduces the rounding error the engine avoids internally. A
`float64` has 15–17 significant decimal digits; a Stellar amount like
`65100.1379550` rounds silently, and the rounding is non-deterministic across
platforms. A `decimal.Decimal` carried through the pipeline and serialized as
a string preserves every digit.

## Decision

Every amount, rate, and percentage crosses the wire as a **decimal string**,
never a JSON number.

```json
{
  "loss_pct": "25.02",
  "send_amount": "100",
  "effective_rate": "1022.70"
}
```

This is tested at the boundary: a test walks every JSON response and asserts
that every money field parses as a valid decimal.

## Consequences

- A client can parse the string into an arbitrary-precision type without
  losing digits.
- The server's internal representation (`decimal.Decimal`) round-trips
  faithfully.
- A `float64` parser in a client will still work — JSON number parsing is
  a superset — but the canonical form encourages correct handling.
- The boundary test catches any field that accidentally becomes a JSON number.

## Evidence

- `README.md` — "Money on the wire" section
- `server/sanitize_test.go` — the boundary test that walks every response
