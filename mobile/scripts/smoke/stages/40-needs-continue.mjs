// ADR-0027 D13: the not-finished footer on a real capped run (the repo's fake ACP agent).

import * as lib from '../lib.mjs';

/**
 * ADR-0027 D13 on mobile: a run that ended `done` at a turn cap reads "Hit the
 * tool-turn limit · not finished" with a Continue button (a normal run next to
 * it shows neither); Continue starts a follow-up run with the prompt "Continue".
 * The runs are REAL ones, executed by the repo's fake ACP agent through the
 * harness's seed-run command.
 */
async function needsContinue(page, harness) {
  const capped = await harness.cmd('seed-run max_turn_requests Capped task');
  const normal = await harness.cmd('seed-run end_turn Normal task');
  if (!capped.ok || capped.disposition !== 'needs-continue') lib.fail(`seeded capped run is not needs-continue: ${JSON.stringify(capped)}`);
  if (!normal.ok || normal.disposition) lib.fail(`seeded normal run has a disposition: ${JSON.stringify(normal)}`);
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Capped task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('the seeded tasks did not appear on the task list');
    return;
  }

  // A normal run: no footer.
  await page.getByText('Normal task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  if (await page.getByText(/not finished/).isVisible().catch(() => false)) lib.fail('a normal done run shows a not-finished footer');
  if (await page.getByRole('button', { name: 'Continue', exact: true }).isVisible().catch(() => false)) lib.fail('a normal done run shows Continue');
  await lib.shootBothSchemes(page, 'run-normal');
  await lib.goBack(page);

  // The capped run: the warning sentence and Continue.
  await page.getByText('Capped task', { exact: true }).click();
  try {
    await page.getByText('Hit the tool-turn limit · not finished', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail('"Hit the tool-turn limit · not finished" did not appear for a run that ended at the turn cap');
    return;
  }
  await lib.shootBothSchemes(page, 'run-needs-continue');

  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  try {
    await page.getByText('user: Continue', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail('Continue did not start a follow-up run (no "Continue" user line)');
    return;
  }
  if (await page.getByText('Hit the tool-turn limit · not finished', { exact: true }).isVisible().catch(() => false)) {
    lib.fail('the footer is still shown after Continue was pressed');
  }
  await lib.shootBothSchemes(page, 'run-continued');
}

export default {
  name: 'needs-continue',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'run-footer');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await needsContinue(page, harness);
    await context.close();
  },
};
