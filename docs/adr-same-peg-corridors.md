# ADR: what a USD→USD corridor measures

- **Status:** Accepted (pending maintainer review — issue [#671](https://github.com/Wayfare-labs/wayfare/issues/671) is `needs-maintainer-review`)
- **Date:** 2026-11-22
- **Decision:** A corridor whose send asset and receive asset track the same currency is benchmarked against the identity rate, **labelled definitional on the wire**, and no reference provider is consulted. Loss on such a corridor is deviation from par. The ordinary integrity states and verdict thresholds apply unchanged.

## Context

PYUSD (Paxos) and USDZ (Zeam) are registered with peg `USD`. The ladder builds the reference pair as `USD/<peg>`, so USDC → PYUSD and USDC → USDZ are benchmarked against `USD/USD`. These are stablecoin swaps, not fiat corridors.

Before this decision the engine had no rule for that case. It asked the configured provider for `USD/USD`, and the provider answered with the `USD: 1` entry of its own base-currency table. Measured live on 2026-11-22:

```
$ go run ./cmd/ladder -to PYUSD -checks=false
...
reference mid: 1.0000 USD/USD via exchangerate-api, as of 2026-11-22T00:02:32Z
```

That line is wrong in a specific way. The figure is right, because a dollar is a dollar, but the label says it was *observed* from exchangerate-api at a moment. Nothing was observed. The provider echoed a constant from its own table. The project's layer rule ("distinguish what was measured from what was inferred", [CONTRIBUTING.md](../CONTRIBUTING.md#measurement-discipline)) forbids presenting a definition as a measurement. The issue's framing names both failure modes: pricing a swap against an FX provider is meaningless, and pricing it silently against 1.0 turns the reference into an unlabelled assumption.

The same runs show that the loss these corridors produce is real and worth measuring:

| Send (USDC) | USDC → PYUSD receive | loss vs par | USDC → USDZ receive | loss vs par |
|---:|---:|---:|---:|---:|
| 0.1 | 0.10 | 0.00% | 0.10 | 0.07% |
| 10 | 10.00 | 0.00% | 9.99 | 0.11% |
| 1000 | 1000.08 | 0.00% | 998.73 | 0.13% |
| 2500 | 2500.07 | 0.00% | 2492.64 | 0.29% |
| 5000 | 4999.93 | 0.00% | 4180.77 | **16.38%** (POOR) |

Source: `go run ./cmd/ladder -to {PYUSD,USDZ} -checks=false`, Horizon mainnet, 2026-11-22T04:22Z. Rates above par report 0.00% loss, as everywhere else in the engine.

USDZ exhausts between 2500 and 5000 USDC. That is a depth finding about a stablecoin swap, and it is exactly the kind of thing the monitor exists to report.

## Options considered

### A. Identity reference, labelled definitional (chosen)

Answer `X/X` with a mid of exactly 1, with `source = "definitional"`, `kind = definitional`, no `as_of` and no `fetched_at`, and a note explaining why. Do not consult any provider.

- The loss figure means something precise: how far the swap delivers below par. A sender swapping USDC to PYUSD wants to know exactly this.
- Par is not an assumption about a market. It is the definition of the peg both issuers publish, and the label says so instead of pretending otherwise.
- No upstream call is needed, so a provider outage cannot make a stablecoin swap unpriceable.

### B. Refuse same-peg corridors

Reject `USD/USD` with an error and drop PYUSD and USDZ from the corridor set.

- Rejected. The assets are verified, registered and actively traded, and the USDZ 5000 rung shows a 16% loss that is worth publishing. Refusing would hide a real measurement because its benchmark is unusual. It would also break `-to PYUSD` / `-to USDZ`, which work today.

### C. A separate "peg deviation" measurement

Treat same-peg corridors as a different product: measure each token's deviation from its own peg using an external stablecoin price feed (e.g. PYUSD/USD from a market aggregator).

- Not now. It needs a new provider class (stablecoin market prices), a new metric shape and new wire fields. That is a larger design with its own provenance questions. It also answers a different question, "is PYUSD holding its peg?", than the one a corridor answers, "what does this swap cost?". Option A already measures the swap's deviation from par, and the `reference_kind` field leaves room to add a measured `USD/USD` (`observed`) later without breaking the wire.

## Decision

1. **Same-peg detection.** A reference pair is same-peg when base and quote are the same ISO-4217 code (`refrate.SamePeg`). Nothing else qualifies. In particular two *different* USD tokens are compared through their common peg, never through each other.
2. **Reference.** `route.Engine` answers a same-peg pair with `refrate.Identity`: mid `1`, source `definitional`, kind `definitional`, no timestamp. The configured provider and the parallel-rate provider are **not** consulted. A parallel market for a currency against itself does not exist, so the parallel block is omitted rather than reported as failed.
3. **What loss means.** Loss is `(par − achieved rate) / par`: the swap's deviation from par in the sender's favour or against it. Rates above par report zero loss, as for every corridor. The verdict thresholds (`GOOD`/`FAIR`/`POOR`/`UNUSABLE`) apply unchanged. A 16% haircut on a dollar-for-dollar swap is as POOR as a 16% haircut on an FX corridor.
4. **Integrity states.** `DIRECT`, `DERIVATIVE` and `NO-MARKET` describe path structure, not the benchmark, so they apply unchanged. A same-peg corridor with no path is `NO-MARKET`. One whose every path routes through a registered fiat-pegged hop is `DERIVATIVE`, exactly as for an FX corridor. That includes a hop with the same peg: USDC → PYUSD forced through USDZ depends on USDZ's book. `classify` is not changed.
5. **Agreement.** A definitional rate is never cross-checked, so `reference_agreement` keeps its zero value, `SINGLE`, which `refrate.Rate` documents as "never cross-checked". It is always scorable. `MALFUNCTION` cannot arise because there is no second feed to diverge.
6. **Wire.** Every corridor document carries `reference_kind`: `observed`, `definitional`, or `unknown` when no reference was obtained. A definitional reference also carries `reference_source: "definitional"` and omits `reference_as_of` and `reference_fetched_at`. A live response also sets `reference_note` to the reason; a stored one does not restore the note. Stored runs carry no kind field. The server recovers it from the stored source (`refrate.KindOfSource`), so a historical same-peg reading never reads back as an observation. The `cmd/ladder` text table prints `reference mid: 1 USD/USD by definition (...)` instead of re-asking a provider.

## Consequences

- USDC → PYUSD and USDC → USDZ are measured exactly as before, with the same figures. Only the reference's label and provenance change.
- `reference_kind` is a new response field, an additive change under the [API compatibility policy](api-compatibility-policy.md).
- A run-store record's bytes are unchanged (no new stored field), so record hashes and the chain are unaffected. Same-peg records written from now on store `source: "definitional"`. Records written earlier store the provider's name and read back as `observed`. That is historically accurate: that is what the code claimed at the time.
- If a measured `USD/USD` benchmark (option C) is ever added, it lands as `reference_kind: "observed"` with a named source, and this ADR is amended.

## Implementation evidence

- `refrate/identity.go`: `Kind`, `SamePeg`, `Identity`, `KindOfSource`; tests in `refrate/identity_test.go`.
- `route/route.go` (`Engine.reference`): the only branch point; tests in `route/same_peg_test.go`, using the live Horizon body for USDC → USDZ at 5000 (2026-11-22T04:24:57Z) as the falsifying fixture.
- `route/wire.go` (`reference_kind`), `server/api.go` (stored-record recovery, `TestStaleJSONRecoversDefinitionalReferenceKind`), `cmd/ladder/main.go` (text output).

## Related

- [ADR 001: reference mids are never averaged](adr-reference-mids.md)
- [Reference providers](reference-providers.md)
- [HTTP API reference](api.md)
