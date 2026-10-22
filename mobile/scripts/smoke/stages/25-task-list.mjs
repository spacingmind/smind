// mobile-zcode-redesign M3: the task list in the ZCode look.

import { join } from 'node:path';
import * as lib from '../lib.mjs';

/**
 * The redesigned task list (M3) against the fake agent: seed tasks so the
 * list shows every state (created / running / done / error / stopped, one
 * capped run reporting needs-continue), then screenshot the list and the
 * connection sheet in both schemes, plus the sheet's open transition.
 */
async function taskList(page, harness) {
  // Seed one task per state, then force-write every status explicitly: the
  // daemon alone never moves a seeded task past created in this harness, so
  // each task's title names the state set-task-status puts it in (Running
  // keeps a real live run underneath via seed-live hang).
  const seeded = [
    ['seed-run end_turn Created task', 'created'],
    ['seed-live hang Running task', 'running'],
    ['seed-run end_turn Done task', 'done'],
    ['seed-run end_turn Error task', 'error'],
    ['seed-run end_turn Stopped task', 'stopped'],
  ];
  const tasks = [];
  for (const [seed, status] of seeded) {
    const r = await harness.cmd(seed);
    if (!r.ok) {
      lib.fail(`seeding failed (${seed}): ${JSON.stringify(r)}`);
      return;
    }
    tasks.push([r.taskId, status]);
  }
  const set = [];
  for (const [taskId, status] of tasks) {
    set.push(await harness.cmd(`set-task-status ${taskId} ${status}`));
  }
  if (set.some((r) => !r.ok)) {
    lib.fail(`set-task-status failed: ${JSON.stringify(set)}`);
    return;
  }

  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Created task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail('the seeded tasks did not appear on the task list');
    return;
  }

  // Every state renders its status word (never colour alone).
  for (const word of ['Created', 'Running', 'Done', 'Error', 'Stopped']) {
    if (!(await page.getByText(word, { exact: true }).first().isVisible().catch(() => false))) {
      lib.fail(`the list does not show the status word "${word}"`);
    }
  }
  // The section header of the seeded workspace's ungrouped tasks.
  if (!(await page.getByText('Ungrouped', { exact: true }).first().isVisible().catch(() => false))) {
    lib.fail('the list does not show the Ungrouped section header');
  }
  await lib.shootBothSchemes(page, 'task-list');

  // The Connection sheet: opens from the header action, shows the status and
  // the device label, a red Disconnect, and closes again.
  for (const scheme of ['light', 'dark']) {
    await lib.shootTransition(page, 'sheet-open', scheme, () => page.getByRole('button', { name: 'Connection' }).click());
    try {
      await page.getByText('Connected', { exact: true }).waitFor({ timeout: 10_000 });
      await page.getByRole('button', { name: 'Disconnect' }).waitFor({ timeout: 10_000 });
    } catch {
      lib.fail(`the connection sheet did not show Connected + Disconnect (${scheme})`);
      return;
    }
    await page.screenshot({ path: join(lib.SMOKE_DIR, `task-list-sheet-${scheme}.png`), fullPage: true });
    console.log(`wrote task-list-sheet-${scheme}.png`);
    await page.keyboard.press('Escape');
    try {
      await page.getByRole('button', { name: 'Disconnect' }).waitFor({ state: 'detached', timeout: 10_000 });
    } catch {
      lib.fail(`the connection sheet did not close (${scheme})`);
      return;
    }
  }
  await page.emulateMedia({ colorScheme: 'light' });
}

export default {
  name: 'task-list',
  harness: { fakeAgent: true, args: ['-window-max', '1'] },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'task-list');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await taskList(page, harness);
    await context.close();
  },
};
