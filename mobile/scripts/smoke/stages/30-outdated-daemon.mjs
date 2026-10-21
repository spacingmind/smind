// A daemon that never sends the device challenge (the harness's -outdated-daemon).

import * as lib from '../lib.mjs';

/**
 * A daemon from before device_hello v2 (the harness's -outdated-daemon): it
 * completes the E2EE handshake and never sends the device challenge, so the app
 * must say the daemon is outdated (after the 5 s challenge window) rather than
 * connect or retry.
 */
async function outdatedDaemon(page, harness) {
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect' }).click();
  try {
    await page.getByText('Daemon outdated or on a different version', { exact: true }).waitFor({ timeout: 40_000 });
  } catch {
    lib.fail('"Daemon outdated or on a different version" did not appear for a daemon that sends no device challenge');
    return;
  }
  if (!(await page.getByText('smind update', { exact: true }).isVisible().catch(() => false))) lib.fail('outdated card does not show `smind update`');
  await lib.shootBothSchemes(page, 'outdated');
}

export default {
  name: 'outdated-daemon',
  harness: { args: ['-outdated-daemon'] },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'outdated');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await outdatedDaemon(page, harness);
    await context.close();
  },
};
