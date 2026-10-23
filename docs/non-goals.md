# Non-goals: what Wayfare refuses to build, and why

Issue [#226](https://github.com/Wayfare-labs/wayfare/issues/226), backlog
`#166`.

This is the single citable statement of what this project will **never** build,
what it **will not build yet** (and under what evidence it would), and the
refusals the code already enforces at the measurement level. It consolidates
reasoning that previously lived scattered across the README, `CONTRIBUTING.md`,
and package docs, so a contributor proposing one of these things can be told
"here is the decision and here is why" in one place.

The dividing line matters and is kept strict throughout:

- **Never** — the activity contradicts what the project is, or would make it
  something that needs a licence or a trust model it deliberately does not
  have. A PR adding one of these will be declined.
- **Not yet / when** — the capability is on the roadmap but refuses to be
  built before its inputs exist, because building it earlier would break the
  project's central rule (§0).

Checked against the tree at `c9bfb75` on 2026-09-24.

---

## 0. The governing rule: a layer can never be more certain than the one beneath it

The architecture is organised by epistemic status (README Architecture; the
layer diagram at `README.md:132-160`). An unavailable layer-1 fact is
*unknown*, never a default; a layer-4 output publishing a layer-3 estimate as
fact is the specific failure this project exists to avoid. Almost every "why
not" below is this rule applied to a concrete temptation:

- no rate that was not fetched is ever presented as current (`CONTRIBUTING.md:64-67`);
- no fact that could not be established is reported as a zero or a false
  (`checks/checks.go:18-20`, `checks.go:295-308`);
- no recommendation is issued when every route is unusable (`README.md:242-252`).

A proposal that requires *inventing certainty* is a refusal on sight.

---

## 1. Never: settlement, custody, and payment execution

Moving money — escrow, custody, payment execution, "just add a send button" —
is the one category with the sharpest boundary.

**The reason is evidence, not licensing.** The project began as a router and
stop being one because live measurement killed the thesis: sending 100 USDC to
NGNC returned ~46% of fair value through the best available route
(`README.md:58-77`). On a corridor that is structurally broken at every size
measured, displaying a "best route" — let alone executing one — would cost the
sender more than half of what they sent while looking helpful. Wayfare
analyses corridors; it does not move money through them (`README.md:566-570`).
"If settlement ever earns a place it is a new project with its own evidence,
not an extension of this one" is the standing answer.

The invariant is hard, not soft: "Non-custodial, always … never holds funds,
issues tokens, signs transactions, or performs KYC. This is what keeps it
shippable by a small team without a money transmitter licence, and it is not
negotiable" (`CONTRIBUTING.md:54-57`).

## 2. Never: running as an anchor (issuing tokens, holding reserves)

The status header of the project is "non-custodial: no funds held, no tokens
issued, no KYC, no keys" (`README.md:10-11`). Issuing a stablecoin or fiat
token and holding the reserves behind it is the definition of an anchor, and
the non-goals for that are in the README: **not an anchor** — never issues
tokens or holds reserves; **not custodial** — never takes possession of funds;
**not a money transmitter** — no custody, so no licensing surface
(`README.md:639-641`).

Identity is the firebreak: the project *measures* other issuers' tokens by
reading their verified `stellar.toml` (`asset/known.go:9-16`); it never issues
one of its own. An "and here is our own token" proposal collapses several
refusals at once (custody + issuance + the independence of the benchmark).

## 3. Never: KYC / identity provision

KYC is delegated to anchors via SEP-12 — the project reads whether an anchor
declares `KYC_SERVER` and reports it as a capability
(`anchor/anchor.go:55-69`, `anchor.go:316-317`); it does not perform KYC
(`README.md:642`). Collecting, storing, or attesting identity documents is a
licensed activity with a privacy surface the read-only measurement model has
no room for. The finding "this anchor advertises SEP-12" — the declared-SEP
inventory renders it (`anchor/anchor.go:147-149`, `SEPs` at
`anchor/anchor.go:167-173`) — is the ceiling of its involvement.

## 4. Never: presenting a non-live number as current

No estimates, no interpolation, no cached figures presented as current, no
fallback to a plausible-looking constant (`CONTRIBUTING.md:64-67`). A rate
that could not be fetched is an error or a labelled `SINGLE`/stale state, not
a guess. On the wire this shows as `live: false` plus a `stale` block when a
live measurement fails and history is served instead (`README.md:438-441`),
and "no route could be priced" rather than a synthesised figure
(`server/api.go`, `README.md:441`).

## 5. Never: averaging the reference mids

A blended mid names no provider; every figure has to be traceable to a source
a reader can check (`refrate/cross.go:137-142`, ADR
[001](adr/001-why-reference-mids-are-never-averaged.md)). The rule is
"never average two provider mids" — one is chosen and the record says which
(`refrate/cross.go:143-151`, and `TestNeverAverageTwoProviderMids` guards it).

## 6. Never: recommending anything when nothing is worth taking

"When no size produces a verdict of `POOR` or better, the monitor recommends
nothing. Not the best of a bad set. Nothing." (`README.md:242-252`). On the
wire `recommended` is present and `null`, never omitted, so a client cannot
read the absence as an oversight (`README.md:249-252`). The engine returns
"these exist, and you should take none of them" (`route/route.go:276-280`).

## 7. Never: mislabelling a fact it could not establish

Three specific, code-enforced refusals in this family:

- **Not determined is not a failure.** A check that could not establish a fact
  reports `determined: false` with a reason; it never reports a `false`
  (`checks/checks.go:13-20`, `checks.go:430-447`). The term "could not
  determine" is reserved and carries a mandatory reason.
- **Unknown integrity is not no-market.** `UNKNOWN` means "structure not
  established", `NO-MARKET` means "no path exists". Both produce zero-valued
  figures and must never be conflated (`route/route.go:157-168`,
  `README.md:264-270`).
- **Refuse a schema it cannot verify.** The run store refuses to open an
  unknown record version rather than guess at it ("refusing to guess at a
  schema it does not know", `runstore/file.go:112-116`), and the snapshot
  replayer refuses a version it does not know (`README.md:346-349`).

## 8. Not yet: prediction and attestation — blocked on evidence, not appetite

These are roadmap items that currently *refuse to be built before their
inputs exist*, which is why they read as non-goals to a contributor who finds
a tempting stub:

- **V4 – prediction (failure probability, expected slippage, anomaly
  detection).** Not built, and deliberately so: it needs months of recorded
  history that does not exist, and today's records carry headline figures
  without the metrics such analysis would need (`README.md:549-558`). Training
  and publishing from that would break the central rule (§0).
- **V3 – expected failure cost.** Explicitly stays *unknown* until failure
  history exists (`README.md:544-547`).
- **V5 – verifiable attestations / oracle.** Deferred because it introduces a
  publisher-trust assumption: today every figure is independently reproducible
  from recorded bytes, and an oracle asks readers to trust the publisher
  instead (`README.md:559-562`).

These are "when the evidence lands", not "never" — but nothing may be built to
*manufacture* the evidence.

## 9. What "no" costs the project, stated plainly

The refusals above are not free. They are the reason the product is a
measurement tool and not a payment rail, and they deliberately forfeit the
highest-value features a corridor tool could offer (execution, issuance, a
compliance-driven KYC surface). `CONTRIBUTING.md:114` names the
stipulation: blast radius, not gatekeeping. A corridor, a check, a metric, or
a reference provider is exactly the contribution this project wants; the
refusal list is about the *activity*, not about contribution.

## 10. One-line register

| Refusal | Kind | Where it is stated / enforced |
|:---|:---|:---|
| Settlement, custody, payment execution | Never | `README.md:566-570`, `CONTRIBUTING.md:54-57` |
| Issuing tokens / holding reserves | Never | `README.md:639-640`, `README.md:10-11` |
| Money transmission | Never | `README.md:641` |
| KYC / identity provision | Never | `README.md:642` |
| Presenting a non-live rate as current | Never | `CONTRIBUTING.md:64-67`, `README.md:438-441` |
| Averaging reference mids | Never | `refrate/cross.go:137-142`, ADR 001 |
| Recommending a nothing-worth-taking corridor | Never | `README.md:242-252` |
| Reporting not-determined as failure or as zero | Never | `checks/checks.go:13-20` |
| Guessing at an unknown schema/version | Never | `runstore/file.go:112-116` |
| Expected failure cost, prediction, attestation | Not yet (blocked on evidence) | `README.md:544-562` |

## Related

- [README.md](../README.md) — Non-goals (§635-645), "Explicitly not planned"
  (§566-570), the recommendation rule (§242-252), the status header (§10-11)
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the invariants that make these hard
  constraints (`#invariants`)
- [checks.md](checks.md) / [`checks/checks.go`](../checks/checks.go) — the
  not-determined contract
- [run-store.md](run-store.md), [snapshot-format.md](snapshot-format.md) —
  the refuse-unknown-version rules
- [spike-adversarial-review.md](spike-adversarial-review.md) — what a would-be
  grader of this boundary could actually push on