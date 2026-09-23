// Issue #270 — Mobile device QA on real hardware.
//
// The issue's own framing is the constraint: "Emulator width is not touch
// behaviour; the .scroll table is the thing to watch." So this pass records
// two different things and keeps them apart:
//
//   1. what emulation can answer — layout, horizontal overflow, whether the
//      wide table is contained in .scroll, the responsive breakpoint, and
//      whether a touch gesture pans that container;
//   2. what it cannot answer — real-device behaviour (inertia, momentum,
//      overscroll, iOS Safari bounce, touch-target comfort, thumb reach).
//
// Real hardware was not available in this environment. That is recorded as the
// finding rather than papered over: the artifact says exactly which checks are
// emulated and which are untested.
//
// Usage:
//
//   node run-mobile.mjs [--base=URL] [--engines=chromium,firefox,webkit]
//
// Note: dispatching a real touch gesture requires the Chrome DevTools Protocol,
// so only the Chromium run performs a genuine touch swipe. Firefox and WebKit
// runs pan the container with a wheel and say so.

import { chromium, firefox, webkit, devices } from 'playwright';
import { settle, writeResult, shoot, now } from './lib.mjs';
import { parseArgs } from './lib.mjs';

const ENGINE_MAP = { chromium, firefox, webkit };

const args = parseArgs();
const BASE = args.base || 'http://127.0.0.1:8099';
const ENGINES = String(args.engines || 'chromium,firefox,webkit').split(',');
const DEVICE_NAMES = String(args.devices || 'iPhone SE,iPhone 13,Pixel 7,iPad Mini').split(',');

// A real touch drag, via CDP. Only Chromium exposes the protocol.
async function cdpSwipe(page, { x, y, dx, steps = 10 }) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  for (let i = 1; i <= steps; i++) {
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ x: x + (dx * i) / steps, y }],
    });
  }
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await page.waitForTimeout(400);
}

// A wheel pan, used where no touch protocol is exposed.
async function wheelPan(page, { x, y, dx }) {
  await page.mouse.move(x, y);
  await page.mouse.wheel(dx, 0);
  await page.waitForTimeout(400);
}

async function probe(page) {
  return page.evaluate(() => {
    const doc = document.scrollingElement;
    const scroll = document.querySelector('.scroll');
    const card = document.querySelector('.integrity-card');
    const table = document.querySelector('#out table');

    // Which element, if any, is forcing the *page* to scroll horizontally?
    // An element inside a clipping container (overflow-x not visible) is not
    // to blame: its overflow is contained and scrolled there. Only elements
    // with no clipping ancestor can widen the document.
    const clipped = (el) => {
      for (let q = el.parentElement; q; q = q.parentElement) {
        if (['auto', 'scroll', 'hidden', 'clip'].includes(getComputedStyle(q).overflowX)) return true;
      }
      return false;
    };
    const vw = document.documentElement.clientWidth;
    const offenders = [];
    for (const el of document.querySelectorAll('*')) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.right > vw + 1 && !clipped(el)) {
        offenders.push({
          tag: el.tagName.toLowerCase(),
          classes: String(el.className || '').slice(0, 60),
          id: el.id || null,
          width: Math.round(r.width),
          right: Math.round(r.right),
          text: (el.textContent || '').trim().slice(0, 40),
        });
      }
    }

    return {
      viewport: { width: window.innerWidth, height: window.innerHeight },
      devicePixelRatio: window.devicePixelRatio,
      hasTouch: 'ontouchstart' in window || navigator.maxTouchPoints > 0,
      maxTouchPoints: navigator.maxTouchPoints,
      pageScrollWidth: doc.scrollWidth,
      pageClientWidth: doc.clientWidth,
      pageHorizontalOverflow: doc.scrollWidth - doc.clientWidth,
      scrollPresent: !!scroll,
      scrollClientWidth: scroll ? scroll.clientWidth : null,
      scrollWidth: scroll ? scroll.scrollWidth : null,
      scrollLeft: scroll ? scroll.scrollLeft : null,
      scrollMax: scroll ? scroll.scrollWidth - scroll.clientWidth : null,
      integrityCardColumns: card ? getComputedStyle(card).gridTemplateColumns : null,
      tablePresent: !!table,
      scrollBox: scroll ? (() => { const r = scroll.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height }; })() : null,
      pageOverflowOffenders: offenders.slice(0, 6),
    };
  });
}

const out = {
  issue: '#270 — Mobile device QA on real hardware',
  ranAt: now(),
  harness: 'docs/qa/browser/run-mobile.mjs',
  base: BASE,
  scope: {
    realHardwareTested: false,
    why: 'No physical device was available in this environment. Every result below is device emulation in a desktop browser engine.',
    emulationCannotAnswer: [
      'Momentum and inertia of a real finger swipe',
      'iOS Safari rubber-band overscroll and its effect on a horizontally scrolling table',
      'Android system back-gesture conflicts with horizontal scroll regions near the screen edge',
      'Real touch-target size and thumb reach',
      'Font metrics and text reflow of the actual device fonts (iOS uses San Francisco, Android Roboto)',
      'Keyboard behaviour when the on-screen keyboard covers the viewport',
    ],
  },
  devices: [],
};

