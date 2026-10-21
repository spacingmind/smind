// ADR-0031 P13c: a daemon with a password.

import * as lib from '../lib.mjs';

const SECRET = 'correct horse battery staple';

/**
 * ADR-0031 P13c: a daemon with a password. Connect -> "Daemon password
 * required" -> a wrong password -> "Incorrect password" -> more wrong ones until
 * the daemon rate-limits this device -> the correct password -> "Too many
 * attempts" with a countdown (the correct password is kept and tried again by
 * itself) -> the task list. The password only ever goes into the prompt's field.
 */
async function daemonPassword(page, harness) {
  const password = SECRET;
  const submit = async (value) => {
    await page.locator('input[type="password"]').fill(value);
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
  };
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Daemon password required', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"Daemon password required" did not appear for a daemon with a password');
    return;
  }
  if ((await page.locator('input[type="password"]').count()) !== 1) lib.fail('the prompt has no masked (type=password) field');
  await lib.shootBothSchemes(page, 'password-required');

  await submit('not the password');
  try {
    await page.getByText('Incorrect password', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"Incorrect password" did not appear after a wrong password');
    return;
  }
  await lib.shootBothSchemes(page, 'password-incorrect');

  // The 6th wrong attempt for one device id starts the daemon's delay; the next hello is rate limited.
  for (let i = 0; i < 5; i++) {
    await submit(`still wrong ${i}`);
    await page.getByText('Incorrect password', { exact: true }).waitFor({ timeout: 30_000 });
  }
  await submit(password);
  try {
    await page.getByText('Too many attempts', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"Too many attempts" did not appear once the daemon rate-limited this device');
    return;
  }
  await lib.shootBothSchemes(page, 'password-locked');
  if (await lib.assertTasks(page)) await lib.shootBothSchemes(page, 'password-tasks');
}

export default {
  name: 'password',
  harness: { args: ['-password', SECRET, '-window-max', '1'] },
  requires: 'password-flag',
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'password');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await daemonPassword(page, harness);
    await context.close();
  },
};
