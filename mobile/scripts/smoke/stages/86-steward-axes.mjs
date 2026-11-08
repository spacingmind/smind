// ADR-0054 P4: the two-axis steward review on the phone at 390 px, light + dark, on fake data: the inbox rows with their
// axis chips and the Partial chip, and the review screen for each state (both accepted, disagreement with tagged findings and
// the standards-changed hint, criteria met/unmet/unverifiable, a collapsed then expanded long criteria list, skipped for no
// spec, a blocked axis, a running axis, a partial review, and the old single-axis payload).
//
// Needs an ISOLATED SMIND_HOME (never ~/.smind): run as
//   SMIND_HOME=<isolated dir> SMOKE_OUT=<dir> node scripts/smoke-web.mjs --full --stage steward-axes
// No model is called; the reviews are seeded through the harness (seed-review <task> <verdict> axes=<scenario>).
//
// Every frame is asserted: mean luminance (light > 0.8, dark < 0.25), no horizontal overflow at the viewport width; tap targets
// are >= 44 px high; no dollar amount, no raw "[standards]"/"[spec]" tag and no "axis spec skipped" copy anywhere on a two-axis
// review; no approve/merge control.

import { existsSync } from 'node:fs';
import * as lib from '../lib.mjs';
import { backArrow, bodyText, has, noForbidden, seed, snap, tapTargets } from '../stewardFrames.mjs';

const HOME = process.env.SMIND_HOME ?? '';

const SCENARIOS = [
  // [key, task title, verdict, scenario (null = the old single-axis payload), a marker text on the review screen]
  ['accepted', 'Axes accepted task', 'ACCEPTED', 'both-accepted', 'Both axes accepted the change.'],
  ['disagree', 'Axes disagree task', 'CHANGES', 'disagreement', 'Changes requested by Standards.'],
  ['criteria', 'Axes criteria task', 'CHANGES', 'criteria', 'Changes requested by Spec.'],
  ['many', 'Axes criteria many task', 'CHANGES', 'criteria-many', 'Changes requested by Spec.'],
  ['skipped', 'Axes skipped task', 'ACCEPTED', 'skipped', 'had nothing to judge.'],
  ['blocked', 'Axes blocked axis task', 'CHANGES', 'blocked-axis', 'did not complete.'],
  ['running', 'Axes running task', 'ACCEPTED', 'running', 'Review in progress.'],
  ['partial', 'Axes partial task', 'CHANGES', 'partial', 'Partial review: only the standards axis ran'],
  ['old', 'Axes old changes task', 'CHANGES', null, 'Two files need work'],
  ['oldok', 'Axes old accepted task', 'ACCEPTED', null, 'Frames and gates agree'],
];

async function onTaskList(page) {
  return page.getByRole('button', { name: /^Inbox/ }).first().isVisible().catch(() => false);
}
async function toTaskList(page) {
  for (let i = 0; i < 4 && !(await onTaskList(page)); i++) {
    await lib.goBack(page).catch(() => {});
    await page.waitForTimeout(500);
  }
}
async function toInbox(page) {
  if (await page.getByText('Steward reviews', { exact: true }).isVisible().catch(() => false)) return true;
  await toTaskList(page);
  await page.getByRole('button', { name: /^Inbox/ }).first().click();
  return has(page, 'Steward reviews', 15_000);
}
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

/** What a two-axis review screen must never say. */
async function twoAxisCopy(page, label) {
  const t = await bodyText(page);
  if (/\$\s?\d|USD|costUsd/.test(t)) lib.fail(`${label}: a dollar amount is on the screen`);
  if (/\[(standards|spec)\]/i.test(t)) lib.fail(`${label}: a raw axis tag is on the screen`);
  if (/axis spec skipped/i.test(t)) lib.fail(`${label}: the "axis spec skipped" copy is on the screen`);
  for (const bad of ['Approve', 'Merge', 'Always allow']) if (t.includes(bad)) lib.fail(`${label}: page text contains "${bad}"`);
}

