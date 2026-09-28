# Design Finding: The Boundary Between Measurement and Inference in the UI

**Date:** 2026-09-25
**Status:** Research finding — no implementation attempted
**Backlog entry:** #147 / GitHub #208 (Initiative E2 — Predictive Intelligence V5)

---

## 1. Executive Summary

This document establishes the visual contract for distinguishing **measurement** (Layers 1–2: observable facts + deterministic calculations) from **inference** (Layer 3: probabilistic inference) in the Wayfare UI. The four-layer epistemic model governs: a layer can never be more certain than the layer beneath it. If inference is ever published, it must be unmistakable at a glance.

**Conclusion:** The boundary can be designed now using only existing wire fields and CSS tokens. No API changes are required. The design uses three mechanisms that compose: a **provenance banner**, a **figure-level inference badge**, and a **panel-level inference container**. All three are already patterned in the codebase for `live`/`stale`, `scored`/`unscored`, and `determined`/`undetermined` distinctions.

---

## 2. Source Material (verified 2026-09-25)

| Source | Location | What it defines |
|--------|----------|-----------------|
| Four-layer model | `docs/backlog.md:124–137` | V1–V6 mapping, governing rule |
| Wire contract | `route/wire.go:19–174` | `CorridorJSON`, `QuoteJSON`, `RungJSON`, `CostPartJSON` |
| Findings wire | `checks/wire.go:33–76` | `MetricJSON`, `FindingsJSON` — `Determined` + `Reason` pattern |
| Live/stale UI | `server/index.html:403–408` | Provenance banner pattern |
| Metrics UI | `server/index.html:611–655` | "Undetermined is not a failure" pattern, value+reason rendering |
| Integrity badges | `server/index.html:67–79, 256–301` | Structural states (DIRECT/DERIVATIVE/NO-MARKET) not severities |
| Verdict thresholds | `server/index.html:657–674` | GOOD≤3% · FAIR≤8% · POOR≤20% · UNUSABLE>20% |
| Checks UI | `server/index.html:563–608` | Three states (PASS/FAIL/UNKNOWN), never two |
| CSS tokens | `server/index.html:8–41` | Semantic colours (--ok, --bad, --warn, --unknown) |

---

## 3. What Exists Today (Measurement Only)

All published figures today are **measurements** (Layers 1–2). The UI already distinguishes:

| Dimension | Wire signal | UI treatment |
|-----------|-------------|--------------|
| **Provenance** | `live: true/false` + `stale` block | Banner: "LIVE MEASUREMENT" vs "RECORDED — NOT CURRENT MARKET DATA" |
| **Scorability** | `scored: true/false` + `reference_agreement` | Unscored: no loss/verdict columns, explanatory block ("no verdict can be issued") |
| **Integrity** | `integrity: DIRECT/DERIVATIVE/NO-MARKET` | Badges: ◆ DIRECT (ok), ↪ DERIVATIVE (warn), ∅ NO-MARKET (bad) — structural, not severity |
| **Verdict** | `verdict: GOOD/FAIR/POOR/UNUSABLE` | Colour classes: `.v-good`, `.v-fair`, `.v-poor`, `.v-unusable` |
| **Checks** | `determined: true/false`, `passed: true/false` | Three states: PASS (ok), FAIL (bad), UNKNOWN (unknown) — undetermined ≠ failure |
| **Metrics** | `determined: true/false`, `value` + `unit` / `reason` | Determined: value+unit; Undetermined: "UNDETERMINED" badge + reason — no number invented |

**Key discipline already in code:** `CostPartJSON` (route/wire.go:48–54) omits `Amount`/`Pct` entirely when `Determined: false` — an undetermined component carries its reason and **no number at all**. This is the pattern inference must follow.

---

## 4. What Inference Would Add (Layer 3)

Per the backlog (docs/backlog.md:108–110, 970–1000), Layer 3 (V5) would publish:

