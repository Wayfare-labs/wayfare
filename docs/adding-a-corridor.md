# Adding a corridor

Wayfare measures any stablecoin → fiat-token corridor. The LINK.IO African-fiat
set is case study #1, not the product, and adding a new corridor is the most
useful contribution you can make: every corridor added is a claim the monitor
can check.

This guide walks the whole path, from reading an issuer's `stellar.toml` to
seeing a verdict curve in the UI. It should take under an hour.

---

## What "verified" means here

**Verified means you read it live from the issuer's own published document,
and recorded the date.** Not copied from a block explorer, a blog post, a
wallet's asset list, or this repository's existing entries.

The reason is blunt: an asset code identifies nothing. Anyone can issue a token
called `USDC` from any account, and a monitor that matched on code alone would
happily price a worthless lookalike and report the result as fact. The issuer
account is the identity. The code is a label on it.

SEP-1 is the standard that makes this checkable: an issuer publishes
`https://<domain>/.well-known/stellar.toml` listing its accounts and its
currencies. That document is the source of truth for this project, and
`anchor/` exists to read it.

Issuers also rotate accounts, so an entry that was correct is not permanently
correct. That is why every entry carries a verification date.

---

## Step 1 — Read the issuer's stellar.toml

Find the issuer's domain, then read it directly:

```bash
curl -s https://ngnc.online/.well-known/stellar.toml
```

You are looking for four things:

| Field | Why it matters |
|:---|:---|
| `NETWORK_PASSPHRASE` | Must be `Public Global Stellar Network ; September 2015`. A testnet token is not a corridor. |
| `[[CURRENCIES]]` → `issuer` | The account that actually issues the token. This is the identity you record. |
| `[[CURRENCIES]]` → `status` | Per SEP-1 only `live` means in service. `pending`, `dead`, `test` and `private` do not. |
| `[[CURRENCIES]]` → `anchor_asset` | The ISO-4217 code the token claims to track. This becomes the benchmark. |

Also note `ANCHOR_QUOTE_SERVER`. If it is absent the anchor publishes no
SEP-38 quote server, so its own rails cannot be priced programmatically and
Wayfare will measure only the on-chain leg. That absence is itself a finding —
record it, never fill it in with an estimate.

Two real-world cautions, both hit while building the existing entries:

- **The document may not be valid TOML.** `ngnc.online` serves a stray `s`
  after a quoted URL, which makes a conforming parser reject the whole file.
  `anchor/salvage.go` recovers from this and records that the document was
  malformed. If you hit something similar, report it upstream rather than
  quietly working around it.
- **Published fields may be wrong.** The KESC entry sets
  `anchor_asset="KESC"`, naming its own token rather than the ISO-4217 code
  `KES` that SEP-1 intends. Record what the document says *and* what you read
  it as, in the comment.

A `status` that is not `live` does not disqualify a corridor. GHSC and KESC are
both `pending` and both are measured — the pending status is part of the
finding. What matters is that you report the status rather than skip it.

---

## Step 2 — Register the corridor entry

Everything lives in [`asset/known.go`](../asset/known.go). Corridor registration
is consolidated into a single `registry` slice, so adding an asset defines its
identity, peg, and verification status in one place.

**a. The issuer account constant.** If the issuer is new, add it. If several
tokens share one account, name the constant for the issuer rather than for one
of its tokens — `LinkIOIssuer` issues NGNC, GHSC and KESC:

```go
const LinkIOIssuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
```

**b. The registry entry.** Add a single `Entry` struct to the `registry` slice
in `asset/known.go`. Use the existing entries as templates:

```go
{
	Code:             "GHSC",
	Issuer:           LinkIOIssuer,
	Peg:              "GHS",
	Status:           "pending",
	VerificationDate: "2026-08-08",
	SourceURL:        "https://ngnc.online/.well-known/stellar.toml",
	HomeDomain:       "ngnc.online",
},
```

The fields do several crucial jobs:

