# Spike: multi-region measurement

Issue [#219](https://github.com/Wayfare-labs/wayfare/issues/219), backlog
[#159](https://github.com/Wayfare-labs/wayfare/blob/main/docs/backlog.md).

**Status: completed. The empirical question is INCONCLUSIVE — this spike did
not measure from a second region, so it does not claim a result it did not
obtain. What it does settle is the mechanism: a corridor's price is a function
of ledger state, not of the observer's region, so a second region adds a second
sample of the same quantity rather than a contradicting one — provided the two
readings are attributed to the state they observed.**

No implementation is attempted. No threshold, integrity semantic, check
composition rule or run-record layout is changed.

---

## The question, split into two

The backlog entry asks two things at once, and they have different answers:

1. **Does a corridor price differently when measured from another region?**
2. **Would that invalidate single-region history?**

The first is empirical and unmeasured here. The second is answerable from the
data model and is answered below.

---

## What a measurement actually depends on

A corridor reading is built from two inputs (`route.Engine.Quote`):

| Input | Source | Region-dependent? |
|:---|:---|:---|
| On-chain price | Horizon `/paths/strict-send` (`dex/dex.go:177-189`) | No — a function of ledger state |
| Reference mid | ISO-4217 provider (`refrate/`) | Possibly — if the provider varies its answer by region or edge cache |

**Pathfinding is a pure function of ledger state.** Given the same ledger
sequence, Horizon returns the same best path and the same destination amount
for a given size, whether the request came from Frankfurt or São Paulo. The
README's own position on this is the reason `dex` delegates pricing to Horizon
rather than walking an order book (`dex/dex.go` package comment): the figure is
"the same engine that will execute the payment", not a local reconstruction.

What genuinely differs by region is therefore not the *price* but:

- **Latency.** A round trip to `horizon.stellar.org` from a distant region is
  slower. With 28 Horizon calls per market corridor
  (see [`qa/artifacts/261-live-ladder-timeout.md`](qa/artifacts/261-live-ladder-timeout.md)),
  latency multiplies — relevant to the 90s timeout, not to the figure.
- **Which ledger state you observe.** Ledgers close roughly every 5 seconds
  ([Stellar docs — Horizon](https://developers.stellar.org/docs/data/apis/horizon),
  checked 2026-09-24). Two regions sampling a few seconds apart are reading
  *different* ledgers. On a thin corridor that is the difference between
  `DIRECT` and `NO-MARKET`, and on any corridor it moves the receive amount.

That second point is the whole risk, and it is a **timing** risk wearing a
regional costume.

---

## Answer to question 2: single-region history is not invalidated

History is keyed by observation time, not by observer location:

- `runstore.Record` carries `RecordedAt` and the corridor key, and **no region
  field** (`runstore/runstore.go:122-146`, checked 2026-09-24).
- `docs/corridor-measurements.md` publishes every figure with its `measured_at`
  and its endpoint.
- `snapshot.Sources` pins the actual Horizon `base_url` for a recorded run
  (`snapshot/snapshot.go:94-103`).

A second region's reading of the same corridor is another sample of the same
quantity, made at a different moment. History is not invalidated by it. What
*would* be wrong is publishing regional readings side by side and letting a
reader infer a regional price difference, when the difference is a few seconds
of ledger time. Any multi-region work must therefore **attribute each reading
to the ledger state it observed**, or refuse to compare.

### The instrument this repository does not have

Nothing in the tree records a ledger sequence. `grep -rn ledger --include=*.go`
returns only prose (`runstore/runstore.go:24`, `checks/issuer_auth_flags.go`)
— no code records it. Horizon's root endpoint reports the latest ledger, so the
data exists; the record does not carry it.

---

## The stop-flag this issue's boilerplate asks for

Recording a region, a ledger sequence, or a Horizon instance on a measurement
means **changing the run-record layout** (`runstore.Record`). The issue
boilerplate is explicit: *"If the work turns out to require changing … the
run-record layout, stop and flag it: that is a different issue with a different
review bar."*

**Flagged.** Nothing in the tree changes the record layout, and this document
recommends the layout change be its own issue, because a new field affects
chain hashing and the Version 1 compatibility guarantee described at
`runstore/runstore.go:154-159`.

---

## A proposed experiment (not run)

Repeatable, and it answers question 1 rather than assuming it:

1. Stand up two wayfared instances, one per region, both pointed at a Horizon
   that can report its ledger (`GET /` on Horizon returns `history_latest_ledger`).
2. In a short window (~60s), request the same corridor, same sizes, `live=1`,
   from both.
3. Compare **per rung**: `receive_amount`, `effective_rate`, `loss_pct`,
   `integrity`. Compare across the same ledger sequence where possible, and
   discard pairs whose ledgers differ by more than one.
4. Separately, compare the reference mids the two regions received — the one
   input that could legitimately differ by region.

**Prediction to falsify:** for readings pinned to the same ledger, all per-rung
figures are identical across regions; only latency differs. If instead two
same-ledger readings disagree, the pricing source is not the pure function this
document claims and the claim above is wrong.

The current deployment is a single region — `render.yaml` sets
`region: frankfurt` (checked 2026-09-24) — so the experiment cannot be run from
the deployed instance as configured. It needs a second deployment or two
workstations.

---

## What was not done

- No second region was measured. Question 1 is **open**.
- No ledger sequence was recorded or compared.
- No change to `runstore.Record`, the integrity taxonomy, thresholds, the check
  composition rule, or the wire shape.
- No cost analysis of a second instance; `docs/deployment.md` prices one
  instance only.

## Sources

| Source | Checked |
|:---|:---|
| `runstore/runstore.go:122-146` (`Record` fields; no region) | 2026-09-24 |
| `runstore/runstore.go:154-159` (Version 1 hash compatibility) | 2026-09-24 |
| `snapshot/snapshot.go:94-103` (`Sources`, Horizon `base_url`) | 2026-09-24 |
| `dex/dex.go` package comment (pathfinding vs order book) | 2026-09-24 |
| `render.yaml` (`region: frankfurt`) | 2026-09-24 |
| `docs/corridor-measurements.md` (`measured_at` per figure) | 2026-09-24 |
| [Stellar docs — Horizon API](https://developers.stellar.org/docs/data/apis/horizon) | 2026-09-24 |
| `docs/qa/artifacts/261-live-ladder-timeout.md` (28 Horizon calls per corridor) | 2026-09-24 |
