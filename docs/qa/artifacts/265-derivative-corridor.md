# DERIVATIVE Corridor QA — Issue #265

**Target:** https://wayfare-cdb9.onrender.com/
**Corridor:** GHSC (Ghanaian Cedi)
**Tested by:** opencode agent on branch `qa/issues-257-263-264-265`
**Timestamp:** 2026-09-23T17:23:52Z
**Endpoint:** `GET /api/corridor?to=GHSC&live=1`

## What the issue asks

GHSC's figures compound NGNC's; the UI says so in prose and nowhere in structure.

## What was observed

### API Response

```json
{
  "send_asset": {"code": "USDC"},
  "receive_asset": {"code": "GHSC", "peg": "GHS"},
  "integrity": "DERIVATIVE",
  "depends_on": [{"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", "peg": "NGN"}],
  "reference_mid": "11.554186",
  "reference_source": "exchangerate-api",
  "reference_pair": "USD/GHS",
  "reference_agreement": "AGREE",
  "scored": true,
  "floor_loss_pct": "59.40",
  "worst_loss_pct": "99.34",
  "recommended": null,
  "live": true,
  "finding": "Derivative corridor: every path from USDC to GHSC routes through NGNC, so GHSC has no independent market and these figures compound NGNC's cost with its own. No usable size..."
}
```

**All 12 rungs are priced.** Every path routes through NGNC:
- USDC → NGNC → GHSC (via AQUA at 0.1 USDC)
- USDC → AQUA → NGNC → GHSC
- USDC → XLM → NGNC → GHSC
- etc.

### UI Rendering (browser)

1. **Integrity badge:** `DERIVATIVE` (amber, "↪") — renders correctly
2. **Title:** "Depends on an upstream corridor" — renders correctly
3. **Help text:** "Every path routes through NGNC; there is no independent market." — renders correctly
4. **Dependency shown:** `NGNC` in the integrity card and as `integrity-dependency` paragraph — renders correctly
5. **Finding text:** "Derivative corridor: every path from USDC to GHSC routes through NGNC..." — renders correctly
6. **Recommendation block:** "No recommendation. Every size tested is graded Unusable, so presenting a 'best' route here would imply one is worth taking." — renders correctly
7. **Curve:** Rendered (loss vs size, all UNUSABLE, rising from ~59% to ~99%)
8. **Table:** All 12 rungs with Loss and Verdict columns, all UNUSABLE
9. **Case study:** "This corridor has no independent market: every path routes through NGNC, so its figures compound NGNC's cost with its own." — renders correctly

### Structural vs Prose: PARTIAL FAILURE ⚠️

The issue title says "the UI says so in prose and nowhere in structure." Let me verify:

**Prose (present):**
- ✅ Integrity badge tooltip: "DERIVATIVE means every available path traverses another fiat token..."
- ✅ Integrity card help: "Every path routes through NGNC; there is no independent market."
- ✅ Dependency paragraph: "depends on NGNC"
- ✅ Finding text: "Derivative corridor: every path routes through NGNC..."
- ✅ Case study: "its figures compound NGNC's cost with its own"
- ✅ Rung warnings: "derivative corridor: every path routes through NGNC, so this rate compounds NGNC's loss with this corridor's own"

**Structure (partially present):**
- ✅ `integrity` field is `"DERIVATIVE"` — a consumer can check this programmatically
- ✅ `depends_on` array is present with full asset details (code, issuer, peg)
- ✅ Each rung has `integrity: "DERIVATIVE"` and `quote.warnings` mentioning the derivative nature
- ⚠️ **The `depends_on` field is the structural element, but it is only on the top-level corridor JSON, not on each individual rung**
- ⚠️ **The loss figures do not explicitly separate GHSC's own loss from NGNC's compounded loss** — they are a single blended figure

**The key finding:** The `depends_on` array provides structural information, and each rung has a warning message. However, there is **no field that explicitly states "this loss figure includes NGNC's loss"** or separates the two components. The finding text says "these figures compound NGNC's cost with its own" but the JSON has a single `loss_pct` per rung with no breakdown.

