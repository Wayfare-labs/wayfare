// Shared helpers for the browser QA harness.
//
// Design note: the harness drives the *real* Wayfare server binary, not a
// mock. Each pass takes one or more base URLs:
//
//   --base          a normal instance (`-history-first`), which serves the UI
//                   and answers the deterministic 400s without touching Horizon
//   --upstream-down an instance whose Horizon URL points at a closed port, so a
//                   live measurement fails and the server returns a real 502
//   --upstream-slow an instance whose Horizon URL black-holes, so a live
//                   measurement exceeds `-timeout` and the server returns a real 504
//
// Only the network-failure class is injected, because a browser cannot be made
// to fetch from an origin that is not listening without aborting the request.
// Everything else is a response the running server actually produced.
//
// Breakpoint sweep (issue #213): the harness can be run at each of the viewport
// widths 320, 375, 768, 1024 and 1440. BREAKPOINTS is the single source of
// truth for those widths; viewportFor() builds a Playwright viewport for one.
// Screenshots taken via shoot() are namespaced by breakpoint when a viewport
// width is supplied, so one result set per corridor state per width is kept.

import { chromium, firefox, webkit } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import url from 'node:url';

export const ENGINES = { chromium, firefox, webkit };
export const ENGINE_NAMES = Object.keys(ENGINES);

// Viewport widths required by the QA backlog entry (#213). Height is fixed so
// the recorded layout is comparable between runs.
export const BREAKPOINTS = [320, 375, 768, 1024, 1440];
export const VIEWPORT_HEIGHT = 900;

export const HERE = path.dirname(url.fileURLToPath(import.meta.url));
export const RESULTS_DIR = path.join(HERE, 'results');
export const SHOTS_DIR = path.join(RESULTS_DIR, 'screenshots');

// parseArgs reads `--key=value` flags. Unknown flags are collected so a typo is
// visible rather than silently ignored.
export function parseArgs(argv = process.argv.slice(2)) {
  const out = { _unknown: [] };
  for (const a of argv) {
    const m = /^--([^=]+)(?:=(.*))?$/.exec(a);
    if (!m) {
      out._unknown.push(a);
      continue;
    }
    out[m[1]] = m[2] === undefined ? true : m[2];
  }
  return out;
}

// parseBreakpoints reads a `--breakpoints=320,375,...` flag. Absent means the
// full BREAKPOINTS set. Any width not in BREAKPOINTS is rejected so a typo
// cannot silently produce an off-spec result set.
export function parseBreakpoints(value) {
  if (value === undefined || value === true) return [...BREAKPOINTS];
  const widths = String(value)
    .split(',')
    .map((s) => Number(s.trim()))
    .filter((n) => n !== 0 || n === 0);
  for (const w of widths) {
    if (!BREAKPOINTS.includes(w)) {
      throw new Error(`unsupported breakpoint ${w}; expected one of ${BREAKPOINTS.join(',')}`);
    }
  }
  return widths;
}

// viewportFor returns the Playwright viewport for one breakpoint width.
export function viewportFor(width) {
  return { width, height: VIEWPORT_HEIGHT };
}

// launch starts an engine and a context. launchOptions is passed to the engine
// itself, which is how a machine without Playwright's own browser download
// points at an installed Chrome or Edge (`{ executablePath }` or
// `{ channel }`).
export async function launch(engine, contextOptions = {}, launchOptions = {}) {
  const browser = await ENGINES[engine].launch(launchOptions);
  const context = await browser.newContext({
    // The UI reads no storage or permissions, but a fixed locale and timezone
    // make the recorded output comparable between runs.
    locale: 'en-GB',
    timezoneId: 'UTC',
    ...contextOptions,
  });
  return { browser, context, version: browser.version() };
}

