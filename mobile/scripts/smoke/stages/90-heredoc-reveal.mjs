// Heredoc reveal toggles on the permission card, on a real run (the fake agent's permission-command scenario: an
// execute permission whose command carries a 1056-byte heredoc). The card shows the elided command and a
// "Show heredoc (1056 bytes)" toggle; tapping it reveals the body (wrapped, no horizontal overflow), tapping again hides it.
// Frames (<name>-<light|dark>.png): heredoc-collapsed, heredoc-expanded. The expanded tool row's toggle is not driven:
// this scenario issues no tool call besides the permission itself.

import * as lib from '../lib.mjs';

const title = 'Heredoc reveal task';
const BYTES = 1056;

const bodyText = (page) => page.evaluate(() => document.body.innerText).catch(() => '');

async function overflows(page) {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}

async function heredoc(page, harness) {
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
    lib.fail('the seeded task did not appear on the task list');
    return;
  }
  await page.getByText(title, { exact: true }).click();
  const show = page.getByRole('button', { name: `Show heredoc (${BYTES} bytes)`, exact: true });
  const hide = page.getByRole('button', { name: `Hide heredoc (${BYTES} bytes)`, exact: true });
  try {
    await show.waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the permission card never showed "Show heredoc (${BYTES} bytes)"; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }

  let text = await bodyText(page);
  if (!text.includes(`[heredoc ${BYTES} bytes]`)) lib.fail(`the card command does not contain "[heredoc ${BYTES} bytes]": ${JSON.stringify(text.slice(0, 600))}`);
  if (text.includes('step 07')) lib.fail('the collapsed card already shows the heredoc body ("step 07")');
  if (await overflows(page)) lib.fail('collapsed card: horizontal overflow');
  await lib.shootBothSchemes(page, 'heredoc-collapsed');

  await show.click();
  try {
    await page.getByText(/step 07/).first().waitFor({ timeout: 15_000 });
  } catch {
    lib.fail(`tapping Show did not reveal the body; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }
  if (!(await hide.isVisible().catch(() => false))) lib.fail(`after Show the button does not read "Hide heredoc (${BYTES} bytes)"`);
  if (await overflows(page)) lib.fail('expanded card: horizontal overflow (scrollWidth > innerWidth)');
  await lib.shootBothSchemes(page, 'heredoc-expanded');

  await hide.click();
  await page.waitForTimeout(400);
  text = await bodyText(page);
  if (text.includes('step 07')) lib.fail('after Hide the body is still shown ("step 07")');
  if (!(await show.isVisible().catch(() => false))) lib.fail('after Hide the button does not read "Show heredoc" again');
}

export default {
  name: 'heredoc-reveal',
  harness: { fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'heredoc-reveal');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await heredoc(page, harness);
    await context.close();
  },
};
