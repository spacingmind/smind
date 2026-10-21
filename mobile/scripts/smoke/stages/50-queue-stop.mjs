// ADR-0021 §9 on mobile (daemon-side prompt queue) plus Stop and list reload.

import * as lib from '../lib.mjs';

/**
 * ADR-0021 §9 on mobile: the daemon-side prompt queue. A live run (the fake
 * agent's `hang` scenario), two prompts sent while it runs are QUEUED on the
 * daemon and shown with Cancel, one is cancelled, then the live run is stopped
 * and the remaining prompt is delivered as a run the daemon starts itself,
 * which the screen adopts and streams. Screenshots: idle, live with 2 queued,
 * after cancel, after delivery.
 */
async function promptQueue(page, harness) {
  const idle = await harness.cmd('seed-run end_turn Idle task');
  const live = await harness.cmd('seed-live hang Queue task');
  const stoppable = await harness.cmd('seed-live hang Stop task');
  if (!idle.ok || !live.ok || !stoppable.ok) {
    lib.fail(`seeding failed: ${JSON.stringify([idle, live, stoppable])}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Queue task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('the seeded tasks did not appear on the task list');
    return;
  }
  await lib.shootBothSchemes(page, 'tasks-seeded');

  // Idle: no queue, the plain compose placeholder.
  // Push and pop transition frames, light then dark, before the stage's own assertions.
  for (const scheme of ['light', 'dark']) {
    await lib.shootTransition(page, 'push-task', scheme, () => page.getByText('Idle task', { exact: true }).click());
    await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
    await lib.shootTransition(page, 'pop-task', scheme, () => lib.goBack(page));
    await page.getByText('Idle task', { exact: true }).waitFor({ timeout: 10_000 });
  }
  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByText('Idle task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  if (await page.getByLabel('Queued prompts').isVisible().catch(() => false)) lib.fail('an idle task shows a queue');
  await lib.shootBothSchemes(page, 'queue-idle');

  // An idle send under whenBusy "queue" starts the run at once; the user's own prompt line shows exactly once, whichever of
  // the run.start response and the run.status event reaches the screen first.
  await page.locator('textarea').fill('hello idle');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  try {
    await page.getByText('user: hello idle', { exact: true }).waitFor({ timeout: 20_000 });
    await page.waitForFunction(() => document.body.innerText.split('assistant: world!').length - 1 >= 2, null, { timeout: 30_000 });
  } catch {
    lib.fail('an idle send did not start a run showing the prompt and its reply');
  }
  if ((await page.getByText('user: hello idle', { exact: true }).count()) !== 1) lib.fail('the sent prompt line is not shown exactly once');
  await lib.goBack(page);

  // Live: two prompts queue on the daemon.
  await page.getByText('Queue task', { exact: true }).click();
  try {
    await page.getByText(/· glm · live$/).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail('the live run did not show as live');
    return;
  }
  const send = async (text) => {
    await page.locator('textarea').fill(text);
    await page.getByRole('button', { name: 'Send', exact: true }).click();
  };
  await send('second prompt');
  await page.getByText('second prompt', { exact: true }).waitFor({ timeout: 20_000 });
  await send('third prompt');
  try {
    await page.getByText('third prompt', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail('the second queued prompt did not appear');
    return;
  }
  if ((await page.getByRole('button', { name: /^Cancel queued prompt/ }).count()) !== 2) lib.fail('expected two queued rows with a Cancel button');
  if (!(await page.locator('textarea').getAttribute('placeholder'))?.startsWith('Queue a follow-up')) lib.fail('live placeholder is not the queue wording');
  await lib.shootBothSchemes(page, 'queue-live-2-queued');

  // Cancel the first queued prompt.
  await page.getByRole('button', { name: /Cancel queued prompt: second prompt/ }).click();
  try {
    await page.getByText('second prompt', { exact: true }).waitFor({ state: 'detached', timeout: 20_000 });
  } catch {
    lib.fail('the cancelled prompt did not leave the queue');
    return;
  }
  await lib.shootBothSchemes(page, 'queue-after-cancel');

  // Stop the live run: the daemon delivers the remaining prompt as a new run the screen adopts.
  await harness.cmd(`set-scenario ${live.taskId} default`);
  await harness.cmd(`stop-run ${live.runId}`);
  try {
    // The delivered run is the daemon's own: its streamed reply ("Hello, world!" from the fake agent's default scenario)
    // appears after the first run's, and the header meta moves to the new run.
    await page.getByText('assistant: world!', { exact: true }).waitFor({ timeout: 40_000 });
    await page.getByText(/· glm · done$/).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail(`the delivered prompt did not appear in the transcript (daemon-started run not adopted); the page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
    return;
  }
  if (await page.getByRole('button', { name: /^Cancel queued prompt/ }).isVisible().catch(() => false)) lib.fail('the delivered prompt is still shown as queued');
  // The prompt of a daemon-delivered run shows as a "user:" line (its streamed events do not carry it), once, before the reply.
  try {
    await page.getByText('user: third prompt', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    lib.fail('the delivered queued prompt is not shown as a "user:" line');
  }
  if ((await page.getByText('user: third prompt', { exact: true }).count()) !== 1) lib.fail('the delivered prompt line is not shown exactly once');
  await lib.shootBothSchemes(page, 'queue-after-delivery');

  // Stop: a live run has a Stop button (run.stop, no confirmation); the run ends as stopped and Stop goes away.
  await lib.goBack(page);
  await page.getByText('Stop task', { exact: true }).click();
  try {
    await page.getByText(/· glm · live$/).waitFor({ timeout: 20_000 });
    await page.getByRole('button', { name: 'Stop', exact: true }).waitFor({ timeout: 10_000 });
  } catch {
    lib.fail('a live run has no Stop button');
    return;
  }
  await lib.shootBothSchemes(page, 'stop-live');
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  try {
    await page.getByText(/· glm · stopped$/).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('Stop did not end the run (the header never read stopped)');
    return;
  }
  if (await page.getByRole('button', { name: /^Stop(ping…)?$/ }).isVisible().catch(() => false)) lib.fail('the Stop button is still shown after the run stopped');
  await lib.shootBothSchemes(page, 'stop-after');

  // The list stays mounted under the task (native stack): returning must reload it (useFocusEffect), so a task the
  // daemon gained meanwhile shows up (the old hand-rolled stack remounted the list on every return).
  const late = await harness.cmd('seed-run end_turn Late task');
  if (!late.ok) lib.fail(`seeding the late task failed: ${JSON.stringify(late)}`);
  await lib.goBack(page);
  try {
    await page.getByText('Late task', { exact: true }).waitFor({ timeout: 15_000 });
  } catch {
    lib.fail('the task list did not reload after returning from a task (a task created meanwhile is missing)');
  }
  await lib.shootBothSchemes(page, 'list-after-stop');
}

export default {
  name: 'queue-stop',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'queue');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await promptQueue(page, harness);
    await context.close();
  },
};
