# Research: anchor-published fee schedules versus measured DEX loss

Research note for issue #685. It probes the SEP-6 and SEP-24 `/fee` and
`/info` endpoints on the anchors behind the three corridors this project
measures, records their published fee schedules, and assesses whether those
schedules can feed the fees component of the cost decomposition (#96).

**Result in one line:** published fee data exists for one of the three
corridors (NGNC via ngnc.online / LINK.IO) but the figures are not
per-route and are inconsistent with the DEX-measured cost. Anchor fee
endpoints can supplement research but cannot replace the measured DEX loss
as the cost figure.

---

## Method

All requests were made live on **2026-11-22**. For each anchor:

1. Read the `stellar.toml` to find `TRANSFER_SERVER` (SEP-6) and
   `TRANSFER_SERVER_SEP0024` (SEP-24).
2. Probe `/fee?operation=deposit&asset_code=CODE&amount=100` (SEP-6/SEP-24
   shared endpoint contract).
3. Probe `/info` and extract any deposit/withdraw fee fields in the asset
   entry.
4. Record the response body and HTTP status verbatim.

---

## Anchors probed

### NGNC (ngnc.online / LINK.IO)

**stellar.toml:** `https://ngnc.online/.well-known/stellar.toml`, read
2026-11-22.

| Field | Value |
|:---|:---|
| `TRANSFER_SERVER` | not declared |
| `TRANSFER_SERVER_SEP0024` | `https://ngnc.online/api/v1/sep24` |
| `ANCHOR_QUOTE_SERVER` | not declared |

**SEP-24 `/info`:** `GET https://ngnc.online/api/v1/sep24/info` returned
HTTP 200 on 2026-11-22. The NGNC deposit entry declares:

```json
{
  "deposit": {
    "NGNC": {
      "enabled": true,
      "fee_fixed": 0,
      "fee_percent": 1.5,
      "min_amount": 10,
      "max_amount": 50000
    }
  },
  "withdraw": {
    "NGNC": {
      "enabled": true,
      "fee_fixed": 500,
      "fee_percent": 0,
      "min_amount": 100,
      "max_amount": 50000
    }
  }
}
```

**SEP-24 `/fee`:** `GET https://ngnc.online/api/v1/sep24/fee?operation=deposit&asset_code=NGNC&amount=100`
returned HTTP 501 Not Implemented on 2026-11-22. The `/info` fields are the
only machine-readable schedule.

**What the DEX shows:** a strict-send probe of 100 USDC → NGNC on 2026-11-22
returned 185,831 NGNC. At a reference mid of ~1,870 NGN/USD the effective
rate is ~1,858 NGNC/USDC, implying a DEX loss of approximately 0.6%.

The `/info` fee (1.5% of deposit) is distinct from the DEX spread: the DEX
loss covers slippage and pool fees; the anchor fee covers the fiat settlement
leg. They apply at different points in the transaction and are additive.

### GHSC (ngnc.online / LINK.IO, same issuer)

The same `stellar.toml` and SEP-24 server as NGNC. The `/info` GHSC entry:

```json
{
  "deposit": {
    "GHSC": {
      "enabled": false
    }
  }
}
```

Deposit for GHSC is **not enabled** on the anchor's own SEP-24 server as of
2026-11-22. No fee data is available; the anchor declares the asset but
does not currently offer it through the hosted flow.

### KESC (ngnc.online / LINK.IO, same issuer)

Same result as GHSC: `enabled: false` for both deposit and withdraw as of
2026-11-22. No fee schedule is available.

---

## Finding

**Published fee data exists but is not suitable as a direct input to the
cost decomposition.**

Three reasons:

1. **Different legs, not the same cost.** The anchor's published fee covers
   the fiat-settlement leg (the KYC'd bank transfer that converts NGNC back
   to NGN in a user's bank account). The DEX loss covers the on-chain swap
   leg. They are additive, not interchangeable.

2. **Coverage is partial.** Only NGNC has an enabled schedule. GHSC and KESC
   are both disabled at the anchor, so they have no published fee at all.

3. **Schedule is not per-route.** The published figures (`fee_percent: 1.5`)
   are static thresholds, not measured outcomes. The actual fee charged by
   the anchor can differ from the schedule (e.g. for different KYC tiers
   or amounts outside the stated range).

---

## Recommendation

Do not substitute anchor fee schedules for the measured DEX loss in the cost
decomposition (#96).

Consider reporting the anchor fee alongside the DEX loss as a separate
"anchor settlement fee" field in the corridor response, clearly labelled as
the anchor's own published schedule rather than a measured figure. Mark it
absent for corridors whose anchor does not enable the flow (currently GHSC
and KESC).

What would change this finding: an anchor that publishes a live `/fee`
endpoint (HTTP 200, not 501) that returns a per-transaction fee for a given
amount would allow automated fee tracking. LINK.IO implements `/info` but
not `/fee`; a future anchor for GHS or KES might implement it differently.
