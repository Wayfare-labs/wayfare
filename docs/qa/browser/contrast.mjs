// contrast.mjs — WCAG 2.2 contrast for the UI palette, parsed from source.
//
// Backlog H3, issue #273. This reads the actual custom-property values out of
// server/index.html rather than re-typing hexes, so the numbers in the audit
// artifact cannot silently drift from the stylesheet they describe. It needs
// only Node — no browser, no network — which is why the colour half of the
// audit is reproducible here even though the browser/AT half is not.
//
//   node contrast.mjs            # print the table
//   node contrast.mjs --json     # also write results/contrast.json
//
// Contrast method: WCAG 2.x relative luminance; sRGB over-alpha composite for
// the two translucent pairings (`.b-derivative`, `.provenance`), whose text
// sits on a tint, not a solid fill. Thresholds: 4.5:1 normal text (1.4.3),
// 3:1 large text / non-text (1.4.11, 1.4.3 large-text).

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(here, '..', '..', '..', 'server', 'index.html'), 'utf8');

// Grab every `name: value;` declaration from a CSS block into a map.
const parseTokens = (block) => {
  const map = {};
  const re = /(--[a-z0-9-]+)\s*:\s*([^;]+);/g;
  let m;
  while ((m = re.exec(block))) map[m[1]] = m[2].trim();
  return map;
};

// The light :root is the first block; dark is inside the media query and only
// overrides primitives. Merging lets a primitive added to light fall through
// when dark does not redefine it.
const lightBlock = html.slice(html.indexOf(':root {'), html.indexOf('@media (prefers-color-scheme'));
const darkBlock = html.slice(html.indexOf('prefers-color-scheme: dark'));
const lightTokens = parseTokens(lightBlock);
const darkTokens = { ...lightTokens, ...parseTokens(darkBlock) };

// Resolve a custom property through its var() chain to a hex literal.
const hex = (scheme, name) => {
  const tokens = scheme === 'dark' ? darkTokens : lightTokens;
  let value = tokens[name];
  const seen = new Set();
  while (value && !value.startsWith('#')) {
    const ref = value.match(/var\((--[a-z0-9-]+)/);
    if (!ref || seen.has(ref[1])) return null; // unresolved or cyclic
    seen.add(ref[1]);
    value = tokens[ref[1]];
  }
  return value && value.startsWith('#') ? value : null;
};

const rgb = (h) => {
  h = h.replace('#', '');
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
};
const chan = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; };
const lum = (h) => { const [r, g, b] = rgb(h); return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b); };
const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
const over = (fg, alpha, bg) => {
  const f = rgb(fg), g = rgb(bg);
  return '#' + f.map((c, i) => Math.round(c * alpha + g[i] * (1 - alpha)).toString(16).padStart(2, '0')).join('');
};

// name, foreground token, background (token | {mix:[token,alpha]}), role.
const PAIRINGS = [
  ['body / headings (ink on panel)', '--ink', '--panel', 'text'],
  ['.sub .meta th .axis (muted on panel)', '--muted', '--panel', 'text'],
  ['.v-poor .cs-derivative (warn on panel)', '--warn', '--panel', 'text'],
  ['.v-good .v-fair .rec-some (ok on panel)', '--ok', '--panel', 'text'],
  ['.err .rec-none .v-unusable (bad on panel)', '--bad', '--panel', 'text'],
  ['.f-pass .b-direct (ok on ok-soft)', '--ok', '--ok-soft', 'text'],
  ['.f-fail .b-nomarket (bad on bad-soft)', '--bad', '--bad-soft', 'text'],
  ['.f-unknown .m-state (neutral on neutral-tint)', '--unknown', '--unknown-soft', 'text'],
  ['.b-derivative (warn on 12% warn over panel)', '--warn', { mix: ['--warn', 0.12, '--panel'] }, 'text'],
  ['.provenance RECORDED banner (warn on 10% warn over bg)', '--warn', { mix: ['--warn', 0.10, '--bg'] }, 'text'],
  ['empty-state glyph (neutral on panel, non-text)', '--availability-undetermined', '--panel', 'non-text'],
  ['panel border vs panel (non-text)', '--border', '--panel', 'non-text'],
];

const bgOf = (scheme, bg) => (typeof bg === 'string' ? hex(scheme, bg) : over(hex(scheme, bg.mix[0]), bg.mix[1], hex(scheme, bg.mix[2])));

const out = [];
for (const scheme of ['light', 'dark']) {
  for (const [name, fg, bg, role] of PAIRINGS) {
    const r = ratio(hex(scheme, fg), bgOf(scheme, bg));
    const need = role === 'non-text' ? 3 : 4.5;
    out.push({ scheme, element: name, ratio: +r.toFixed(2), need, pass: r >= need });
  }
}

for (const row of out) {
  const flag = row.pass ? 'pass' : 'FAIL';
  console.log(`${row.scheme.padEnd(5)} ${row.ratio.toFixed(2).padStart(5)} (need ${row.need}) ${flag.padEnd(4)} ${row.element}`);
}

if (process.argv.includes('--json')) {
  mkdirSync(join(here, 'results'), { recursive: true });
  writeFileSync(join(here, 'results', 'contrast.json'), JSON.stringify({
    generatedFrom: 'server/index.html', method: 'WCAG 2.x relative luminance', results: out,
  }, null, 2) + '\n');
  console.log('\nwrote results/contrast.json');
}