async function scenario(page, harness) {
  const tasks = {};
  for (const [key, title] of SCENARIOS.map((s) => [s[0], s[1]])) tasks[key] = (await seed(harness, `seed-run end_turn ${title}`)).taskId;
  for (const [key, , verdict, sc] of SCENARIOS) await seed(harness, `seed-review ${tasks[key]} ${verdict}${sc ? ` axes=${sc}` : ''}`);

  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  if (!(await has(page, 'Axes old accepted task', 30_000))) return lib.fail('connect: the seeded tasks did not appear');

  // The inbox: rows with axis chips, the Partial chip on the partial review only.
  if (!(await toInbox(page))) return lib.fail('the inbox did not open');
  await backArrow(page, 'inbox');
  await page.waitForTimeout(1200);
  const rowBtn = page.getByRole('button', { name: /^Review.*: Axes .* task\./ });
  await tapTargets(page, 'inbox', [['review row', rowBtn]]);
  const partialChips = await page.getByLabel('Partial review').count();
  if (partialChips !== 1) lib.fail(`inbox: ${partialChips} Partial chip(s) on the list, want exactly 1 (the partial review's row)`);
  await twoAxisCopy(page, 'inbox');
  await snap(page, 'axes-inbox');

  for (const [key, title, , sc, marker] of SCENARIOS) {
    const name = `axes-review-${key}`;
    if (!(await openReview(page, title, marker, name))) {
      await lib.goBack(page);
      continue;
    }
    await tapTargets(page, name, [['Open task', page.getByRole('button', { name: 'Open task' })], ['Resolve', page.getByRole('button', { name: 'Resolve' })]]);
    if (sc) await twoAxisCopy(page, name);
    else {
      const t = await bodyText(page);
      if (/Review axes|Partial/.test(t)) lib.fail(`${name}: an old single-axis review shows axis UI`);
    }
    // The inbox screen stays mounted under the pushed review screen and holds the partial row's chip (1): a review screen
    // adds one more only when the review itself is partial.
    const partialNow = await page.getByLabel('Partial review').count();
    if (sc === 'partial' && !(partialNow === 2 && (await page.getByLabel('Partial review').last().isVisible().catch(() => false)))) lib.fail(`${name}: no visible Partial chip on the review screen (${partialNow} in the DOM)`);
    if (sc !== 'partial' && partialNow !== 1) lib.fail(`${name}: ${partialNow} Partial chip(s) in the DOM, want only the inbox row's (a ${sc ? 'full' : 'single-axis'} review is not partial)`);
    await noForbidden(page, name);
    if (sc === 'criteria-many') {
      // Collapsed by default (more than 3 criteria): the toggle is a 44 pt button; tap it for the expanded frame.
      const toggle = page.getByRole('button', { name: 'Show criteria' });
      if ((await toggle.count()) === 0) lib.fail(`${name}: no "Show criteria" toggle`);
      else {
        await tapTargets(page, name, [['criteria toggle', toggle]]);
        await toggle.first().evaluate((e) => e.scrollIntoView({ block: 'center' }));
        await page.waitForTimeout(300);
        await snap(page, `${name}-collapsed`, false);
        await toggle.first().click();
        await page.waitForTimeout(400);
        if ((await page.getByRole('button', { name: 'Hide criteria' }).count()) === 0) lib.fail(`${name}: the toggle did not switch to "Hide criteria"`);
        await snap(page, `${name}-expanded`, false);
      }
    }
    await snap(page, name);
    await lib.goBack(page);
  }
}

export default {
  name: 'steward-axes',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    if (!HOME || !existsSync(HOME)) {
      console.log('skipping the steward-axes stage: set SMIND_HOME to an isolated directory (never ~/.smind)');
      return;
    }
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'steward-axes');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await scenario(page, harness);
    await context.close();
  },
};
