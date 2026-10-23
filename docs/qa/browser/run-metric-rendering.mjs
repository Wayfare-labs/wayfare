// #268: record observations of the real renderer, including negative results.
// History responses are real. The separate render-only passes deliberately
// supply schema fixtures to findings(); they do not pretend to be API data.
import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { launch, settle, parseArgs, HERE, writeResult } from './lib.mjs';

const args = parseArgs();
const base = String(args.base || 'http://127.0.0.1:8268');
const engine = String(args.engine || 'chromium');
const resultName = path.basename(String(args.result || 'metric-rendering.json'));
const root = path.resolve(HERE, '../../..');
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');
const at = '2026-08-21T12:00:00Z';
// Values copied from server/api_test.go:metricsSchemaFixture. These are UI
// fixtures, NOT observations of the market at this date or endpoint.
const measured = {
  id: 'spread.bid-ask', scope: 'asset', subject: 'NGNC', determined: true,
  value: '0.0004', unit: 'ratio', summary: 'bid-ask spread on the USDC/NGNC book',
  observed_at: at,
  evidence: [{ source: 'https://horizon.stellar.org/order_book', observed: '0.0004', observed_at: at }],
};
const unknown = {
  id: 'depth.observed-executable', scope: 'asset', subject: 'NGNC', determined: false,
  reason: 'no executable side at 5000 USDC', unit: 'amount',
  summary: 'could not determine: no executable side at 5000 USDC', observed_at: at, evidence: [],
};
// A labelled UI-only check provides the failure state for visual comparison.
const failedCheck = { id: 'qa.failed-check', determined: true, passed: false,
  severity: 'warning', summary: 'UI fixture: failed check for comparison', evidence: [] };
