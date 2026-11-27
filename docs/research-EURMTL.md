# Corridor research: USDC → EURMTL (euro)

**Status:** completed, 2026-11-22. **Finding:** **VERIFIED**. USDC → EURMTL is DIRECT at every size. Its floor is 0.08% against the reference mid at 1 USDC. Loss grows smoothly to 2.55% at 1,000 USDC and 5.42% at 5,000. The issuer's identity still verifies, but from a **different home domain** than the one the registry records.

> Measurement issue: [#673](https://github.com/Wayfare-labs/wayfare/issues/673). Method: [`docs/corridor-research-template.md`](corridor-research-template.md). Snapshot: [`testdata/snapshots/usdc-eurmtl-20261122T043036Z`](../testdata/snapshots/usdc-eurmtl-20261122T043036Z).

All on-chain figures below were read live from Horizon (`https://horizon.stellar.org`) on **2026-11-22**. The ladder figures are parsed from the snapshot, recorded 04:30:36Z–04:30:38Z. The reference mid was read from the project's existing rate provider in the same recording: **0.911256 USD/EUR** via exchangerate-api (`open.er-api.com`, `time_last_update_utc` 2026-11-22T00:02:32Z).

---

## Why this matters

EUR is a free-floating major currency with deep interbank markets and no exchange-control gap. The reference mid for USD/EUR is therefore about as unambiguous as a benchmark gets. That makes a EUR corridor a **control**. If it shows a large loss, the loss can't be blamed on a dual-rate fiat market (the NGN and ARS explanation). It would point at Stellar liquidity for the token itself. A small, smoothly growing loss says the measurement apparatus reads a healthy book as healthy.

Per [`docs/adding-a-corridor.md`](adding-a-corridor.md), the issuer account is the identity: an asset code identifies nothing.

---

## Candidate issuers and identity verification

EURMTL is already registered (`asset.MonteliberoIssuer`, verified 2026-08-26). No candidate search is needed; the registered issuer is re-verified here.

- **Issuer account:** `GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V`
- **Declared home domain (on-chain, 2026-11-22):** **`mtlf.me`**
- **Account auth flags:** none set (`auth_required`, `auth_revocable`, `auth_immutable`, `auth_clawback_enabled` all false). The project's `issuer.auth-flags` check reports "the issuer can neither freeze nor claw back this asset".
- **Authorized holders & amount outstanding (Horizon `/assets`, 04:31:46Z):** 4,717 holders; 358,920.68 EURMTL; 27 liquidity pools.

### SEP-1 (`stellar.toml`) check

**The home domain has moved.** The registry records `HomeDomain: "mtl.montelibero.org"` and `SourceURL: "https://mtl.montelibero.org/.well-known/stellar.toml"`. On 2026-11-22:

- `https://mtl.montelibero.org/.well-known/stellar.toml` still returns HTTP 200 (19,974 bytes, 04:31:23Z). But it now carries only Montelibero's `BP1369…` real-estate NFTs. It has **no `[[CURRENCIES]]` entry for EURMTL and does not mention the issuer account**. "EURMTL" appears only inside an NFT's description.
- The issuer's on-chain `home_domain` is `mtlf.me`. `https://mtlf.me/.well-known/stellar.toml` returned HTTP 200 with a valid certificate (curl `ssl_verify_result=0`, 11,337 bytes, 04:31:23Z). It lists the account as the sole entry in `ACCOUNTS` and declares:

```toml
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
ACCOUNTS=["GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"]
TRANSFER_SERVER_SEP0024="https://anchor.mtlf.me/sep24"
WEB_AUTH_ENDPOINT="https://anchor.mtlf.me/auth"

[DOCUMENTATION]
ORG_NAME="Montelibero"
ORG_URL="https://mtlf.me"

[[CURRENCIES]]
code="EURMTL"
issuer="GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"
status="live"
anchor_asset_type="fiat"
anchor_asset="EUR"
is_asset_anchored=true
desc="EURMTL stablecoin with 100% reservation, which MTL Foundation undertakes to exchange to bearers in Montenegro at the rate of 1 to 1 for euros."
```

Identity is **verified** from the issuer's current home domain, with the same code, issuer, peg and status as registered. The project's `toml.home-domain-roundtrip` check passes ("the account home_domain and stellar.toml both identify EURMTL").

The stale registry domain is visible in the checks' output. In the same run, `toml.anchor-asset-iso4217` reported "the resolved stellar.toml lists no [[CURRENCIES]] entry matching this code and issuer". `sep10.endpoint-responds` and `sep24.info-lists-asset` reported that the anchor "declares no WEB_AUTH_ENDPOINT" and "no TRANSFER_SERVER_SEP0024". `mtlf.me` declares both. Those three results describe the old document, not the issuer's current one.

---

## Horizon path data (USDC → EURMTL)

USDC issuer used as the source: Circle's mainnet account `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`.

Recorded with `go run ./cmd/ladder -to EURMTL -record testdata/snapshots` from a clean tree at `git_revision` `035390c`, with 16 upstream responses. The figures are parsed from the recorded strict-send bodies. "vs mid" is the effective rate against 0.911256. A route above the mid reports 0.00% engine loss.

| Send (USDC) | Receive (EURMTL) | Effective rate (EURMTL/USDC) | vs mid | Loss (engine) | Verdict | Best path |
|---:|---:|---:|---:|---:|:---|:---|
| 0.1 | 0.0912225 | 0.912225 | +0.11% | 0.00% | GOOD | USDC → XLM → AQUA → EURMTL |
| 1 | 0.9105484 | 0.910548 | −0.08% | 0.08% | GOOD | USDC → XLM → EURMTL |
| 5 | 4.5521192 | 0.910424 | −0.09% | 0.09% | GOOD | USDC → XLM → MTL → EURMTL |
| 10 | 9.1030166 | 0.910302 | −0.10% | 0.10% | GOOD | USDC → XLM → MTL → EURMTL |
| 25 | 22.7483840 | 0.909935 | −0.14% | 0.14% | GOOD | USDC → XLM → MTL → EURMTL |
| 50 | 45.4662716 | 0.909325 | −0.21% | 0.21% | GOOD | USDC → XLM → MTL → EURMTL |
| 100 | 90.8108006 | 0.908108 | −0.35% | 0.35% | GOOD | USDC → XLM → MTL → EURMTL |
| 250 | 226.1187997 | 0.904475 | −0.74% | 0.74% | GOOD | USDC → XLM → MTL → EURMTL |
| 500 | 449.2423422 | 0.898485 | −1.40% | 1.40% | GOOD | USDC → XLM → MTL → EURMTL |
| 1000 | 888.0458723 | 0.888046 | −2.55% | 2.55% | GOOD | USDC → EURMTL |
| 2500 | 2213.5751160 | 0.885430 | −2.83% | 2.83% | GOOD | USDC → EURMTL |
| 5000 | 4309.4474988 | 0.861889 | −5.42% | 5.42% | FAIR | USDC → EURMTL |

- **Reference mid:** 0.911256 USD/EUR via exchangerate-api, as of 2026-11-22T00:02:32Z. A cross-check run against currency-api at 04:22Z (0.9117, as of 2026-11-21) gave 0.17% loss at 0.1 USDC, 2.60% at 1,000 and 5.47% at 5,000.
- **Integrity:** **DIRECT** at every size. The direct USDC/EURMTL book is returned at every size, and from 1,000 USDC up it is the only path. The smaller sizes route through XLM and the unregistered MTL and AQUA tokens (non-fiat, surfaced as unregistered hops). At 500 USDC Horizon also offers a path through `EURC`. None of the best paths crosses a registered fiat-pegged hop.
- **Structural floor:** **0.08%** at 1 USDC. The 0.1-USDC rung beats the mid by 0.11%. That is plausibly rounding on a dust trade through two pools (an inference, not measured), and it is not read as a floor. The direct book alone delivers 0.0905701 at 0.1 USDC (−0.61%), so the multi-hop routes earn their keep at small sizes.
- **Depth:** loss grows smoothly and monotonically. It stays under 0.4% to 100 USDC, under 1.5% to 500 USDC and under 3% to 2,500 USDC, then reaches 5.42% at 5,000 USDC. At 5,000 the engine's "thin liquidity" warning fires (5.3% worse than the 10-USDC rate). Nothing collapses. This is a modest book being walked, not a pool emptying.

---

## SEP-38 (Quote Server) check

Neither Montelibero document declares `ANCHOR_QUOTE_SERVER`, and the `sep38.quote-server-published` check confirms it. The EUR off-ramp leg is not machine-priceable and is omitted, not estimated. `mtlf.me` does publish SEP-24 (`anchor.mtlf.me/sep24`) and SEP-10. Neither prices a quote.

---

## Finding & Conclusion

**VERIFIED.** EURMTL's issuer is verified from its own current `stellar.toml` (`mtlf.me`). USDC → EURMTL is a **DIRECT** corridor with a **0.08% structural floor**. Loss rises smoothly to **2.55% at 1,000 USDC** and **5.42% at 5,000 USDC** (FAIR). Every rung through 2,500 USDC grades GOOD.

**As a control, it behaves as one.** With an unambiguous fiat benchmark and no exchange-control gap, the corridor shows no structural loss at all. Its cost is pure depth: a few hundred thousand euros of supply and 27 pools produce a few percent of slippage at thousands of dollars, and nothing worse. Set beside NGNC's structural floor ([`corridor-measurements.md`](corridor-measurements.md)), this is evidence that the NGNC floor is not an artifact of the measurement method: the same engine, ladder and provider read this book as healthy. That points the NGNC loss at the NGN market and the NGNC token's own liquidity, as an inference this control supports rather than proves. That is the hypothesis issue #673 set out to test, and the control supports it.

**One registry defect found.** The registered `SourceURL`/`HomeDomain` (`mtl.montelibero.org`) no longer declares EURMTL. The issuer moved its home domain to `mtlf.me`. The identity is unchanged, but three counterparty checks currently read the stale document and report its absences as the anchor's.

---

## Recommendation

1. **Keep EURMTL registered.** The measurement supports it.
2. **Follow-up (out of scope for #673): re-verify the registry entry against `mtlf.me`.** Update `HomeDomain`, `SourceURL` and `VerificationDate` for EURMTL in `asset/known.go`, and the 2026-08-26 comment block, so the counterparty checks read the issuer's current document. This touches `asset/known.go` and is a separate, reviewable change.
3. **Re-measure** if EURMTL supply or pool count changes materially. The 5,000-USDC rung is the one that will move first.

---

## How to reproduce

```bash
# Replay offline from the committed snapshot (what this document's table was parsed from)
ls testdata/snapshots/usdc-eurmtl-20261122T043036Z/responses

# Re-measure live (a new snapshot under a new timestamp)
go run ./cmd/ladder -to EURMTL -record testdata/snapshots
go run ./cmd/ladder -to EURMTL -ref currency-api

# Identity
curl -s https://horizon.stellar.org/accounts/GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V   # home_domain
curl -s https://mtlf.me/.well-known/stellar.toml                # declares EURMTL
curl -s https://mtl.montelibero.org/.well-known/stellar.toml    # registered source; no longer declares it
curl -s "https://horizon.stellar.org/assets?asset_code=EURMTL&asset_issuer=GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"
```

---

## Related

- Issue [#673](https://github.com/Wayfare-labs/wayfare/issues/673): this measurement
- [`docs/corridor-research-template.md`](corridor-research-template.md): the format
- [`docs/research-ZAR.md`](research-ZAR.md): the ZARZ measurement taken in the same session
- [`docs/research-measurement-noise.md`](research-measurement-noise.md): how much ladder movement is noise
