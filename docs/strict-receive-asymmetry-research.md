# Research: does strict-receive price a corridor differently from strict-send?

**Status: completed.** Measured the same corridor in both of Horizon's pathfinding directions at matched notional for NGNC and NGNT on **2026-10-22**, and compared the effective rates at every size on the shared ladder.

**Finding: no. At every matched size on both naira corridors the two directions are exactly inverse — asking Horizon what it costs to deliver the amount strict-send said a size buys returns that very size, so the asymmetry is 0.0000% at all 24 measured rungs. The strict-send ladder does not describe half the corridor on these tokens; it describes all of it.**

> Written in response to issue #595. Measured against live mainnet with `cmd/asymmetry`, recorded in `testdata/snapshots/asymmetry/`, and pinned by `TestAsymmetrySnapshotPinsFinding` in `cmd/asymmetry/snapshot_test.go`.

---

## Why this was asked

A remittance sender often fixes the received amount: "the recipient gets ₦18,000". Pricing only fixes the send amount ("you send $10"). If the two directions diverge on a thin book, a strict-send-only ladder describes only half of the corridor, and the half it omits is the one the recipient cares about.

## Method

The comparison is defined so neither direction is given an advantage:

1. For each size **S** in the shared ladder (`route.DefaultSizes`, 0.1 → 5,000 USDC):
   `GET /paths/strict-send` with `source_assets=USDC`, `source_amount=S`.
2. Take the best delivered amount **D** across the returned records (the maximum, not merely the first — Horizon's ordering is not a documented guarantee).
3. `GET /paths/strict-receive` with `destination_assets=<token>`, `destination_amount=D`.
4. Take the cheapest required source amount **R** across the returned records.
5. Report the asymmetry **(R − S) / S × 100**.

No external mid-market rate is consulted: both legs are Horizon's answer to Horizon's question, so the comparison isolates the pathfinder's two directions rather than mixing in a benchmark.

This is a self-consistency test by construction — if Horizon's two endpoints agree, the asymmetry is zero. That is exactly what makes a non-zero reading meaningful: it would mean the endpoints selected different paths or venues at the same notional, which is the divergence this issue exists to detect.

## Results

### USDC → NGNC (snapshot `usdc-ngnc-20261022T113517Z`, 2026-10-22 11:35 UTC)

| Send (USDC) | Delivered (NGNC) | Required (USDC) | Asymmetry | Best strict-send path |
|---:|---:|---:|---:|:---|
| 0.1 | 147.47 | 0.10 | 0.0000% | USDC → XRP → AQUA → NGNC |
| 1 | 1,462.81 | 1.00 | 0.0000% | USDC → XLM → NGNC |
| 5 | 7,111.71 | 5.00 | 0.0000% | USDC → XLM → NGNC |
| 10 | 13,749.18 | 10.00 | 0.0000% | USDC → XLM → NGNC |
| 25 | 31,262.38 | 25.00 | 0.0000% | USDC → XLM → NGNC |
| 50 | 54,336.32 | 50.00 | 0.0000% | USDC → XLM → NGNC |
| 100 | 86,116.49 | 100.00 | 0.0000% | USDC → XLM → NGNC |
| 250 | 132,676.20 | 250.00 | 0.0000% | USDC → XLM → NGNC |
| 500 | 161,843.64 | 500.00 | 0.0000% | USDC → XLM → NGNC |
| 1,000 | 181,830.37 | 1,000.00 | 0.0000% | USDC → XLM → NGNC |
| 2,500 | 196,382.79 | 2,500.00 | 0.0000% | USDC → XLM → NGNC |
| 5,000 | 201,768.49 | 5,000.00 | 0.0000% | USDC → XLM → NGNC |

### USDC → NGNT (snapshot `usdc-ngnt-20261022T113533Z`, 2026-10-22 11:35 UTC)

| Send (USDC) | Delivered (NGNT) | Required (USDC) | Asymmetry | Best strict-send path |
|---:|---:|---:|---:|:---|
| 0.1 | 924.91 | 0.10 | 0.0000% | USDC → XLM → NGNT |
| 1 | 9,143.73 | 1.00 | 0.0000% | USDC → XLM → NGNT |
| 5 | 43,506.86 | 5.00 | 0.0000% | USDC → XLM → NGNT |
| 10 | 82,057.42 | 10.00 | 0.0000% | USDC → XLM → NGNT |
| 25 | 175,254.20 | 25.00 | 0.0000% | USDC → XLM → NGNT |
| 50 | 282,059.99 | 50.00 | 0.0000% | USDC → XLM → NGNT |
| 100 | 424,304.93 | 100.00 | 0.0000% | USDC → XLM → NGNT |
| 250 | 711,775.06 | 250.00 | 0.0000% | USDC → XLM → NGNT |
| 500 | 1,010,425.82 | 500.00 | 0.0000% | USDC → XLM → NGNT |
| 1,000 | 1,379,925.42 | 1,000.00 | 0.0000% | USDC → XLM → NGNT |
| 2,500 | 2,181,313.42 | 2,500.00 | 0.0000% | USDC → XLM → NGNT |
| 5,000 | 3,032,207.60 | 5,000.00 | 0.0000% | USDC → XLM → NGNT |

Every strict-receive answer returned exactly the strict-send source amount on its matched leg. The 0.1 USDC size is worth noting: strict-send routed it through XRP and AQUA rather than XLM, and strict-receive inverted **that** route — so agreement is not an artifact of both endpoints picking one hardcoded path.

## Recommendation

**Do not add strict-receive to the pricing wire.** Keep `StrictSendPaths` as the pricing input and keep strict-receive reachable only through this research tool. On this evidence the strict-send ladder is complete for both naira corridors: a strict-receive ladder would publish the same numbers twice, and a second pricing path is a second thing to keep correct.

Re-run the cross-check — `go run ./cmd/asymmetry -to NGNC` and `-to NGNT` — when either of these changes:

- Horizon's pathfinding behaviour or the endpoint's contract;
- the corridors' liquidity (a two-sided book emerging would be the first case where the two directions can genuinely disagree);
- the ladder itself (`route.DefaultSizes`).

A materially non-zero asymmetry would be a finding worth a wire change, with new recorded snapshots behind it. Nothing in this measurement supports proposing one today.

## Limitations

- The matched amount is derived from the strict-send answer, not chosen independently. This measures whether the two endpoints agree; it does not compare, say, "deliver a fixed ₦50,000" against "send a fixed $33". The latter is a different (and still useful) question that would need its own size ladder expressed in the receive asset.
- One day, two tokens, one reference-free comparison. Both snapshots carry their `git_revision` (`6119fd7`) and per-body SHA-256 pins, and are replayed offline by the committed test, so the figures above cannot drift without the test failing.
- No reference-rate leg exists in this cross-check by design; the published loss figures for these corridors continue to come from the ladder and its own recorded benchmark.

## Reproduce

```bash
go run ./cmd/asymmetry -to NGNC -record testdata/snapshots/asymmetry
go run ./cmd/asymmetry -to NGNT -record testdata/snapshots/asymmetry
go test ./cmd/asymmetry/ -run TestAsymmetrySnapshot -v
```

## Related

- [snapshot-record-replay.md](snapshot-record-replay.md) — recording and replaying the bytes behind these figures
- [stellar-fiat-tokens-twosided-book-research.md](stellar-fiat-tokens-twosided-book-research.md) — why one-sided books are the norm on these corridors
- [reverse-corridor-research.md](reverse-corridor-research.md) — the other direction question: what exiting a fiat token costs (#596)
- [ladder-sizes.md](ladder-sizes.md) — why the ladder spans four orders of magnitude
