// Issue #751 — Scheduled WCAG re-audit of the corridor UI.
//
// Automatically re-runs the accessibility audit (issue #273) with the browser
// harness and axe-core, asserting that no new violations have been introduced
// beyond the documented baseline in docs/qa/artifacts/273-accessibility-audit.md.
//
// Usage:
//   node run-accessibility.mjs [--base=URL] [--engine=chromium] [--fail-on-new] [--json]
//
// Exit codes:
//   0 — All checks clean or only known baseline findings observed.
//   1 — New accessibility violations detected (regressions fail).
//   2 — Harness or network error.

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import AxeBuilder from '@axe-core/playwright';
import { launch, settle, parseArgs, RESULTS_DIR, now } from './lib.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const args = parseArgs();

const BASE = String(args.base || 'http://127.0.0.1:8099');
const ENGINE = String(args.engine || 'chromium');
const FAIL_ON_NEW = args['fail-on-new'] !== false && args['fail-on-new'] !== 'false';
const BASELINE_PATH = String(args.baseline || join(HERE, 'results', 'accessibility-baseline.json'));

// Load known baseline findings from #273
let baseline = { baselineRules: [] };
if (existsSync(BASELINE_PATH)) {
  try {
    baseline = JSON.parse(readFileSync(BASELINE_PATH, 'utf8'));
  } catch (err) {
    console.warn(`warning: could not parse baseline from ${BASELINE_PATH}: ${err.message}`);
  }
}

// Find available browser launch options (support system Chrome if Playwright's download is missing)
function resolveLaunchOptions() {
  const opts = { headless: true };
  const chromePaths = [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium-browser',
    '/usr/bin/chromium',
  ];
  for (const p of chromePaths) {
    if (existsSync(p)) {
      opts.executablePath = p;
      break;
    }
  }
  return opts;
}

function matchesBaseline(ruleId, targetSelector) {
  for (const b of baseline.baselineRules || []) {
    if (b.id !== ruleId) continue;
    for (const sel of b.allowedSelectors || []) {
      if (targetSelector.includes(sel)) return { isMatch: true, findingId: b.findingId, sc: b.sc };
    }
  }
  return { isMatch: false };
}

async function auditState(context, name, url) {
  const page = await context.newPage();
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 15000 });
    await settle(page, 5000);

    const builder = new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']);

    const results = await builder.analyze();
    return { name, url, violations: results.violations || [], passes: results.passes || [] };
  } finally {
    await page.close().catch(() => {});
  }
}

async function main() {
  console.log(`Starting WCAG accessibility re-audit (issue #751)...`);
  console.log(`Target: ${BASE}`);

  const launchOpts = resolveLaunchOptions();
  let browser, context;
  try {
    const launched = await launch(ENGINE, {}, launchOpts);
    browser = launched.browser;
    context = launched.context;
  } catch (err) {
    console.error(`Error launching ${ENGINE}: ${err.message}`);
    process.exit(2);
  }

  const states = [
    { name: 'Direct corridor (light scheme)', url: `${BASE}/?to=NGNC`, ctx: { colorScheme: 'light' } },
    { name: 'Direct corridor (dark scheme)', url: `${BASE}/?to=NGNC`, ctx: { colorScheme: 'dark' } },
    { name: 'Derivative corridor (light scheme)', url: `${BASE}/?to=KESC`, ctx: { colorScheme: 'light' } },
    { name: 'Initial load / Empty state', url: `${BASE}/`, ctx: { colorScheme: 'light' } },
  ];

  const allKnown = [];
  const allNew = [];
  const stateReports = [];

  try {
    for (const s of states) {
      console.log(`\nAuditing: ${s.name}`);
      const rep = await auditState(context, s.name, s.url);
      stateReports.push(rep);

      for (const v of rep.violations) {
        for (const node of v.nodes) {
          const targetStr = (node.target || []).join(' ');
          const baselineMatch = matchesBaseline(v.id, targetStr);

          const item = {
            state: s.name,
            ruleId: v.id,
            impact: v.impact,
            help: v.help,
            helpUrl: v.helpUrl,
            target: targetStr,
            html: node.html,
            failureSummary: node.failureSummary,
          };

          if (baselineMatch.isMatch) {
            allKnown.push({ ...item, findingId: baselineMatch.findingId, sc: baselineMatch.sc });
          } else {
            allNew.push(item);
          }
        }
      }
    }
  } finally {
    await browser.close().catch(() => {});
  }

  console.log(`\n========================================`);
  console.log(`WCAG 2.2 AA Re-Audit Summary`);
  console.log(`========================================`);
  console.log(`Audited states:             ${states.length}`);
  console.log(`Known baseline violations:  ${allKnown.length}`);
  console.log(`New violations detected:    ${allNew.length}`);

  if (allKnown.length > 0) {
    console.log(`\nKnown baseline findings (issue #273):`);
    for (const k of allKnown) {
      console.log(`  - [${k.findingId || 'KNOWN'} · SC ${k.sc || 'AA'}] ${k.ruleId}: ${k.target}`);
    }
  }

  if (allNew.length > 0) {
    console.error(`\nFAIL: New accessibility violations detected:`);
    for (const n of allNew) {
      console.error(`  - Rule: ${n.ruleId} (${n.impact})`);
      console.error(`    State:   ${n.state}`);
      console.error(`    Target:  ${n.target}`);
      console.error(`    Summary: ${n.failureSummary}`);
      console.error(`    Help:    ${n.helpUrl}\n`);
    }
  } else {
    console.log(`\nPASS: No new accessibility violations detected.`);
  }

  if (process.argv.includes('--json') || args.json) {
    mkdirSync(RESULTS_DIR, { recursive: true });
    const resultPayload = {
      timestamp: now(),
      base: BASE,
      engine: ENGINE,
      states: stateReports.map((r) => ({ name: r.name, url: r.url, violationsCount: r.violations.length })),
      knownCount: allKnown.length,
      newCount: allNew.length,
      knownViolations: allKnown,
      newViolations: allNew,
      verdict: allNew.length === 0 ? 'PASS' : 'FAIL',
    };
    const outPath = join(RESULTS_DIR, 'accessibility.json');
    writeFileSync(outPath, JSON.stringify(resultPayload, null, 2) + '\n');
    console.log(`Wrote JSON report to ${outPath}`);
  }

  if (allNew.length > 0 && FAIL_ON_NEW) {
    process.exit(1);
  }
  process.exit(0);
}

main().catch((err) => {
  console.error(`Unexpected harness error: ${err.stack || err}`);
  process.exit(2);
});
