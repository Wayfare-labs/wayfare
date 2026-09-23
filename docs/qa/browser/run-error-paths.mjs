// Issue #266 — QA every error path in the browser.
//
// Walks each distinct cause the UI can surface (network failure, upstream
// failure, upstream timeout, unknown asset, malformed size, unknown query
// parameter, server error) across every surface that renders one (initial
// load, "Measure live", "Show trend", the corridor selector), and records what
// the browser actually displayed.
//
// Usage (see docs/qa/README.md for the full procedure):
//
//   node run-error-paths.mjs [--base=URL] [--upstream-down=URL]
//                            [--upstream-slow=URL] [--engine=chromium,firefox]
//
// Every response is produced by a real server instance except where a cause is
// unreachable from the UI at all; those cases are replayed from bytes the real
// server returned, and marked `injected: true` with the reason.

import { launch, settle, snapshotUI, recordAPIBodies, writeResult, shoot, now, waitForSelectorSoft } from './lib.mjs';
import { parseArgs } from './lib.mjs';

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const DOWN = args['upstream-down'] || 'http://127.0.0.1:8098';
const SLOW = args['upstream-slow'] || 'http://127.0.0.1:8097';
const ENGINES = String(args.engine || 'chromium').split(',');

// ---------------------------------------------------------------------------
// Ground truth: the exact status and body each server returns, captured before
// the browser runs so the artifact proves the replayed bytes are real.
// ---------------------------------------------------------------------------

async function grab(url) {
  try {
    const r = await fetch(url, { signal: AbortSignal.timeout(30000) });
    let body = null;
    let raw = null;
    try {
      raw = await r.text();
      body = JSON.parse(raw);
    } catch {
      /* leave body null; raw is recorded */
    }
    return { url: url.replace(/^https?:\/\/[^/]+/, ''), status: r.status, body, raw: raw?.slice(0, 600) ?? null };
  } catch (e) {
    return { url: url.replace(/^https?:\/\/[^/]+/, ''), status: null, error: String(e.message || e) };
  }
}

async function groundTruth() {
  return {
    capturedAt: now(),
    normal: {
      base: BASE,
      probes: [
        { label: 'live corridor (history-first, success)', ...(await grab(`${BASE}/api/corridor?to=NGNC`)) },
        { label: 'unknown receive asset', ...(await grab(`${BASE}/api/corridor?to=SCAMC`)) },
        { label: 'malformed size', ...(await grab(`${BASE}/api/corridor?to=NGNC&sizes=abc`)) },
        { label: 'unknown query parameter', ...(await grab(`${BASE}/api/corridor?tp=NGNC`)) },
      ],
    },
    upstreamDown: {
      base: DOWN,
      probes: [{ label: 'live measurement, Horizon unreachable', ...(await grab(`${DOWN}/api/corridor?to=NGNC&live=1`)) }],
    },
    upstreamSlow: {
      base: SLOW,
      probes: [{ label: 'live measurement, Horizon black-holed', ...(await grab(`${SLOW}/api/corridor?to=NGNC&live=1`)) }],
    },
  };
}

const GROUND = await groundTruth();

function bodyFor(kind) {
  const find = (group, label) => {
    const p = GROUND[group].probes.find((x) => x.label === label);
    return p?.body;
  };
  switch (kind) {
    case 'unknown_asset':
      return [400, find('normal', 'unknown receive asset')];
    case 'malformed_size':
      return [400, find('normal', 'malformed size')];
    case 'unknown_query_param':
      return [400, find('normal', 'unknown query parameter')];
    case 'upstream_failure':
      return [502, find('upstreamDown', 'live measurement, Horizon unreachable')];
    case 'upstream_timeout':
      return [504, find('upstreamSlow', 'live measurement, Horizon black-holed')];
    default:
      return null;
  }
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

const isCorridor = (u) => new URL(u).pathname === '/api/corridor';
const isTrend = (u) => new URL(u).pathname === '/api/corridor/trend';
const isAssets = (u) => new URL(u).pathname === '/api/assets';

async function fulfilJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json; charset=utf-8',
    body: JSON.stringify(body ?? {}),
  });
}

