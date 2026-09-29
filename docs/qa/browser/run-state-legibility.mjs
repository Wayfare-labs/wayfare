// Issue #304 — every state is legible without colour.
//
// The contract: no state on this page may be carried by hue alone. A verdict
// was a `.v-good` / `.v-poor` / `.v-unusable` colour class and nothing else, so
// the moment colour went away — a greyscale print, a forced-colors mode, a
// reader who does not separate these hues — the grade went with it.
//
// What this runner does is read each state the way a reader without colour
// would: it looks for a word, a mark or a shape, and records what it found
// next to the colour the browser computed. The strongest evidence is the
// forced-colors pass, where the browser itself removes every author colour:
// if the four verdicts are still told apart there, they were never being told
// apart by colour.
//
// Usage (see ../README.md; servers via ./servers.sh):
//
//   node run-state-legibility.mjs [--base=URL] [--upstream-down=URL]
//                                 [--engine=chromium] [--executable=PATH]
//
// The corridors are chosen because the states they render are disjoint:
// NGNC is a live DIRECT corridor carrying an undetermined check and FAIR and
// POOR rungs, NGNT carries the GOOD rungs, GHSC is DERIVATIVE with every rung
// unusable, KESC has no market at all. The recorded chain — what a cold start
// renders — carries the stale banner and no findings, so the stale state is
// checked on every pass rather than asked for.
//
// Every response rendered here is one the running server produced. The one
// exception is the dark and forced-colors passes, which replay the exact bytes
// the light pass measured rather than spending a second full ladder of upstream
// requests on the same numbers; `liveSource` records which was which.

import { launch, settle, writeResult, shoot, now, parseArgs } from './lib.mjs';

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const DOWN = args['upstream-down'] || 'http://127.0.0.1:8098';
const ENGINE = String(args.engine || 'chromium');
const SCHEMES = ['light', 'dark'];
const WIDTHS = [320, 375, 768, 1024, 1440];

// A machine without Playwright's own browser download can point this at an
// installed Chrome or Edge instead.
const LAUNCH_OPTS = args.executable ? { executablePath: String(args.executable) } : {};

const CORRIDORS = [
  { code: 'NGNC', expected: 'DIRECT; FAIR, POOR and UNUSABLE rungs; an undetermined check' },
  { code: 'NGNT', expected: 'DIRECT; GOOD rungs; an undetermined check' },
  { code: 'GHSC', expected: 'DERIVATIVE; every rung unusable; no recommendation' },
  { code: 'KESC', expected: 'NO-MARKET; no rung priced' },
];

function check(name, pass, detail = null) {
  return { name, pass: !!pass, detail };
}

// A state is legible without colour when it carries at least one channel that
// survives the loss of colour: a word, a mark or a shape. This is the whole
// contract in one predicate, and every check below is a statement about a
// state that turned out to be on screen.
function hasNonColourChannel(state) {
  return !!(state.word || state.mark || state.shape);
}

// A grade key from a state label. "GOOD", "graded GOOD" and "GOOD." are the
// same state, so punctuation and the sentence around it are dropped before
// states are compared with one another.
const stateKey = (w) => String(w || '').replace(/[^A-Za-z-]/g, '').toUpperCase();