for (const engine of ENGINES) {
  for (const deviceName of DEVICE_NAMES) {
    const descriptor = devices[deviceName];
    if (!descriptor) {
      out.devices.push({ engine, device: deviceName, error: 'no such Playwright device descriptor' });
      continue;
    }

    if (!ENGINE_MAP[engine]) {
      out.devices.push({ engine, device: deviceName, error: `unknown engine ${engine}` });
      continue;
    }
    const browser = await ENGINE_MAP[engine].launch();
    const context = await browser.newContext({ ...descriptor, locale: 'en-GB', timezoneId: 'UTC' });
    const page = await context.newPage();

    const record = { engine, device: deviceName, descriptor: { viewport: descriptor.viewport, isMobile: descriptor.isMobile, hasTouch: descriptor.hasTouch, deviceScaleFactor: descriptor.deviceScaleFactor } };

    try {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page, 20000);
      record.layout = await probe(page);

      // Touch/tap: does the primary control respond to a tap at all?
      const before = await page.evaluate(() => document.getElementById('status').textContent);
      await page.touchscreen.tap(
        await page.evaluate(() => {
          const r = document.getElementById('run').getBoundingClientRect();
          return r.x + r.width / 2;
        }),
        await page.evaluate(() => {
          const r = document.getElementById('run').getBoundingClientRect();
          return r.y + r.height / 2;
        }),
      );
      await page.waitForTimeout(150);
      const after = await page.evaluate(() => ({
        status: document.getElementById('status').textContent,
        disabled: document.getElementById('run').disabled,
      }));
      record.tapMeasure = { before, after, responded: after.status !== before || after.disabled === true };

      // Reload to get back to a quiet page before the pan test.
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await settle(page, 20000);

      // The table sits well below the fold on a phone, so a gesture dispatched
      // at its layout position would land outside the visible viewport and be
      // recorded as a phantom failure. Bring it into view first, then use
      // viewport-relative coordinates.
      await page.locator('.scroll').scrollIntoViewIfNeeded();
      await page.waitForTimeout(300);

      const box = (await probe(page)).scrollBox;
      if (box && box.width > 0) {
        const startX = box.x + box.width * 0.75;
        const y = Math.min(Math.max(box.y + 20, 20), (await page.evaluate(() => window.innerHeight)) - 20);
        try {
          if (engine === 'chromium') {
            await cdpSwipe(page, { x: startX, y, dx: -180 });
            record.pan = { method: 'CDP Input.dispatchTouchEvent (real touch gesture)', performed: true };
          } else if (engine === 'firefox') {
            await wheelPan(page, { x: startX, y, dx: 180 });
            record.pan = { method: 'mouse wheel (this engine exposes no touch-drag API)', performed: true };
          } else {
            record.pan = {
              method: null,
              performed: false,
              reason: 'Playwright raises "Mouse wheel is not supported in mobile WebKit" and exports no touch-drag API; a swipe could not be performed.',
            };
          }
          const afterPan = await probe(page);
          if (record.pan.performed) {
            record.pan.scrollLeftBefore = 0;
            record.pan.scrollLeftAfter = afterPan.scrollLeft;
            record.pan.panned = (afterPan.scrollLeft ?? 0) !== 0;
            record.pan.documentScrolledInstead = (await page.evaluate(() => document.scrollingElement.scrollLeft)) !== 0;
            record.pan.scrollMax = afterPan.scrollMax;
          }
        } catch (e) {
          record.pan = { method: null, performed: false, error: String(e.message || e) };
        }
      }

      record.screenshot = await shoot(page, `mobile-${engine}-${deviceName.replace(/\s+/g, '')}`);
    } catch (e) {
      record.error = String(e.message || e);
    }

    out.devices.push(record);
    await context.close();
    await browser.close();
  }
}


const file = writeResult('mobile.json', out);
console.log(`wrote ${file}`);
for (const d of out.devices) {
  if (d.error) {
    console.log(`${d.device} (${d.engine}) ERROR ${d.error}`);
    continue;
  }
  const l = d.layout || {};
  console.log(
    `${d.device} (${d.engine}): viewport ${l.viewport?.width}x${l.viewport?.height} ` +
      `scroll ${l.scrollClientWidth}/${l.scrollWidth} overflow=${l.scrollMax} ` +
      `page=${l.pageScrollWidth}/${l.pageClientWidth} pan=${d.pan?.panned}`,
  );
}
