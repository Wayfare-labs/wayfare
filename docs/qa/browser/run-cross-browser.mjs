// Issue #269 — Cross-browser QA matrix.
//
// Drives the real UI in every browser engine available in this environment and
// records, per engine, what was actually rendered: the corridor selector, the
// integrity badge, the measurements table, the findings panel, the trend view,
// the error paths, and the dark colour scheme.
//
// Usage:
//
//   node run-cross-browser.mjs [--base=URL] [--engines=chromium,firefox,webkit]
//
// Honesty note that the artifact repeats: Chromium is not Microsoft Edge and
// WebKit is not Safari. They share rendering engines, which is the part that
// matters for a static page, but the substitutions are recorded rather than
// glossed over. Real Edge and real Safari were not available.

import {
  launch,
  settle,
  snapshotUI,
  writeResult,
  shoot,
  now,
  waitForSelectorSoft,
  ENGINE_NAMES,
} from './lib.mjs';
import { parseArgs } from './lib.mjs';

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const ENGINES = String(args.engines || 'chromium,firefox,webkit').split(',');

// Map each engine to the browser it is here standing in for.
const SUBSTITUTION = {
  chromium: {
    standsInFor: ['Google Chrome', 'Microsoft Edge'],
    caveat:
      'Chromium is Chrome\'s and Edge\'s engine. Edge adds its own UI chrome (title bar, menu, font defaults) but renders this page with the same engine, so no Edge-specific rendering difference is expected — and none was tested.',
  },
  firefox: { standsInFor: ['Mozilla Firefox'], caveat: null },
  webkit: {
    standsInFor: ['Apple Safari'],
    caveat:
      'WebKit is Safari\'s engine on Apple platforms, but Playwright\'s WebKit build is not Safari and does not run on macOS. Safari-specific behaviour (font rendering, scrollbars, system-link handling) cannot be concluded from these results.',
  },
};

async function runChecks(page, engine, hasFindings) {
  const checks = [];
  const add = (id, description, expected, observed, pass) =>
    checks.push({ id, description, expected, observed, pass });

  // 1. The page renders a corridor at all.
  const ui = await snapshotUI(page);
  add(
    'renders_corridor',
    'Initial load renders a corridor document (provenance banner present)',
    true,
    ui.hasProvenance,
    ui.hasProvenance === true,
  );

  // 2. The corridor selector: is it populated from /api/assets?
  const assetCalls = await page.evaluate(() => (window.__qaAssetCalls ?? 0));
  add(
    'selector_populated',
    'Corridor selector lists the measured corridors',
    'a list of corridors, no placeholder',
    { options: ui.selectorOptions, value: ui.selectorValue, disabled: ui.selectorDisabled, assetCalls },
    !!ui.selectorOptions && ui.selectorOptions.length > 1 && !ui.selectorOptions[0].startsWith('Loading'),
  );

  // 3. Integrity badge.
  const badge = await page.evaluate(() => {
    const b = document.querySelector('.integrity-card .badge, .integrity-cell .badge');
    if (!b) return null;
    const cs = getComputedStyle(b);
    return { text: b.textContent.trim(), className: b.className, color: cs.color, background: cs.backgroundColor };
  });
  add('integrity_badge', 'Integrity badge renders with a state and a distinct colour', 'a badge with text', badge, !!badge && !!badge.text);

  // 4. Measurements table + .scroll wrapper.
  const table = await page.evaluate(() => {
    const scroll = document.querySelector('.scroll');
    const t = document.querySelector('#out table');
    if (!t) return { table: false, scroll: !!scroll };
    return {
      table: true,
      scroll: !!scroll,
      rows: t.querySelectorAll('tbody tr').length,
      headers: [...t.querySelectorAll('thead th')].map((h) => h.textContent.trim()),
      scrollClientWidth: scroll?.clientWidth ?? null,
      scrollWidth: scroll?.scrollWidth ?? null,
    };
  });
  add('measurements_table', 'Measurements table renders with rows and headers', 'header row + >=1 data row', table, table.table === true && table.rows > 0);

  // 5. Horizontal overflow stays inside .scroll rather than the page.
  const overflow = await page.evaluate(() => {
    const doc = document.scrollingElement;
    return { docScrollWidth: doc.scrollWidth, innerWidth: window.innerWidth };
  });
  add(
    'no_page_horizontal_overflow',
    'The page itself does not scroll horizontally (any wide table is contained)',
    'doc scrollWidth <= viewport width + 1',
    overflow,
    overflow.docScrollWidth <= overflow.innerWidth + 1,
  );

  // 6. Findings panel, checked against the contract rather than against a
  // fixed expectation: a findings block is rendered exactly when the response
  // carried one. The embedded history predates recorded checks, so its absence
  // here is correct behaviour, not a rendering failure.
  const findings = await page.evaluate(() => {
    const panels = [...document.querySelectorAll('#out .panel')];
    const f = panels.find((p) => /Counterparty checks/.test(p.textContent));
    if (!f) return null;
    return {
      heading: f.querySelector('h2')?.textContent.trim(),
      rows: f.querySelectorAll('.finding-row').length,
      states: [...f.querySelectorAll('.f-state')].map((s) => s.textContent.trim()),
    };
  });
  add(
    'findings_panel_matches_contract',
    'Findings panel is rendered exactly when the response carried a findings block',
    `findings in body: ${hasFindings} -> panel rendered: ${hasFindings}`,
    { hasFindingsInBody: hasFindings, panelRendered: !!findings, detail: findings },
    hasFindings ? !!findings && findings.rows > 0 : findings === null,
  );

  // 7. Trend view.
  await page.click('#trend');
  await settle(page, 15000);
  const trend = await page.evaluate(() => {
    const out = document.getElementById('out');
    return {
      svg: !!out.querySelector('svg'),
      headings: [...out.querySelectorAll('h2')].map((h) => h.textContent.trim()),
      rows: out.querySelectorAll('table tbody tr').length,
    };
  });
  add('trend_view', 'Trend view renders a chart and a stored-runs table', 'svg + headings', trend, trend.svg === true);

  return { checks, uiDuringLoad: ui };
}

