// M6: the create-a-task form. Seeds one workspace task, connects, then
// walks the form in both schemes: empty frame, open transition, filled,
// the disabled Create button, the close transition, then a real submit
// (title + prompt) asserting exactly one task exists afterwards, and a
// title-only task asserting the detail screen opens.

import * as lib from '../lib.mjs';

// providers[0] can be a real unauthenticated provider; the harness's fake
// agent (glm) is the one whose runs actually answer.
async function pickFakeProvider(page) {
  const select = page.locator('select').first();
  const options = await select.locator('option').evaluateAll((os) => os.map((o) => ({ v: o.value, t: o.textContent ?? '' })));
  const hit = options.find((o) => /glm|fake/i.test(o.t)) ?? options[0];
  await select.selectOption(hit.v);
}

async function openForm(page, scheme) {
  await lib.shootTransition(page, 'new-task-open', scheme, () => page.getByRole('button', { name: 'New task' }).click());
  try {
    await page.getByRole('button', { name: 'Create task' }).waitFor({ timeout: 10_000 });
  } catch {
    lib.fail(`the new-task form did not open (${scheme})`);
    return false;
  }
  return true;
}

async function newTaskFlow(page, harness) {
  const seeded = await harness.cmd('seed-run end_turn Existing task');
  if (!seeded.ok) {
    lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Existing task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('the seeded task did not appear on the task list');
    return;
  }

  for (const scheme of ['light', 'dark']) {
    if (!(await openForm(page, scheme))) return;
    await page.screenshot({ path: `${lib.SMOKE_DIR}/new-task-form-${scheme}.png`, fullPage: true });
    console.log(`wrote new-task-form-${scheme}.png`);
    await page.getByRole('textbox').nth(0).fill('Smoke title');
    await page.getByRole('textbox').nth(1).fill('A first prompt');
    await page.screenshot({ path: `${lib.SMOKE_DIR}/new-task-filled-${scheme}.png`, fullPage: true });
    console.log(`wrote new-task-filled-${scheme}.png`);
    // Empty-title error frame: impossible (the button disables), so assert the disabled state instead.
    await page.getByRole('textbox').nth(0).fill('');
    const createDisabled = await page.getByRole('button', { name: 'Create task' }).isDisabled();
    if (!createDisabled) lib.fail(`Create task is enabled with an empty title (${scheme})`);
    // Close via Cancel.
    await lib.shootTransition(page, 'new-task-close', scheme, () => page.getByRole('button', { name: 'Cancel' }).click());
    try {
      await page.getByText('Existing task', { exact: true }).waitFor({ timeout: 10_000 });
    } catch {
      lib.fail(`Cancel did not close the form (${scheme})`);
      return;
    }
  }

  // Headless submit flow: title + prompt, once per scheme (submit transition + created frame in each).
  for (const scheme of ['light', 'dark']) {
    const title = scheme === 'light' ? 'Created from phone' : 'Created from phone dark';
    await page.emulateMedia({ colorScheme: scheme });
    await page.getByRole('button', { name: 'New task' }).click();
    await page.getByRole('button', { name: 'Create task' }).waitFor({ timeout: 10_000 });
    await pickFakeProvider(page);
    await page.getByRole('textbox').nth(0).fill(title);
    await page.getByRole('textbox').nth(1).fill('A first prompt');
    await lib.shootTransition(page, 'new-task-submit', scheme, () => page.getByRole('button', { name: 'Create task' }).click());
    // A second tap during submit must create no second task (counted in the list below).
    await page.getByRole('button', { name: 'Create task' }).click({ force: true }).catch(() => {});
    try {
      await page.getByRole('heading', { name: title }).waitFor({ timeout: 20_000 });
    } catch {
      lib.fail(`the task screen did not open after submit (${scheme}); page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
      return;
    }
    // The first prompt is the user's own line, whether the run was still live or already finished when the screen loaded.
    try {
      await page.getByText('A first prompt', { exact: true }).first().waitFor({ timeout: 10_000 });
    } catch {
      lib.fail(`the first prompt is not shown as a user line on the new task (${scheme})`);
    }
    if (!(await page.getByText(/^run [0-9a-f]{8} ·/).isVisible().catch(() => false))) {
      lib.fail(`the new task screen shows no run strip (${scheme})`);
    }
    await page.waitForTimeout(200);
    await page.screenshot({ path: `${lib.SMOKE_DIR}/new-task-created-${scheme}.png`, fullPage: true });
    console.log(`wrote new-task-created-${scheme}.png`);
    await lib.goBack(page);
    try {
      await page.getByText(title, { exact: true }).waitFor({ timeout: 10_000 });
    } catch {
      lib.fail(`the new task is not in the list after going back (${scheme})`);
      return;
    }
    const copies = await page.getByText(title, { exact: true }).count();
    if (copies !== 1) lib.fail(`expected exactly one "${title}" in the list after a double tap, found ${copies}`);
  }
  await page.emulateMedia({ colorScheme: 'light' });

  // Title-only task: the screen opens, not blank.
  const title2 = 'Title only task';
  await page.getByRole('button', { name: 'New task' }).click();
  await page.getByRole('button', { name: 'Create task' }).waitFor({ timeout: 10_000 });
  await page.getByRole('textbox').nth(0).fill(title2);
  await lib.shootTransition(page, 'new-task-submit-title-only', 'light', () => page.getByRole('button', { name: 'Create task' }).click());
  try {
    await page.getByRole('heading', { name: title2 }).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail(`the title-only task screen did not open; page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
    return;
  }
  await lib.shootBothSchemes(page, 'new-task-empty-composer');
}

export default {
  name: 'new-task',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'new-task');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await newTaskFlow(page, harness);
    await context.close();
  },
};
