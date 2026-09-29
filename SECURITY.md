# Security policy

## Supported versions

Wayfare is pre-MVP and has no released versions. The only supported version is
the current `main` branch, and that is what fixes land on.

## Reporting a vulnerability

Report privately, through GitHub's [private vulnerability reporting][report]
form on the repository's Security tab. Please do not open a public issue for a
suspected vulnerability.

Include, as far as you can:

- what the flaw is, and which package it lives in;
- the corridor, send amount, and source responses that reproduce it — a
  `cmd/ladder` invocation or an `/api/corridor` request is ideal;
- what a sender loses because of it.

You should get an acknowledgement within a week. There is no bug bounty —
Wayfare is an early open-source project and cannot offer payment. If a report is
confirmed, we'll agree a disclosure timeline with you and credit you in the
advisory unless you'd rather we didn't.

[report]: https://github.com/Wayfare-labs/wayfare/security/advisories/new

## What counts as a vulnerability here

Wayfare holds no funds, no keys, and no user data — it is read-only by design,
so the usual list does not map cleanly. What Wayfare produces is a **claim about
how much of someone's money survives a corridor**. The security-relevant
failures are the ones that make that claim wrong in the direction that costs the
sender money.

### Critical: anything that makes a route look better than it is

These are the bugs that matter most, because someone sends money on the output.

- A quote reporting a **higher** receive amount, or a lower all-in cost, than
  the source actually offers.
- Fee arithmetic that adds units of one currency to another, or that applies a
  `fee.asset` denomination the wrong way round — the SEP-38 fee identity
  documented in the README is load-bearing, and breaking it produces a number
  that looks plausible and is meaningless.
- A verdict of "viable" for a route whose loss against the mid-market rate says
  otherwise, or any path where a missing reference rate results in a ranking
  rather than a refusal to score. Scoring against an independent mid is the
  point; degrading to "cheapest of what we found" is a vulnerability, not a
  fallback.
- A failed or unreachable source rendered as a priced route, or silently
  dropped from the set so the survivors look like the whole market.
- Reading a price from a `stellar.toml` or SEP-38 response for an asset the
  anchor does not actually issue — including a code-only match passing as a
  verified issuer.
- Any use of `float64` in a pricing path. Money is decimal; a rounding artefact
  here is a wrong quote, not a cosmetic bug. This extends to the wire: money
  crosses it as decimal strings, and emitting a JSON number invites a client to
  reintroduce the same bug downstream.
- Corridor integrity reported better than it is: a `DERIVATIVE` corridor
  presented as `DIRECT`, a dependency dropped from `depends_on`, or a
  `NO-MARKET` corridor rendered as a priced one. The absence of a market and a
  bad price are different findings, and collapsing them hides the reason a
  corridor failed.

### Also in scope

- Presenting an anchor's or Horizon's number as a Wayfare conclusion, or
  dropping the source and timestamp from a quote.
- Divergence between the HTTP API and `cmd/ladder -json` for the same
  measurement. They share `route.ToCorridorJSON` precisely so one cannot
  understate what the other reports; a second, drifting shape is a finding.
- Injection through issuer-controlled content — asset codes, `home_domain`,
  `stellar.toml` fields — into CLI output or into the UI served by `wayfared`.
  Issuer-controlled strings are untrusted input, and the TOML salvage path in
  particular parses hostile, malformed documents.
- Denial of service in the fetch layer: unbounded reads, missing timeouts, or a
  hostile domain able to stall a quote indefinitely.

### Out of scope

- **A corridor being terrible.** Wayfare reporting that the best route loses
  56% is Wayfare working. That is a measurement, not a bug.
- **An anchor's rate being bad, or its quote expiring.** Quotes are
  point-in-time and carry a timestamp; the anchor owns its pricing.
- **Horizon or a third-party rate provider returning wrong data.** Those are
  consumed and attributed, not curated by us. Report them upstream.
- **Missing corridors or missing anchors.** A source Wayfare does not price yet
  is a feature request — open an issue.

## Third-party code in the shipped image

The service ships as a static binary on
`gcr.io/distroless/static-debian12:nonroot`, so its third-party surface is two
things: the base image, and whatever the Go toolchain compiles into the binary.

CI scans the image it has just built. The report covers OS packages and language
packages; a fixable `HIGH` or `CRITICAL` finding in the image's own packages
fails the build, because the fix is a newer base image and that is a change this
repository can make.

**Measured 2026-09-25** against the image this repository builds: the image's
own packages were clean, and `/wayfared` carried 22 `HIGH`/`CRITICAL` advisories
with fixes — one `CRITICAL` — all in `stdlib` at Go 1.22.12. Every fix is on the
1.24, 1.25, 1.26 or 1.27 line; none is on the 1.22 line the Dockerfile, `go.mod`
and CI are pinned to. Those findings are **recorded by the scan and deliberately
not gated**: no change in this repository fixes them while the toolchain stays
at 1.22, and a required check that is always red teaches people to ignore it.
The scan is the record that they exist and are being carried knowingly. Both
invocations also pass `--ignore-unfixed`, so an advisory with no available patch
cannot fail a build either.

A finding you believe is a false positive can be accepted deliberately: put its
identifier in a `.trivyignore` file at the repository root and say in the pull
request why it does not apply. Accepting a finding is a decision, and a decision
belongs in a diff rather than in a re-run.

What the scan cannot see is behaviour — it names known-bad versions, not flaws
in how this code uses them. Those are the findings below.

## No funds move

Wayfare is non-custodial and read-only: it never issues tokens, never holds
reserves, never takes possession of funds, and never signs or submits a
transaction. There is no key material in the codebase and nothing to drain. If
you find a code path that submits anything to the network, that is itself a
critical finding — report it.
