// Issue #272 — QA both colour schemes.
//
// The dark block in server/index.html had never been reviewed. This records
// what the browser actually resolves in each scheme, rather than what the
// stylesheet's hex values suggest it should.
//
// Usage:
//
//   node run-color-schemes.mjs [--base=URL] [--engine=chromium]
//
// It captures, per scheme, the resolved value of every colour custom property
// and the computed colour of every element whose contrast is asserted in
// artifacts/272-color-schemes.md, then screenshots the live and trend views.
//
// Honesty note the artifact repeats: Chromium only in this environment. The
// colour variables are engine-independent CSS values, so the resolution is not
// expected to differ, but "not expected to differ" is not "measured", and only
// Chromium was measured here.

import { launch, settle, writeResult, shoot, now, parseArgs } from './lib.mjs';

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const ENGINE = String(args.engine || 'chromium');

// The colour custom properties the stylesheet declares. Order is the file's.
const TOKENS = [
  '--bg', '--panel', '--border', '--ink', '--muted', '--accent',
  '--bad', '--warn', '--ok', '--ok-soft', '--bad-soft', '--unknown',
  '--unknown-soft', '--grid',
];

// Selectors whose resolved colour matters, as they appear in the artifact's
// matrices. Absent elements are recorded as null rather than silently skipped,
// because "the element was not on screen" is itself an observation.
const ELEMENTS = [
  ['body', 'body'],
  ['sub', '.sub'],
  ['meta', '.meta'],
  ['case', '.case'],
  ['caseStrong', '.case strong'],
  ['panel', '.panel'],
  ['panelErr', '.panel.err'],
  ['provenance', '.provenance'],
  ['provenanceLive', '.provenance-live'],
  ['mValue', '.m-value'],
  ['mUnit', '.m-unit'],
  ['mState', '.m-state'],
  ['integrityHelp', '.integrity-help'],
  ['integrityDependency', '.integrity-dependency'],
  ['csInfo', '.cs-info'],
  ['badge', '.badge'],
  ['badgeDirect', '.b-direct'],
  ['badgeDerivative', '.b-derivative'],
  ['badgeNomarket', '.b-nomarket'],
  ['findingRowPass', '.finding-row:has(.f-pass)'],
  ['findingRowFail', '.finding-row:has(.f-fail)'],
  ['findingRowUnknown', '.finding-row:has(.f-unknown)'],
  ['chipPass', '.f-pass'],
  ['chipFail', '.f-fail'],
  ['chipUnknown', '.f-unknown'],
  ['findingId', '.f-id'],
  ['findingLimit', '.f-limit'],
  ['findingEvidence', '.f-evidence'],
  ['findingSummary', '.f-summary'],
  ['tableHead', 'th'],
  ['tableCell', 'td.num'],
  ['scroll', '.scroll'],
  ['legend', '.trend-legend'],
  ['footer', 'footer'],
  ['recommendNone', '.rec-none'],
  ['recommendSome', '.rec-some'],
  ['verdictPoor', '.v-poor'],
  ['verdictGood', '.v-good'],
];

const PROPS = ['color', 'backgroundColor', 'borderTopColor', 'borderLeftColor', 'fill', 'fontSize'];

async function capture(page) {
  return page.evaluate(
    ({ tokens, elements, props }) => {
      const root = document.documentElement;
      const rcs = getComputedStyle(root);
      const tokenValues = {};
      for (const t of tokens) tokenValues[t] = rcs.getPropertyValue(t).trim();

      const resolved = {};
      for (const [name, sel] of elements) {
        const el = document.querySelector(sel);
        if (!el) {
          resolved[name] = null;
          continue;
        }
        const cs = getComputedStyle(el);
        const rec = { selector: sel };
        for (const p of props) rec[p] = cs[p];
        resolved[name] = rec;
      }

      return {
        prefersDark: matchMedia('(prefers-color-scheme: dark)').matches,
        tokens: tokenValues,
        elements: resolved,
        horizontalOverflow: (() => {
          const d = document.scrollingElement;
          return { scrollWidth: d.scrollWidth, innerWidth: window.innerWidth };
        })(),
      };
    },
    { tokens: TOKENS, elements: ELEMENTS, props: PROPS },
  );
}

const out = {
  issue: '#272 — QA both colour schemes',
  ranAt: now(),
  harness: 'docs/qa/browser/run-color-schemes.mjs',
  base: BASE,
  engine: ENGINE,
  scope: {
    enginesRun: [ENGINE],
    caveat:
      'Only Chromium was available in this environment. The colour variables are engine-independent CSS values and are not expected to differ, but that is not measured here.',
    screenshots: 'live and trend views, light and dark',
  },
  schemes: {},
  consoleErrors: [],
};

const { browser, context, version } = await launch(ENGINE);
out.browserVersion = version;

for (const scheme of ['light', 'dark']) {
  const ctx = await browser.newContext({
    colorScheme: scheme,
    locale: 'en-GB',
    timezoneId: 'UTC',
  });
  const page = await ctx.newPage();
  const consoleErrors = [];
  page.on('console', (m) => m.type() === 'error' && consoleErrors.push(m.text()));

  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
  await settle(page, 20000);

  const live = await capture(page);
  const liveShot = await shoot(page, `color-schemes-${scheme}-live`);

  // The trend view draws the integrity strip, whose label sits over a
  // 0.75-opacity fill — the one place a colour is composited rather than
  // painted, so it gets its own capture.
  let trend = null;
  let trendShot = null;
  if (await page.$('#trend')) {
    await page.click('#trend');
    await settle(page, 20000);
    trend = await capture(page);
    trend = {
      ...trend,
      strip: await page.evaluate(() => {
        const rects = [...document.querySelectorAll('#out svg rect')];
        return rects.slice(0, 6).map((r) => {
          const cs = getComputedStyle(r);
          return {
            fill: cs.fill,
            opacity: cs.opacity,
            textFill: (() => {
              const t = r.nextElementSibling;
              return t && t.tagName.toLowerCase() === 'text' ? getComputedStyle(t).fill : null;
            })(),
          };
        });
      }),
    };
    trendShot = await shoot(page, `color-schemes-${scheme}-trend`);
  }

  out.schemes[scheme] = {
    prefersDark: live.prefersDark,
    live,
    liveScreenshot: liveShot,
    trend,
    trendScreenshot: trendShot,
  };
  out.consoleErrors.push(...consoleErrors.map((e) => ({ scheme, text: e })));

  await ctx.close();
}

await context.close();
await browser.close();

const file = writeResult('color-schemes.json', out);
console.log(`wrote ${file}`);
for (const scheme of ['light', 'dark']) {
  const s = out.schemes[scheme];
  console.log(`${scheme}: prefers-dark=${s.prefersDark} bg=${s.live.tokens['--bg']} ink=${s.live.tokens['--ink']} muted=${s.live.tokens['--muted']}`);
  const missing = Object.entries(s.live.elements).filter(([, v]) => v === null).map(([k]) => k);
  if (missing.length) console.log(`  not on screen in ${scheme}: ${missing.join(', ')}`);
}
if (out.consoleErrors.length) {
  console.log(`console errors (${out.consoleErrors.length}):`);
  for (const e of [...new Set(out.consoleErrors.map((x) => x.text))]) console.log(`  - ${e}`);
}
