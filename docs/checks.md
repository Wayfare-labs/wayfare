# The check contract

**Status: v1, implemented.** This is the shape contributors will build a few dozen
checks against, so it is written as a contract before any check exists — the
way [snapshot-format.md](snapshot-format.md) and [run-store.md](run-store.md)
are contracts. Once checks encode against it, changing it is expensive.

**Version 1.**

---

## What a check is for

The engine already answers two questions well: *can this corridor execute*
(integrity) and *at what cost against fair value* (verdict). Both come from
pathfinding and a reference rate.

Neither says anything about the things a corridor depends on. Whether the
issuer can freeze your balance. Whether the anchor's declared endpoints
actually answer. Whether the asset's home domain resolves back to the document
that claims it. Those are facts about counterparties, and they change what a
number means without changing the number.

A check is one such fact, observed and recorded so a reader can verify it.

---

## The three-valued result, and why the third value is the point

```
DETERMINED + PASSED      the check ran and the fact holds
DETERMINED + not PASSED  the check ran and the fact does not hold
NOT DETERMINED           the check could not establish either way
```

**Undetermined is not a failure.** An anchor that does not publish a SEP-10
endpoint is a different fact from one that publishes a SEP-10 endpoint that
does not answer, and both differ from one that works. Collapsing the first into
"fail" would be the same category error as reporting `NO-MARKET` as a bad
price — which the integrity taxonomy exists to prevent.

This is the project's whole posture, so it is structural rather than
conventional: `Determined` is a separate field from `Passed`, and `Passed` is
meaningless unless `Determined` is true. There is no way to express "unknown"
as a zero, a default, or a false.

Every undetermined result **must** carry a `Reason`. "Could not determine" with
no explanation is an assertion, not an observation.

---

## Checks and metrics are separate shapes

This is the sharpest design question here, so the reasoning is spelled out.

Most of the contributor backlog is not boolean. Spread, depth, price impact,
liquidity concentration, executable-versus-observed depth — these produce
*quantities*. Forcing them through a pass/fail interface would discard the
number that carries the meaning: a 2% spread and a 200% spread both merely
"fail", and the 128.8% spread this project measured on the USDC/NGNC book
would have been unreportable as a figure.

The alternative — two entirely parallel contracts — duplicates scheduling,
evidence and reporting machinery.

**So: two shapes over one machinery.**

```go
// Observation is what every check and every metric records.
type Observation struct {
    ID       string      // stable identifier, e.g. "issuer.auth-flags"
    Scope    Scope       // anchor, asset, or corridor
    Subject  string      // what was examined
    At       time.Time

    Determined bool      // false means UNABLE TO DETERMINE
    Reason     string    // why, always required when not determined
    Evidence   []Evidence
}

// CheckResult is a boolean fact.
type CheckResult struct {
    Observation
    Passed   bool        // meaningful only when Determined
    Severity Severity
}

// MetricResult is a quantity.
type MetricResult struct {
    Observation
    Value decimal.Decimal  // meaningful only when Determined
    Unit  Unit             // percent, ratio, count, or an asset code
}
```

The split follows a separation this project already makes everywhere, and that
separation is why its numbers are defensible: `refrate` produces a mid and
`route` applies thresholds to it; integrity describes structure and the verdict
grades loss. **Measurement is not judgement.**

It also puts the blast radius in the right place. A metric is a measurement,
and measuring spread correctly is contributor work. Turning a spread into "this
corridor is unacceptable" is a threshold — the same class of decision as the
verdict bands — and stays maintainer-owned. A contributor can measure
correctly without holding any authority over what the measurement means.

A check may be *derived* from a metric by applying a threshold. That derivation
is where the judgement lives, and it is declared explicitly rather than buried
inside the measurement.

---

## What a check declares about itself

The scheduler must be able to run cheap checks often and expensive ones rarely
without knowing what any of them do.

```go
type Descriptor struct {
    ID    string
    Scope Scope
    Cost  Cost

    // Title is one line for a reader.
    Title string

    // CanDetermine and CannotDetermine are prose, and both are required.
    // A check that does not state its limits invites a reader to over-read
    // its result — the AUTH_REVOCABLE check can prove the flag is set, and
    // cannot prove the issuer will never use it.
    CanDetermine    string
    CannotDetermine string
}

type Cost int
const (
    CostFree       Cost = iota // derivable from data already fetched
    CostOneRequest             // a single network round trip
    CostExpensive              // several round trips
)
```

`CannotDetermine` being mandatory is deliberate. The most likely way this
system misleads is not a wrong result — it is a correct result read as
answering more than it does.

---

## Evidence

Every result names what was observed and where.

