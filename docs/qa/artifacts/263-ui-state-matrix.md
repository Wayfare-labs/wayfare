# UI State Matrix — Issue #263

**Target:** https://wayfare-cdb9.onrender.com/
**Tested by:** opencode agent on branch `qa/issues-257-263-264-265`
**Timestamp:** 2026-09-23T17:23:52Z
**Matrix:** integrity {DIRECT, DERIVATIVE, NO-MARKET, UNKNOWN} × scored {true, false} × live {true, false} × findings {present, absent, empty}

## Method

Each cell in the matrix is reached by a specific API request pattern. The UI renders differently for each combination. This document records what the UI actually shows for each reachable cell.

**Unreachable cells:**
- `integrity=UNKNOWN` with `scored=true` — the engine never produces an UNKNOWN integrity when scoring succeeds. UNKNOWN integrity only appears when pricing fails entirely, which also means no scoring.
- `findings=empty` — the `findings` block is either absent (no checks runner) or present with content. There is no "checked, nothing found" state in the current implementation because the checks runner always produces results when configured.

## Reachable Cells

### Cell 1: DIRECT × scored=true × live=true × findings=present

**Request:** `GET /api/corridor?to=NGNC&live=1`
**Timestamp:** 2026-09-23T17:23:52Z

| Field | Value |
|:---|:---|
| `integrity` | `"DIRECT"` |
| `scored` | `true` |
| `live` | `true` |
| `findings` | Present — 7 checks (SEP-10 failed, SEP-24 failed, SEP-38 not published, etc.) |
| `recommended` | Present (GOOD, 0% loss) |
| `rungs` | 12 priced rungs |

**UI rendering:**
1. Integrity badge: `DIRECT` (green, "Independent market found")
2. Finding text: "No usable size..." — but note: this finding is now stale because live data shows a GOOD route at 0.1 USDC
3. Recommendation block: Present (rec-some)
4. Curve: Rendered (loss vs mid by size)
5. Table: All 12 rungs shown with Send, Receive, Rate, Loss, Verdict, Path
6. Findings panel: Present with 7 checks (2 FAIL, 5 undetermined)
7. Provenance: "LIVE MEASUREMENT" banner

**Contradiction:** The stored finding text says "No usable size. Loss against the exchangerate-api mid is 27.15% at 0.1 USDC" but the live data now shows a GOOD route at 0.1 USDC. The case study text in the UI still references "about 25% at dust size" which contradicts the live measurement.

### Cell 2: DERIVATIVE × scored=true × live=true × findings=present

**Request:** `GET /api/corridor?to=GHSC&live=1`
**Timestamp:** 2026-09-23T17:23:52Z

| Field | Value |
|:---|:---|
| `integrity` | `"DERIVATIVE"` |
| `scored` | `true` |
| `live` | `true` |
| `findings` | Present — 7 checks |
| `recommended` | `null` |
| `rungs` | 12 priced rungs |

**UI rendering:**
1. Integrity badge: `DERIVATIVE` (amber, "Depends on an upstream corridor")
2. Dependency: `NGNC` shown in integrity card
3. Finding text: "Derivative corridor: every path from USDC to GHSC routes through NGNC..."
4. Recommendation block: "No recommendation. Every size tested is graded Unusable" (rec-none)
5. Curve: Rendered (loss rising with size, all UNUSABLE)
6. Table: All 12 rungs with Loss and Verdict columns
7. Case study: "This corridor has no independent market: every path routes through NGNC, so its figures compound NGNC's cost with its own."

**Verification:** The UI correctly shows the dependency in the integrity card and the case study text. However, the dependency is shown in prose only — there is no structured field that a consumer can extract programmatically beyond `depends_on` in the JSON.

### Cell 3: NO-MARKET × scored=true × live=true × findings=present

**Request:** `GET /api/corridor?to=KESC&live=1`
**Timestamp:** 2026-09-23T17:23:53Z

| Field | Value |
|:---|:---|
| `integrity` | `"NO-MARKET"` |
| `scored` | `true` |
| `live` | `true` |
| `findings` | Present — 7 checks |
| `recommended` | `null` |
| `rungs` | 0 priced, 12 unpriced |

**UI rendering:**
1. Integrity badge: `NO-MARKET` (red, "No market exists")
2. Finding text: "No market. Horizon returned no path from USDC to KESC at any of the 12 sizes tested."
3. Recommendation block: "No route exists at any size tested. There is nothing to recommend, and nothing to price."
4. Curve: **Not rendered** (because `scored && priced.length` is false — no priced rungs)
5. Table: Shows all 12 rungs as unpriced with "no path exists at this size" in the error column
6. Case study: "This corridor has no market at all, which is a different result from expensive pricing and is reported as such."

**Verification:** The page degrades honestly. No empty table is rendered — the table shows all rungs with the NO-MARKET integrity cell and the error message. The curve is absent (correctly, since there is no data to plot).

### Cell 4: DIRECT × scored=false × live=true × findings=present

**Request:** `GET /api/corridor?to=NGNC&live=1` with reference providers in DISAGREE state
**Status:** **NOT REACHED IN THIS TEST**

The live deployment currently has both reference providers agreeing (AGREE, divergence 0.2612%). To reach this cell, the reference providers would need to diverge beyond the disagreement threshold. This cell is not currently reachable on the live deployment.

