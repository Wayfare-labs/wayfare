# Adding a check

[docs/checks.md](checks.md) is the contract. This is the worked example it
promised: the same prose path [`docs/adding-a-corridor.md`](adding-a-corridor.md)
takes for a corridor, walked once for a check, end to end.

**Checked against the code at commit `35d7d8d`, 2026-09-25.** Every claim about
the code below was read from the tree at that commit, not from README prose.
The one measurement (a table of live `stellar.toml` fetches) carries its source
and the date it was taken. Nothing here describes a capability the repository
does not have.

A check is a small, self-contained contribution: one fact about a counterparty,
observed and evidenced. It cannot move a verdict or an integrity state, so it
cannot break a published measurement — see
[the composition rule](#step-6--register-it-or-it-never-runs).

---

## What you are adding

One fact. Not a judgement, not a score, not a threshold.

| | A check answers | A metric answers |
|:---|:---|:---|
| Shape | does this fact hold? | what is this quantity? |
| Result | `determined` + `passed`, or **not determined** | a `decimal.Decimal` and a unit |
| Example | "the declared SEP-10 endpoint returns a challenge" | "the bid/ask spread is 128.8% of mid" |

If your contribution is a number rather than a fact, stop and read
[adding-a-metric.md](adding-a-metric.md) instead.

The third result state is the whole point of the contract, so it is worth
restating in your own terms before you write code:

```
DETERMINED + PASSED      the check ran and the fact holds
DETERMINED + not PASSED  the check ran and the fact does not hold
NOT DETERMINED           the check could not establish either way — not a failure
```

`Determined` is a separate field from `Passed` in Go and on the wire. There is
no way to express *unknown* as a zero, a default or a false, and every
undetermined result carries a `Reason`.

---

## The whole contract, and where it lives

```go
type Check interface {
    Describe() Descriptor
    Run(ctx context.Context, s Subject) CheckResult
}
```

| Piece | File | What it gives you |
|:---|:---|:---|
| `Descriptor` | `checks/checks.go` | identity, scope, cost, severity, and the two prose fields |
| `Subject` | `checks/checks.go` | what is being examined, already resolved |
| `Observation`, `Evidence` | `checks/checks.go` | the recorded fact and where it was seen |
| `CheckResult`, `Pass`, `Fail`, `Undetermined` | `checks/checks.go` | the result constructors |
| `checks.Run` | `checks/checks.go` | validates the descriptor, recovers a panic, repairs missing identity |
| `checks.Runner` | `checks/runner.go` | resolves the anchor once, runs every check, aggregates |
| `checks.GuardedClient` | `checks/transport.go` | the HTTP client safe to point at an audited party's URL |
| Wire form | `checks/wire.go` | `CheckJSON`, `FindingsJSON` |

Read `checks/checks.go` once, in full, before writing anything. Every rule this
document describes is enforced there, so the code is the authority and this
page is the tour.

---

## Step 1 — Choose a fact, and name it

The existing default set is the worked-example library. Read the ones nearest
your fact before writing a new one — they are all in `checks/`:

| ID | File | Scope | Cost | Severity |
|:---|:---|:---|:---|:---|
| `toml.anchor-asset-iso4217` | `toml_anchor_asset.go` | asset | free | notice |
| `sep10.endpoint-responds` | `sep10_endpoint.go` | anchor | one-request | warning |
| `sep24.info-lists-asset` | `sep24_info_lists_asset.go` | asset | one-request | notice |
| `sep38.quote-server-published` | `sep38_quote_server.go` | anchor | free | notice |
| `issuer.auth-flags` | `issuer_auth_flags.go` | asset | one-request | critical |
| `issuer.auth-immutable` | `issuer_flag_immutability.go` | asset | one-request | info |
| `toml.home-domain-roundtrip` | `toml_home_domain_roundtrip.go` | asset | expensive | notice |

Those seven are what `Runner.Default()` returns, so they run on every corridor
sweep. `checks/issuer_drift.go` also implements `Check` (`issuer.drift`) but is
**not** in the default set, so it does not run — and its `Run` re-reads the
issuer's auth flags without comparing them to any earlier observation, so it
cannot report drift today. It is a file to read, not a shape to copy.

**IDs are `<source>.<fact>`, lower-case, hyphenated.** `<source>` names where
the fact comes from — `toml`, `sep10`, `sep24`, `sep38`, `issuer` — and `<fact>`
says what is being observed. The ID is the stable handle a reader correlates
findings by across runs, so it must not encode a result: `issuer.auth-flags`,
not `issuer.no-clawback`.

**Choose `Cost` honestly.** `CostFree` means derivable from data already
fetched (that is what `Subject.Profile` is for); `CostOneRequest` is a single
round trip; `CostExpensive` is several. The descriptor exists so a scheduler can
run cheap checks often without knowing what any of them do, and a check that
understates its cost breaks that.

**Choose `Severity` carefully, and remember it carries no arithmetic weight.**
It orders what a reader sees first:

| Severity | Use it for |
|:---|:---|
| `Critical` | a failure that can cost the user their funds — clawback enabled, authorization revocable |
| `Warning` | a failure that makes the route less reliable |
| `Notice` | a discrepancy worth knowing — declared behaviour differing from observed |
| `Info` | context; not a problem |

---

## Step 2 — Decide what the subject must contain

`Subject` is typed, and the runner populates what it can before your check runs
(`checks/runner.go`, `ForAsset`):

| Field | Populated when | Note |
|:---|:---|:---|
| `Domain` | the asset has a **verified** home domain in `asset/known.go` | a guessed domain would send your check at somebody else's document |
| `Profile` | that domain's `stellar.toml` resolved | shared, read-only, and the reason a `CostFree` check costs nothing |
| `Asset` | always, for `ScopeAsset` | code **and** issuer; the code alone identifies nothing |
| `Send`, `Receive` | only for corridor-scoped work | never populated by `Runner.ForAsset` today |
| `Integrity`, `Underlying`, `ReferenceAgreement` | when the caller has one to give | corridor metrics, not checks |

**Missing input is not evidence of a problem with the subject.** A field your
check needs but does not have is an `Undetermined` result with a reason, never
a `Fail`. The rule and its reasoning:

> A check must state what it needs in its descriptor and return an undetermined
> result — never a failure — when the field it needs is absent.

`checks/toml_anchor_asset.go` is the model: a nil `Profile` produces *"no
stellar.toml has been resolved for this asset, so nothing was declared to
check"*, and an entry with no `anchor_asset` produces another undetermined
result, because a token that declares no peg has not said something wrong — it
has said nothing, and those differ.

`Subject.Profile` is **shared, not owned.** The runner resolves it once per
corridor and hands the same pointer to every check, so treat it as read-only. A
mutation through it reaches the rest of the sweep.

---

## Step 3 — Write `Describe()`

Both prose fields are mandatory, and `checks.Run` calls `Descriptor.Validate()`
before your `Run` — a descriptor missing `Title`, `CanDetermine` or
`CannotDetermine` produces an undetermined result rather than a broken sweep.

```go
func (MyCheck) Describe() Descriptor {
    return Descriptor{
        ID:       "toml.my-fact",
        Scope:    ScopeAnchor,
        Cost:     CostFree,
        Severity: SeverityNotice,
        Title:    "One line naming what is checked",

        CanDetermine: "...",

        CannotDetermine: "...",
    }
}
```

**`CannotDetermine` is the most valuable thing you will write here.** The
likeliest way this system misleads is not a wrong result — it is a correct
result read as answering more than it does. `issuer.auth-flags` can prove a
clawback flag is set; it cannot prove the issuer will ever use it, and the
reader is told so in the descriptor.

Write it by answering: *what would a reader wrongly conclude from a pass?* For
`sep10.endpoint-responds` the answer is threefold — the challenge may be
mis-signed, the anchor may be down now rather than permanently, and one probe is
a moment rather than a pattern.

A check must not declare a `Venue`. `Venue` is a metric field
(`Descriptor.ValidateAsMetric` rejects a venue on a corridor-less metric and is
not called for checks); a check that set one would suggest it publishes a market
figure it does not produce.

---

## Step 4 — Write `Run()`

The shape is always the same: read the subject, refuse if the input is absent,
observe, evidence, and return.

```go
func (c MyCheck) Run(ctx context.Context, s Subject) CheckResult {
    d := c.Describe()
    at := time.Now().UTC()

    if s.Profile == nil {
        return Undetermined(d, s, "no stellar.toml resolved for this anchor, so nothing was declared to check")
    }

    observed := strings.TrimSpace(s.Profile.TOML.SomeField)
    ev := Evidence{
        Source:     anchor.TOMLURL(s.Profile.Domain) + " → SOME_FIELD",
        Observed:   quoteOrAbsent(observed),
        ObservedAt: at,
    }

    ...
    return Pass(d, s, "one line for a reader", ev)
}
```

A handful of rules come up every time.

**Evidence or it did not happen.** Every determined result names what was
observed and where: a URL, an account ID, or a `TOML field path`, plus the value
seen verbatim where practical — `ObservedAt` non-zero, `Source` and `Observed`
non-blank. On this path that obligation is the author's: `checks.Run` validates
the descriptor, not the evidence. (The metric path does enforce it mechanically
— `checks.RunMetric` replaces a result whose evidence is incomplete with an
undetermined one. Nothing stops the same guard being added for checks; until it
is, an evidenced result is a convention you keep, not one the runner can
check.)

**Whose failure is it?** A transport error means different things depending on
**who published the address**, and getting this backwards is easy and quiet:

| Situation | Result | Why |
|:---|:---|:---|
| Horizon is unreachable while reading issuer flags | **undetermined** | our own data source failed; that says nothing about the issuer |
| A declared `WEB_AUTH_ENDPOINT` does not respond | **determined failure** | the anchor published that address, and it does not answer |
| The domain serving a `stellar.toml` refuses us | **undetermined** | the document did not arrive; see the worked example below |

**Absence and wrongness are different facts.** An anchor that publishes no
`WEB_AUTH_ENDPOINT` offers no programmatic authentication, which is a legitimate
position and not a failure. Fail on a *discrepancy*, not on silence.

**Say why you are not failing.** Every undetermined branch is a sentence a
reader can act on: which document, which field, which endpoint, and what was
seen. `undetermined: "could not fetch"` is an assertion, not an observation.

Network I/O goes through the `HTTPClient` seam so your check is replayable from a
snapshot and testable with no network at all. Take the client as a struct field
with a guarded default, exactly as `sep10_endpoint.go` does:

```go
func (c MyCheck) client() *http.Client {
    if c.HTTPClient != nil {
        return c.HTTPClient
    }
    return GuardedClient(15 * time.Second)
}
```

`GuardedClient` refuses loopback, private, link-local and unique-local addresses
on every hop, including redirects. That is not paranoia here: a `stellar.toml`
is published by the party being audited, so an anchor could otherwise point
every deployment at its own cloud metadata service.

**No new dependencies, and `decimal.Decimal` for every quantity.** Never
`float64`. If your check needs a bounded read of a response body, use
`maxErrorBody` (`checks/transport.go`) rather than reading it whole.

---

## Step 5 — Test it offline

Tests run with **no network** in CI, inside a network namespace. If your check
touches the wire, it must be driven by a recorded transport or an in-process
`httptest` server, and `snapshot.Replayer` returns `ErrNotRecorded` rather than
falling through to the network — a test that reaches out fails rather than
passing intermittently.

The patterns already in the package:

| What to test | Existing example |
|:---|:---|
| A stub check that returns a chosen result, to test the runner | `runner_test.go`, `stubCheck` |
| A recorded HTTP transport | `runner_test.go`, `recordedTransport` |
| A check's own branches against fixtures | `checks_test.go`, `TestAnchorAssetISO4217` |
| The three states staying distinct | `checks_test.go`, `TestSEP10ThreeStatesAreDistinct` |
| A transport failure on a source *we* chose | `checks_test.go`, `TestIssuerAuthFlagsUnreachableIsUndetermined` |
| A declared endpoint that is dead | `checks_test.go`, `TestSEP10DeclaredButDeadIsAFailure` |
| A nil profile | `checks_test.go`, `TestSEP10NoProfileIsUndetermined` |
| The runner's default wiring | `runner_test.go`, `TestRunnerHandsTheClientToDefaultChecks` |

For a new check, the cases worth a table test are:

```
profile is nil                          -> undetermined, reason names the missing document
the field is absent                     -> undetermined or failed, and you can say which and why
the field holds a value                 -> passed, with evidence
the field holds a wrong-shaped value    -> failed, with evidence
the upstream returns a transport error  -> undetermined (not failed) for a source you chose
the endpoint the subject published fails -> failed
```

```bash
go test ./checks/ -run TestMyCheck -count=1
make fmt vet test race
```

`make race` matters here: the runner runs checks sequentially today
(`RunAll`, deliberately), but a check that starts a goroutine must still be race
clean. CI runs `go test -race ./...` on every push.

---

## Step 6 — Register it, or it never runs

A check that is not in `Runner.Default()` is dead code. That is the one line
that makes your work reach a reader:

```go
func (r *Runner) Default() []Check {
    return []Check{
        AnchorAssetISO4217{},
        SEP10EndpointResponds{HTTPClient: r.client()},
        // ...
        MyCheck{HTTPClient: r.client()}, // <- your check
    }
}
```

Add it with the client (or `HorizonURL`) the runner already threads through, so
one transport — and therefore one snapshot — covers the whole sweep. Results
compose through `route.WithFindings`, which is the **only** composition point
and branches on nothing:

> Checks qualify the headline. They never move it.

No result, at any severity, may change `integrity` or a verdict. That is
enforced by `Findings` having no path back into the engine, not by asking
callers to behave. Adding your check to the default set is a change to what
every corridor response reports — expect it to be reviewed as one, and check
the affected golden fixtures under `server/` and `testdata/` if they assert a
finding count.

---

## The worked example: `toml.network-mainnet`

A complete check, written the way the repository writes them. It is an
illustration, not a registered check — the file it would live in is
`checks/toml_network_mainnet.go`, and nothing below has been added to
`Runner.Default()`.

**The fact.** A `stellar.toml` declares which network the infrastructure in it
operates on via `NETWORK_PASSPHRASE`. Wayfare measures the public network; every
registered asset's issuer was verified against a document that said so. If the
document now says something else, the asset identity this project relies on is
no longer supported by the issuer's own publication.

```go
package checks

import (
    "context"
    "strings"
    "time"

    "github.com/Wayfare-labs/wayfare/anchor"
)

// NetworkMainnet checks that an anchor's stellar.toml declares the public
// Stellar network.
//
// Every registered corridor asset was verified against a document that named
// the public network. A document that now names a different one is a
// discrepancy between the claim Wayfare is scoring and the document it came
// from, which is exactly the class of fact a check records.
//
// Costs nothing: it reads the stellar.toml the runner has already resolved.
type NetworkMainnet struct{}

// Describe implements Check.
func (NetworkMainnet) Describe() Descriptor {
    return Descriptor{
        ID:       "toml.network-mainnet",
        Scope:    ScopeAnchor,
        Cost:     CostFree,
        Severity: SeverityWarning,
        Title:    "stellar.toml declares the public Stellar network",
        CanDetermine: "Whether the document's NETWORK_PASSPHRASE is the public " +
            "network this deployment measures, or names a different one.",
        CannotDetermine: "Whether assets from that document are also reachable on " +
            "the public network — an asset's issuer is the identity, and only the " +
            "ledger can say whether a given account exists there. Nor is the " +
            "passphrase proof of anything: it is a declaration, and a document can " +
            "declare it while its accounts live elsewhere.",
    }
}

// Run implements Check.
func (c NetworkMainnet) Run(_ context.Context, s Subject) CheckResult {
    d := c.Describe()

    if s.Profile == nil {
        return Undetermined(d, s,
            "no stellar.toml has been resolved for this anchor, so no network was declared to check")
    }

    declared := strings.TrimSpace(s.Profile.TOML.NetworkPassphrase)
    ev := Evidence{
        Source:     anchor.TOMLURL(s.Profile.Domain) + " → NETWORK_PASSPHRASE",
        Observed:   quoteOrAbsent(declared),
        ObservedAt: time.Now().UTC(),
    }

    // Silence is not a discrepancy. This check does not get to invent a
    // requirement the repository has never asserted about a registered
    // anchor, so an absent passphrase is undetermined rather than failed —
    // the same distinction SEP10EndpointResponds draws for an anchor that
    // publishes no auth endpoint.
    if declared == "" {
        return Undetermined(d, s,
            "the document declares no NETWORK_PASSPHRASE, so it does not say which network "+
                "these assets are on", ev)
    }

    if declared != anchor.MainnetPassphrase {
        return Fail(d, s,
            "the document declares \""+declared+"\", not the public network, so the "+
                "assets it lists are on a network Wayfare does not measure — every "+
                "figure published about this asset would name an issuer whose own "+
                "document no longer claims to be on that network", ev)
    }

    return Pass(d, s, "the document declares the public network", ev)
}
```

The section after the next one is what that check would say about the six issuer
domains in the registry today.

### What `Profile.Mainnet` already collapses, and why this check does not use it

`anchor.Profile` exposes a `Mainnet` boolean, and it is `true` only when
`t.NetworkPassphrase == anchor.MainnetPassphrase` (`anchor/anchor.go`,
`profileFrom`). **Absent and wrong are the same value in that boolean.** A check
that needs to distinguish them — to say *"the document declares no network"*
rather than *"the document declares another network"* — has to read the raw
field, which is one more reason `Subject` carries the whole profile rather than
a bag of derived booleans.

### What it would report today

Measured 2026-09-25 by fetching each registered asset's home domain directly, at
the well-known path `anchor.TOMLURL` builds
(`https://<domain>/.well-known/stellar.toml`, `anchor/anchor.go`). The home
domains come from `asset/known.go` (`registry`).

| Asset | Home domain | HTTP | `NETWORK_PASSPHRASE` |
|:---|:---|:---|:---|
| NGNC, GHSC, KESC | `ngnc.online` | 200 | `Public Global Stellar Network ; September 2015` |
| NGNT | `cowrie.exchange` | 200 | `Public Global Stellar Network ; September 2015` |
| USDZ, ZARZ | `zeam.money` | 200 | `Public Global Stellar Network ; September 2015` |
| EURMTL | `mtl.montelibero.org` | 200 | `Public Global Stellar Network ; September 2015` |
| PYUSD | `token-metadata.paxos.com` | 403 | — |
| USDC | `circle.com` | 301 → 404 at `www.circle.com` | — |

Three things follow, and the third is the reason this section exists.

- **Four of the six domains pass.** No determined failure was observed on
  2026-09-25.
- **Two are undetermined, not failed.** `token-metadata.paxos.com` answered
  HTTP 403 from a CloudFront distribution refusing the request's origin
  country; `circle.com` redirects to `www.circle.com`, which answers 404 at the
  well-known path. Neither is a statement about the issuer's document — the
  document did not arrive — so a check that failed here would be blaming a
  subject for a request that never reached it.
- **The registry has its own, separate question.** `circle.com` is the
  `HomeDomain` recorded for USDC while the recorded `SourceURL` is
  `https://www.circle.com/usdc/.well-known/stellar.toml`, which also answers 404
  as of the same date. Whether the primary USDC issuer can be verified from its
  own publication at all is tracked as [#6](https://github.com/Wayfare-labs/wayfare/issues/6)
  and is **not** something this check decides. The README's verification table
  already records the USDC issuer as *not yet verified* against
  `circle.com`. This section and the README agree, and neither is evidence that
  the issuer is anything other than what it appears to be: absence of a
  document is not a finding about an issuer.

This is what "a negative or inconclusive finding is a valid result" looks like
in a check: today it would report four passes and two unknowns, which is honest
and is not a defect in the check.

---

## Checklist

- [ ] The fact is one fact, and the ID is `<source>.<fact>`
- [ ] `Cost` and `Severity` are declared honestly, and no `Venue` is set
- [ ] `CanDetermine` and `CannotDetermine` both say something a reader can act on
- [ ] Missing input produces `Undetermined` with a reason, never `Fail`
- [ ] A transport failure reaching a source we chose is undetermined; one
      reaching an address the subject published is a failure
- [ ] Every determined result carries `Source`, `Observed` and a non-zero
      `ObservedAt`
- [ ] Network I/O goes through an injectable `HTTPClient` with a
      `GuardedClient` default
- [ ] No new dependency; no `float64`
- [ ] Tests cover the undetermined branch, the pass branch and the fail branch,
      offline
- [ ] The check is added to `Runner.Default()` with the runner's client
- [ ] `make fmt vet test race` is clean

## Related

- [docs/checks.md](checks.md) — the contract this document walks through
- [docs/adding-a-metric.md](adding-a-metric.md) — the other shape, for
  quantities rather than facts
- [docs/adding-a-corridor.md](adding-a-corridor.md) — the same style of
  walkthrough for a corridor
- [docs/maintainer-owned-areas.md](maintainer-owned-areas.md) — why the check
  *engine* is maintainer-owned while individual checks are open contribution
- [docs/offline-testing.md](offline-testing.md) — why a test must not reach the
  network, and how CI enforces it
- [docs/glossary.md](glossary.md) — the three-valued result and severity levels
