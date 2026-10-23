# Reverse corridors: the exit leg as a first-class measurement

**Status: decision recorded and one reverse corridor measured end to end.**

**Decision: a corridor is an ordered pair, and reverse corridors are first-class measurements. `NGNC → USDC` is measured as its own corridor, with its own integrity state, its own run-store key and its own snapshot. Scheduling reverse corridors in the monitor sweep and publishing them in the UI is deferred and named below.**

**Measurement: on 2026-10-22 the naira exit leg was not absent — it was ruinously expensive. NGNC → USDC priced at every size through `NGNC → XLM → USDC` (state `DIRECT`), at 10.03% loss for 150 NGNC rising monotonically to 97.57% for 7,500,000 NGNC. Only sizes up to 15,000 NGNC (≈ $10) were graded better than UNUSABLE.**

> Written in response to issue #596, following [stellar-fiat-tokens-twosided-book-research.md](stellar-fiat-tokens-twosided-book-research.md), which found zero NGNC bids: a holder can enter the token but the measurement said nothing about whether they could leave.

---

## The problem

Corridors were send-asset to fiat-token only: USDC → NGNC, USDC → GHSC, and so on. Every published figure answered "what does it cost to get naira on chain". None answered "what does it cost to get back out", and the two-sided-book research showed why that omission matters: with zero bids on the XLM/NGNC book, a receiver holds an asset nobody on the open market is buying. A corridor that only measures entry hides that the money is trapped once it arrives.

## The decision

**Adopted now — reverse corridors are first-class at the measurement layer.**

| Layer | Status | Where |
|:---|:---|:---|
| Corridor identity | Ordered `(send, receive)` pair | `cmd/ladder -from/-to`; the snapshot's `corridor` block |
| Run-store key | Already directional, no schema change | `runstore.CorridorKey(send, receive)` → `NGNC-USDC` is a different chain from `USDC-NGNC` |
| Integrity state | Own state per corridor, computed by the same engine | `route.LadderResult.Integrity` |
| Benchmark | The inverted peg pair (`NGN/USD`, not `USD/NGN`) | `-from NGNC` resolves the reference pair from the token's registered peg |
| Sizes | The shared USD ladder scaled into the send asset | `reverseSizes()`: `route.DefaultSizes` × 1500 NGN/USD |
| Recorded evidence | Own snapshot, replayed offline by a committed test | `testdata/snapshots/ngnc-usdc-*`; `TestReverseCorridorReplay` |

The `-from` flag is the whole interface: with `-from USDC` (the default) the tool measures the historical forward corridor, and with `-from <token> -to USDC` it measures the exit leg. A token-to-token request (`-from NGNC -to GHSC`) is rejected rather than silently reinterpreted — that is a different corridor nobody asked for, and publishing it under either name would be wrong.

**Deferred — scheduling and publishing.**

- `monitor.DefaultCorridors` still sweeps forward corridors only. Adding eight reverse corridors doubles the scheduled Horizon load and the committed chain (`data/*.ndjson`) for figures the product does not yet display.
- The UI has no answer to the question the exit leg actually asks. Its headline for the exit corridor would be "10% loss at $0.10, 97% at $5,000" — a size-dependent cost with no recommended size above ~$10 — which needs a design decision about how to present "the exit exists but is not usable", not a new endpoint.
- Neither is needed to answer this issue, which asks for a decision and one corridor measured end to end.

Both are worth doing on a follow-up once the decision above is accepted; the measurement layer is ready for them.

## The measurement

Recorded with `cmd/ladder` from a clean tree at `6119fd7`, **2026-10-22 11:35 UTC**, snapshot `testdata/snapshots/ngnc-usdc-20261022T113542Z` (18 recorded responses: one reference-rate body, twelve strict-send bodies, five deduplicated slippage probes).

- Corridor: NGNC → USDC
- Sizes: 150 → 7,500,000 NGNC (the shared ladder scaled by 1500)
- Benchmark: `NGN/USD`, exchangerate-api mid `0.0007` as of 2026-10-22T00:02:31Z
- Best path at every size: `NGNC → XLM → USDC`

