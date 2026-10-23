# Spike: who would consume this API, and what shape do they need

Issue [#217](https://github.com/Wayfare-labs/wayfare/issues/217), backlog
[#157](https://github.com/Wayfare-labs/wayfare/blob/main/docs/backlog.md).

**Status: completed. The "who" question is INCONCLUSIVE, and that is reported
as the result rather than padded into a positive one.** The "what shape"
question is answered conditionally: this document lists what each named
consumer class would have to be able to do against the wire contract *as it
exists on 2026-09-24*, and what would falsify each requirement. No code was
written, and none is proposed by this issue.

---

## Why the primary question cannot be answered from here

Backlog `#157` states the premise plainly: "Wallets, PSPs and anchors are named
as intended users; none has been asked" (`docs/backlog.md:1057`, checked
2026-09-24). The question the spike exists to settle — *who actually would* —
is settled by asking a human at one of those organisations. No such outreach
happened, because none was performed. **This spike therefore returns no
consumer list.** Reporting a plausible-sounding list compiled from public
marketing pages would satisfy the letter of the issue and be worth nothing: the
issue's own text calls out that the consumers have not been asked, and a
desk-research list is not that.

There is a second reason to stop short of naming projects, and it is in this
repository's own backlog. `#273` / GitHub
[#326](https://github.com/Wayfare-labs/wayfare/issues/326) —
"Map the Stellar ecosystem projects Wayfare could inform" — is listed as "Named
wallets, PSPs and anchors, with what each would need from the API — **the input
#157 needs to be worth answering**" (`docs/backlog.md:1697-1699`, checked
2026-09-24). The ecosystem map is a separate, unstarted issue. Producing it
here would be answering #326 inside #217.

**Dependency, stated explicitly: #217 is blocked on #326 (or on primary
outreach) for its "who" half.** `docs/spike-sdk-surface.md` reached the same
conclusion from the other end on 2026-09-23, describing itself as "gated on
#217".

---

## What the repository does establish

What follows is checked against the tree at `81da004` on 2026-09-24, not
against roadmap prose.

### The API surface a consumer would bind to is small and read-only

`docs/api.md` (status line dated 2026-08-27) documents five endpoints:
`GET /api/corridor`, `GET /api/corridor/trend`, `GET /api/assets`,
`GET /healthz`, and `/` for the embedded UI. All are read-only. There is no
write path, no account, and no key — `CONTRIBUTING.md` states the non-custodial
invariant as non-negotiable.

### Two properties already exist specifically for consumers

- The service sends `Access-Control-Allow-Origin: *`, so a browser consumer on
  another origin needs no proxy (`README.md:450`, checked 2026-09-24).
- Money crosses the wire as **decimal strings, not JSON numbers**, so a
  JavaScript consumer cannot silently lose precision by parsing into a float.
  This is ADR-006 (`docs/adr/006-why-money-crosses-the-wire-as-decimal-strings.md`).

### The reading rules a correct consumer must implement

These are the parts of the contract a consumer can get *wrong*, and they are
the real "shape" question:

| Field | Rule | Where |
|:---|:---|:---|
| `live` / `stale` | A `live: false` response is a stored reading with an age; rendering it as current is the failure the envelope exists to prevent | `docs/api.md`; `server/api.go` `staleJSON` |
| `scored` | When the two reference providers diverge badly enough to be a malfunction, `scored` is false and the loss figures must not be presented | `refrate/cross.go`; `docs/api.md` |
| `recommended: null` | Null means "nothing is worth taking", not "no data". A ranking implies a winner, which the project refuses on a broken corridor | `CONTRIBUTING.md`; `route` |
| `integrity` | `NO-MARKET` means no price exists; `DERIVATIVE` means the price is not the corridor's own | `docs/checks.md` |

A consumer that ignores `scored` or renders `stale` as live is using the API
incorrectly in a way the API cannot prevent. That is the concrete shape
finding: **the contract's difficulty is not the schema, it is the discipline.**
Backlog `#269` / [#322](https://github.com/Wayfare-labs/wayfare/issues/322)
("A minimal API consumer example … The reference implementation of reading
Wayfare correctly") exists precisely because that discipline is undocumented by
example.

---

## Conditional shape requirements, by consumer class

Each row is a **hypothesis to be tested by asking**, not a finding. The third
column names what would falsify it.

| Consumer class | Hypothesis: what it needs | Falsified if |
|:---|:---|:---|
| Wallet | A per-corridor reading it can render next to a send flow, with `live`/`scored` respected and no verdict shown when `scored` is false | Asked consumers only want a single size, not the ladder |
| PSP / payment operator | Historical `trend` plus the cost decomposition, to decide whether to route at all | They source corridor health elsewhere and want only an alert |
| Anchor | Its own corridor measured independently, and its SEP-38 quote compared against the DEX price | Anchors consider a third-party monitor a compliance risk and want none of it |
| Researcher / analyst | The raw rungs and reference provenance, reproducible | Consumers want a number and treat provenance as noise |

Common thread, and the one thing that would survive falsification of every row
above: **every class needs the provenance to travel with the figure.** That is
already the design (ADR-001: reference mids are never averaged; `reference_mid`
and `reference_source` are mandatory). Any future consumer work should assume
it, and no future shape should be built that drops it.

### What would have to exist before this spike can be re-run usefully

- A named list of projects (#326), or transcripts of outreach.
- One concrete question per respondent about a specific corridor they actually
  quote or send on.
- Their answer to "which of `live`, `scored`, `recommended: null` do you
  currently ignore?" — the failure mode above predicts at least one is ignored.

---

## What this spike did not do

- It did not contact any wallet, PSP, or anchor.
- It did not measure consumer demand. There is no usage data in the tree; the
  free instance exposes no analytics.
- It did not design or build an SDK or client. `docs/spike-sdk-surface.md`
  covers the design question; this document does not duplicate it.

## Constraints check

- Assertions are checked against the tree, not against README prose.
- Every figure and document reference carries a date checked (2026-09-24) or
  the date its own status line records (2026-08-27 for `docs/api.md`,
  2026-09-23 for `docs/spike-sdk-surface.md`).
- Future capabilities are marked as future; nothing here is described as built.
- A negative/inconclusive result is reported as one.
- **No implementation is attempted as part of this issue.**

## Sources

| Source | Checked |
|:---|:---|
| `docs/backlog.md:1057` (#157 consumer premise) | 2026-09-24 |
| `docs/backlog.md:1697-1699` (#273/#326, the missing input) | 2026-09-24 |
| `docs/api.md` endpoint reference (doc's own status date 2026-08-27) | 2026-09-24 |
| `docs/adr/006-why-money-crosses-the-wire-as-decimal-strings.md` | 2026-09-24 |
| `docs/spike-sdk-surface.md` (states #217 still open) | 2026-09-24 |
| `README.md:450` (CORS for browser consumers) | 2026-09-24 |
| [SEP-38 spec](https://github.com/stellar/stellar-protocol/blob/master/ecosystem/sep-0038.md) | 2026-09-24 |
| [Stellar docs — SEP-38 quotes](https://developers.stellar.org/docs/build/apps/wallet/sep38) | 2026-09-24 |
| [Stellar Anchor Platform](https://developers.stellar.org/docs/platforms/anchor-platform) | 2026-09-24 |
