// ADR-0031 P13b: the device lifecycle against the real relay and daemon hub.

import * as lib from '../lib.mjs';

/**
 * ADR-0031 P13b's device lifecycle in the browser, against the real relay and
 * daemon hub: Connect -> "Waiting for approval" (with the approve command
 * naming this device) -> `approve` on the daemon -> the task list appears by
 * itself -> `revoke` on the daemon -> "This device was revoked".
 */
async function deviceLifecycle(page, harness) {
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  // Connect -> status-card transition frames (light only: the connection happens
  // once, so the dark frame would need a second full lifecycle to be cheap).
  await lib.shootTransition(page, 'connect-card', 'light', () => page.getByRole('button', { name: 'Connect' }).click());

  try {
    await page.getByText('Waiting for approval', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"Waiting for approval" did not appear for an unapproved device');
    return;
  }
  const command = await page.locator('text=/smind relay devices approve [a-z2-7]{26}/').first().innerText().catch(() => '');
  const deviceId = command.match(/approve ([a-z2-7]{26})/)?.[1];
  if (!deviceId) {
    lib.fail(`pending card does not show the approve command for this device (saw: ${JSON.stringify(command)})`);
    return;
  }
  const listed = (await harness.cmd('list')).devices?.find((d) => d.id === deviceId);
  if (!listed || listed.state !== 'pending') lib.fail(`daemon does not list the shown device id as pending: ${JSON.stringify(listed)}`);
  await lib.shootBothSchemes(page, 'pending');

  await harness.cmd(`approve ${deviceId}`);
  if (await lib.assertTasks(page)) await lib.shootBothSchemes(page, 'tasks');

  await harness.cmd(`revoke ${deviceId}`);
  try {
    // The status screen's native header repeats the card title: either match proves the screen.
    await page.getByText('This device was revoked', { exact: true }).first().waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('"This device was revoked" did not appear after the daemon revoked the device');
    return;
  }
  await lib.shootBothSchemes(page, 'revoked');
}

export default {
  name: 'device-lifecycle',
  harness: {},
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'full');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await deviceLifecycle(page, harness);
    await context.close();
  },
};