| Send (NGNC) | Receive (USDC) | Effective rate | Loss vs mid | Verdict |
|---:|---:|---:|---:|:---|
| 150 | 0.10 | 0.00067386 | 10.03% | POOR |
| 1,500 | 1.00 | 0.00066952 | 10.61% | POOR |
| 7,500 | 4.88 | 0.00065088 | 13.10% | POOR |
| 15,000 | 9.44 | 0.00062900 | 16.02% | POOR |
| 37,500 | 21.43 | 0.00057137 | 23.72% | UNUSABLE |
| 75,000 | 37.18 | 0.00049568 | 33.82% | UNUSABLE |
| 150,000 | 58.78 | 0.00039186 | 47.68% | UNUSABLE |
| 375,000 | 90.24 | 0.00024065 | 67.87% | UNUSABLE |
| 750,000 | 109.84 | 0.00014646 | 80.45% | UNUSABLE |
| 1,500,000 | 123.23 | 0.00008215 | 89.03% | UNUSABLE |
| 3,750,000 | 132.94 | 0.00003545 | 95.27% | UNUSABLE |
| 7,500,000 | 136.53 | 0.00001820 | 97.57% | UNUSABLE |

Corridor state: **DIRECT** (the only hop is XLM, a declared bridge, so nothing in the route is a fiat dependency). Floor **10.03%** at 150 NGNC; worst **97.57%** at 7,500,000 NGNC. The engine's best available rung is the smallest, and it is graded POOR; none of the twelve sizes is graded better.

### What this says

The exit leg is not missing; it is unaffordable. The absence of bids on the NGNC book means there is no direct buyer of NGNC, so every exit routes through XLM, and the bridge's price impact compounds with size: by 375,000 NGNC (≈ $247) the holder loses two thirds of the value, and at the top of the ladder they lose 97.57%. That is the quantitative form of "the money is trapped once it arrives" — a holder can technically exit, but only in amounts too small to matter, at a double-digit haircut.

What it adds to the forward view is not a second opinion on entry but the cost of leaving: the forward ladder prices getting naira on chain, and this one prices getting the dollar value back out — a step the forward figures never charged, because it never happened on chain.

## What about NO-MARKET?

The issue asks for "NO-MARKET correctly reported when there are no bids". This run is the other case: there are no NGNC bids, but there **are** executable paths (the XLM bridge), so the measurement reports `DIRECT` with the loss curve above rather than claiming no market. Reporting NO-MARKET here would have been wrong — it would have said the corridor cannot be executed at all, when it can be executed at 97% loss.

The NO-MARKET path itself is already covered and pinned by the fixture that exercises it: the USDC → KESC snapshot (`testdata/snapshots/usdc-kesc-20260821T222949Z`) returns zero records, and the ladder reports `IntegrityNoMarket` with the finding "No market. Horizon returned no path … at any of the 12 sizes tested." The reverse corridor uses that same engine path, so a token whose exit answers with no path at all will report NO-MARKET on its own chain with no further work.

## Reproduce

```bash
go run ./cmd/ladder -from NGNC -to USDC -record testdata/snapshots
go run ./cmd/ladder -from NGNC -to USDC -json
go test ./cmd/ladder/ -run TestReverseCorridorReplay -v
```

The replay test asserts the state, the floor, the worst case, the finding text, and that every rung priced — all from the recorded bytes, with no network.

## Related

- [stellar-fiat-tokens-twosided-book-research.md](stellar-fiat-tokens-twosided-book-research.md) — the zero-bids finding this measurement puts a price on
- [strict-receive-asymmetry-research.md](strict-receive-asymmetry-research.md) — the other direction question: does fixing the received amount change the price (#595)
- [snapshot-record-replay.md](snapshot-record-replay.md) — how the recorded bytes are verified and replayed
- [run-store.md](run-store.md) — why corridor chains are keyed by ordered pair
- [ladder-sizes.md](ladder-sizes.md) — the design of the ladder the reverse sizes scale
