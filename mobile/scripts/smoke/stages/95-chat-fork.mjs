// ADR-0057 PR 1b: fork a finished chat on a real run (the fake agent's run is not a Claude session, so the native fork
// is refused and the labelled summary fork is offered), then the forked chat's banner, divider and inherited summary,
// and "View original" back to the parent. The platform confirm is window.confirm on the web target: the stage records
// the two questions and accepts them (a browser dialog is not part of a page screenshot, so no frame of the dialogs).
// Frames (<name>-<light|dark>.png): fork-action, fork-child, fork-child-summary, fork-original.

import * as lib from '../lib.mjs';

const title = 'Fork task';

const bodyText = (page) => page.evaluate(() => document.body.innerText).catch(() => '');
const overflows = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);

async function fork(page, harness) {
  const seeded = await harness.cmd(`seed-run end_turn ${title}`);
  if (!seeded.ok) {
    lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText(title, { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the seeded task did not appear on the task list; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 400))}`);
    return;
  }
  await page.getByText(title, { exact: true }).click();
  const action = page.getByRole('button', { name: 'Fork from here', exact: true });
  try {
    await action.waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`a finished run does not offer "Fork from here"; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }
  if (await page.getByText(/^Forked from /).first().isVisible().catch(() => false)) lib.fail('an unforked chat shows a fork banner');
  if (await overflows(page)) lib.fail('fork action: horizontal overflow');
  await lib.shootBothSchemes(page, 'fork-action');

  // The two questions: the native fork first, then (the agent has no session fork) the summary fork.
  const asked = [];
  page.on('dialog', (d) => {
    asked.push(d.message());
    void d.accept();
  });
  await action.click();
  try {
    await page.getByText(/^Forked from /).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the fork did not open the forked chat; asked ${JSON.stringify(asked)}; the page says: ${JSON.stringify((await bodyText(page)).slice(0, 600))}`);
    return;
  }
  if (asked.length !== 2) lib.fail(`expected two questions (native, then summary), got ${JSON.stringify(asked)}`);
  if (asked[0] && !/^From "[^"]+", turn 1\n/.test(asked[0])) lib.fail(`the first question does not name the turn: ${JSON.stringify(asked[0])}`);
  if (asked[1] && !asked[1].includes('this agent has no session fork')) lib.fail(`the second question does not say why: ${JSON.stringify(asked[1])}`);

  const text = await bodyText(page);
  if (!/Forked from .+ at turn 1\./.test(text)) lib.fail(`the banner does not read "Forked from <chat> at turn 1.": ${JSON.stringify(text.slice(0, 400))}`);
  for (const t of ['Summary sent to the agent', 'Forked here', 'Continue from the fork']) if (!text.includes(t)) lib.fail(`the forked chat does not show "${t}"`);
  if (!(await page.getByRole('button', { name: 'View original', exact: true }).isVisible().catch(() => false))) lib.fail('no "View original" on the banner');
  if (await page.getByRole('button', { name: 'Fork from here', exact: true }).isVisible().catch(() => false)) lib.fail('a forked chat with no run of its own offers "Fork from here"');
  if (await overflows(page)) lib.fail('forked chat: horizontal overflow');
  await lib.shootBothSchemes(page, 'fork-child');

  await page.getByRole('button', { name: 'Summary sent to the agent', exact: true }).click();
  try {
    await page.getByText(/does not remember tool calls or reasoning/).waitFor({ timeout: 15_000 });
  } catch {
    lib.fail('expanding the summary shows nothing');
  }
  if (await overflows(page)) lib.fail('expanded summary: horizontal overflow');
  await lib.shootBothSchemes(page, 'fork-child-summary');

  await page.getByRole('button', { name: 'View original', exact: true }).click();
  try {
    await page.getByRole('button', { name: 'Fork from here', exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"View original" did not open the parent chat');
    return;
  }
  if (await page.getByText(/^Forked from /).first().isVisible().catch(() => false)) lib.fail('the parent chat shows a fork banner');
  await lib.shootBothSchemes(page, 'fork-original');
}

export default {
  name: 'chat-fork',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'chat-fork');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await fork(page, harness);
    await context.close();
  },
};