async function runErrorPaths(page) {
  const results = {};
  for (const [id, cause] of [
    ['unknown_asset', 'unknown asset'],
    ['network', 'network failure'],
  ]) {
    await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    await settle(page);
    if (id === 'network') {
      await page.route((u) => new URL(u).pathname === '/api/corridor', (r) => r.abort('failed'));
    } else {
      await page.route(
        (u) => new URL(u).pathname === '/api/corridor',
        (r) =>
          r.fulfill({
            status: 400,
            contentType: 'application/json; charset=utf-8',
            body: JSON.stringify({
              code: 'unknown_receive_asset',
              error: 'unknown receive asset "SCAMC"; verified assets are EURMTL, GHSC, KESC, NGNC, NGNT, PYUSD, USDC, USDZ, ZARZ',
            }),
          }),
      );
    }
    await page.click('#run');
    await settle(page, 15000);
    results[id] = await page.evaluate(() => {
      const p = document.querySelector('#out .panel.err');
      return p ? { classes: p.className, text: p.innerText.replace(/\s+/g, ' ').trim() } : null;
    });
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  }
  return results;
}

async function darkScheme(browser) {
  const ctx = await browser.newContext({ colorScheme: 'dark', locale: 'en-GB', timezoneId: 'UTC' });
  const page = await ctx.newPage();
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
  await settle(page, 15000);
  const observed = await page.evaluate(() => {
    const cs = (el) => (el ? getComputedStyle(el) : null);
    const body = cs(document.body);
    const badge = cs(document.querySelector('.badge'));
    const panel = cs(document.querySelector('#out .panel'));
    return {
      prefersDark: matchMedia('(prefers-color-scheme: dark)').matches,
      bodyBackground: body?.backgroundColor ?? null,
      bodyColor: body?.color ?? null,
      badgeColor: badge?.color ?? null,
      panelBackground: panel?.backgroundColor ?? null,
    };
  });
  await shoot(page, 'dark-mode');
  await ctx.close();
  return observed;
}

const out = {
  issue: '#269 — Cross-browser QA matrix',
  ranAt: now(),
  harness: 'docs/qa/browser/run-cross-browser.mjs',
  base: BASE,
  scope: {
    requestedByIssue: 'Chrome, Firefox, Safari, Edge — current and one prior major',
    actuallyAvailable: ENGINE_NAMES,
    substitution: SUBSTITUTION,
    notTested: [
      'Microsoft Edge (Chromium shells only; no Edge binary in this environment)',
      'Apple Safari (WebKit shells only; no Safari and no macOS in this environment)',
      'Renderer versions older than the ones below (no prior-major builds installed)',
    ],
  },
  engines: [],
};

for (const engine of ENGINES) {
  const { browser, context, version } = await launch(engine);
  const page = await context.newPage();
  const consoleErrors = [];
  const pageErrors = [];
  page.on('console', (m) => m.type() === 'error' && consoleErrors.push(m.text()));
  page.on('pageerror', (e) => pageErrors.push(String(e.message || e)));

  // Count /api/assets calls so the selector finding is evidenced by the network,
  // not only by the DOM.
  await page.addInitScript(() => {
    window.__qaAssetCalls = 0;
    const orig = window.fetch;
    window.fetch = function (...a) {
      const u = String(a[0]);
      if (u.includes('/api/assets')) window.__qaAssetCalls++;
      return orig.apply(this, a);
    };
  });

  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
  await settle(page, 20000);
  await waitForSelectorSoft(page, '#out .panel');

  // Ground truth for the findings check: does the response itself carry one?
  const hasFindings = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/corridor?to=NGNC');
      const b = await r.json();
      return !!b.findings;
    } catch {
      return null;
    }
  });

  const { checks, uiDuringLoad } = await runChecks(page, engine, hasFindings);
  const errors = await runErrorPaths(page);
  const shot = await shoot(page, `${engine}-cross-browser`);

  const dark = await darkScheme(browser);

  out.engines.push({
    engine,
    browserVersion: version,
    standsInFor: SUBSTITUTION[engine]?.standsInFor ?? [],
    caveat: SUBSTITUTION[engine]?.caveat ?? null,
    checks,
    errorRendering: errors,
    darkScheme: dark,
    renderedState: uiDuringLoad,
    findingsInBody: hasFindings,
    consoleErrors,
    pageErrors,
    screenshot: shot,
    summary: {
      passed: checks.filter((c) => c.pass).length,
      failed: checks.filter((c) => !c.pass).length,
    },
  });

  await context.close();
  await browser.close();
}

const file = writeResult('cross-browser.json', out);
console.log(`wrote ${file}`);
for (const e of out.engines) {
  console.log(`${e.engine} ${e.browserVersion}: ${e.summary.passed} passed, ${e.summary.failed} failed`);
  for (const c of e.checks.filter((x) => !x.pass)) {
    console.log(`  FAIL ${c.id}: expected ${c.expected}; observed ${JSON.stringify(c.observed)}`);
  }
}
