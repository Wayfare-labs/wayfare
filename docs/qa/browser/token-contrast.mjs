// #286: measure the browser's CSS cascade, including nested translucent fills.
// Offline style specimens only: no product scripts, API calls or invented data.
// node token-contrast.mjs [--executable=PATH] [--check] [--candidate] [--screenshots]
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { chromium } from 'playwright';

const source = readFileSync(new URL('../../../server/index.html', import.meta.url), 'utf8');
const candidate = process.argv.includes('--candidate');
const suffix = candidate ? '-candidate' : '';
const sourceCSS = source.match(/<style>([\s\S]*?)<\/style>/)[1];
// The candidate lifts the existing explicit dark block outside its OS query.
// Production source is never written. This is a design proposal, not a fix.
const dark = sourceCSS.match(/:root\[data-theme="dark"\]\s*\{([^}]+)\}/);
if (!dark) throw new Error('Explicit dark block changed; review the candidate');
const proposal = readFileSync(new URL('token-contrast-candidate.css', import.meta.url), 'utf8');
const css = sourceCSS;
// A separate sheet avoids inheriting an unclosed rule in the audited source.
const candidateStyle = candidate ? `<style>:root[data-theme="dark"] {${dark[1]}}\n${proposal}</style>` : '';
const executablePath = process.argv.find(a => a.startsWith('--executable='))?.slice(13);
const browser = await chromium.launch({ executablePath, headless: true, args: ['--renderer-process-limit=1'] });
const results = [];
// Both sides and the exact boundary of every width media query, plus 320/900.
const boundaries = [...css.matchAll(/@media[^{}]*\((?:max|min)-width:\s*(\d+)px\)/g)].map(m => Number(m[1]));
const widths = [...new Set([320, 900, ...boundaries.flatMap(w => [w - 1, w, w + 1])])].sort((a, b) => a - b);
const specimen = `
  <div class="wrap">
    <h1>CSS specimens: no corridor measurements</h1>
    <p class="sub" data-audit="muted on page">Muted text</p>
    <div class="provenance" data-audit="recorded provenance">RECORDED</div>
    <div class="provenance provenance-live" data-audit="live provenance">LIVE</div>
    <section class="panel">
      <span data-audit="ink on panel">Panel text</span>
      <span class="meta" data-audit="muted on panel">Muted text</span>
      <span class="m-unit" data-audit="small metric unit">Unit label</span>
      <span class="badge b-direct" data-audit="DIRECT">DIRECT</span>
      <span class="badge b-derivative" data-audit="DERIVATIVE">DERIVATIVE</span>
      <span class="badge b-nomarket" data-audit="NO-MARKET">NO-MARKET</span>
      <span class="v-good" data-audit="good verdict">GOOD</span>
      <span class="v-poor" data-audit="poor verdict">POOR</span>
      <span class="v-unusable" data-audit="unusable verdict">UNUSABLE</span>
      <a class="to-table" href="#" data-audit="table link">View table</a>
      <details class="cost-decomp"><summary data-audit="cost disclosure">Cost details</summary></details>
      ${['pass', 'fail', 'unknown'].map(state => `<div class="finding-row">
        <span class="f-state f-${state}" data-audit="${state} chip on row">${state.toUpperCase()}</span>
        <span class="f-id" data-audit="muted on ${state} row">Check identifier</span>
      </div>`).join('')}
      ${['pass', 'fail', 'unknown'].map(state => `<span class="f-state f-${state}" data-audit="${state} legend chip">${state.toUpperCase()}</span>`).join('')}
      <div class="cs-info">${['direct', 'derivative', 'nomarket'].map(state => `<span class="cs-state cs-${state}" data-audit="${state} selector state">${state.toUpperCase()}</span>`).join('')}</div>
      <div style="background:var(--panel-alt)"><span class="meta" data-audit="muted on alternate panel">Muted alternate surface</span></div>
      <span class="m-state" data-audit="UNDETERMINED">UNDETERMINED</span>
      <button data-audit="button label">Inspect</button>
    </section>
  </div>`;

