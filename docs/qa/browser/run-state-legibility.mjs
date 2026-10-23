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
//
// Stale-reading regression (#209): the recorded chain must render with a
// stale/recorded provenance banner, never as live. Every pass asserts that the
// banner is present, that it names a recorded reading, and that its border style
// differs from the live banner. The live-banner expectation is asserted against
// the live measurement, so a recorded reading that renders as live fails here.

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

// provenanceChecks is the stale-reading regression guard (#209). A recorded
// reading must render with a recorded provenance banner and a dashed border; a
// live reading must render with a live banner and a solid border. Either way the
// banner must be present whenever the state is on screen at all — a reading
// with no provenance banner is a reading whose currency is unknown.
function provenanceChecks(label, states, expectLive) {
  if (!states.provenance) {
    return [
      check(`${label}: a provenance banner is present`, false,
        { note: 'no .provenance element rendered; currency of the reading is unknown' }),
    ];
  }
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
            text: norm(prov.innerText),
            live: prov.classList.contains('provenance-live'),
            borderTopStyle: getComputedStyle(prov).borderTopStyle,
            colour: getComputedStyle(prov).color,
          }
        : null,
    };
  });
}