### Contradictions Found

1. **Issue says "nowhere in structure"** — this is partially contradicted. The `depends_on` array and per-rung warnings ARE structural elements. However, the loss figures themselves are not decomposed into GHSC-only vs NGNC-compounded components.

2. **The `depends_on` array is on the corridor level but not per-rung** — a consumer parsing individual rungs cannot tell they are derivative without checking the corridor-level `integrity` field.

3. **The recommended field is `null`** — correct, because all sizes are UNUSABLE. But there is no structural indication that even if a recommendation existed, it would be invalid because it depends on NGNC's liquidity.

4. **The case study text says "the corridor its issuer declares live still loses about 25% at dust size"** — this refers to NGNC, not GHSC. GHSC's floor loss is 59.40% at 0.1 USDC. The case study conflates the two corridors' stories.

5. **The stored data (2026-08-22) shows floor_loss_pct: 74.63% but the live data shows 59.40%** — the DEX paths and rates have changed since the stored record was taken. The stored data is significantly stale.

### What Works

- ✅ Integrity badge renders correctly (amber, "DERIVATIVE")
- ✅ Dependency is shown in the integrity card
- ✅ Finding text explicitly states the derivative nature
- ✅ Case study explains the compounding
- ✅ Per-rung warnings mention "derivative corridor"
- ✅ `depends_on` array provides structured dependency information
- ✅ No recommendation is shown (all UNUSABLE)
- ✅ Curve renders correctly showing rising loss

### What Needs Improvement (filed as separate issues)

- **#104** — Book-based metrics exclude AMM liquidity while the ladder prices through it — the derivative nature means the "book" and "DEX" describe different markets
- **#154** — Spread on the underlying pair for DERIVATIVE corridors — measuring NGNC's book and saying so explicitly
- **#55** — Define what book metrics report for DERIVATIVE and NO-MARKET corridors — "no pair by construction" vs "empty book" vs "fetch failed"
- **#232** — Drive the corridor selector from `/api/assets` — hardcoded select
- **#290** — Make the corridor state URL-addressable

## Reproduction Steps

1. Navigate to https://wayfare-cdb9.onrender.com/
2. Wait for the page to load
3. Select "GHSC (cedi)" from the corridor dropdown
4. Observe the integrity badge, dependency information, finding text, and case study

**Alternative reproduction (API only):**
```bash
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?to=GHSC&live=1" | python3 -m json.tool
```

**Verify the derivative structure:**
```bash
curl -s "https://wayfare-cdb9.onrender.com/api/corridor?to=GHSC&live=1" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f'integrity: {d[\"integrity\"]}')
print(f'depends_on: {json.dumps(d[\"depends_on\"], indent=2)}')
print(f'priced rungs: {len([r for r in d[\"rungs\"] if r[\"priced\"]])}')
print(f'recommended: {d[\"recommended\"] is not None}')
for r in d['rungs']:
    if r['priced']:
        print(f'  {r[\"send_amount\"]} USDC: loss={r[\"quote\"][\"loss_pct\"]}%, verdict={r[\"quote\"][\"verdict\"]}, path={r[\"quote\"][\"description\"]}')
        if r['quote']['warnings']:
            print(f'    warnings: {r[\"quote\"][\"warnings\"]}')
"
```

## Timestamps and Endpoints

| Item | Timestamp | Endpoint |
|:---|:---|:---|
| Live measurement | 2026-09-23T17:23:52Z | `GET /api/corridor?to=GHSC&live=1` |
| Stored measurement | 2026-08-22T12:10:05Z | `GET /api/corridor?to=GHSC` |
| Stored record age | 32 days | — |
| Reference mid | 11.554186 USD/GHS | exchangerate-api, as_of 2026-08-22T00:00:00Z |
| Secondary mid | 11.5510847 USD/GHS | currency-api, as_of 2026-08-22T00:00:00Z |