// legibilityChecks is the same contract applied to every group of states on
// screen: each state carries a non-colour channel, and no two states in a
// group are told apart by the same one. Sharing a glyph is as much a failure as
// having none — two states that print the same mark are one state as far as a
// reader without colour is concerned. A group that is not on screen is not
// asserted; "the element was absent" is recorded as absent.
function legibilityChecks(label, states) {
  const groups = {
    verdicts: states.verdicts,
    checkStates: states.checkStates,
    integrity: states.integrity,
    metrics: states.metrics,
  };
  const out = [];
  for (const [group, items] of Object.entries(groups)) {
    if (items.length === 0) continue;
    const marks = items.map((i) => i.mark).filter(Boolean);
    const markOwner = new Map();
    let shared = null;
    for (const i of items) {
      if (!i.mark) continue;
      const key = stateKey(i.word);
      if (markOwner.has(i.mark) && markOwner.get(i.mark) !== key) {
        shared = { mark: i.mark, states: [markOwner.get(i.mark), key] };
      }
      markOwner.set(i.mark, key);
    }
    out.push(
      check(`${label}: every ${group === 'verdicts' ? 'verdict' : group === 'checkStates' ? 'check state' : group === 'integrity' ? 'integrity state' : 'metric state'} carries a non-colour channel`,
        items.every(hasNonColourChannel), items),
      check(`${label}: ${group} states are not told apart by one shared mark`,
        !shared, shared || { states: [...new Set(items.map((i) => stateKey(i.word)))] }),
    );
    if (group === 'verdicts') {
      // The mark is decoration in front of the grade; assistive tech must read
      // the grade once, and a mark with no word in front of it is a mark
      // carrying the verdict alone.
      out.push(
        check(`${label}: every verdict mark is hidden from assistive tech and followed by a grade`,
          items.every((v) => v.ariaHidden === 'true' && /[A-Z]/.test(v.word)), items),
      );
    }
    if (marks.length === 0 && items.some((i) => i.word)) {
      out.push(
        check(`${label}: ${group} states carry a word beyond the mark`, true,
          { note: 'words only, which is a non-colour channel', marks: 0 }),
      );
    }
  }
  return out;
}

// geometryChecks catches a mark that is present in the DOM but wrong on the
// page: overlapping the grade it precedes, sitting above its line, or pushing
// out of the cell it belongs to. A text assertion passes on all three.
function geometryChecks(label, states) {
  const g = states.markGeometry;
  if (!g) return [];
  return [
    check(`${label}: the mark does not overlap the grade word`, g.overlapPx === 0, g),
    check(`${label}: the mark sits on the same line as the grade`,
      g.centreDeltaPx <= 5, g),
    check(`${label}: the mark stays inside its cell`, g.insideCell, g),
  ];
}

function provenanceChecks(label, states, expectLive) {
  if (!states.provenance) return [];
  return [
    check(`${label}: provenance word names a ${expectLive ? 'live' : 'recorded'} reading`,
      states.provenance.live === expectLive, states.provenance),
    // The banner differs in border style as well as hue, so "is this current?"
    // is answerable from the shape of the box before the words are read.
    check(`${label}: provenance border style distinguishes live from recorded`,
      expectLive ? states.provenance.borderTopStyle === 'solid'
                 : states.provenance.borderTopStyle === 'dashed',
      states.provenance),
  ];
}

function trendChecks(label, states) {
  const checks = [];
  if (states.runMarks.length > 0) {
    checks.push(
      check(`${label}: every plot mark is a shape, not only a fill`,
        states.runMarks.every((m) => !!m.shape), states.runMarks),
      // The key has to explain the shapes the plot actually uses, or the
      // shape is a puzzle rather than a channel.
      check(`${label}: the key draws every shape the plot uses`,
        states.runMarks.every((m) => states.keyMarks.some((k) => k.shape === m.shape)),
        { plot: states.runMarks, key: states.keyMarks }),
    );
  }
  if (states.keyMarks.length > 0) {
    checks.push(
      check(`${label}: the key names all three integrity states as shapes`,
        states.keyMarks.length === 3, states.keyMarks),
      // Named and given a role that can hold a name: aria-label on a plain div
      // is inert, so a key that names itself in the source can still be
      // announced as nothing at all.
      check(`${label}: the key is announced as a named group`,
        !!states.keyGroup && states.keyGroup.role === 'group' && !!states.keyGroup.name,
        states.keyGroup),
    );
  }
  return checks;
}

// waitForLive waits for the live button to be re-enabled, which is the only
// signal the UI emits when a measurement finishes either way. settle() is not
// enough here: a recorded render is already on screen, so its provenance
// element appears before the live request has even started.
async function waitForLive(page, timeout = 180000) {
  const settled = await page
    .waitForFunction(
      () => {
        const run = document.getElementById('run');
        return !!run && !run.disabled &&
          !!document.querySelector('#out .provenance-live, #out .panel.err');
      },
      null,
      { timeout },
    )
    .then(() => true)
    .catch(() => false);
  await page.waitForTimeout(200);
  return settled;
}

