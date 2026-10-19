# Spike: Soroban cost of verifying a corridor claim

Issue [#215](https://github.com/Wayfare-labs/wayfare/issues/215), backlog
[#155](https://github.com/Wayfare-labs/wayfare/blob/main/docs/backlog.md).

**Status: completed. The feasibility number is bounded: verifying a corridor
claim on Soroban costs on the order of 10³ stroops (≈10⁻⁴ XLM) as a read-only
call, and 10⁴ stroops (≈10⁻³ XLM) if the call also writes an anchor and emits
an event. The cost driver is ledger access and event emission, not
cryptography. Persistent anchoring adds rent of order 10⁻² XLM per 120–180
days for a small entry.**

No implementation is attempted. No contract is written, deployed, or
simulated. No threshold, integrity semantic, check composition rule or
run-record layout is changed.

The number below is a **modelled estimate**, not a measured one. Every
component is derived from the live mainnet cost schedule and the published
host cost model, both cited. A definitive figure requires running
`simulateTransaction` against an actual contract, which this issue
deliberately does not build — see [What could not be determined](#what-could-not-be-determined).

---

## The question, split into three

The backlog entry asks for "a feasibility number, not a contract". That
decomposes into:

1. **What is being verified?** — what bytes does a "corridor claim" consist
   of, and what would a verifier check?
2. **What does a Soroban transaction cost?** — the fee model, at the values
   the network is running today.
3. **What is the total?** — the sum, per verification.

---

## 1. What the claim is

A corridor claim in this repository is a `runstore` record. It is a JSON
document (schema version 3) carrying `version`, `seq`, `recorded_at`,
`corridor`, `integrity`, `depends_on`, `reference`, the sweep `rungs`, the
`checks` and `metrics` taken with the measurement, and two hashes
(`docs/run-store.md:36-89`).

The verifiable part is the hash chain. The rule is
(`docs/run-store.md:118-139`):

```
hash = sha256(preimage)
```

where the preimage is the record's JSON encoding **with the `hash` field
omitted**, `prev_hash` included, produced by Go's `encoding/json` with
`SetEscapeHTML(false)`, no indentation, struct-declaration order, and the
trailing newline `Encoder.Encode` appends. `prev_hash` is inside the preimage,
which is what chains records together.

A verification contract therefore needs, at minimum, to:

1. receive the preimage bytes and a claimed `hash`;
2. recompute `sha256` over those bytes;
3. compare.

Chain-linking adds a second comparison, against the `prev_hash` inside the
preimage and a previously stored head. A signature check is **not** part of the
repo's current claim: the project holds no keys and the publisher-trust model is
a separate, unbuilt question (backlog #152/#156). It is modelled below as an
optional add-on, because it is the one operation that could plausibly dominate
the compute cost.

The record sizes are known from the repository's own store: the three stored
records in `data/*.ndjson` are **1,572, 3,406 and 3,664 bytes** (measured
2026-09-24). The preimage is a few dozen bytes smaller (the omitted `hash`
field). The estimate below uses 1,500 / 3,000 / 4,000-byte preimages to bracket
that range.

Layer 4 is not built and is deferred on purpose — the README labels it
"not built — needs a trust model" and notes that Layers 3 and 4 have no
packages and no stubs (`README.md:158-164`, ADR 003). This document measures
what a verifier *would* cost; it does not move that line.

---

## 2. What Soroban charges for

A Soroban transaction's fee is the **inclusion fee** plus a **resource fee**.
The resource fee is built from the exact resources the transaction consumed
([Stellar docs — Fees, Resource Limits & Metering](https://developers.stellar.org/docs/learn/fundamentals/fees-resource-limits-metering),
checked 2026-09-24). The components are:

| Component | Charged per | Live value (mainnet) | Source |
|:---|:---|---:|:---|
| Instructions | 10,000 instructions | 7 stroops | `CONFIG_SETTING_CONTRACT_COMPUTE_V0` |
| Disk read, per entry | ledger entry | 1,563 stroops | `CONFIG_SETTING_CONTRACT_LEDGER_COST_V0` |
| Disk write, per entry | ledger entry | 2,500 stroops | `CONFIG_SETTING_CONTRACT_LEDGER_COST_V0` |
| Disk read, per KB | 1 KB | 447 stroops | `CONFIG_SETTING_CONTRACT_LEDGER_COST_V0` |
| Write, per KB | 1 KB | 875 stroops | `CONFIG_SETTING_CONTRACT_LEDGER_COST_EXT_V0` |
| Transaction size | 1 KB | 406 stroops | `CONFIG_SETTING_CONTRACT_BANDWIDTH_V0` |
| Contract events | 1 KB | 5,000 stroops | `CONFIG_SETTING_CONTRACT_EVENTS_V0` |
| Rent | see below | — | `CONFIG_SETTING_STATE_ARCHIVAL` |
| Inclusion fee (Soroban) | transaction | mode 200 (min 100, max 200) | RPC `getFeeStats` |

All config-setting values above were read directly from the live mainnet
ledger on **2026-09-24** by querying the Soroban RPC `getLedgerEntries` method
over the `CONFIG_SETTING` ledger keys, at mainnet protocol **28**. The
inclusion-fee distribution is from `mainnet.sorobanrpc.com` `getFeeStats` at
the same time (3,904 transactions over 50 ledgers; p10–p99 all 200 stroops).

### How instructions are metered

Instruction cost follows the host's cost model
([`rs-soroban-env` `soroban-env-host/src/budget/model.rs`](https://github.com/stellar/rs-soroban-env/blob/main/soroban-env-host/src/budget/model.rs),
checked 2026-09-24):

```
cost = const_term + floor(linear_term × input / 2^7)
```

The `linear_term` is stored **scaled by 2⁷** (128), so the effective per-byte
cost is `linear_term / 128`. The relevant live parameters
(`CONFIG_SETTING_CONTRACT_COST_PARAMS_CPU_INSTRUCTIONS`, mainnet, 2026-09-24):

| Cost type | const_term | linear_term (scaled) | effective /byte |
|:---|---:|---:|---:|
| `ComputeSha256Hash` | 3,636 | 7,013 | ≈ 54.8 |
| `VerifyEd25519Sig` | 377,551 | 4,059 | ≈ 31.7 |
| `ComputeEd25519PubKey` | 40,256 | 0 | — |
| `VmCachedInstantiation` | 41,142 | 634 | ≈ 5.0 |
| `VmInstantiation` | 417,482 | 45,712 | ≈ 357 |
| `ValDeser` | 331 | 4,369 | ≈ 34.1 |

`ComputeSha256Hash` is charged with `input = number of bytes in the buffer`
and `VerifyEd25519Sig` with `input = length of the signed message`
(`soroban-env-host/src/crypto/mod.rs`, checked 2026-09-24). Hashing a
3,000-byte preimage therefore costs `3,636 + floor(7,013×3,000/128)` ≈
**168,000 instructions**; verifying a signature over a 32-byte digest costs
`377,551 + floor(4,059×32/128)` ≈ **378,500 instructions**.

For scale: the per-transaction instruction ceiling is 400,000,000 and the
per-ledger ceiling is 580,000,000 (`CONFIG_SETTING_CONTRACT_COMPUTE_V0`,
2026-09-24). A whole verification is well under 0.1% of the per-transaction
budget.

---

## 3. The estimate

The model below assumes a small, already-deployed verifier contract
(~3,000 bytes of WASM, so instantiation is the cheaper cached path), the
record passed as a single `Bytes` argument, one `sha256`, a comparison, and
ordinary host dispatch. Instruction counts are modelled from the parameters
above plus ~40,000 instructions for the contract's own WASM logic and glue;
they are **estimates, not measurements**. The ledger, transaction-size, event
and inclusion fees are exact given the stated footprint.

| Scenario | Preimage | Instructions | Fee (stroops) | Fee (XLM) |
|:---|---:|---:|---:|---:|
| Verify only, no state change | 1,500 B | 236,475 | 1,180 | 0.0001180 |
| Verify only, no state change | 3,000 B | 369,858 | 2,083 | 0.0002083 |
| Verify only, no state change | 4,000 B | 458,780 | 2,552 | 0.0002552 |
| Verify + emit event (no storage) | 3,000 B | 369,858 | 6,677 | 0.0006677 |
| Verify + anchor write + TTL extend | 3,000 B | 369,858 | 9,968 | 0.0009968 |
| … + one event | 3,000 B | 369,858 | 14,968 | 0.0014968 |
| … + ed25519 signature verify | 3,000 B | 788,679 | 10,262 | 0.0010262 |
| … + ed25519 + event | 3,000 B | 788,679 | 15,262 | 0.0015262 |

**Feasibility number: order 10³ stroops (≈10⁻⁴ XLM) to verify a claim; order
10⁴ stroops (≈10⁻³ XLM) to verify it, anchor the result on-chain and emit an
event.**

### The estimate reconciles with live traffic

Real Soroban invocations on mainnet on 2026-09-24, decoded from their
envelopes, charged `fee_charged` of **5,551 / 34,886 / 71,796 stroops** for
contract calls of unknown shape. The anchor scenarios above (≈10,000 stroops)
sit inside that observed band; the read-only scenarios sit below it. The
estimate is therefore the right order of magnitude, which is what a feasibility
number is for.

### What dominates

- **Ledger access and events, not cryptography.** In the anchor scenarios the
  instruction fee is ~260 stroops while the entry read/write and event fees are
  thousands. Adding ed25519 verification — the most expensive crypto operation
  available — adds only ~290 stroops (about 3% of the transaction).
- **The preimage size barely matters.** `sha256` is ~55 instructions/byte, so a
  4,000-byte record costs ~137,000 more instructions to hash than a 1,500-byte
  one: about 100 stroops.
- **Writing state costs several times more than not writing it.** A read-only
  verify of a 3,000-byte record is ~2,000 stroops; a verify that reads one
  entry, writes one entry and extends its TTL is ~10,000.

---

## 4. Rent is a separate, recurring cost

Anchoring is cheap per call, but keeping the anchor *live* is not free. Rent is
charged when a persistent entry's TTL is extended, and is computed at apply
time ([CAP-0046-07](https://github.com/stellar/stellar-protocol/blob/master/core/cap-0046-07.md),
checked 2026-09-24; `soroban-env-host/src/fees.rs`, checked 2026-09-24):

```
rent = ceil(entry_size_bytes × fee_per_rent_1kb × ledgers /
            (1024 × persistent_rent_rate_denominator))
```

where `fee_per_rent_1kb` is interpolated from the Soroban state size and has a
floor of **1,000 stroops** (`MINIMUM_RENT_WRITE_FEE_PER_1KB`). At the live
state size of **1,681,237,636 bytes (~1.68 GB)**, below the target of
**3,000,000,000 bytes (~3 GB)**, the interpolation clamps to that 1,000 floor
(`CONFIG_SETTING_CONTRACT_LEDGER_COST_V0` and
`CONFIG_SETTING_LIVE_SOROBAN_STATE_SIZE_WINDOW`, mainnet, 2026-09-24).
`persistent_rent_rate_denominator` is **1,215**; `minPersistentTTL` is
**2,073,600 ledgers** (~120 days at ~5s/ledger) and `maxEntryTTL` is
**3,110,400** (~180 days) (`CONFIG_SETTING_STATE_ARCHIVAL`, 2026-09-24).

| Anchor entry size | TTL | Rent (stroops) | Rent (XLM) |
|:---|---:|---:|---:|
| 100 B | 120 days | 166,667 | 0.0167 |
| 100 B | 180 days | 250,000 | 0.0250 |
| 200 B | 120 days | 333,334 | 0.0333 |
| 200 B | 180 days | 500,000 | 0.0500 |
| 500 B | 120 days | 833,334 | 0.0833 |

That is roughly **0.52 XLM per KB per year** of persistent state. Rent is one
to two orders of magnitude larger than the per-verification transaction fee,
and it recurs for as long as the anchor is kept. A design that only needs the
claim verifiable at a point in time — for example emitting the hash as an event
and storing nothing — avoids rent entirely, at the cost of not being readable
by another contract.

This is the most important design consequence of the spike: **the cheap part
is the verification; the expensive part is remembering it.**

---

## What could not be determined

- **No contract was built or simulated.** The instruction counts are modelled
  from the host cost model, not read from `simulateTransaction` against a real
  contract. The ledger, size and event fees are exact given the footprint, but
  the footprint itself is assumed. A definitive number needs a deployed
  verifier and one `simulateTransaction` call.
- **The record's real preimage length distribution is unknown.** Only three
  records exist in `data/*.ndjson`, at 1,572–3,664 bytes. Corridors with many
  rungs, checks and metrics will be larger.
- **Rent assumes a TTL policy that does not exist yet.** The figures use the
  protocol's minimum and maximum persistent TTLs; an actual anchoring design
  would choose its own extension cadence, which changes the per-call cost.
- **Signature verification is priced but not specified.** The publisher-trust
  and key-custody questions (backlog #152/#156) are out of scope; the ed25519
  line is included only to show it does not change the order of magnitude.

None of these is a negative result about feasibility — the question asked for a
feasibility number, and the answer is affirmative and small.

---

## What was not done

- No contract was written, deployed, or simulated.
- No change to `runstore.Record`, the integrity taxonomy, thresholds, the check
  composition rule, or the wire shape.
- No key, signer, or on-chain state was created.

## Sources

| Source | Checked |
|:---|:---|
| `docs/run-store.md:36-89` (record shape) | 2026-09-24 |
| `docs/run-store.md:118-139` (preimage rule, hash chain) | 2026-09-24 |
| `data/*.ndjson` (record sizes 1,572 / 3,406 / 3,664 B) | 2026-09-24 |
| `README.md:158-164` (Layer 4 not built) | 2026-09-24 |
| `docs/adr/003-why-layers-3-and-4-have-no-packages.md` | 2026-09-24 |
| [Stellar docs — Fees, Resource Limits & Metering](https://developers.stellar.org/docs/learn/fundamentals/fees-resource-limits-metering) | 2026-09-24 |
| Mainnet config-setting ledger entries (protocol 28): `CONTRACT_COMPUTE_V0`, `CONTRACT_LEDGER_COST_V0`, `CONTRACT_LEDGER_COST_EXT_V0`, `CONTRACT_BANDWIDTH_V0`, `CONTRACT_EVENTS_V0`, `CONTRACT_HISTORICAL_DATA_V0`, `CONTRACT_COST_PARAMS_CPU_INSTRUCTIONS`, `STATE_ARCHIVAL`, `LIVE_SOROBAN_STATE_SIZE_WINDOW` — via Soroban RPC `getLedgerEntries` | 2026-09-24 |
| `mainnet.sorobanrpc.com` `getFeeStats` (inclusion fee; 3,904 txs / 50 ledgers) | 2026-09-24 |
| `mainnet.sorobanrpc.com` `getTransaction` / Horizon (real `fee_charged` samples) | 2026-09-24 |
| [CAP-0046-07](https://github.com/stellar/stellar-protocol/blob/master/core/cap-0046-07.md) (fee model, rent formula) | 2026-09-24 |
| [`rs-soroban-env` `soroban-env-host/src/budget/model.rs`](https://github.com/stellar/rs-soroban-env/blob/main/soroban-env-host/src/budget/model.rs) (linear model, 2⁷ scaling) | 2026-09-24 |
| [`rs-soroban-env` `soroban-env-host/src/fees.rs`](https://github.com/stellar/rs-soroban-env/blob/main/soroban-env-host/src/fees.rs) (rent, minimum rent floor) | 2026-09-24 |
| [`rs-soroban-env` `soroban-env-host/src/crypto/mod.rs`](https://github.com/stellar/rs-soroban-env/blob/main/soroban-env-host/src/crypto/mod.rs) (sha256/ed25519 input semantics) | 2026-09-24 |
| [Stellar docs — `getFeeStats`](https://developers.stellar.org/docs/data/apis/rpc/api-reference/methods/getFeeStats) | 2026-09-24 |
