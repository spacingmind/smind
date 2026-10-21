// Item 1: static build, no daemon -- the PairingScreen renders (title, input,
// Connect), fills the viewport, and screenshots light + dark.

import { join } from 'node:path';
import * as lib from '../lib.mjs';

async function assertPairingScreen(page, url) {
  await page.goto(url, { waitUntil: 'load' });
  const title = page.getByText('smind pairing', { exact: true });
  await title.waitFor({ timeout: 20_000 });
  if ((await page.locator('textarea, input[type="text"]').count()) === 0) {
    lib.fail('pairing URL input not found');
  }
  await page.getByRole('button', { name: 'Connect' }).waitFor({ timeout: 5_000 });
  // Regression check for the PR #183 root-<Host> squeeze: the app's root
  // view must span the full viewport width, and the theme background
  // must reach both edges (a themed app would otherwise show a white
  // strip next to the squeezed panel, invisible to DOM assertions).
  const width = await page.evaluate(() => {
    const el = document.querySelector('#root > div');
    return el ? el.getBoundingClientRect().width : 0;
  });
  const viewport = page.viewportSize();
  if (width < viewport.width - 1) {
    lib.fail(`app fills only ${Math.round(width)}px of ${viewport.width}px viewport width (root-Host squeeze?)`);
  }
  const bgAt = (x) =>
    page.evaluate(
      (px) => {
        const el = document.elementFromPoint(px, Math.round(window.innerHeight / 2));
        return el ? getComputedStyle(el).backgroundColor : 'none';
      },
      x,
    );
  const [leftBg, rightBg] = await Promise.all([bgAt(2), bgAt(viewport.width - 3)]);
  if (leftBg !== rightBg) {
    lib.fail(`background differs at viewport edges: left=${leftBg} right=${rightBg} (squeezed panel?)`);
  }
  if (leftBg === 'rgb(255, 255, 255)' && await page.evaluate(() => matchMedia('(prefers-color-scheme: dark)').matches)) {
    lib.fail(`dark scheme renders white background (${leftBg}) at viewport edges`);
  }
}

export default {
  name: 'pairing',
  harness: null,
  full: false,
  run: async ({ browser, url }) => {
    for (const scheme of ['light', 'dark']) {
      const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: scheme, ignoreHTTPSErrors: true });
      const page = await context.newPage();
      lib.trackErrors(page, scheme);
      await assertPairingScreen(page, url);
      await page.screenshot({ path: join(lib.SMOKE_DIR, `pairing-${scheme}.png`), fullPage: true });
      console.log(`wrote pairing-${scheme}.png`);
      await context.close();
    }
  },
};
