// M4: the task transcript in the ZCode look, on a real run (the fake agent's markdown-permission scenario): markdown
// (heading, bold, inline code, list, fenced code), a collapsed tool-call row, a permission card whose option is tapped,
// and the push transition showing a loading skeleton (never an empty page) in light and dark.

import * as lib from '../lib.mjs';

const title = 'Transcript task';

/** At t~90 ms after the click the new screen must already show something: the loading skeleton or the loaded run strip. */
async function pushWithLoadingAssertion(page, scheme) {
  let probe;
  await lib.shootTransition(page, 'push-transcript', scheme, () => {
    probe = page.waitForTimeout(90).then(() =>
      page.evaluate(() => ({
        loading: document.querySelector('[aria-label="Loading task"]') !== null,
        strip: /run [0-9a-f]{8} ·/.test(document.body.innerText),
        permission: document.body.innerText.includes('Run go test'),
      })),
    );
    return page.getByText(title, { exact: true }).click();
  });
  const seen = await probe;
  if (!seen.loading && !seen.strip) lib.fail(`push-transcript (${scheme}): at ~90 ms the task screen is an empty page (no skeleton, no run strip): ${JSON.stringify(seen)}`);
}

async function transcript(page, harness) {
  const seeded = await harness.cmd(`seed-live markdown-permission ${title}`);
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

  // The FIRST push is asserted too: web async routes are off (app.json expo-router plugin `asyncRoutes.web: false`), so the
  // task route is not a lazily fetched chunk that would show the stack header over an empty body before any screen code runs.
  const listRow = async () => {
    for (let i = 0; i < 100; i++) {
      if ((await page.getByText(title, { exact: true }).count()) === 1) return; // the popped screen's header title is gone
      await page.waitForTimeout(100);
    }
  };
  // Push/pop in both schemes: the loading state (a skeleton, never a blank body) is asserted at ~90 ms.
  await pushWithLoadingAssertion(page, 'light');
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  await lib.goBack(page);
  await listRow();
  await pushWithLoadingAssertion(page, 'dark');
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  await lib.goBack(page);
  await listRow();
  await page.getByText(title, { exact: true }).click();
  await page.emulateMedia({ colorScheme: 'light' });

  try {
    await page.getByText('Plan', { exact: true }).waitFor({ timeout: 30_000 });
    await page.getByText('Run go test ./...', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the markdown heading or the permission card did not render; the page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
    return;
  }
  // Markdown: bold is heavier, inline code is monospace, the list and the fenced block render as their own text.
  const weight = await page.getByText('fix', { exact: true }).first().evaluate((el) => getComputedStyle(el).fontWeight);
  if (!(weight === 'bold' || Number(weight) >= 600)) lib.fail(`bold text has fontWeight ${weight}`);
  const family = await page.getByText('retry', { exact: true }).first().evaluate((el) => getComputedStyle(el).fontFamily);
  if (!/mono/i.test(family)) lib.fail(`inline code is not monospace (${family})`);
  for (const text of ['read the test', 'patch the backoff', 'backoff = base']) {
    if (!(await page.getByText(text, { exact: true }).isVisible().catch(() => false))) lib.fail(`markdown text "${text}" is not rendered`);
  }
  // Raw markdown syntax must not leak through.
  const body = await page.evaluate(() => document.body.innerText);
  for (const syntax of ['## Plan', '**fix**', '`retry`', '```']) if (body.includes(syntax)) lib.fail(`raw markdown "${syntax}" is shown`);
  // The tool call is a collapsed row: name + title.
  if (!(await page.getByText('retry_test.go', { exact: true }).isVisible().catch(() => false))) lib.fail('the tool-call row is not shown');
  for (const name of ['Allow', 'Always allow', 'Deny']) {
    if (!(await page.getByRole('button', { name, exact: true }).isVisible().catch(() => false))) lib.fail(`permission option "${name}" is not a button`);
  }
  await lib.shootBothSchemes(page, 'permission');

  // Tap the primary option: the card resolves, the run answers and finishes.
  await page.getByRole('button', { name: 'Allow', exact: true }).click();
  try {
    // The card first reads `chosen: Allow` (this device's optimistic tap), then the daemon's permission_resolved event --
    // authoritative by design -- flips it to `resolved: Allow` within moments: either one proves the tap resolved it.
    await page.getByText(/^(chosen|resolved): Allow$/).waitFor({ timeout: 20_000 });
    await page.getByText('chose:allow-1', { exact: true }).waitFor({ timeout: 30_000 });
    await page.getByText(/· glm · done$/).waitFor({ timeout: 30_000 });
  } catch {
    lib.fail(`the permission tap did not resolve and finish the run; the page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
    return;
  }
  if (await page.getByRole('button', { name: 'Deny', exact: true }).isVisible().catch(() => false)) lib.fail('the resolved permission still shows its option buttons');
  await lib.shootBothSchemes(page, 'transcript');
}

export default {
  name: 'transcript',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'transcript');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await transcript(page, harness);
    await context.close();
  },
};
