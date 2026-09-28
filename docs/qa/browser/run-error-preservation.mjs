// Issue #293 — QA that an error preserves the page (selection and input).
//
// The contract: a failed fetch never empties #out. The last successful
// render stays visible with an error banner above it, the corridor selection
// stays as the reader left it, banners do not stack on retry, and the
// recovery affordances (retry button, reload link) are keyboard reachable.
//
// Usage (see ../README.md; servers via ../../browser/servers.sh):
//
//   node run-error-preservation.mjs [--base=URL] [--upstream-down=URL]
//                                   [--engine=chromium]
//
// Causes come from real servers wherever the browser can reach them: the
// live-measurement failure is replayed as the actual 502 bytes the
// upstream-down instance returns; only the network-failure class is injected
// with route.abort(), because a browser cannot fetch from a dead origin
// without aborting the request itself — the same convention as
// run-error-paths.mjs.

import { launch, settle, snapshotUI, writeResult, shoot, now, parseArgs } from './lib.mjs';

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const DOWN = args['upstream-down'] || 'http://127.0.0.1:8098';
const ENGINES = String(args.engine || 'chromium').split(',');

const SCHEMES = ['light', 'dark'];
const WIDTHS = [320, 375, 768, 1024, 1440];

function check(name, pass, detail = null) {
  return { name, pass: !!pass, detail };
}

// readState observes #out the way the reader does: what is on screen, which
// banners exist, what the controls hold.
async function readState(page) {
  return page.evaluate(() => {
    const out = document.getElementById('out');
    const select = document.getElementById('to');
    const banners = [...out.querySelectorAll('.panel.err')];
    return {
      bannerCount: banners.length,
      bannerTexts: banners.map((b) => b.innerText.replace(/\s+/g, ' ').trim()),
      bannerRoles: banners.map((b) => b.getAttribute('role')),
      // A recorded render is present if the provenance label survived.
      hasProvenance: !!out.querySelector('.provenance'),
      hasTrend: out.innerText.includes('Trend'),
      hasLanding: !!out.querySelector('#loading'),
      selection: select ? select.value : null,
      runEnabled: !document.getElementById('run').disabled,
      trendEnabled: !document.getElementById('trend').disabled,
      statusEmpty: !document.getElementById('status').textContent,
    };
  });
}

async function driveScenario(engine, scheme, width) {
  const { browser, version } = await launch(engine, {
    colorScheme: scheme,
    viewport: { width, height: 900 },
  });
  const context = await browser.newContext();
  const results = { engine, version, scheme, width, steps: [] };
  try {
    const page = await context.newPage();

    // 1. Boot with the corridor fetch failing (cold start): the landing
    // panel must survive under the banner, and the reload link must be
    // keyboard reachable from the page.
    //
    // The abort is switched by a flag rather than unroute(): Playwright
    // cannot reliably remove a predicate-matched route, and a stale aborter
    // would silently poison every later step.
    let abortCorridor = true;
    await page.route((u) => /\/api\/corridor\?/.test(u.href) && !u.href.includes('live=1'),
      (route) => abortCorridor ? route.abort() : route.fallback());
    await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    await settle(page);
    let s = await readState(page);
    results.steps.push(
      check('cold-start: banner shown, landing retained',
        s.bannerCount === 1 && s.hasLanding && s.bannerRoles[0] === 'alert',
        s),
    );
    await page.keyboard.press('Tab'); // corridor select
    await page.keyboard.press('Tab'); // Measure live
    await page.keyboard.press('Tab'); // Show trend
    const focus = await page.evaluate(() => {
      const link = document.querySelector('#out .panel.err a');
      if (link) link.focus();
      return document.activeElement === link ? 'link' : String(document.activeElement && document.activeElement.tagName);
    });
    results.steps.push(check('cold-start: reload link focusable', focus === 'link', focus));
    await shoot(page, `293-${engine}-${scheme}-${width}-coldstart`);

    // 2. A good render, then a live measurement that fails with the real
    // 502 bytes from the upstream-down instance.
    abortCorridor = false;
    // Reload so the boot fetch succeeds: this is the reading that must be
    // preserved under the next failure.
    await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    await settle(page);
    s = await readState(page);
    results.steps.push(check('recorded render present', s.bannerCount === 0 && s.hasProvenance, s));

    let failLive = true;
    await page.route('**/*live=1*', async (route) => {
      if (!failLive) return route.fallback();
      const u = new URL(route.request().url());
      const real = await fetch(DOWN + u.pathname + u.search);
      route.fulfill({
        status: real.status,
        headers: { 'content-type': 'application/json' },
        body: await real.text(),
      });
    });
    const selectionBefore = (await page.evaluate(() => document.getElementById('to').value));
    await page.click('#run');
    await settle(page);
    await page.click('#run'); // second failure: banners must not stack
    await settle(page);
    s = await readState(page);
    results.steps.push(
      check('measure failure: banner above retained reading, no stacking',
        s.bannerCount === 1 && s.hasProvenance &&
        /Could not measure/.test(s.bannerTexts[0] || ''),
        s),
    );
    results.steps.push(
      check('measure failure: selection and controls preserved',
        s.selection === selectionBefore && s.runEnabled && s.statusEmpty, s),
    );
    await shoot(page, `293-${engine}-${scheme}-${width}-measure`);

    // 3. A good trend, then a trend load that fails: the trend stays.
    failLive = false;
    await page.click('#trend');
    await settle(page);
    s = await readState(page);
    results.steps.push(check('trend rendered', s.hasTrend && s.bannerCount === 0, s));

    await page.route('**/api/corridor/trend*', (route) => route.abort());
    await page.click('#trend');
    await settle(page);
    s = await readState(page);
    results.steps.push(
      check('trend failure: banner above retained trend',
        s.bannerCount === 1 && s.hasTrend &&
        /Could not load history/.test(s.bannerTexts[0] || ''),
        s),
    );
    await shoot(page, `293-${engine}-${scheme}-${width}-trend`);
  } finally {
    await context.close().catch(() => {});
    await browser.close();
  }
  return results;
}

const all = [];
let failed = 0;
for (const engine of ENGINES) {
  for (const scheme of SCHEMES) {
    for (const width of WIDTHS) {
      const r = await driveScenario(engine, scheme, width);
      for (const step of r.steps) {
        if (!step.pass) {
          failed++;
          console.error(`FAIL ${engine}/${scheme}/${width}: ${step.name}`, JSON.stringify(step.detail));
        }
      }
      console.log(`${engine}/${scheme}/${width}: ${r.steps.filter((x) => x.pass).length}/${r.steps.length} checks passed`);
      all.push(r);
    }
  }
}

const file = writeResult('error-preservation.json', {
  ran_at: now(),
  base: BASE,
  upstream_down: DOWN,
  scenarios: all,
});
// Like the other runners, a failed check is recorded, not a non-zero exit:
// the result set is the deliverable and the printed summary is read by the
// person running it.
console.log(`\n${failed === 0 ? 'all checks passed' : failed + ' checks FAILED'} — result: ${file}`);