- **Failure probability** — "this corridor has a 12% chance of failing to price at $500"
- **Expected slippage** — "at $1,000 the expected additional loss beyond measured depth is 1.8%"
- **Anomaly flags** — "loss at $100 deviates 2.3σ from the corridor's history"

**Critical constraint:** None of these exist in the engine today. The `runstore` records headline figures only (no metrics, no checks — backlog #62). Any inference would be computed *from* stored measurements, not measured directly.

---

## 5. Design Principle: The Inference Contract

> **If a figure was not produced by the measurement engine, it must not look like one that was.**

This extends the existing rule for metrics (server/index.html:611–617): *"A metric that could not be measured shows why instead, styled as unknown — a number that was not produced is a different fact from a check that failed, and must not look like one."*

For inference, the rule is stronger: **an inferred number is not a measurement at all**, and the UI must make that distinction unmistakable at a glance, without requiring the reader to read fine print.

---

## 6. Proposed Visual Mechanisms

### 6.1 Provenance Banner Extension (Page Level)

**Current:** Banner shows `live` vs `stale` (server/index.html:403–408).

**Extension:** Add a third mode — `inference` — when any Layer 3 figure is present.

```html
<!-- Current live -->
<div class="provenance provenance-live">
  <strong>LIVE MEASUREMENT</strong> · measured 2026-09-25T14:30:00Z
</div>

<!-- Current stale -->
<div class="provenance">
  <strong>RECORDED — NOT CURRENT MARKET DATA</strong> · 6h ago · recorded 2026-09-25T08:30:00Z
</div>

<!-- Proposed inference -->
<div class="provenance provenance-inference">
  <strong>CONTAINS INFERENCE</strong> · measurements from 2026-09-25T14:30:00Z · probabilistic estimates below
</div>
```

**CSS token:** `--inference` (new), `--inference-soft` (new). Colour: distinct from `--ok`, `--bad`, `--warn`, `--unknown`. Suggest: a muted violet (`#7c6fd0` / `--inference-soft: #efeefc`) — not used elsewhere in the semantic palette, signalling "this is a different epistemic category".

**Rule:** The banner appears if **any** inference figure is rendered on the page. It does not replace `live`/`stale`; it compounds: a stale reading containing inference shows both badges.

---

### 6.2 Figure-Level Inference Badge (Per-Value)

Every inferred figure carries an inline badge, using the existing badge machinery (server/index.html:67–79, 256–301).

```html
<!-- Measurement (existing) -->
<span class="m-value">0.8472<span class="m-unit">NGNC/USDC</span></span>

<!-- Inference (proposed) -->
<span class="m-value">
  <span class="m-state m-inference">INFERRED</span>
  0.8321<span class="m-unit">NGNC/USDC</span>
</span>
```

**CSS:**
```css
.m-inference {
  color: var(--inference);
  background: var(--inference-soft);
  border-color: var(--inference);
}
.m-inference::before { content: "◆"; }  /* distinct from ◆/↪/∅/?/✓/× */
```

**Placement:** Before the value, same visual weight as the `UNDETERMINED` badge for metrics (server/index.html:625–626). The value itself renders in normal ink (`--ink`), not a semantic colour — the badge carries the category.

**Tooltip/aria-label:** "Inferred figure — not a measurement. Computed from historical observations using [method name]. See methodology."

---

### 6.3 Panel-Level Inference Container (Grouping)

When multiple inference figures appear together (e.g., a failure-probability curve across sizes), they are wrapped in a distinct panel, analogous to the existing Metrics panel (server/index.html:646–655).

```html
<div class="panel panel-inference">
  <h2>Failure Probability <span class="inference-tag">INFERRED</span></h2>
  <!-- inference figures here -->
  <p class="meta inference-meta">
    These are probabilistic estimates, not measurements. Each is computed from
    stored history using [method: Bayesian logistic regression on 1,080 runs].
    No threshold exists for any of them. <strong>Inference is not a measurement</strong> —
    it describes what <em>might</em> happen, not what <em>did</em> happen.
  </p>
</div>
```

**CSS:**
```css
.panel-inference {
  border-color: var(--inference);
  background: color-mix(in srgb, var(--inference) 4%, var(--panel));
}
.inference-tag {
  font: 600 .6rem/1 var(--mono);
  color: var(--inference);
  background: var(--inference-soft);
  padding: .1rem .35rem; border-radius: 3px; border: 1px solid var(--inference);
  text-transform: uppercase; letter-spacing: .05em;
}
.inference-meta { border-top: 1px solid var(--inference); padding-top: .7rem; }
```

---

### 6.4 Curve/Chart Distinction

If an inference curve is drawn (e.g., expected slippage vs size), it **must not share the measurement curve's visual language**.

| Aspect | Measurement curve (existing) | Inference curve (proposed) |
|--------|------------------------------|----------------------------|
| Line colour | `var(--bad)` (red) | `var(--inference)` (violet) |
| Line style | Solid | Dashed (4, 3) |
| Dots | Filled circles | Hollow diamonds |
| Threshold line | 20% UNUSABLE (red dashed) | None — inference has no verdict thresholds |
| Axis label | "Loss against mid" | "Expected additional loss (inferred)" |
| Legend | "Loss against mid rising with trade size" | "Probabilistic estimate — not measured" |

**Rule from existing code (server/index.html:492–493):** *"Must not draw a line through sizes that did not price."* — applies equally: inference curves only connect sizes where inference was actually computed.

---

### 6.5 Wire Contract for Inference (No API Change Required)

The existing wire shape **already supports** carrying inference alongside measurement without conflation:

1. **`FindingsJSON.Metrics`** (checks/wire.go:75) — `MetricJSON` has `Determined`, `Value`/`Unit` OR `Reason`, `Venue`, `Summary`, `Evidence`. An inference metric would set:
   - `Determined: true` (it *is* determined — the model produced a number)
   - `Value: "0.12"`, `Unit: "probability"`
   - `Venue: "inference"` (new venue value — see below)
   - `Summary: "Failure probability at $500 via Bayesian logistic regression"`
   - `Evidence: [{source: "runstore", observed: "1080 runs", observed_at: "..."}]`

2. **New `Venue` value:** `"inference"` — distinct from `"order-book"` and `"pathfinding"` (checks/wire.go:45–50). The existing comment: *"A consumer must never reconcile two figures with different venues by arithmetic."* Inference is a third venue that must never be arithmetically combined with measured venues.

3. **`ExecutionRateCurveJSON`** (route/wire.go:70–75) — already carries `NonMonotonic` flag and `ObservationCount`. An inference curve would be a separate `InferenceCurveJSON` block (new field on `CorridorJSON`), not mixed into `Curve`.

**No new API fields required** for the UI design — the UI reads what the engine emits. If the engine never emits inference, the UI never renders it. This satisfies the constraint: *"If this work needs a field the API does not expose, stop — that is a backend data-contract issue first."*

---

## 7. State Matrix: Measurement vs Inference

| State | Measurement (L1–2) | Inference (L3) |
|-------|-------------------|----------------|
| **Provenance** | LIVE / RECORDED | CONTAINS INFERENCE (compounds with above) |
| **Figure badge** | None (value speaks) | `INFERRED` badge (violet) |
| **Panel** | Standard / `panel-inference` for metrics | `panel-inference` always |
| **Curve** | Solid red, filled dots | Dashed violet, hollow diamonds |
| **Thresholds** | 3/8/20% (verdict bands) | None — no verdicts on inference |
| **Undetermined** | "UNDETERMINED" + reason (grey) | N/A — inference either computes or doesn't |
| **Copy** | "measured", "priced", "loss against mid" | "estimated", "probabilistic", "expected", "might" |
| **Epistemic claim** | "This is what the market showed at this time" | "This is what a model projects from history" |

---

## 8. Negative Findings (What This Design Does NOT Do)

| Proposed approach | Why rejected |
|-------------------|--------------|
| **Confidence intervals on measurement figures** | Would blur the boundary. A measured loss of 12.3% is 12.3% — the reference agreement and divergence already express benchmark uncertainty. Adding ± on top implies a precision the engine does not claim. |
| **Blending inference into the loss curve** | `"Must not draw a line through sizes that did not price"` (server/index.html:492). Inference between measured points is exactly the interpolation the project forbids. |
| **A single "confidence score" for the corridor** | Backlog #149: *"Integrity is deliberately carried alongside the verdict because collapsing them discards the reason. A score collapses further."* |
| **Colour-coding inference by "risk level"** | Inference is not a severity. A 12% failure probability is not "better" than 15% in the way GOOD is better than POOR. The violet badge is categorical, not ordinal. |
| **Hiding inference behind a toggle** | If it's published, it must be unmistakable — not opt-in. The provenance banner is always visible when inference is present. |
| **Using `--warn` (amber) for inference** | Amber already means DERIVATIVE integrity (structural) and POOR verdict (measured loss 8–20%). Inference is a different epistemic category — needs its own colour. |

---

## 9. Accessibility & Reduced Motion

- **Colour not sole carrier:** The `INFERRED` badge uses text + icon (◆) + colour, matching the existing integrity badge pattern (server/index.html:67–79).
- **Screen readers:** `aria-label="Inferred figure — not a measurement. Computed from historical observations using Bayesian logistic regression."` on every badge.
- **Reduced motion:** Inference curves use dashed lines (static), no animation. Panel expansion respects `prefers-reduced-motion` (future work #252).

---

## 10. Implementation Readiness Checklist

| Item | Status | Notes |
|------|--------|-------|
| CSS tokens (`--inference`, `--inference-soft`) | Ready to add | Extends existing `:root` block (server/index.html:8–41) |
| Badge component (`.m-inference`) | Ready to add | Pattern exists at server/index.html:134–140, 256–301 |
| Panel variant (`.panel-inference`) | Ready to add | Pattern exists at server/index.html:646–655 |
| Curve renderer (dashed, hollow) | Ready to add | `curve()` and `trendChart()` at server/index.html:469–504, 851–971 |
| Provenance banner variant | Ready to add | Pattern at server/index.html:403–408, 143–145 |
| Wire contract support | **Already present** | `MetricJSON.Venue = "inference"`, separate `InferenceCurveJSON` field if needed |
| Backend data contract | **Not required** | UI reads what engine emits; if engine emits nothing, UI renders nothing |

---

## 11. Conclusion

The boundary between measurement and inference can be designed **today** using only:

1. **Two new CSS custom properties** (`--inference`, `--inference-soft`)
2. **Three CSS rules** (badge, panel, curve variants)
3. **Existing wire fields** (`MetricJSON.Venue`, `FindingsJSON.Metrics`, `CorridorJSON.Curve` + new sibling)

No implementation is attempted as part of this finding. The design is ready for review. When (and if) the measurement engine produces Layer 3 output, the UI contract is defined.

---

## 12. References

- Four-layer model: `docs/backlog.md:124–137` (checked 2026-09-25)
- Wire contract: `route/wire.go:19–174` (checked 2026-09-25)
- Findings wire: `checks/wire.go:33–76` (checked 2026-09-25)
- Live/stale UI: `server/index.html:403–408` (checked 2026-09-25)
- Metrics UI: `server/index.html:611–655` (checked 2026-09-25)
- Integrity badges: `server/index.html:67–79, 256–301` (checked 2026-09-25)
- Verdict thresholds: `server/index.html:657–674` (checked 2026-09-25)
- Checks UI: `server/index.html:563–608` (checked 2026-09-25)
- CSS tokens: `server/index.html:8–41` (checked 2026-09-25)
- CostPartJSON undetermined pattern: `route/wire.go:48–54` (checked 2026-09-25)
- Backlog E2 spikes: `docs/backlog.md:970–1000` (checked 2026-09-25)

---

*This document is a research finding per Initiative E (V4+). It produces no code. Per the governing rule: a layer can never be more certain than the layer beneath it. This design ensures that if Layer 3 output ever reaches the UI, it carries its epistemic category visibly and irreversibly.*