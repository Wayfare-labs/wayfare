# NO-MARKET Corridor QA — Issue #264

**Target:** https://wayfare-cdb9.onrender.com/
**Corridor:** KESC (Kenyan Shilling)
**Tested by:** opencode agent on branch `qa/issues-257-263-264-265`
**Timestamp:** 2026-09-23T17:23:53Z
**Endpoint:** `GET /api/corridor?to=KESC&live=1`

## What the issue asks

KESC returns no path at any size; the whole page must degrade honestly rather than render an empty table.

## What was observed

### API Response

```json
{
  "send_asset": {"code": "USDC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
  "receive_asset": {"code": "KESC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", "peg": "KES"},
  "integrity": "NO-MARKET",
  "depends_on": [],
  "reference_mid": "129.453743",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/KES",
  "reference_agreement": "AGREE",
  "scored": true,
  "floor_loss_pct": "0.00",
  "floor_size": "0",
  "worst_loss_pct": "0.00",
  "worst_size": "0",
  "recommended": null,
  "live": true,
  "finding": "No market. Horizon returned no path from USDC to KESC at any of the 12 sizes tested. This is the absence of a price, not a bad price: the corridor cannot be executed at all."
}
```

**All 12 rungs have `priced: false`.** The rung array contains entries like:
```json
{"send_amount": "0.1", "priced": false, "integrity": "NO-MARKET"}
```

### UI Rendering (browser)

When loading the page and selecting KESC:

1. **Integrity badge:** `NO-MARKET` (red, "∅") — renders correctly
2. **Title:** "No market exists" — renders correctly
3. **Help text:** "No path exists at any size tested. This is the absence of a price — not a bad price." — renders correctly
4. **Finding text:** "No market. Horizon returned no path from USDC to KESC at any of the 12 sizes tested." — renders correctly
5. **Recommendation block:** "No route exists at any size tested. There is nothing to recommend, and nothing to price." — renders correctly
6. **Curve panel:** **NOT rendered** — the `curve()` function is skipped because `priced.length === 0`. This is correct behavior but produces a gap in the UI.
7. **Table:** Shows all 12 rungs with `priced: false`, displaying "no path exists at this size" in the error column. The integrity cell shows the `NO-MARKET` badge.
8. **Case study:** "This corridor has no market at all, which is a different result from expensive pricing and is reported as such." — renders correctly
9. **Measure button:** Still enabled and functional — clicking it re-fetches and still returns NO-MARKET

### Honest Degradation: PASS ✅

The page does NOT render an empty table. Instead:
- All 12 rung rows are present
- Each row shows the size and a clear "no path exists at this size" message
- The integrity badge is prominently shown
- The finding text explains the situation
- The recommendation block explicitly says there is nothing to recommend

### Contradictions Found

1. **`floor_loss_pct: "0.00"` with `floor_size: "0"`:** The floor loss is 0% and floor size is 0, which could be misinterpreted as "no loss at zero size." The `floor_size: "0"` is a sentinel meaning "no priced rungs." This is not documented clearly in the wire format — a consumer might read this as a real measurement.

2. **`worst_loss_pct: "0.00"` with `worst_size: "0"`:** Same issue as above. These zero values are not actual measurements — they are defaults when no rungs are priced.

3. **Case study says "the corridor its issuer declares live still loses about 25% at dust size":** This refers to NGNC, not KESC. KESC is mentioned in the case study context but the 25% claim is about NGNC. The case study text could be clearer about which corridor it refers to.

4. **The backlog says KESC "returns no path at any size"** — confirmed. The documentation is accurate.

5. **Reference rate is still fetched:** Even though KESC has no market, the API still fetches a reference rate (129.453743 USD/KES via exchangerate-api) and returns `scored: true`. This is correct behavior — the reference rate is independent of the market existence — but a consumer might be confused about why a rate is present for a corridor with no market.

### What Works

- ✅ Integrity badge renders correctly (red, "NO-MARKET")
- ✅ Finding text clearly explains the situation
- ✅ Recommendation block says "nothing to recommend"
- ✅ Table shows all sizes with clear error messages
- ✅ No empty table rendered
- ✅ Case study provides context

### What Needs Improvement (filed as separate issues)

- **#300** — Distinct empty states (the NO-MARKET table shows all rows but no priced data — could benefit from a more prominent empty-state indicator)
- **#235** — Preserve selection and input across an error (not relevant here, but the measure button still works)
- **#264** (this issue) — The page degrades honestly but the `floor_loss_pct: "0.00"` and `worst_loss_pct: "0.00"` with zero-size sentinels could mislead a consumer parsing the JSON

## Reproduction Steps

1. Navigate to https://wayfare-cdb9.onrender.com/
2. Wait for the page to load (cold start may take ~60s)
3. Select "KESC (shilling)" from the corridor dropdown
4. Observe the integrity badge, finding text, recommendation block, and table

**Alternative reproduction (API only):**
```bash
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?to=KESC&live=1" | python3 -m json.tool
```

## Timestamps and Endpoints

| Item | Timestamp | Endpoint |
|:---|:---|:---|
| Live measurement | 2026-09-23T17:23:53Z | `GET /api/corridor?to=KESC&live=1` |
| Stored measurement | 2026-08-22T12:10:09Z | `GET /api/corridor?to=KESC` |
| Stored record age | 32 days | — |