```go
type Evidence struct {
    Source     string    // URL, account ID, or TOML field path
    Observed   string    // the value seen, verbatim where practical
    ObservedAt time.Time
}
```

A verdict without evidence is an assertion. This is the same standard the
snapshot format applies to measurements: a reader must be able to go and look.

The spread metric records the direct book's bid, ask, and independently useful
mid in its evidence. The mid is retained as a decimal diagnostic even though
the metric value itself remains the spread percentage.

---

## How results compose — and what they may never do

**Checks qualify the headline. They never move it.**

Pathfinding structure (`Integrity`) and loss against mid (`Verdict`) remain
authoritative. No check result, at any severity, may change either. A corridor
that is `DIRECT` with a `POOR` verdict stays `DIRECT` and `POOR` even if every
check on it fails.

This is enforced in code, not just documented: results are attached to a
`Findings` block on the wire and the run record, and the composition function
takes them as input while returning nothing that feeds back into integrity or
verdict.

The reason is that the headline states are derived from measurements this
project can defend arithmetically, and check results are observations about
third parties. Letting the second silently rewrite the first would make the
headline unfalsifiable — a reader could no longer tell whether a corridor was
downgraded because its liquidity moved or because someone added a check.

Severity orders what a reader sees first; it carries no arithmetic weight:

| Severity | Meaning |
|:---|:---|
| `Critical` | A failure that can cost the user their funds — clawback enabled, authorization revocable |
| `Warning` | A failure that makes the route less reliable |
| `Notice` | A discrepancy worth knowing — declared behaviour differs from observed |
| `Info` | Context; not a problem |

The **corridor health score** — how these compose into a single published
number — is deliberately not in this document. It is a decision of the same
class as the verdict thresholds, and it needs its components to exist first.

---

## Whose failure is it? — learned from implementing

A transport error means different things depending on **who published the
address**, and the three reference checks split on exactly this:

| Situation | Result | Why |
|:---|:---|:---|
| Horizon is unreachable while reading issuer flags | **undetermined** | Our own data source failed. That says nothing about the issuer, and failing the check would blame a subject for our network |
| A declared `WEB_AUTH_ENDPOINT` does not respond | **determined failure** | The anchor published this address. That it does not answer is a fact about the anchor |

The rule: **a transport failure reaching an endpoint the subject declared is a
finding about the subject. A transport failure reaching a source we chose is
not.**

Getting this backwards is easy and quiet. Treating every network error as
undetermined would make a dead declared endpoint invisible — the exact case the
SEP-10 check exists to catch. Treating every network error as a failure would
blame issuers for Horizon outages.

## Constraints

- **A failing check never crashes a measurement.** Errors are results:
  `Determined: false` with the error in `Reason`. A panic in a contributed
  check must not take down a sweep.
- **Network I/O goes through the `http.RoundTripper` seam** that `snapshot`
  already uses, so every check is replayable from a recorded snapshot and
  testable with no live network.
- **No new third-party dependencies.**
- **`decimal.Decimal` for every quantity.** No `float64`.
- **Unknown is never zero.** A `MetricResult` that could not be determined
  carries `Determined: false`, not `Value: 0`.

---

## The three reference checks

Deliberately structurally different, so implementing them tests the contract
rather than one shape of it:

| Check | Kind | Why this one |
|:---|:---|:---|
| `toml.anchor-asset-iso4217` | pure parse, `CostFree` | Derivable from a `stellar.toml` already fetched. Has a real failing case in published data: KESC declares `anchor_asset="KESC"`, naming its own token rather than the shilling |
| `sep10.endpoint-responds` | network probe, `CostOneRequest` | Tests the RoundTripper seam and the declared-versus-actual distinction |
| `issuer.auth-flags` | on-chain, `CostOneRequest` | `AUTH_REQUIRED`, `AUTH_REVOCABLE`, clawback — these determine whether a payment can be blocked or reversed, which is the most consequential thing a check can report |

If implementing the third shows the contract is wrong, the contract changes —
that is why three exist before the backlog does.

---

## Open questions for review

1. **Is `Subject` as a string enough?** A corridor-scoped check needs a send
   and a receive asset; a string forces parsing. A typed subject is cleaner but
   makes the interface generic over three shapes.
2. **Should `Severity` live on the check or on the result?** On the descriptor
   it is fixed per check; on the result a check could escalate. Fixed is
   simpler and harder to abuse.
3. **Do metrics need a threshold-derived check at all in v1**, or should that
   wait until a metric exists that anyone wants a verdict on?

## Related

- [glossary.md](glossary.md) — every state a reader can meet, including the
  three-valued check result and severity levels
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
- [snapshot-format.md](snapshot-format.md) — how checks stay replayable
- [run-store.md](run-store.md) — where results are recorded
