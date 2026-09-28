---
issue: 274
title: "Screen-reader walkthrough of a full measurement"
date: "2026-09-26"
---

# Screen-reader walkthrough of a full measurement

## What was observed

When performing a measurement by clicking "Measure live" or "Show trend", the application dynamically renders the results into the `<div id="out">` element using `innerHTML`.

For a user relying on a screen reader (such as VoiceOver, NVDA, or JAWS), this change is **completely silent**. Because the container lacks an `aria-live` attribute, the screen reader does not announce that the measurement has completed or that the content of the page has updated.

## Steps to reproduce

1. Turn on a screen reader (e.g., VoiceOver on macOS, NVDA on Windows).
2. Navigate to the Wayfare interface.
3. Select a corridor in the dropdown (e.g., `USDC → NGNC (naira)`).
4. Navigate to the "Measure live" button and activate it.
5. Notice that the button state changes and visually the text "measuring live..." appears, then a few moments later the full results panel is displayed.
6. **Observation:** The screen reader remains silent throughout this entire lifecycle. The results are not announced, and the user must manually navigate down the DOM to discover if anything changed.

## Technical details

The target container in `server/index.html` is:
```html
<div id="out">
  <!-- Dynamic content injected here -->
</div>
```

The injection happens in `measure()` and `loadTrend()` via:
```javascript
$('out').innerHTML = parts.join('');
```

## Impact

This is a critical accessibility issue. Users who cannot see the screen have no feedback that their action was successful, how long it took, or what the outcome was. The application violates WCAG guidelines for dynamic content updates.

## Next Actions

As instructed by the issue constraints, this is a report of the observed behavior only. The fix (such as adding `aria-live="polite"` to the container or managing focus) should be implemented in a separate PR.