const fixtures = [
  { name: 'metrics-omitted', payload: { checks: [] }, metrics: [] },
  { name: 'metrics-empty', payload: { checks: [], metrics: [] }, metrics: [] },
  { name: 'measured-only', payload: { checks: [], metrics: [measured] }, metrics: [measured] },
  { name: 'undetermined-only', payload: { checks: [], metrics: [unknown] }, metrics: [unknown] },
  { name: 'mixed-with-failed-check', payload: { checks: [failedCheck], failed: 1, metrics: [measured, unknown] }, metrics: [measured, unknown] },
];
const result = {
  issue: 268, ranAt: new Date().toISOString(), base, engine,
  sourceCommit: execFileSync('git', ['-c', `safe.directory=${root.replaceAll('\\', '/')}`, '-C', root, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(),
  harnessSHA256: sha256(fs.readFileSync(fileURLToPath(import.meta.url))),
  fixtureProvenance: 'server/api_test.go:metricsSchemaFixture; comparison check is explicitly synthetic',
  scope: 'Recorded API absence checks plus render-only schema fixtures; no live market measurement or metric-producing API integration is claimed.',
  fixtures, history: [], rendering: [], errors: [],
};
const check = (name, pass, detail = null) => ({ name, pass: Boolean(pass), detail });
const { browser, context, version } = await launch(engine, {}, args.executable ? { executablePath: String(args.executable) } : {});
result.browserVersion = version;
try {
  const page = await context.newPage();
  page.on('pageerror', e => result.errors.push(e.message));
  page.on('console', message => { if (message.type() === 'error') result.errors.push(message.text()); });
  await page.goto(base, { waitUntil: 'networkidle' });
  await settle(page);
  const htmlResponse = await context.request.get(base);
  const htmlBytes = await htmlResponse.body();
  result.servedHTMLSHA256 = sha256(htmlBytes);
  result.contentSecurityPolicy = htmlResponse.headers()['content-security-policy'];
  result.sourceHasCRLF = htmlBytes.includes(Buffer.from('\r\n'));
  result.bootstrap = await page.evaluate(() => ({ rendererAvailable: typeof findings === 'function',
    corridorOptions: [...(document.querySelector('#to')?.options || [])].filter(option => option.value).length }));
  result.bootstrap.checks = [check('metric renderer available', result.bootstrap.rendererAvailable),
    check('corridor selector initialized', result.bootstrap.corridorOptions > 0)];
  if (!result.bootstrap.rendererAvailable || !result.bootstrap.corridorOptions) {
    const name = `268-bootstrap-${result.sourceHasCRLF ? 'crlf' : 'lf'}.png`;
    const file = path.join(HERE, 'results/screenshots', name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    await page.screenshot({ path: file });
    result.bootstrap.screenshot = `screenshots/${name}`;
    result.bootstrap.limitation = 'Normal navigation did not initialize. Subsequent checks call the real findings() renderer directly; they are not end-to-end navigation evidence.';
    if (!result.bootstrap.rendererAvailable) throw new Error('Renderer unavailable; metric cases were not run. See browser errors and CSP.');
  }
  for (const corridor of ['NGNC', 'NGNT', 'GHSC', 'KESC']) {
    const endpoint = `${base}/api/corridor?to=${corridor}`;
    const requestedAt = new Date().toISOString();
    const response = await context.request.get(endpoint);
    const bytes = await response.body();
    const body = JSON.parse(bytes.toString());
    // Observe the real production renderer independently of the broken shell
    // bootstrap. This is not a selector/navigation integration assertion.
    await page.evaluate(findingsPayload => {
      document.getElementById('out').innerHTML = findingsPayload ? findings(findingsPayload) : '';
    }, body.findings || null);
    const metrics = body.findings?.metrics || [];
    const panels = await page.locator('#out [aria-label="Metrics"]').count();
    result.history.push({ mode: 'recorded API response rendered via findings()', corridor, endpoint, requestedAt, status: response.status(),
      responseSHA256: sha256(bytes), recordedAt: body.recorded_at || body.measured_at || body.observed_at || null,
      live: body.live, metrics, response: body,
      checks: [check('API request succeeded', response.ok()),
        check('API and metrics-panel presence agree', panels === (metrics.length ? 1 : 0), { panels, metrics: metrics.length })],
    });
  }
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    for (const width of [1440, 375]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const fixture of fixtures) {
        const observed = await page.evaluate(payload => {
          document.getElementById('out').innerHTML = findings(payload);
          const panel = document.querySelector('#out [aria-label="Metrics"]');
          const rows = [...(panel?.querySelectorAll('.finding-row') || [])];
          const failed = document.querySelector('#out .f-fail');
          const probe = document.createElement('span');
          probe.style.color = 'var(--ink)';
          document.body.append(probe);
          const neutralColour = getComputedStyle(probe).color;
          probe.remove();
          return {
            panelCount: document.querySelectorAll('#out [aria-label="Metrics"]').length,
            heading: panel?.querySelector('h2')?.innerText || null,
            rows: rows.map(row => ({
              text: row.innerText, value: row.querySelector('.m-value')?.innerText,
              unit: row.querySelector('.m-unit')?.innerText || null,
              unitSourceText: row.querySelector('.m-unit')?.textContent || null,
              state: row.querySelector('.m-state')?.innerText || null,
              reason: row.querySelector('.m-unknown')?.innerText || null,
              classes: [...row.querySelectorAll('[class]')].map(node => node.className),
              valueColour: getComputedStyle(row.querySelector('.m-value')).color,
              evidence: [...row.querySelectorAll('.f-evidence')].map(node => node.innerText),
              identifier: row.querySelector('.f-id')?.innerText,
              horizontalOverflow: row.scrollWidth > row.clientWidth + 1,
            })),
            failure: failed ? { text: failed.innerText, colour: getComputedStyle(failed).color } : null,
            unknownColour: panel?.querySelector('.m-state') ? getComputedStyle(panel.querySelector('.m-state')).color : null,
            neutralColour,
          };
        }, fixture.payload);
        const checks = [
          check('empty/omitted metrics panel absent; populated panel present', observed.panelCount === (fixture.metrics.length ? 1 : 0)),
          check('every metric has exactly one row', observed.rows.length === fixture.metrics.length),
          check('metric rows contain no pass/fail/verdict classes', observed.rows.every(row => row.classes.every(c => !/f-(pass|fail)|v-(good|fair|poor|unusable)/.test(c)))),
        ];
        fixture.metrics.forEach((metric, i) => {
          const row = observed.rows[i];
          checks.push(check(`${metric.id}: identifier and observation timestamp reachable`, row?.identifier?.includes(metric.id) && row.identifier.includes(metric.observed_at)));
          if (metric.determined) {
            checks.push(check(`${metric.id}: exact value and unit visible (CSS casing allowed)`,
              row?.value?.includes(metric.value) && Boolean(row.unit) && row.unitSourceText === metric.unit));
            checks.push(check(`${metric.id}: every evidence source and timestamp reachable`, metric.evidence.every(e => row?.evidence.some(text => text.includes(e.source) && text.includes(e.observed_at)))));
            checks.push(check(`${metric.id}: no verdict badge`, row?.state === null));
            checks.push(check(`${metric.id}: measured value uses neutral ink`, row?.valueColour === observed.neutralColour));
          } else {
            checks.push(check(`${metric.id}: explicit UNDETERMINED and exact reason`, row?.state === 'UNDETERMINED' && row.reason.includes(metric.reason)));
            checks.push(check(`${metric.id}: no fabricated numeric value/unit`, row?.unit === null && !Object.hasOwn(metric, 'value')));
          }
        });
        if (observed.failure) checks.push(check('unknown metric differs in word and colour from failed check',
          observed.failure.text === 'FAIL' && observed.unknownColour !== observed.failure.colour));
        checks.push(check('metric rows fit viewport', observed.rows.every(row => !row.horizontalOverflow)));
        const entry = { mode: 'render-only schema fixture', fixture: fixture.name, scheme, width,
          observedAt: new Date().toISOString(), observed, checks };
        if (fixture.name === 'mixed-with-failed-check') {
          // Capture the settled panel, not its 240ms entrance fade.
          await page.locator('#out').evaluate(async out => {
            await Promise.all(out.getAnimations({ subtree: true }).map(animation => animation.finished.catch(() => {})));
          });
          const name = `268-metrics-${scheme}-${width}.png`;
          const file = path.join(HERE, 'results/screenshots', name);
          fs.mkdirSync(path.dirname(file), { recursive: true });
          await page.locator('#out').screenshot({ path: file });
          entry.screenshot = `screenshots/${name}`;
        }
        result.rendering.push(entry);
      }
    }
  }
} catch (error) {
  result.executionError = error.message;
} finally {
  await browser.close();
}
const checks = [result.bootstrap || { checks: [check('browser bootstrap completed', false)] },
  ...result.history, ...result.rendering].flatMap(row => row.checks);
result.summary = { checks: checks.length, passed: checks.filter(c => c.pass).length,
  failed: checks.filter(c => !c.pass).length, pageErrors: result.errors.length };
console.log(JSON.stringify(result.summary));
console.log(writeResult(resultName, result));