// The trend panel carries none of the elements settle() waits for, so it gets
// its own wait on the heading it is titled by.
async function waitForTrend(page, timeout = 30000) {
  const settled = await page
    .waitForFunction(
      () => /Trend/.test((document.getElementById('out') || {}).innerText || ''),
      null,
      { timeout },
    )
    .then(() => true)
    .catch(() => false);
  await page.waitForTimeout(200);
  return settled;
}

// readStates observes #out as a reader without colour would: for every state
// element it records the words, marks and shapes present, alongside the colour
// the browser resolved, so the two can be compared. Nothing is asserted here.
async function readStates(page) {
  return page.evaluate(() => {
    const out = document.getElementById('out');
    const norm = (s) => (s || '').replace(/\s+/g, ' ').trim();

    // The glyph a chip draws is a ::before, so it exists only in the computed
    // style, not in the text content.
    const glyphOf = (el) => {
      const c = getComputedStyle(el, '::before').content;
      return c && c !== 'none' && c !== 'normal' ? c.replace(/^["']|["']$/g, '') : null;
    };

    // A chart mark's shape: its tag, plus whether it is rotated, so a circle, a
    // diamond and a square read as three different things rather than one.
    const shapeOf = (el) =>
      el.tagName.toLowerCase() + (el.getAttribute('transform') ? '+rot' : '');

    // The text a mark sits in front of: everything after it inside its own
    // parent, so a table cell yields the grade and the recommendation line
    // yields the rest of the sentence.
    const after = (el) => {
      let s = '';
      for (let n = el.nextSibling; n; n = n.nextSibling) {
        s += n.nodeType === 3 ? n.textContent : n.nodeType === 1 ? n.innerText : '';
      }
      return norm(s);
    };

    const prov = out.querySelector('.provenance');
    const region = out.querySelector('.scroll');
    const doc = document.scrollingElement;

    return {
      viewport: { width: window.innerWidth, height: window.innerHeight },
      layout: {
        pageOverflowPx: doc.scrollWidth - doc.clientWidth,
        scrollRegion: region
          ? {
              overflowPx: region.scrollWidth - region.clientWidth,
              tabindex: region.getAttribute('tabindex'),
              role: region.getAttribute('role'),
              name: region.getAttribute('aria-label'),
            }
          : null,
      },
      verdicts: [...out.querySelectorAll('.v-mark')].map((m) => ({
        mark: m.textContent.trim(),
        word: after(m),
        colour: getComputedStyle(m.parentElement).color,
        ariaHidden: m.getAttribute('aria-hidden'),
      })),
      checkStates: [...out.querySelectorAll('.f-state')].map((c) => ({
        word: norm(c.innerText),
        mark: glyphOf(c),
        colour: getComputedStyle(c).color,
      })),
      integrity: [...out.querySelectorAll('.badge')].map((b) => ({
        word: norm(b.innerText),
        mark: glyphOf(b),
      })),
      metrics: [...out.querySelectorAll('.m-state')].map((m) => ({
        word: norm(m.innerText),
        mark: glyphOf(m),
      })),
      provenance: prov
        ? {
            word: norm(prov.innerText),
            live: prov.classList.contains('provenance-live'),
            borderTopStyle: getComputedStyle(prov).borderTopStyle,
            borderTopColor: getComputedStyle(prov).borderTopColor,
          }
        : null,
      runMarks: [...out.querySelectorAll('.run-mark')].map((e) => ({
        shape: shapeOf(e),
        fill: getComputedStyle(e).fill,
        box: (() => { const r = e.getBoundingClientRect(); return { w: +r.width.toFixed(2), h: +r.height.toFixed(2) }; })(),
      })),
      keyMarks: [...out.querySelectorAll('.key-mark')].map((e) => ({
        shape: shapeOf(e),
        fill: getComputedStyle(e).fill,
        box: (() => { const r = e.getBoundingClientRect(); return { w: +r.width.toFixed(2), h: +r.height.toFixed(2) }; })(),
      })),
      // The key is a group of three states, so it is named as one thing. A
      // label on a plain div is ignored by assistive tech, which would leave
      // the markup present and inert.
      keyGroup: (() => {
        const k = out.querySelector('.trend-legend');
        return k ? { role: k.getAttribute('role'), name: k.getAttribute('aria-label') } : null;
      })(),

      // The geometry of a mark next to the word it precedes. A chip is easy to
      // get wrong in CSS: an inline-flex box with no baseline of its own can
      // sit above the line or overlap the grade beside it, and neither shows
      // up in a text assertion. This measures both rectangles instead.
      markGeometry: (() => {
        const m = out.querySelector('.v-mark');
        if (!m) return null;
        const cell = m.closest('td') || m.parentElement;
        let node = m.nextSibling, textNode = null;
        while (node && !textNode) {
          if (node.nodeType === 3 && node.textContent.trim()) textNode = node;
          node = node.nextSibling;
        }
        if (!textNode) return null;
        const range = document.createRange();
        range.selectNode(textNode);
        const mr = m.getBoundingClientRect();
        const tr = range.getBoundingClientRect();
        const cr = cell.getBoundingClientRect();
        const box = (r) => ({ left: +r.left.toFixed(2), right: +r.right.toFixed(2), top: +r.top.toFixed(2), w: +r.width.toFixed(2), h: +r.height.toFixed(2) });
        return {
          mark: box(mr),
          text: box(tr),
          cell: box(cr),
          markFontSize: getComputedStyle(m).fontSize,
          markHasGlyph: document.fonts.check(`700 ${getComputedStyle(m).fontSize} ${getComputedStyle(m).fontFamily}`, m.textContent),
          overlapPx: Math.max(0, Math.min(mr.right, tr.right) - Math.max(mr.left, tr.left)),
          centreDeltaPx: +Math.abs((mr.top + mr.height / 2) - (tr.top + tr.height / 2)).toFixed(2),
          insideCell: mr.left >= cr.left - 0.5 && mr.right <= cr.right + 0.5,
        };
      })(),

      // What widens the page at a narrow viewport. An element inside a
      // clipping container is not to blame — its overflow is scrolled there —
      // so only unclipped elements are listed. Mirrors run-mobile.mjs.
      overflowOffenders: (() => {
        const vw = document.documentElement.clientWidth;
        const clipped = (el) => {
          for (let q = el.parentElement; q; q = q.parentElement) {
            if (['auto', 'scroll', 'hidden', 'clip'].includes(getComputedStyle(q).overflowX)) return true;
          }
          return false;
        };
        const found = [];
        for (const el of out.querySelectorAll('*')) {
          const r = el.getBoundingClientRect();
          if (r.width > 0 && r.right > vw + 1 && !clipped(el)) {
            found.push({ tag: el.tagName.toLowerCase(), className: String(el.className || '').slice(0, 48), right: Math.round(r.right), text: (el.textContent || '').trim().slice(0, 40) });
          }
        }
        return found.slice(0, 8);
      })(),
      recommend: {
        some: !!out.querySelector('.rec-some'),
        none: !!out.querySelector('.rec-none'),
      },
      banners: [...out.querySelectorAll('.panel.err')].map((b) => ({
        role: b.getAttribute('role'),
        word: norm(b.innerText),
      })),
    };
  });
}

// keyboardReach walks the tab order from the first control, reports how many
// stops it took to reach the horizontally scrolling region, and whether the
// arrow keys then move it. At 320px the verdict column is off the right edge,
// so this is the difference between a grade a keyboard can read and one it
// cannot.
async function keyboardReach(page, maxTabs = 12) {
  await page.evaluate(() => document.getElementById('to').focus());
  let tabs = 0;
  let reached = false;
  for (; tabs < maxTabs; tabs++) {
    await page.keyboard.press('Tab');
    reached = await page.evaluate(() => {
      const s = document.querySelector('#out .scroll');
      return !!s && document.activeElement === s;
    });
    if (reached) { tabs++; break; }
  }
  const before = await page.evaluate(() => document.querySelector('#out .scroll')?.scrollLeft ?? null);
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await page.waitForTimeout(250);
  const after = await page.evaluate(() => {
    const s = document.querySelector('#out .scroll');
    return s ? { scrollLeft: s.scrollLeft, max: s.scrollWidth - s.clientWidth } : null;
  });
  return {
    reached,
    tabsFromFirstControl: reached ? tabs : null,
    scrollLeftBefore: before,
    scrollLeftAfter: after ? after.scrollLeft : null,
    maxScroll: after ? after.max : null,
  };
}

// captureLive keeps the bytes of a live response so the dark and
// forced-colors passes can render the same measurement. Everything comes from
// the server; this only avoids paying for a second full ladder of upstream
// requests on identical numbers.
function captureLive(page, into) {
  page.on('response', async (res) => {
    const u = res.url();
    if (!/\/api\/corridor\?.*live=1/.test(u)) return;
    try {
      into.set(new URL(u).searchParams.get('to'), {
        body: await res.text(),
        status: res.status(),
        recordedAt: now(),
      });
    } catch { /* consumed or aborted: the pass records the state it got instead */ }
  });
}

// replayLive answers a live corridor request with bytes an earlier pass
// measured — the same convention run-error-preservation.mjs uses for a 502. A
// miss falls through to the server, and is counted, so a pass can never look
// covered by a replay that did not happen.
function replayLive(page, recorded) {
  const seen = { hits: 0, misses: [] };
  page.route(/\/api\/corridor\?.*live=1/, async (route) => {
    const to = new URL(route.request().url()).searchParams.get('to');
    const rec = recorded.get(to);
    if (!rec) {
      seen.misses.push(to);
      return route.continue();
    }
    seen.hits++;
    await route.fulfill({
      status: rec.status,
      headers: { 'content-type': 'application/json' },
      body: rec.body,
    });
  });
  return seen;
}

const out = {
  issue: '#304 — communicate every state without relying on colour',
  ranAt: now(),
  harness: 'docs/qa/browser/run-state-legibility.mjs',
  base: BASE,
  upstream_down: DOWN,
  scope: {
    schemes: SCHEMES,
    widths: WIDTHS,
    forcedColors:
      'Chromium is asked to drop every author colour, so whatever the passes still tell apart cannot be colour.',
    liveSource:
      'Measured once per corridor in the light pass; the dark and forced-colors passes replay those exact bytes, recorded per pass as liveSource.',
    notCovered: [
      'Firefox and WebKit, unless named with --engine',
      'The metrics panel: no code path wires a metric into a corridor response today, so the UNDETERMINED metric state is unreachable from any API response and is asserted only when present',
      'print stylesheets and prefers-contrast, which artifacts/272-color-schemes.md already records as open (C1–C4)',
    ],
    preExistingFindings: [
      // Recorded, not fixed: the finding-state legend keeps three columns at
      // 320 and 375px, so "Could not determine" overhangs the viewport by 35px
      // and 17px. Measured on the commit this change starts from, at the same
      // widths and with the same server, so the marks this change adds are not
      // what causes it. A responsive fix to .legend-grid is its own change.
      'the page scrolls sideways at 320 and 375px by 35px and 17px, from the three-column .legend-grid (also present before this change)',
    ],
  },
  passes: [],
};

const liveRecordings = new Map();
const allChecks = [];
function collect(pass, checks) {
  allChecks.push(...checks);
  pass.checks = checks;
  out.passes.push(pass);
  const failed = checks.filter((c) => !c.pass);
  console.log(
    `${pass.name}: ${checks.length - failed.length}/${checks.length} checks passed` +
    (failed.length ? ` — FAILED: ${failed.map((f) => f.name).join('; ')}` : ''),
  );
}

async function withBrowser(contextOptions, name, run) {
  const { browser, context, version } = await launch(ENGINE, contextOptions, LAUNCH_OPTS);
  const pass = { name, browserVersion: version, width: contextOptions.viewport?.width, checks: [] };
  try {
    const page = await context.newPage();
    pass.consoleErrors = [];
    page.on('console', (m) => m.type() === 'error' && pass.consoleErrors.push(m.text()));
    await run(page, pass);
  } catch (e) {
    pass.error = String(e.message || e);
    collect(pass, [check(`${name}: pass completed`, false, pass.error)]);
  } finally {
    await context.close().catch(() => {});
    await browser.close().catch(() => {});
  }
  return pass;
}

// Pass 1 — the state inventory, one corridor at a time: every verdict, every
// integrity state, every check state, and the stale banner, from real
// responses.
for (const corridor of CORRIDORS) {
  for (const scheme of SCHEMES) {
    await withBrowser(
      { colorScheme: scheme, viewport: { width: 1024, height: 900 } },
      `states/${corridor.code}/${scheme}`,
      async (page, pass) => {
        Object.assign(pass, { corridor: corridor.code, expected: corridor.expected, scheme });
        const measured = scheme === 'light';
        if (measured) captureLive(page, liveRecordings);
        else pass.replay = replayLive(page, liveRecordings);
        pass.liveSource = measured ? 'measured' : 'replayed';

        await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
        await settle(page, 30000);

        // The recorded reading a cold start renders: stale banner, no findings.
        if ((await page.evaluate(() => document.getElementById('to').value)) !== corridor.code) {
          await page.selectOption('#to', corridor.code);
          await settle(page, 30000);
        }
        const recorded = await readStates(page);
        pass.recorded = recorded;
        pass.recordedScreenshot = await shoot(page, `304-${corridor.code}-${scheme}-1024-recorded`);

        await page.click('#run');
        pass.liveSettled = await waitForLive(page);
        const live = await readStates(page);
        pass.live = live;
        pass.liveScreenshot = await shoot(page, `304-${corridor.code}-${scheme}-1024-live`);

        await page.click('#trend');
        pass.trendSettled = await waitForTrend(page);
        const trend = await readStates(page);
        pass.trend = trend;
        pass.trendScreenshot = await shoot(page, `304-${corridor.code}-${scheme}-1024-trend`);

        collect(pass, [
          ...legibilityChecks('recorded', recorded),
          ...geometryChecks('recorded', recorded),
          ...provenanceChecks('recorded', recorded, false),
          ...legibilityChecks('live', live),
          ...geometryChecks('live', live),
          ...provenanceChecks('live', live, true),
          ...legibilityChecks('trend', trend),
          ...trendChecks('trend', trend),
          check('live: the measurement settled', !!pass.liveSettled, { liveSettled: pass.liveSettled }),
          check('live: no error banner', live.banners.length === 0, live.banners),
        ]);
      },
    );
  }
}

// Pass 2 — the width matrix. Layout is the question at the five widths the
// issue names, in both schemes: nothing overflows the page, the state words
// survive at 320px, and the verdict column is reachable by keyboard through a
// region that takes focus.
for (const scheme of SCHEMES) {
  for (const width of WIDTHS) {
    await withBrowser(
      { colorScheme: scheme, viewport: { width, height: 900 } },
      `widths/${scheme}/${width}`,
      async (page, pass) => {
        Object.assign(pass, { scheme });
        await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
        await settle(page, 30000);
        const recorded = await readStates(page);
        pass.recorded = recorded;
        pass.keyboard = await keyboardReach(page);

        // Only the extremes are screenshotted; the recorded JSON carries the
        // measurements for all five, and a fifty-shot artifact helps nobody.
        if (width === 320 || width === 1440) {
          pass.recordedScreenshot = await shoot(page, `304-widths-${scheme}-${width}-recorded`);
        }

        await page.click('#trend');
        await waitForTrend(page);
        const trend = await readStates(page);
        pass.trend = trend;
        if (width === 320) {
          pass.trendScreenshot = await shoot(page, `304-widths-${scheme}-${width}-trend`);
        }

        collect(pass, [
          check(`${width}: the page does not scroll sideways`,
            recorded.layout.pageOverflowPx <= 1,
            { ...recorded.layout, offenders: recorded.overflowOffenders }),
          check(`${width}: the state words survive at this width`,
            recorded.verdicts.length > 0 && recorded.verdicts.every((v) => /[A-Z]/.test(v.word)),
            recorded.verdicts),
          check(`${width}: the scrolling table region takes focus and is named`,
            !!recorded.layout.scrollRegion &&
            recorded.layout.scrollRegion.tabindex === '0' &&
            recorded.layout.scrollRegion.role === 'region' &&
            !!recorded.layout.scrollRegion.name,
            recorded.layout.scrollRegion),
          check(`${width}: the verdict column is keyboard reachable`,
            pass.keyboard.reached &&
            (pass.keyboard.maxScroll === 0 || pass.keyboard.scrollLeftAfter > 0),
            pass.keyboard),
          ...legibilityChecks(`${width}`, recorded),
          ...geometryChecks(`${width}`, recorded),
          ...trendChecks(`${width}`, trend),
        ]);
      },
    );
  }
}

// Pass 3 — forced colors. The browser drops every author colour, so the
// verdicts have to be told apart by their marks and words alone. This is the
// pass that fails without the change.
for (const corridor of CORRIDORS.slice(0, 2)) {
  await withBrowser(
    { colorScheme: 'light', forcedColors: 'active', viewport: { width: 1024, height: 900 } },
    `forced-colors/${corridor.code}`,
    async (page, pass) => {
      Object.assign(pass, { corridor: corridor.code, scheme: 'light + forced-colors: active', liveSource: 'replayed' });
      pass.replay = replayLive(page, liveRecordings);
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page, 30000);
      if ((await page.evaluate(() => document.getElementById('to').value)) !== corridor.code) {
        await page.selectOption('#to', corridor.code);
        await settle(page, 30000);
      }
      await page.click('#run');
      pass.liveSettled = await waitForLive(page);
      const live = await readStates(page);
      pass.live = live;
      pass.screenshot = await shoot(page, `304-forced-${corridor.code}-1024-live`);

      const colours = [...new Set(live.verdicts.map((v) => v.colour))];
      const words = new Set(live.verdicts.map((v) => stateKey(v.word)));
      const marks = new Set(live.verdicts.map((v) => v.mark));
      collect(pass, [
        ...legibilityChecks('forced', live),
        ...geometryChecks('forced', live),
        // The premise of the pass, recorded rather than assumed: with author
        // colours dropped one colour is left, so nothing in this render can
        // be reading a hue.
        check('forced: the browser did drop the author colours', colours.length === 1, colours),
        check('forced: the verdicts are still told apart one from another',
          words.size > 1 && words.size === marks.size, { words: [...words], marks: [...marks] }),
      ]);
    },
  );
}

// Pass 4 — the error banner is a state too, and it is exercised against the
// real 502 the upstream-down instance returns rather than an injected failure.
await withBrowser(
  { colorScheme: 'light', viewport: { width: 1024, height: 900 } },
  'error-banner',
  async (page, pass) => {
    Object.assign(pass, { base: DOWN });
    await page.goto(DOWN + '/', { waitUntil: 'domcontentloaded' });
    await settle(page, 30000);
    await page.click('#run');
    pass.liveSettled = await waitForLive(page, 60000);
    const states = await readStates(page);
    pass.states = states;
    pass.screenshot = await shoot(page, '304-error-banner-1024');
    collect(pass, [
      check('error: the banner is announced and names the failure',
        states.banners.length === 1 && states.banners[0].role === 'alert' &&
        states.banners[0].word.length > 0, states.banners),
    ]);
  },
);

const failed = allChecks.filter((c) => !c.pass);
out.summary = {
  passes: out.passes.length,
  checks: allChecks.length,
  failed: failed.length,
  failedChecks: failed.map((c) => c.name),
};

const file = writeResult('state-legibility.json', out);
console.log(
  `\n${failed.length === 0 ? 'all checks passed' : failed.length + ' checks FAILED'} ` +
  `(${allChecks.length} across ${out.passes.length} passes) — result: ${file}`,
);
