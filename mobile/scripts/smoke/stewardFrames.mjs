// Shared frame helpers of the steward stages (luminance and overflow checks, scroll paging, both-scheme snaps, tap-target
// checks). Copied from stages/85-steward-review.mjs rather than imported: that stage keeps them private and lib.mjs is frozen.

import { join } from 'node:path';
import * as lib from './lib.mjs';

export async function has(page, text, timeout = 15_000) {
  try {
    await page.getByText(text).first().waitFor({ timeout });
    return true;
  } catch {
    return false;
  }
}

export async function expectText(page, text, label, timeout = 15_000) {
  if (await has(page, text, timeout)) return true;
  lib.fail(`${label}: "${text}" did not appear`);
  return false;
}

/** Mean luminance (0..1) of a PNG buffer, decoded in the page (no image dependency). */
export async function luminance(page, buf) {
  return page.evaluate(async (b64) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const w = 96;
    const h = Math.max(1, Math.round((img.height / img.width) * w));
    const c = document.createElement('canvas');
    c.width = w;
    c.height = h;
    const ctx = c.getContext('2d');
    ctx.drawImage(img, 0, 0, w, h);
    const d = ctx.getImageData(0, 0, w, h).data;
    let sum = 0;
    for (let i = 0; i < d.length; i += 4) sum += (0.2126 * d[i] + 0.7152 * d[i + 1] + 0.0722 * d[i + 2]) / 255;
    return sum / (d.length / 4);
  }, buf.toString('base64'));
}

/** No horizontal page scroll and no element wider than the viewport. */
export async function overflow(page, name) {
  const r = await page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const bad = [];
    for (const el of document.querySelectorAll('body *')) {
      const b = el.getBoundingClientRect();
      if (b.width === 0 || b.height === 0) continue;
      if (b.right > vw + 1 || b.left < -1) bad.push(`${el.tagName.toLowerCase()} ${Math.round(b.left)}..${Math.round(b.right)} "${(el.textContent ?? '').trim().slice(0, 40)}"`);
    }
    return { sw: document.documentElement.scrollWidth, cw: vw, bad: bad.slice(0, 6), n: bad.length };
  });
  if (r.sw > r.cw) lib.fail(`${name}: page scrolls horizontally (scrollWidth ${r.sw} > ${r.cw})`);
  if (r.n > 0) lib.fail(`${name}: ${r.n} element(s) outside the viewport: ${r.bad.join(' | ')}`);
}

/** The main vertical scroller: the element with the largest overflowing scrollHeight. */
export async function scrollInfo(page) {
  return page.evaluate(() => {
    let best = null;
    for (const el of document.querySelectorAll('*')) {
      const o = getComputedStyle(el).overflowY;
      if ((o === 'auto' || o === 'scroll') && el.scrollHeight > el.clientHeight + 4 && el.clientHeight > 200) {
        // Only the screen on top: a stack keeps earlier screens mounted, and their scrollers must not win.
        const r = el.getBoundingClientRect();
        const top = document.elementFromPoint(r.left + r.width / 2, Math.min(r.top + r.height / 2, innerHeight - 5));
        if (!top || !el.contains(top)) continue;
        if (!best || el.scrollHeight > best.scrollHeight) best = el;
      }
    }
    if (!best) return null;
    best.setAttribute('data-smoke-scroller', '1');
    return { total: best.scrollHeight, view: best.clientHeight, top: best.scrollTop, tag: `${best.tagName} ${best.className.toString().slice(0, 60)}` };
  });
}

export async function scrollTo(page, y) {
  await page.evaluate((top) => {
    const el = document.querySelector('[data-smoke-scroller]');
    if (el) {
      el.style.scrollBehavior = 'auto';
      el.scrollTop = top;
    }
  }, y);
  await page.waitForTimeout(150);
}