const page = await browser.newPage({ reducedMotion: 'reduce' });
await page.route('**/*', route => route.abort());
try {
  for (const os of ['light', 'dark']) {
    for (const theme of ['auto', 'light', 'dark']) {
      await page.emulateMedia({ colorScheme: os });
      // No browser requests, including fonts or scripts, are permitted.
      await page.setContent(`<html${theme === 'auto' ? '' : ` data-theme="${theme}"`}><head><style>${css}</style>${candidateStyle}</head><body>${specimen}</body></html>`);
      for (const width of widths) {
        await page.setViewportSize({ width, height: 1000 });
        const resolvedPanel = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--panel').trim().toUpperCase());
        const expectedScheme = theme === 'auto' ? os : theme;
        const themePass = resolvedPanel === (expectedScheme === 'dark' ? '#0F172A' : '#FFFFFF');
        const measured = await page.evaluate(() => {
          // Canvas converts computed CSS colour syntax (including color-mix)
          // to sRGB bytes. Alpha is composited through every ancestor surface.
          const canvas = document.createElement('canvas');
          canvas.width = canvas.height = 1;
          const ctx = canvas.getContext('2d', { willReadFrequently: true });
          const rgba = color => {
            ctx.clearRect(0, 0, 1, 1);
            ctx.fillStyle = color;
            ctx.fillRect(0, 0, 1, 1);
            return [...ctx.getImageData(0, 0, 1, 1).data];
          };
          const over = (fg, bg) => fg.slice(0, 3).map((c, i) => c * fg[3] / 255 + bg[i] * (1 - fg[3] / 255));
          const luminance = rgb => rgb.map(c => c / 255).map(c => c <= .04045 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4)
            .reduce((sum, c, i) => sum + c * [.2126, .7152, .0722][i], 0);
          return [...document.querySelectorAll('[data-audit]')].map(el => {
            const ancestors = [];
            for (let node = el; node; node = node.parentElement) ancestors.unshift(node);
            let background = [255, 255, 255];
            for (const node of ancestors) background = over(rgba(getComputedStyle(node).backgroundColor), background);
            const style = getComputedStyle(el);
            const foreground = over(rgba(style.color), background);
            const a = luminance(foreground), b = luminance(background);
            const ratio = (Math.max(a, b) + .05) / (Math.min(a, b) + .05);
            return { element: el.dataset.audit, color: style.color, background: background.map(c => +c.toFixed(2)),
              fontSize: style.fontSize, ratio: +ratio.toFixed(3), need: 4.5, pass: ratio >= 4.5 };
          });
        });
        results.push({ os, theme, width, expectedScheme, resolvedPanel, themePass, measurements: measured });
        if (process.argv.includes('--screenshots') && theme === 'auto' && width === 900) {
          await page.screenshot({ path: fileURLToPath(new URL(`results/token-contrast${suffix}-${os}.png`, import.meta.url)), fullPage: true });
        }
      }
    }
  }
  const report = { checkedAt: new Date().toISOString(), candidate,
    sourceCommit: execFileSync('git', ['-c', `safe.directory=${fileURLToPath(new URL('../../../', import.meta.url))}`, 'rev-parse', 'HEAD'], { cwd: fileURLToPath(new URL('../../../', import.meta.url)), encoding: 'utf8' }).trim(),
    proposalSha256: candidate ? createHash('sha256').update(proposal).digest('hex') : null, source: 'server/index.html', sha256: createHash('sha256').update(source).digest('hex'),
    browser: await browser.version(), method: 'WCAG 2.x sRGB luminance; browser-resolved CSS with ancestor alpha compositing',
    scope: 'Offline CSS specimens; normal text threshold for all samples. Not a live API, full-page or assistive-technology audit.', results };
  const { results: rows, ...metadata } = report;
  const serialized = JSON.stringify(metadata, null, 2).slice(0, -2) + ',\n  \"results\": [\n' + rows.map(row => '    ' + JSON.stringify(row)).join(',\n') + '\n  ]\n}\n';
  writeFileSync(new URL(`results/token-contrast${suffix}.json`, import.meta.url), serialized);
  const failures = results.flatMap(r => r.measurements.filter(m => !m.pass));
  const themeFailures = results.filter(r => !r.themePass);
  console.log(`${themeFailures.length} theme selection failures`);
  console.log(`${results.length} theme/width combinations; ${failures.length} failing text samples`);
  for (const r of results.filter(r => r.width === 900)) {
    console.log(`${r.os} OS / ${r.theme}: ${r.measurements.filter(m => !m.pass).map(m => `${m.element} ${m.ratio}:1`).join(', ') || 'all pass'}`);
  }
  if (process.argv.includes('--check') && (failures.length || themeFailures.length)) process.exitCode = 1;
} finally {
  await browser.close();
}