// settle waits until the UI has rendered a terminal state: either an error
// panel (`.err`) or a completed corridor/provenance render. It returns quietly
// on timeout so a hung render is recorded as "never settled" rather than
// aborting the whole run.
export async function settle(page, timeout = 10000) {
  await page
    .waitForFunction(
      () => {
        const out = document.getElementById('out');
        if (!out) return false;
        return !!out.querySelector('.err, .provenance, .rec-none, .rec-some');
      },
      null,
      { timeout },
    )
    .catch(() => {});
  // One more tick so `finally { status.textContent = '' }` has run.
  await page.waitForTimeout(200);
}

// snapshotUI captures the observable state of the page after a request. It
// reads the DOM the way a user reads the screen rather than the way the code
// writes it, so the result is independent of how the markup is generated.
export async function snapshotUI(page) {
  return page.evaluate(() => {
    const el = (id) => document.getElementById(id);
    const text = (n) => (n ? n.innerText.replace(/\s+/g, ' ').trim() : null);
    const out = el('out');
    const select = el('to');
    const errs = out ? [...out.querySelectorAll('.panel.err')] : [];
    return {
      url: location.pathname + location.search,
      viewportWidth: window.innerWidth,
      outText: text(out),
      errorPanels: errs.map((p) => ({
        classes: p.className,
        text: text(p),
        // The machine-readable code the server sent is not in the DOM unless
        // the UI puts it there; record whether any panel references it.
        mentionsCodeField: /\bcode\b/i.test(p.innerHTML),
      })),
      hasProvenance: !!out?.querySelector('.provenance'),
      provenanceText: text(out?.querySelector('.provenance')),
      selectorOptions: select ? [...select.options].map((o) => o.textContent.trim()) : null,
      selectorValue: select ? select.value : null,
      selectorDisabled: select ? select.disabled : null,
      corridorDetail: text(el('cs-detail')),
      status: text(el('status')),
      footerProvenance: text(el('footer-provenance')),
    };
  });
}

// recordAPI attaches a response listener so the exact bytes the UI received are
// part of the artifact, not only the UI's rendering of them.
export function recordAPI(page) {
  const seen = [];
  page.on('response', (res) => {
    const u = res.url();
    if (!/\/api\/|\/healthz/.test(u)) return;
    const req = res.request();
    seen.push({
      method: req.method(),
      url: u.replace(/^https?:\/\/[^/]+/, ''),
      status: res.status(),
    });
  });
  return seen;
}

// recordAPIBodies is the same listener, but buffers the JSON bodies too. Only
// used where the body is the finding (the error-path matrix).
export function recordAPIBodies(page) {
  const seen = [];
  page.on('response', async (res) => {
    const u = res.url();
    if (!/\/api\//.test(u)) return;
    const entry = {
      method: res.request().method(),
      url: u.replace(/^https?:\/\/[^/]+/, ''),
      status: res.status(),
      body: null,
      bodyError: null,
    };
    try {
      entry.body = await res.json();
    } catch (e) {
      entry.bodyError = String(e.message || e);
    }
    seen.push(entry);
  });
  return seen;
}

export function ensureDirs() {
  fs.mkdirSync(SHOTS_DIR, { recursive: true });
}

export function writeResult(name, data) {
  ensureDirs();
  const file = path.join(RESULTS_DIR, name);
  fs.writeFileSync(file, JSON.stringify(data, null, 2) + '\n');
  return file;
}

// shoot writes a screenshot. When `width` is given the file is namespaced by
// breakpoint (`<name>@<width>.png`) so the per-breakpoint screenshot set for a
// corridor state can be produced without overwriting another width's shot.
export async function shoot(page, name, width) {
  ensureDirs();
  const base = width === undefined ? name : `${name}@${width}`;
  const file = path.join(SHOTS_DIR, `${base}.png`);
  await page.screenshot({ path: file, fullPage: false });
  return path.relative(HERE, file);
}

export function now() {
  return new Date().toISOString();
}

// waitForSelectorSoft resolves to false instead of throwing, so a missing
// element is a recorded observation rather than a crashed run.
export async function waitForSelectorSoft(page, selector, timeout = 5000) {
  try {
    await page.waitForSelector(selector, { timeout });
    return true;
  } catch {
    return false;
  }
}
