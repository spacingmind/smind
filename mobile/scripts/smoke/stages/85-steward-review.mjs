// ADR-0047 S3b: the steward screens at 390 px, light + dark, on fake data: the inbox review rows (verdict chips, the
// Permission policy link), the review screen (reached by tapping its inbox row), Resolve, the permission card's policy
// line, and the read-only policy page (reached by the inbox link). Every pushed screen's back arrow is asserted.
//
// Needs an ISOLATED SMIND_HOME (never ~/.smind): run as
//   SMIND_HOME=<isolated dir> SMOKE_OUT=<dir> node scripts/smoke-web.mjs --full --stage steward-review
// The stage writes $SMIND_HOME/config.yaml (steward.policy.enabled) and $SMIND_HOME/steward/policy.yaml BEFORE the
// daemon (the devharness) starts, seeds fake reviews / audit rows through the harness steward commands (seed-review,
// seed-audit) and runs the fake agent's permission-command scenarios. No model is called.
//
// Frames (<name>-<light|dark>.png; a tall screen adds <name>-p2-..., one per further scroll page): inbox-reviews,
// inbox-stale-before, inbox-stale-after, review-accepted, review-changes, review-blocked, review-frames-collapsed,
// review-frames-expanded (+ seq-expand-<scheme>-NN), inbox-after-resolve, permission-matched, permission-nomatch,
// inbox-policy-link, policy-page-top, policy-page-rules, policy-page-audit, policy-audit-expanded, policy-invalid.
// Every frame is asserted: mean luminance (light > 0.8, dark < 0.25), no horizontal overflow at the viewport width;
// tap targets are >= 44 px high. A screen that does not exist is reported as PENDING (console + PENDING.txt), not as a
// failure, unless STEWARD_STRICT=1.

import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import * as lib from '../lib.mjs';

const HOME = process.env.SMIND_HOME ?? '';
const STRICT = process.env.STEWARD_STRICT === '1';
const pending = [];

const POLICY_OK = `version: 1
rules:
  - id: npm-test
    action: allow
    why: "Run the project's tests inside the task worktree"
    match:
      tool: Bash
      command:
        argv0: npm
        subcommand: test
      paths:
        within: ["{worktree}"]
  - id: git-status
    action: allow
    why: "Read-only: show the working tree status"
    match:
      tool: Bash
      command:
        argv0: git
        subcommand: status
      paths:
        within: ["{worktree}"]
  - id: ask-npm
    action: ask
    why: "A human reads every npm run: the command can run any package script"
    match:
      tool: Bash
      command:
        argv0: npm
  - id: deny-force-push
    action: deny
    why: "Force pushing rewrites shared history; only the Human does it"
    match:
      tool: Bash
      command:
        argv0: git
        subcommand: push
        args:
          deny_flags: ["-f", "--force", "--force-with-lease"]
`;
const POLICY_BAD = 'version: 1\nrules:\n  - id: [broken\n    action: allow: nope\n';

function w(path, content) {
  mkdirSync(join(path, '..'), { recursive: true });
  writeFileSync(path, content);
}

function markPending(what) {
  pending.push(what);
  console.log(`PENDING: ${what}`);
  if (STRICT) lib.fail(`pending screen (strict): ${what}`);
}

async function has(page, text, timeout = 15_000) {
  try {
    await page.getByText(text).first().waitFor({ timeout });
    return true;
  } catch {
    return false;
  }
}

async function expectText(page, text, label, timeout = 15_000) {
  if (await has(page, text, timeout)) return true;
  lib.fail(`${label}: "${text}" did not appear`);
  return false;
}