**Expected UI rendering (from code analysis):**
- Integrity badge shown (structural state is independent of scoring)
- `unscoredBlock` rendered instead of `recommendationBlock`
- No loss curve (because `scored && priced.length` is false)
- Table shows Send, Receive, Rate, Path columns but NOT Loss or Verdict
- No verdicts or recommendations shown

### Cell 5: DIRECT × scored=true × live=false × findings=present

**Request:** `GET /api/corridor?to=NGNC` (no `live=1`, serves from history)
**Timestamp:** 2026-09-23T17:23:53Z

| Field | Value |
|:---|:---|
| `integrity` | `"DIRECT"` |
| `scored` | `true` |
| `live` | `false` |
| `stale.age_human` | `"32d ago"` |
| `findings` | Present — 7 checks |

**UI rendering:**
1. Integrity badge: `DIRECT`
2. Provenance: "RECORDED — NOT CURRENT MARKET DATA" banner
3. Recommendation block: Present (from stored data)
4. Table: From stored rungs
5. Findings panel: Present
6. Footer provenance: "The reading below was recorded 32d ago..."

**Verification:** The stale banner works correctly. The `live` field is `false`, and the `stale` block is present. The provenance footer correctly identifies this as not current market data.

### Cell 6: DIRECT × scored=false × live=false × findings=present

**Status:** **NOT REACHED** — requires stored data where reference providers disagreed. The stored records all have `scored: true`.

### Cell 7: DERIVATIVE × scored=false × live=true × findings=present

**Status:** **NOT REACHED** — requires a DERIVATIVE corridor with disagreeing reference providers.

### Cell 8: NO-MARKET × scored=false × live=true × findings=present

**Status:** **NOT REACHED** — requires a NO-MARKET corridor with disagreeing reference providers. In practice, NO-MARKET is determined by pathfinding, not by reference agreement.

### Cell 9: UNKNOWN × scored={true,false} × live={true,false} × findings=present

**Status:** **NOT REACHED** — `IntegrityUnknown` is only set when pricing fails for an unrelated reason. If pricing fails, there are no quotes to score, so `scored` would be false.

### Cell 10: DIRECT × scored=true × live=true × findings=absent

**Request:** `GET /api/corridor?to=NGNC&live=1` with `Checks` disabled on server
**Status:** **NOT REACHED ON LIVE DEPLOYMENT** — the server has `Checks` configured. To reach this cell, the checks runner would need to be nil.

**Expected rendering:** No findings panel. The measurement panel would show only integrity, finding, recommendation, curve, and table.

### Cell 11: DIRECT × scored=true × live=true × findings=empty

**Status:** **NOT REACHED** — The `findings` field in `CorridorJSON` is either `*checks.FindingsJSON` (present with content) or absent (nil). There is no "checked, nothing found" state because the checks runner always produces results when configured. The `WithFindings` function returns the corridor unchanged when there are no checks and no metrics, meaning the `findings` key is absent, not empty.

### Cell 12: UNKNOWN × scored=false × live=true × findings=absent

**Status:** **NOT REACHED** — requires complete upstream failure with no checks configured.

## Summary Table

| integrity | scored | live | findings | Reached? | Status |
|:---|:---|:---|:---|:---|:---|
| DIRECT | true | true | present | ✅ | Observed — Cell 1 |
| DERIVATIVE | true | true | present | ✅ | Observed — Cell 2 |
| NO-MARKET | true | true | present | ✅ | Observed — Cell 3 |
| DIRECT | false | true | present | ❌ | Not reachable on live deployment |
| DIRECT | true | false | present | ✅ | Observed — Cell 5 |
| DERIVATIVE | false | true | present | ❌ | Not reachable on live deployment |
| NO-MARKET | false | true | present | ❌ | Not reachable on live deployment |
| UNKNOWN | {any} | {any} | {any} | ❌ | Not reachable on live deployment |
| DIRECT | true | true | absent | ❌ | Not reachable on live deployment |
| DIRECT | true | true | empty | ❌ | Not reachable (no empty findings state) |
| UNKNOWN | false | true | absent | ❌ | Not reachable on live deployment |

## Contradictions Found

1. **Case study text is stale:** The UI case study says NGNC "loses about 25% at dust size" but the live measurement now shows a GOOD route at 0.1 USDC (0% loss). The stored data from 2026-08-22 still shows 27.15%.

2. **Finding text is stale:** The stored finding for NGNC says "No usable size" but the live measurement now has a recommended route. The finding text in the live response was regenerated and now says the correct thing for the current state.

3. **Curve rendering for NO-MARKET:** The curve is correctly absent when there are no priced rungs, but there is no explicit "no data to chart" message — the panel is simply empty except for the table.

4. **The `findings` key absence vs empty:** The UI treats absent `findings` as "not checked" and present `findings` as "checked." There is no visual distinction between "no checks configured" and "checks returned nothing" because both result in the same rendering (no findings panel).

## Filed as Separate Issues

- **#266** — QA every error path in the browser (five distinct causes sharing one panel)
- **#267** — QA the stale banner once it exists (stale reading rendering as live)
- **#245** — Make integrity states visually distinct (UNKNOWN falls through to DIRECT style)
- **#232** — Drive the corridor selector from `/api/assets` (currently hardcoded)
- **#234** — Make the corridor state URL-addressable
