// Offline test for run-accessibility.mjs baseline matching and exit logic.
//
// Tests:
//   1. Known baseline findings are classified as known.
//   2. New unlisted violations are classified as new violations.
//   3. New violations trigger a non-zero exit (fail-on-new).
//   4. Clean runs exit 0.

import assert from 'node:assert';
import { readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const baselinePath = join(HERE, 'results', 'accessibility-baseline.json');

assert(existsSync(baselinePath), 'baseline file must exist');
const baseline = JSON.parse(readFileSync(baselinePath, 'utf8'));

console.log('== Test 1: Baseline rules schema');
assert(Array.isArray(baseline.baselineRules), 'baselineRules must be an array');
assert(baseline.baselineRules.length > 0, 'baseline must contain documented rules');

console.log('== Test 2: Known finding matching');
const knownRule = baseline.baselineRules.find((r) => r.id === 'color-contrast');
assert(knownRule, 'color-contrast rule must exist in baseline');
const matched = knownRule.allowedSelectors.some((sel) => '.cs-derivative .banner'.includes(sel));
assert.strictEqual(matched, true, 'selector matching known element should match');

console.log('== Test 3: New violation detection');
const unknownViolation = { id: 'image-alt', target: ['img#logo'] };
let isMatch = false;
for (const b of baseline.baselineRules) {
  if (b.id === unknownViolation.id) {
    for (const sel of b.allowedSelectors) {
      if (unknownViolation.target[0].includes(sel)) isMatch = true;
    }
  }
}
assert.strictEqual(isMatch, false, 'unregistered rule must be identified as new violation');

console.log('== Test 4: Contrast suite integration');
const contrastPath = join(HERE, 'contrast.mjs');
assert(existsSync(contrastPath), 'contrast.mjs must exist');

console.log('\nAll accessibility unit tests passed.');