/** Mean luminance (0..1) of a PNG buffer, decoded in the page (no image dependency). */
async function luminance(page, buf) {
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
async function overflow(page, name) {
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
async function scrollInfo(page) {
  return page.evaluate(() => {
    let best = null;
    for (const el of document.querySelectorAll('*')) {
      const o = getComputedStyle(el).overflowY;
      if ((o === 'auto' || o === 'scroll') && el.scrollHeight > el.clientHeight + 4 && el.clientHeight > 200) {
        if (!best || el.scrollHeight > best.scrollHeight) best = el;
      }
    }
    if (!best) return null;
    best.setAttribute('data-smoke-scroller', '1');
    return { total: best.scrollHeight, view: best.clientHeight };
  });
}

async function scrollTo(page, y) {
  await page.evaluate((top) => document.querySelector('[data-smoke-scroller]')?.scrollTo(0, top), y);
  await page.waitForTimeout(150);
}

/** One checked screenshot. */
async function shot(page, name, scheme, fullPage = false) {
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
async function snap(page, name, pages = true) {
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

async function backArrow(page, label) {
  const n = await page.getByRole('link', { name: /(, back|^Go back)$/ }).count();
  if (n === 0) lib.fail(`${label}: no back arrow in the native-stack header`);
  return n > 0;
}

async function bodyText(page) {
  return page.evaluate(() => document.body.innerText);
}

async function noForbidden(page, label) {
  const t = await bodyText(page);
  for (const bad of ['Always allow', 'Approve all', 'Merge']) {
    if (t.includes(bad)) lib.fail(`${label}: page text contains "${bad}"`);
  }
}

async function sequence(page, name, scheme, act) {
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

async function seed(harness, line) {
  const r = await harness.cmd(line);
  if (!r.ok) lib.fail(`${line}: ${JSON.stringify(r)}`);
  return r;
}

/** Every given locator is a real tap target of at least 44 px height. */
async function tapTargets(page, label, locators) {
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

async function onTaskList(page) {
  return page.getByRole('button', { name: /^Inbox/ }).first().isVisible().catch(() => false);
}

/** Back to the task list (pressing the header back arrow as often as needed). */
async function toTaskList(page) {
  for (let i = 0; i < 4 && !(await onTaskList(page)); i++) {
    await lib.goBack(page).catch(() => {});
    await page.waitForTimeout(500);
  }
}

/** Task list -> Inbox through the real header entry. */
async function toInbox(page) {
  if (await page.getByText('Steward reviews', { exact: true }).isVisible().catch(() => false)) return true;
  await toTaskList(page);
  await page.getByRole('button', { name: /^Inbox/ }).first().click();
  return has(page, 'Steward reviews', 15_000);
}

/** Inbox -> review screen by tapping the real row. */
async function openReview(page, title, marker, name) {
  if (!(await toInbox(page))) return lib.fail(`${name}: the inbox did not open`), false;
  await page.getByRole('button', { name: new RegExp(`^Review.*: ${title}\\.`) }).first().click();
  if (!(await has(page, marker, 15_000))) {
    lib.fail(`${name}: the review screen did not show "${marker}"; page: ${JSON.stringify((await bodyText(page)).slice(0, 300))}`);
    return false;
  }
  await backArrow(page, name);
  return true;
}

async function noReviewForbidden(page, label) {
  const t = await bodyText(page);
  for (const bad of ['Approve', 'Merge', 'Always allow']) {
    if (t.includes(bad)) lib.fail(`${label}: page text contains "${bad}"`);
  }
}

async function scenario(page, harness) {
  const t = {};
  for (const [key, title] of [['changes', 'Review changes task'], ['accepted', 'Review accepted task'], ['blocked', 'Review blocked task'], ['stale', 'Review stale task']]) {
    t[key] = (await seed(harness, `seed-run end_turn ${title}`)).taskId;
  }
  await seed(harness, `seed-review ${t.changes} CHANGES`);
  await seed(harness, `seed-review ${t.accepted} ACCEPTED`);
  await seed(harness, `seed-review ${t.blocked} BLOCKED`);
  await seed(harness, `seed-audit ${t.changes} 24`);
  for (const [sc, title] of [['permission-command', 'Policy matched task'], ['permission-command-nomatch', 'Policy nomatch task']]) {
    await seed(harness, `seed-live ${sc} ${title}`);
  }

  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  if (!(await expectText(page, 'Review stale task', 'connect', 30_000))) return;

  // Inbox: reviews with verdict chips, the policy link at the bottom.
  if (!(await toInbox(page))) return lib.fail('the inbox did not open from the Tasks header');
  await backArrow(page, 'inbox');
  for (const v of ['Changes requested', 'Accepted', 'Blocked']) {
    if (!(await page.getByText(v, { exact: true }).first().waitFor({ timeout: 8_000 }).then(() => true, () => false))) lib.fail(`inbox verdict chip "${v}" did not appear`);
  }
  const rowBtn = page.getByRole('button', { name: /^Review.*: Review (changes|accepted|blocked) task\./ });
  await tapTargets(page, 'inbox', [['review row', rowBtn], ['Permission policy link', page.getByRole('button', { name: 'Permission policy' })]]);
  await snap(page, 'inbox-reviews');

  // Stale: a review whose head moved must not show.
  await snap(page, 'inbox-stale-before', false);
  await seed(harness, `seed-review ${t.stale} CHANGES stale`);
  await page.waitForTimeout(1500);
  await toTaskList(page);
  await toInbox(page);
  if (await page.getByText('Review stale task').first().isVisible().catch(() => false)) lib.fail('a stale review appears in the inbox (Review stale task)');
  await snap(page, 'inbox-stale-after', false);

  // Review screens, each reached by tapping its inbox row.
  if (await openReview(page, 'Review accepted task', 'Frames and gates agree', 'review-accepted')) {
    await tapTargets(page, 'review-accepted', [['Open task', page.getByRole('button', { name: 'Open task' })], ['Resolve', page.getByRole('button', { name: 'Resolve' })]]);
    if (await page.getByText('failing', { exact: false }).first().isVisible().catch(() => false)) lib.fail('review-accepted: an accepted review shows a failing gate');
    await snap(page, 'review-accepted');
    await noReviewForbidden(page, 'review-accepted');
  }
  await lib.goBack(page);
  if (await openReview(page, 'Review changes task', 'Two files need work', 'review-changes')) {
    await expectText(page, 'mobile/src/api.ts', 'review-changes findings by file', 5_000);
    await tapTargets(page, 'review-changes', [['Open task', page.getByRole('button', { name: 'Open task' })], ['Resolve', page.getByRole('button', { name: 'Resolve' })]]);
    await snap(page, 'review-changes');
    await noReviewForbidden(page, 'review-changes');

    // Frames: collapsed at the bottom, then a tapped thumbnail expands (10 frames, ~40 ms apart).
    const thumb = page.getByRole('button', { name: /^Frame .*tap to expand/ }).first();
    if ((await thumb.count()) === 0) {
      lib.fail('review-frames: no frame thumbnail on the review screen');
    } else {
      await thumb.evaluate((e) => e.scrollIntoView({ block: 'center' }));
      await page.waitForTimeout(300);
      await snap(page, 'review-frames-collapsed', false);
      for (const scheme of ['light', 'dark']) {
        await page.emulateMedia({ colorScheme: scheme });
        await page.waitForTimeout(400);
        const tgt = page.getByRole('button', { name: /^Frame .*tap to (expand|collapse)/ }).first();
        if (/tap to collapse/.test((await tgt.getAttribute('aria-label')) ?? '')) await tgt.click();
        await page.waitForTimeout(400);
        await sequence(page, 'seq-expand', scheme, () => tgt.click());
        await page.waitForTimeout(500);
        await shot(page, 'review-frames-expanded', scheme);
        await tgt.click(); // collapse again for the next scheme
        await page.waitForTimeout(400);
      }
      await page.emulateMedia({ colorScheme: 'light' });
      await page.waitForTimeout(400);
    }
  }
  await lib.goBack(page);
  if (await openReview(page, 'Review blocked task', 'ran out of time', 'review-blocked')) {
    await snap(page, 'review-blocked');
    await noReviewForbidden(page, 'review-blocked');
  }
  await lib.goBack(page);

  // Resolve: the accepted review leaves the inbox.
  if (await openReview(page, 'Review accepted task', 'Frames and gates agree', 'review-resolve')) {
    await page.getByRole('button', { name: 'Resolve' }).click();
    if (!(await has(page, 'Steward reviews', 15_000))) lib.fail('Resolve did not return to the inbox');
    await page.waitForTimeout(800);
    if (await page.getByText('Review accepted task').first().isVisible().catch(() => false)) lib.fail('the resolved review is still in the inbox');
    await snap(page, 'inbox-after-resolve', false);
  }

  // Permission cards: matched rule and no match.
  for (const [title, marker, frame, tail] of [
    ['Policy matched task', 'Matched rule', 'permission-matched', 'file.test.ts'],
    ['Policy nomatch task', 'No rule matched', 'permission-nomatch', 'file.sh'],
  ]) {
    await toTaskList(page);
    await page.getByText(title, { exact: true }).first().click({ timeout: 20_000 }).catch(() => lib.fail(`${frame}: task ${title} not on the list`));
    if (!(await has(page, marker, 20_000))) {
      lib.fail(`${frame}: "${marker}" did not appear on the permission card; page: ${JSON.stringify((await bodyText(page)).slice(0, 400))}`);
      continue;
    }
    if (frame === 'permission-matched') await expectText(page, 'ask-npm', frame, 3_000);
    if (!(await page.evaluate((s) => document.body.textContent.includes(s), tail))) lib.fail(`${frame}: the full command's tail "${tail}" is not in the DOM`);
    await noForbidden(page, frame);
    await snap(page, frame);
  }

  // Policy page: the inbox's bottom link.
  await toInbox(page);
  await page.getByRole('button', { name: 'Permission policy' }).evaluate((e) => e.scrollIntoView({ block: 'center' }));
  await snap(page, 'inbox-policy-link', false);
  await page.getByRole('button', { name: 'Permission policy' }).click();
  if (!(await has(page, 'Policy file', 10_000))) return lib.fail('the Permission policy link did not open the policy screen');
  await backArrow(page, 'policy page');
  await snap(page, 'policy-page-top');
  await expectText(page, 'deny-force-push', 'policy rules', 3_000);
  await page.getByText('Rules', { exact: true }).first().evaluate((e) => e.scrollIntoView({ block: 'start' }));
  await snap(page, 'policy-page-rules', false);
  await page.getByText('Recent decisions', { exact: true }).first().evaluate((e) => e.scrollIntoView({ block: 'start' }));
  await snap(page, 'policy-page-audit', false);
  const row = page.getByRole('button', { name: /xxxxxxxxxx/ }).first();
  if ((await row.count()) > 0) {
    await row.evaluate((e) => e.scrollIntoView({ block: 'start' }));
    await row.click();
    await page.waitForTimeout(300);
    if (!(await page.evaluate(() => document.body.textContent.includes('x'.repeat(300))))) lib.fail('policy-audit-expanded: the 300-char target is not shown in full after the tap');
    await snap(page, 'policy-audit-expanded', false);
  } else {
    lib.fail('policy page: no audit row to expand');
  }
  // Invalid file: the daemon reloads on its own; the screen shows it live or after re-entering.
  w(join(HOME, 'steward', 'policy.yaml'), POLICY_BAD);
  if (!(await has(page, 'Policy file is invalid', 20_000))) {
    await lib.goBack(page);
    await page.getByRole('button', { name: 'Permission policy' }).click();
    if (await has(page, 'Policy file is invalid', 10_000)) console.log('note: policy-invalid appeared only after reopening the screen (no live refetch)');
    else lib.fail(`policy-invalid: "Policy file is invalid" never appeared; page: ${JSON.stringify((await bodyText(page)).slice(0, 300))}`);
  }
  await page.getByText('Policy file is invalid').first().evaluate((e) => e.scrollIntoView({ block: 'start' })).catch(() => {});
  await snap(page, 'policy-invalid', false);
  w(join(HOME, 'steward', 'policy.yaml'), POLICY_OK);
}

export default {
  name: 'steward-review',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    if (!HOME || !existsSync(HOME)) {
      console.log('skipping the steward-review stage: set SMIND_HOME to an isolated directory (never ~/.smind)');
      return;
    }
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'steward-review');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await scenario(page, harness);
    if (pending.length > 0) writeFileSync(join(lib.SMOKE_DIR, 'PENDING.txt'), pending.join('\n') + '\n');
    await context.close();
  },
  // The orchestrator starts the harness before run(); the files must already exist then.
  prepare() {},
};

// Written at import time: the orchestrator loads every stage before it starts any harness, and the daemon
// reads config.yaml / policy.yaml from SMIND_HOME as it goes.
if (HOME && existsSync(HOME) && process.argv.includes('steward-review')) {
  w(join(HOME, 'config.yaml'), 'steward:\n  policy:\n    enabled: true\n');
  w(join(HOME, 'steward', 'policy.yaml'), POLICY_OK);
}
