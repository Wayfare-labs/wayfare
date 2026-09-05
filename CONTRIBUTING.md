# Contributing to Wayfare

Wayfare is a corridor-integrity monitor: it prices a stablecoin → fiat-token
corridor across trade sizes, scores every route against an independent
reference rate, and states plainly when none of them are worth taking.

Contributions are welcome. This document covers what the project will and
will not accept, because the constraints here are unusual and worth knowing
before you write code.

## Getting set up

```bash
git clone https://github.com/Wayfare-labs/wayfare
cd wayfare
make test
```

Go 1.22 or later. Dependencies are `shopspring/decimal` and `BurntSushi/toml`.

Useful targets:

```bash
make          # fmt, vet, test, build
make test     # tests
make race     # tests with the race detector
make cover    # coverage report -> coverage.html
make lint     # golangci-lint
make run      # measure USDC -> NGNC against live mainnet
```

`make lint` needs [golangci-lint](https://golangci-lint.run/welcome/install/)
installed separately.

## Invariants

These are not style preferences. A change that breaks one of them will be
declined regardless of how well it is written.

**Non-custodial, always.** Wayfare never holds funds, issues tokens, signs
transactions, or performs KYC. It is a read-only measurement tool. This is
what keeps it shippable by a small team without a money transmitter licence,
and it is not negotiable.

**Never recommend a route when every route is unusable.** A ranking implies
its winner is worth taking. On a broken corridor that implication is false and
expensive. When nothing clears the threshold, the monitor says so and
recommends nothing.

**Never display a rate that did not come from a live source.** No estimates,
no interpolation, no cached figures presented as current, no fallback to a
plausible-looking constant. If a rate cannot be fetched, the correct output is
an error, not a guess.

**`decimal.Decimal` for all money. No `float64` in any pricing path.** Binary
floating point cannot represent decimal fractions exactly, and rounding drift
in a tool whose entire purpose is measuring small differences is a
correctness bug.

**Anchors without SEP-38 are omitted, never estimated.** If an anchor
publishes no `ANCHOR_QUOTE_SERVER`, there is no machine-readable rate to
fetch. That fact is reported. It is never filled in with a plausible number.

## Measurement discipline

The project's claims are all reproducible measurements, so:

**Breadth follows evidence.** A corridor, metric, or score is added only when
its underlying observation can be reproduced, evidenced, and evaluated under
the project's measurement contract.

**Verify against live sources; do not encode remembered values.** Issuer
accounts are read from the issuer's own `stellar.toml` per SEP-1, not from a
block explorer or a blog post. Asset code alone never identifies an asset —
anyone can issue a token called `USDC`.

**Record the date and the source with any measurement.** Everything in
`docs/corridor-measurements.md` carries a timestamp, the endpoint it came
from, and the reference mid it was scored against.

**Do not round in a direction that flatters a result.** If a figure is
unflattering to the project's own thesis, publish it unflattering.

**Keep findings descriptive.** Wayfare reports what the ledger and published
SEP-1 documents say. It does not characterise intent, and it distinguishes
what was measured from what was inferred.

## Maintainer-owned areas

Not contributor work, because an error in any of them invalidates published
measurements rather than breaking a feature:

- `dex` pricing arithmetic
- the verdict thresholds
- the integrity taxonomy
- SEP-38 fee handling
- the check engine and how results compose
- the corridor health score, when it exists

**This is about blast radius, not gatekeeping.** Individual checks and metrics
are exactly the contribution this project wants — see
[docs/checks.md](docs/checks.md) and the issues labelled `good first issue`.

### What the labels mean

Contributors have to be able to trust these:

- **`blocked`** — do not start. It waits on something that does not exist yet
- **`needs-maintainer-review`** — the design is not settled, and a PR may be
  rejected on approach rather than on execution. Agree the shape first
- `difficulty:easy` — well-scoped, a few hours
- `difficulty:medium` — multi-file, needs design judgement
- `difficulty:hard` — architectural; discuss before building

## Code conventions

- Comments explain *why*, not *what*. The code already says what it does.
- Match the surrounding style — comment density, naming, and structure vary
  by package and should stay internally consistent.
- Every exported symbol needs a doc comment.
- Tests for financial logic should use fixtures that can actually falsify the
  implementation. The SEP-38 fee tests use the spec's own worked example for
  exactly this reason; a fixture you derived from your own code proves nothing.

## Before opening a pull request

```bash
make fmt vet test race lint
```

CI runs `gofmt`, `go vet`, `go test -race`, `go build`, and `golangci-lint`.
All must pass.

In the pull request, describe what changed and why. If it touches pricing,
say how you verified correctness — and if you measured something live,
include the raw figures and the timestamp.

## Reporting a corridor

Measurements of other corridors are genuinely valuable, and the tool is built
to run against any stablecoin → fiat-token pair, not only the one in the case
study. Open an issue with:

- the send and receive assets, with issuer accounts
- the reference pair you benchmarked against, and the source
- raw `cmd/ladder` output, with its timestamp
- the issuer's `stellar.toml` status for the asset

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0, the same terms as the project.
