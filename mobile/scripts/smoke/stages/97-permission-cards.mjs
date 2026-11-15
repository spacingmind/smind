// The permission card's on-request deny reason and its resolved copy, on a real run (the fake agent's
// permission-command scenario). Frames (<name>-<light|dark>.png): permission-pending (reason collapsed, one
// "Deny with reason" toggle), permission-reason-open (field open with typed text), permission-reason-collapsed
// (the text kept, field hidden), permission-resolved (after Deny: "You denied this", no option id).
// Not driven: the question card (the fake agent has no question scenario) and a failed answer.

import * as lib from '../lib.mjs';

const title = 'Permission cards task';
const REASON = 'Run the unit tests only and leave the config alone.';

const bodyText = (page) => page.evaluate(() => document.body.innerText).catch(() => '');
const overflows = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);

async function cards(page, harness) {
  const seeded = await harness.cmd(`seed-live permission-command ${title}`);
  if (!seeded.ok) {
    lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText(title, { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the seeded task did not appear; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 400))}`);
    return;
  }
  await page.getByText(title, { exact: true }).click();
  const toggle = page.getByRole('button', { name: 'Deny with reason', exact: true });
  try {
    await toggle.waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the permission card never showed "Deny with reason"; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }
  const reasonField = page.getByLabel('Reason for denying (optional)');
  if (await reasonField.count()) lib.fail('the reason field is on screen before "Deny with reason" was pressed');
  if (await overflows(page)) lib.fail('pending card: horizontal overflow');
  await lib.shootBothSchemes(page, 'permission-pending');

  await toggle.click();
  await reasonField.first().waitFor({ timeout: 10_000 });
  await reasonField.first().fill(REASON);
  if (await overflows(page)) lib.fail('reason open: horizontal overflow');
  await lib.shootBothSchemes(page, 'permission-reason-open');

  await page.getByRole('button', { name: 'Hide reason', exact: true }).click();
  await page.waitForTimeout(300);
  if (await reasonField.count()) lib.fail('the reason field is still on screen after "Hide reason"');
  await lib.shootBothSchemes(page, 'permission-reason-collapsed');

  await page.getByRole('button', { name: 'Deny with reason', exact: true }).click();
  await reasonField.first().waitFor({ timeout: 10_000 });
  const kept = await reasonField.first().inputValue().catch(() => '');
  if (kept !== REASON) lib.fail(`the reason text did not survive a collapse and re-open: ${JSON.stringify(kept)}`);

  const deny = page.getByRole('button', { name: /^Deny$/ }).first();
  await deny.click();
  try {
    await page.getByText('You denied this', { exact: true }).waitFor({ timeout: 15_000 });
  } catch {
    lib.fail(`after Deny the card did not read "You denied this"; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }
  const text = await bodyText(page);
  if (/chose|chosen|deny-1|\?$/m.test(text.split('You denied this')[0].slice(-80))) lib.fail('the resolved row carries a raw option id');
  await lib.shootBothSchemes(page, 'permission-resolved');
}

export default {
  name: 'permission-cards',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'permission-cards');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await cards(page, harness);
    await context.close();
  },
};