/** One checked screenshot. */
export async function shot(page, name, scheme, fullPage = false) {
  const path = join(lib.SMOKE_DIR, `${name}-${scheme}.png`);
  const ok = (l) => (scheme === 'light' ? l > 0.8 : l < 0.25);
  let lum = await luminance(page, await page.screenshot({ path, fullPage }));
  if (!ok(lum)) {
    // The theme repaint can lag behind the media flip: look once more before calling it a defect.
    await page.waitForTimeout(1000);
    const again = await luminance(page, await page.screenshot({ path, fullPage }));
    if (ok(again)) console.log(`note: ${name}-${scheme} needed a second look (first lum ${lum.toFixed(3)}, then ${again.toFixed(3)}): the theme repaint lagged >300 ms`);
    lum = again;
    if (!ok(again)) {
      const mm = await page.evaluate(() => ({ dark: matchMedia('(prefers-color-scheme: dark)').matches, light: matchMedia('(prefers-color-scheme: light)').matches }));
      console.log(`debug: ${name}-${scheme} still wrong after 1 s; matchMedia ${JSON.stringify(mm)}`);
      // A real flip through the other scheme: if THAT repaints, the app had missed the media event (stale theme).
      await page.emulateMedia({ colorScheme: scheme === 'light' ? 'dark' : 'light' });
      await page.waitForTimeout(250);
      await page.emulateMedia({ colorScheme: scheme });
      await page.waitForTimeout(500);
      lum = await luminance(page, await page.screenshot({ path, fullPage }));
      console.log(`note: STALE-THEME ${name}-${scheme}: the app kept the other theme while matchMedia said ${scheme}; after a flip lum ${lum.toFixed(3)} (${ok(lum) ? 'repainted' : 'still wrong'})`);
    }
  }
  if (scheme === 'light' && !(lum > 0.8)) lib.fail(`${name}-light: mean luminance ${lum.toFixed(3)} is not > 0.8`);
  if (scheme === 'dark' && !(lum < 0.25)) lib.fail(`${name}-dark: mean luminance ${lum.toFixed(3)} is not < 0.25`);
  await overflow(page, `${name}-${scheme}`);
  console.log(`wrote ${name}-${scheme}.png (lum ${lum.toFixed(2)})`);
}

/** Both schemes; a tall scroller adds one frame per extra page (name-p2, name-p3...). */
export async function snap(page, name, pages = true) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.waitForTimeout(300);
    await page.evaluate(() => document.querySelector('[data-smoke-scroller]')?.removeAttribute('data-smoke-scroller'));
    const info = pages ? await scrollInfo(page) : null;
    await shot(page, name, scheme);
    if (info) {
      const step = Math.max(200, info.view - 80);
      const max = info.total - info.view;
      const ys = [];
      for (let y = step; y < max; y += step) ys.push(y);
      if (max > 40 && (ys.length === 0 || max - ys[ys.length - 1] > 40)) ys.push(max);
      for (const [i, y] of ys.slice(0, 5).entries()) {
        await scrollTo(page, y);
        await shot(page, `${name}-p${i + 2}`, scheme);
      }
      await scrollTo(page, 0);
    }
  }
  await page.emulateMedia({ colorScheme: 'light' });
  await page.waitForTimeout(400);
}

export async function backArrow(page, label) {
  const n = await page.getByRole('link', { name: /(, back|^Go back)$/ }).count();
  if (n === 0) lib.fail(`${label}: no back arrow in the native-stack header`);
  return n > 0;
}

export async function bodyText(page) {
  return page.evaluate(() => document.body.innerText);
}

export async function noForbidden(page, label) {
  const t = await bodyText(page);
  for (const bad of ['Always allow', 'Approve all', 'Merge']) {
    if (t.includes(bad)) lib.fail(`${label}: page text contains "${bad}"`);
  }
}

export async function sequence(page, name, scheme, act) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.waitForTimeout(250);
  let done = null;
  for (let i = 0; i < 10; i++) {
    if (i === 1) done = act();
    await page.screenshot({ path: join(lib.SMOKE_DIR, `${name}-${scheme}-${String(i).padStart(2, '0')}.png`) });
    await page.waitForTimeout(25);
  }
  await done;
  console.log(`wrote ${name}-${scheme}-00..09.png`);
}

export async function seed(harness, line) {
  const r = await harness.cmd(line);
  if (!r.ok) lib.fail(`${line}: ${JSON.stringify(r)}`);
  return r;
}

/** Every given locator is a real tap target of at least 44 px height. */
export async function tapTargets(page, label, locators) {
  for (const [what, loc] of locators) {
    const n = await loc.count();
    if (n === 0) {
      lib.fail(`${label}: tap target "${what}" not found`);
      continue;
    }
    for (let i = 0; i < Math.min(n, 4); i++) {
      const b = await loc.nth(i).boundingBox();
      if (b && b.height < 43.5) lib.fail(`${label}: tap target "${what}" is ${Math.round(b.height)} px high (< 44)`);
    }
  }
}