- **`Code` & `Issuer`** identify the asset unambiguously on Stellar.
- **`Peg`** supplies the benchmark fiat currency (ISO-4217) and makes derivative corridors detectable. A token with no registered peg cannot be measured.
- **`Status`** records the issuer-declared SEP-1 status (`live`, `pending`, etc.).
- **`VerificationDate` & `SourceURL`** record when and where the issuer document was verified.
- **`HomeDomain`** binds the asset to the domain publishing its `stellar.toml`.

Lookup maps (`known`, `fiatPegs`, and `homeDomains`) are derived automatically from `registry` at startup.

**c. The constructor (optional).** Add a convenience constructor if the token is
referenced directly across packages and tests:

```go
// GHSC is the Ghanaian cedi token from the same issuer as NGNC.
func GHSC() Asset { return Stellar("GHSC", LinkIOIssuer) }
```

---

## Step 3 — Measure it

```bash
go run ./cmd/ladder -to GHSC
```

This prices the corridor across the default size ladder and prints the
effective rate, loss against the reference mid, verdict, and integrity state at
each size. It needs live network access to Horizon and the reference rate
provider; there are no cached figures to fall back on, by design.

Read the `INTEGRITY` column first:

| State | Meaning |
|:---|:---|
| `DIRECT` | An independent market exists — at least one path avoids other fiat tokens. |
| `DERIVATIVE` | Every path routes through another fiat token. The corridor has no market of its own. |
| `NO-MARKET` | Horizon returns no path at all. This is the absence of a price, not a bad one. |

Then read the bottom rung. At 0.1 units price impact is negligible, so whatever
loss remains there is the corridor's **structural floor** — its spread rather
than its depth. A floor above 20% means no size can be acceptable, because the
zero-size limit is already unacceptable.

To see it in the UI:

```bash
go run ./cmd/wayfared      # then open http://localhost:8080
```

Custom sizes, if the default ladder is the wrong shape for your corridor:

```bash
go run ./cmd/ladder -to GHSC -sizes 0.1,1,10,100,1000
```

---

## Step 4 — Record what you measured

If you are adding a corridor to the repository, add its figures to
[`docs/corridor-measurements.md`](corridor-measurements.md) in the same form as
the existing entries: raw ladder output, the timestamp, the endpoint, and the
reference mid each size was scored against.

Two rules on that document:

**Do not round in a direction that flatters the result.** If a figure is
unflattering — including to this project's own thesis — publish it unflattering.

**Keep it descriptive.** Report what the ledger and the published SEP-1
document say. Do not characterise intent, and keep what you measured separate
from what you inferred.

---

## Step 5 — Test and open the PR

```bash
make fmt vet test race
```

If your corridor exercises a new classification path — a derivative corridor
with a different dependency shape, or a token whose peg is unusual — add a test
with the real Horizon response as the fixture. See the `TestDerivativeCorridorIsFlagged`
and `TestNoMarketIsDistinctFromUnusable` cases in
[`route/route_test.go`](../route/route_test.go) for the pattern: real measured
data, not a payload derived from the implementation you are testing.

In the PR, include the raw `cmd/ladder` output with its timestamp, and the
issuer's `stellar.toml` status for the asset.

---

## Checklist

- [ ] Issuer account read live from the issuer's own `stellar.toml`, not copied
- [ ] `NETWORK_PASSPHRASE` confirmed as public mainnet
- [ ] Registered in `asset/known.go` `registry` with `Code`, `Issuer`, `Peg`, `Status`, `VerificationDate`, `SourceURL`, and `HomeDomain`
- [ ] `go run ./cmd/ladder -to CODE` produces a sane curve
- [ ] Integrity state is what you expect, and you can say why
- [ ] Figures recorded in `docs/corridor-measurements.md` with a timestamp
- [ ] `make fmt vet test race` clean

## Related

- [CONTRIBUTING.md](../CONTRIBUTING.md) — the project's invariants. They are
  hard constraints, not style preferences.
- [docs/corridor-measurements.md](corridor-measurements.md) — what has been
  measured so far, and what the figures showed.