const SCENARIOS = [
  {
    id: 'baseline_boot_success',
    cause: 'none (control)',
    surface: 'initial load',
    how: `GET ${BASE}/ (boot fetches /api/corridor?to=NGNC against the real server)`,
    injected: false,
    async run(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'boot_network_failure',
    cause: 'network failure',
    surface: 'initial load',
    how: 'page loads from the normal server; the boot fetch is aborted at the transport',
    injected: 'abort (a browser cannot fetch from an origin that is not listening)',
    async setup(page) {
      await page.route(isCorridor, (r) => r.abort('failed'));
    },
    async run(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'boot_upstream_failure',
    cause: 'upstream failure',
    surface: 'initial load',
    how: `GET ${DOWN}/ — real instance whose Horizon URL is a closed port`,
    injected: false,
    async run(page) {
      await page.goto(DOWN + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'boot_upstream_timeout',
    cause: 'upstream timeout',
    surface: 'initial load',
    how: `GET ${SLOW}/ — real instance whose Horizon URL black-holes, -timeout=2s`,
    injected: false,
    async run(page) {
      await page.goto(SLOW + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'measure_network_failure',
    cause: 'network failure',
    surface: 'Measure live',
    how: 'boot succeeds against the normal server, then the measure fetch is aborted',
    injected: 'abort',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.route(isCorridor, (r) => r.abort('failed'));
    },
    async run(page) {
      await page.click('#run');
    },
  },
  {
    id: 'measure_upstream_failure',
    cause: 'upstream failure',
    surface: 'Measure live',
    how: `boot + click Measure against ${DOWN} (real 502 measurement_failed)`,
    injected: false,
    async run(page) {
      await page.goto(DOWN + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.click('#run');
    },
  },
  {
    id: 'measure_upstream_timeout',
    cause: 'upstream timeout',
    surface: 'Measure live',
    how: `boot + click Measure against ${SLOW} (real 504 upstream_timeout)`,
    injected: false,
    async run(page) {
      await page.goto(SLOW + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.click('#run');
    },
  },
  {
    id: 'measure_unknown_asset',
    cause: 'unknown asset',
    surface: 'Measure live',
    how: 'the API returns its real 400 unknown_receive_asset body',
    injected: 'yes — the UI cannot select an unknown asset, so the response is replayed into the call',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      const [status, body] = bodyFor('unknown_asset');
      await page.route(isCorridor, (r) => fulfilJSON(r, status, body));
    },
    async run(page) {
      await page.click('#run');
    },
  },
  {
    id: 'measure_malformed_size',
    cause: 'malformed size',
    surface: 'Measure live',
    how: 'the API returns its real 400 invalid_sizes body',
    injected: 'yes — the UI has no size input, so the response is replayed into the call',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      const [status, body] = bodyFor('malformed_size');
      await page.route(isCorridor, (r) => fulfilJSON(r, status, body));
    },
    async run(page) {
      await page.click('#run');
    },
  },
  {
    id: 'measure_unknown_query_param',
    cause: 'unknown query parameter',
    surface: 'Measure live',
    how: 'the API returns its real 400 invalid_query body',
    injected: 'yes — the UI sends only known parameters, so the response is replayed into the call',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      const [status, body] = bodyFor('unknown_query_param');
      await page.route(isCorridor, (r) => fulfilJSON(r, status, body));
    },
    async run(page) {
      await page.click('#run');
    },
  },
  {
    id: 'measure_server_error',
    cause: 'server error',
    surface: 'Measure live',
    how: 'no handler in server/api.go raises an unplanned 500 on this endpoint, so the documented internal_error shape is injected',
    injected: 'yes — synthetic, matching server/api.go codeInternalError',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.route(isCorridor, (r) =>
        fulfilJSON(r, 500, { code: 'internal_error', error: 'listing stored corridors: read /data: input/output error' }),
      );
    },
    async run(page) {
      await page.click('#run');
    },
  },
  {
    id: 'measure_listing_server_error',
    cause: 'server error',
    surface: 'initial load (selector)',
    how: 'the documented internal_error shape is injected on /api/assets',
    injected: 'yes — synthetic, matching server/api.go codeInternalError',
    async setup(page) {
      await page.route(isAssets, (r) => fulfilJSON(r, 500, { code: 'internal_error', error: 'listing stored corridors: input/output error' }));
    },
    async run(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'assets_network_failure',
    cause: 'network failure',
    surface: 'initial load (selector)',
    how: '/api/assets is aborted at the transport',
    injected: 'abort',
    async setup(page) {
      await page.route(isAssets, (r) => r.abort('failed'));
    },
    async run(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
    },
  },
  {
    id: 'trend_network_failure',
    cause: 'network failure',
    surface: 'Show trend',
    how: 'boot succeeds, then the trend fetch is aborted',
    injected: 'abort',
    async setup(page) {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.route(isTrend, (r) => r.abort('failed'));
    },
    async run(page) {
      await page.click('#trend');
    },
  },
  {
    id: 'trend_no_history',
    cause: 'unknown asset',
    surface: 'Show trend',
    how: `Show trend against ${DOWN}, whose store is empty`,
    injected: false,
    async run(page) {
      await page.goto(DOWN + '/', { waitUntil: 'domcontentloaded' });
      await settle(page);
      await page.click('#trend');
    },
  },
];

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

const results = {
  issue: '#266 — QA every error path in the browser',
  ranAt: now(),
  harness: 'docs/qa/browser/run-error-paths.mjs',
  servers: { normal: BASE, upstreamDown: DOWN, upstreamSlow: SLOW },
  engines: [],
  groundTruth: GROUND,
  scenarios: [],
};

for (const engine of ENGINES) {
  const { browser, context, version } = await launch(engine);
  const engineBlock = { engine, browserVersion: version, runs: [] };

  for (const sc of SCENARIOS) {
    const page = await context.newPage();
    const api = recordAPIBodies(page);
    const consoleErrors = [];
    const pageErrors = [];
    page.on('console', (m) => {
      if (m.type() === 'error') consoleErrors.push(m.text());
    });
    page.on('pageerror', (e) => pageErrors.push(String(e.message || e)));

    let error = null;
    try {
      if (sc.setup) await sc.setup(page);
      await sc.run(page);
      await settle(page, 15000);
      await page.waitForTimeout(400); // let the response listeners drain
    } catch (e) {
      error = String(e.message || e);
    }

    const ui = error ? null : await snapshotUI(page);
    let shot = null;
    if (!error) shot = await shoot(page, `${engine}-${sc.id}`);

    engineBlock.runs.push({
      id: sc.id,
      cause: sc.cause,
      surface: sc.surface,
      how: sc.how,
      injected: sc.injected,
      ui,
      api,
      consoleErrors,
      pageErrors,
      error,
      screenshot: shot,
      observations: {
        showsErrorPanel: !!ui && ui.errorPanels.length > 0,
        showsMeasurement: !!ui && ui.hasProvenance,
        mentionsMachineReadableCode: !!ui && ui.errorPanels.some((p) => p.mentionsCodeField),
      },
    });

    await page.close();
  }

  results.engines.push(engineBlock);
  await context.close();
  await browser.close();
}

// ---------------------------------------------------------------------------
// Cross-scenario finding: how many visually distinct panels do the causes share?
// ---------------------------------------------------------------------------

for (const eb of results.engines) {
  const byText = new Map();
  for (const r of eb.runs) {
    if (!r.observations.showsErrorPanel) continue;
    const key = r.ui.errorPanels.map((p) => p.classes).join('|') + ' :: ' + r.ui.errorPanels.map((p) => p.text).join('|');
    if (!byText.has(key)) byText.set(key, []);
    byText.get(key).push(r.id);
  }
  eb.distinctErrorPanels = byText.size;
  eb.errorPanelGroups = [...byText.entries()].map(([k, ids]) => ({ panel: k, scenarios: ids }));
}

const file = writeResult('error-paths.json', results);
console.log(`wrote ${file}`);
for (const eb of results.engines) {
  const failed = eb.runs.filter((r) => r.error).length;
  console.log(
    `${eb.engine} ${eb.browserVersion}: ${eb.runs.length} scenarios, ${failed} harness errors, ` +
      `${eb.distinctErrorPanels} distinct error panel(s) across causes`,
  );
}
