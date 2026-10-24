// ADR-0041 M1, mobile read-only task tree: the Subtasks section on a task that has children (docs/plans/active/
// mobile-task-tree.md). Frames (light + dark, ~390 px): tree-nested, tree-nested-collapsed, tree-collapse-t{0,90,220},
// tree-expand-t{0,90,220}, tree-opened-lead (a tapped node opens that task), tree-permission, tree-ratelimited,
// tree-failed, tree-big (the "n more" row), tree-leaf (no section).

import { join } from 'node:path';
import * as lib from '../lib.mjs';

async function expectText(page, text, label) {
  try {
    await page.getByText(text).first().waitFor({ timeout: 20_000 });
    return true;
  } catch {
    lib.fail(`${label}: "${text}" did not appear`);
    return false;
  }
}

/** Opens a list row, runs `fn`, then returns to the list. */
async function visit(page, title, fn, returnToList = true) {
  await page.getByText(title, { exact: true }).first().click();
  try {
    await fn();
  } finally {
    if (returnToList) {
      await lib.goBack(page);
      await page.getByText('Leaf task', { exact: true }).waitFor({ timeout: 20_000 }).catch(() => lib.fail(`${title}: did not return to the task list`));
    }
  }
}

async function tree(page, harness) {
  for (const shape of ['nested', 'permission', 'ratelimited', 'failed', 'big']) {
    const r = await harness.cmd(`seed-tree ${shape}`);
    if (!r.ok) return lib.fail(`seed-tree ${shape} failed: ${JSON.stringify(r)}`);
  }
  const leaf = await harness.cmd('seed-run end_turn Leaf task');
  if (!leaf.ok) return lib.fail(`seed-run leaf failed: ${JSON.stringify(leaf)}`);

  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Leaf task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    return lib.fail('the seeded tasks did not appear on the task list');
  }

  await visit(page, 'Tree nested', async () => {
    if (!(await expectText(page, 'Subtasks · 6', 'nested'))) return;
    for (const word of ['Supervisor', 'Lead', 'Coder', 'n/a', '$0.42', 'Running', 'Idle']) {
      if (!(await page.getByText(word, { exact: true }).first().isVisible().catch(() => false))) lib.fail(`nested: "${word}" not visible`);
    }
    await lib.shootBothSchemes(page, 'tree-nested');
    const header = page.getByRole('button', { name: /^Subtasks · 6/ });
    for (const scheme of ['light', 'dark']) {
      await lib.shootTransition(page, 'tree-collapse', scheme, () => header.click());
      await page.screenshot({ path: join(lib.SMOKE_DIR, `tree-nested-collapsed-${scheme}.png`) });
      await lib.shootTransition(page, 'tree-expand', scheme, () => header.click());
    }
    // Tapping a node opens that task (the existing task screen), which shows its own subtree.
    await page.getByRole('button', { name: /^lead-1, / }).click();
    if (await expectText(page, 'Subtasks · 2', 'tapping lead-1')) await lib.shootBothSchemes(page, 'tree-opened-lead');
    await lib.goBack(page);
    await expectText(page, 'Subtasks · 6', 'back from lead-1');
  });

  await visit(page, 'Tree permission', async () => {
    if (!(await expectText(page, 'Waiting for permission', 'permission'))) return;
    if (!(await page.getByText('1 permission', { exact: true }).first().isVisible().catch(() => false))) lib.fail('permission: the "1 permission" badge is not visible');
    await lib.shootBothSchemes(page, 'tree-permission');
  });

  await visit(page, 'Tree ratelimited', async () => {
    if (!(await expectText(page, /Rate limited/, 'ratelimited'))) return;
    if (!(await page.getByText(/resets in \d+m/).first().isVisible().catch(() => false))) lib.fail('ratelimited: no "resets in Nm" countdown');
    await lib.shootBothSchemes(page, 'tree-ratelimited');
  });

  await visit(page, 'Tree failed', async () => {
    if (!(await expectText(page, '1 failed', 'failed'))) return;
    await lib.shootBothSchemes(page, 'tree-failed');
  });

  await visit(page, 'Tree big', async () => {
    if (!(await expectText(page, 'Subtasks · 130', 'big'))) return;
    const more = page.getByText(/^\d+ more$/).first();
    await more.scrollIntoViewIfNeeded().catch(() => {});
    if (!(await more.isVisible().catch(() => false))) lib.fail('big: the "n more" row is not visible');
    await lib.shootBothSchemes(page, 'tree-big');
  });

  await visit(page, 'Leaf task', async () => {
    await page.getByText(/^run [0-9a-f]{8}/).first().waitFor({ timeout: 20_000 }).catch(() => lib.fail('leaf: the task screen did not load'));
    await page.waitForTimeout(1500); // a tree section would have appeared by now
    if (await page.getByText(/^Subtasks/).first().isVisible().catch(() => false)) lib.fail('leaf: a leaf task shows a Subtasks section');
    await lib.shootBothSchemes(page, 'tree-leaf');
  }, false);
}

export default {
  name: 'task-tree',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'task-tree');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await tree(page, harness);
    await context.close();
  },
};
