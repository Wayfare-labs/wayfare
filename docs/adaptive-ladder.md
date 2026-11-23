# Adaptive midpoint rungs

Issue [#166](https://github.com/Wayfare-labs/wayfare/issues/166). Optional,
opt-in, and off by default.

The fixed ladder (`0.1 … 5000`) samples four orders of magnitude at twelve
sizes. That spacing is deliberate, but it under-samples exactly where the curve
bends hardest — the point a sender choosing an amount most needs resolved. This
rule adds a small, bounded number of rungs at those bends.

**It never interpolates a loss, a rate or any other figure.** An added rung is
chosen by size and then quoted by the same pathfinder as every requested rung.
If the quote fails, the rung is dropped; nothing is estimated to fill it.

---

## The rule

```go
type AdaptiveRule struct {
    MaxExtraRungs int
    ThresholdPct  decimal.Decimal
}
```

| Parameter | Default | Meaning |
|:---|:---|:---|
| `MaxExtraRungs` | 4 | Most rungs the rule may add. Capped at `MaxAdaptiveExtraRungs` (8), whatever the caller asks. `≤ 0` disables the rule. |
| `ThresholdPct` | 5 | Smallest adjacent loss difference, in percentage points, that triggers a rung. Strictly `more than`, not `at least`. `≤ 0` disables the rule. |

The defaults are `route.DefaultAdaptiveRule()`. Five percentage points is the
width of the `FAIR` verdict band, so a gap that large has crossed a band
boundary and is material rather than noise between neighbours. Four extras
keeps the default ladder's request cost at most one third higher.

### Choosing candidates

1. Walk the ladder in ascending size order and compare **adjacent entries**.
2. If both priced and `|loss(current) − loss(previous)| > ThresholdPct`,
   propose the arithmetic midpoint of the two send amounts.
3. A rung that did not price breaks adjacency. The sizes on either side of a
   hole were not neighbours in the requested ladder, and filling that hole is a
   different measurement from resolving a bend in a complete curve.
4. Sort proposals by loss difference, largest first; ties go to the smaller
   size. This spends the bounded budget where the curve bends most and keeps
   selection deterministic.
5. Take at most `min(MaxExtraRungs, MaxAdaptiveExtraRungs)` proposals, dedupe
   any size already present, and price them.
6. One pass only. Added rungs are not used to find further rungs, so the bound
   is not re-spent.

### Request-cost bound

A ladder can never cost more than

```
len(requested sizes) + min(MaxExtraRungs, 8)   rung quotes
```

because of adaptive sampling. That is the whole bound, and it is tested
(`TestAdaptiveLadderRespectsItsRequestCostBound`). The hard ceiling exists so a
caller cannot turn one request into an unbounded Horizon fan-out.

Adaptive work also spends from the measurement's **existing** budget, not a new
one. The server's `X-Measurement-Budget` still describes the requested ladder;
the optional pass is abandoned if the context expires rather than extending the
request. A failed or unmeasured adaptive rung is dropped rather than published,
so it cannot make a complete ladder look partially measured.

---

## Marking on the wire

Every added rung carries `"adaptive": true` on `rungs[]`, and its point in
`curve.points[]` carries the same marker. Requested rungs omit the field
entirely, so their wire shape is unchanged. A client can therefore tell which
rungs it asked for and which the monitor chose without keeping the request
around.

```json
{
  "send_amount": "2",
  "adaptive": true,
  "priced": true,
  "integrity": "DIRECT",
  "quote": { "effective_rate": "500", "...": "..." }
}
```

The field is additive and optional; `openapi.yaml` documents it on the `Rung`
schema.

---

## Exposing it

- API: `GET /api/corridor?...&adaptive=1` uses the default rule.
- CLI: `go run ./cmd/ladder -adaptive`.
- Programmatic: set `LadderRequest.Adaptive` to a rule, or leave it `nil`.

The scheduler does not enable adaptive sampling, so recorded run-store records
keep exactly the requested ladder. That is also why the run-store schema does
not change: a stored record never contains an adaptive rung.

---

## Out of scope

- Interpolating any value, including a loss, rate or verdict.
- Recursive refinement of added rungs.
- Extending the HTTP measurement budget to fit extra rungs.
- A run-store schema change for adaptive rungs.
- UI rendering of adaptive rungs.
