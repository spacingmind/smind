// smoke-web.mjs (docs/plans/active/mobile-web-smoke-test.md): drives the
// app's Expo web target in headless Chromium via Playwright. This is
// verification tooling, not product code -- it is not part of `npm test`
// and not wired into CI (it needs a browser, and --full needs the Go
// toolchain). Screenshots land in the gitignored mobile/.smoke/ and are
// never committed.
//
// Modes (see package.json's smoke:web / smoke:web:full):
//   default  Item 1: static build, assert PairingScreen renders (title,
//            input, Connect), no console/page errors, screenshot light+dark.
//   --full   Item 2: also start the Go relay harness (built and spawned the
//            same way as mobile/src/relay/__tests__/integration.node.test.ts;
//            its READY line carries the pairing URL), paste the URL into
//            the pairing input, press Connect, and screenshot the task list
//            screen in both schemes. The harness's relay uses a self-signed
//            cert, hence ignoreHTTPSErrors on the browser context.
//
// The build is deterministic (`npx expo export --platform web` + a tiny
// static file server), not the Metro dev server, so the run does not
// depend on dev-server timing.

import { spawn, execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const MOBILE_DIR = fileURLToPath(new URL('..', import.meta.url));
const REPO_ROOT = resolve(MOBILE_DIR, '..');
const DIST = join(MOBILE_DIR, 'dist');
const SMOKE_DIR = join(MOBILE_DIR, '.smoke');
const PORT = 8931;
const FULL = process.argv.includes('--full');

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.ico': 'image/x-icon',
  '.json': 'application/json',
  '.png': 'image/png',
};

const failures = [];
const pageErrors = [];

function fail(message) {
  failures.push(message);
  console.error(`FAIL: ${message}`);
}

function buildWeb() {
  execFileSync('npx', ['expo', 'export', '--platform', 'web'], { cwd: MOBILE_DIR, stdio: 'inherit' });
}

async function startStaticServer() {
  const server = createServer((req, res) => {
    const path = req.url === '/' ? '/index.html' : req.url.split('?')[0];
    const file = join(DIST, path);
    if (!file.startsWith(DIST) || !existsSync(file)) {
      res.writeHead(404).end('not found');
      return;
    }
    res.writeHead(200, { 'content-type': MIME[extname(file)] ?? 'application/octet-stream' });
    res.end(readFileSync(file));
  });
  await new Promise((r) => server.listen(PORT, '127.0.0.1', r));
  return server;
}

async function startHarness() {
  // Same build/spawn + "READY <url>" handshake as integration.node.test.ts,
  // minus its NODE_TLS_REJECT_UNAUTHORIZED override: the browser context
  // uses ignoreHTTPSErrors for the harness's self-signed cert instead.
  const tmpDir = mkdtempSync(join(tmpdir(), 'smind-smoke-harness-'));
  const binary = join(tmpDir, 'relayharness');
  execFileSync('go', ['build', '-o', binary, './internal/relay/bridge/harness'], {
    cwd: REPO_ROOT,
    stdio: 'inherit',
  });
  const proc = spawn(binary, [], { stdio: ['ignore', 'pipe', 'pipe'] });
  const pairingUrl = await new Promise((resolveUrl, reject) => {
    let stdout = '';
    const onData = (chunk) => {
      stdout += chunk.toString('utf8');
      const match = stdout.match(/^READY (\S+)$/m);
      if (match) {
        proc.stdout.off('data', onData);
        resolveUrl(match[1]);
      }
    };
    proc.stdout.on('data', onData);
    proc.stderr.on('data', (c) => process.stderr.write(`[harness] ${c}`));
    proc.on('error', reject);
    proc.on('exit', (code) => reject(new Error(`harness exited early with code ${code}`)));
    setTimeout(() => reject(new Error('harness did not print READY in time')), 15_000);
  });
  return {
    pairingUrl,
    cleanup: () => {
      proc.kill();
      rmSync(tmpDir, { recursive: true, force: true });
    },
  };
}

function trackErrors(page, label) {
  page.on('pageerror', (e) => pageErrors.push(`[${label}] pageerror: ${e.message}`));
  page.on('console', (msg) => {
    if (msg.type() === 'error') pageErrors.push(`[${label}] console.error: ${msg.text()}`);
  });
}

async function assertPairingScreen(page, url) {
  await page.goto(url, { waitUntil: 'load' });
  const title = page.getByText('smind pairing', { exact: true });
  await title.waitFor({ timeout: 20_000 });
  if ((await page.locator('textarea, input[type="text"]').count()) === 0) {
    fail('pairing URL input not found');
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
    fail(`app fills only ${Math.round(width)}px of ${viewport.width}px viewport width (root-Host squeeze?)`);
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
    fail(`background differs at viewport edges: left=${leftBg} right=${rightBg} (squeezed panel?)`);
  }
  if (leftBg === 'rgb(255, 255, 255)' && await page.evaluate(() => matchMedia('(prefers-color-scheme: dark)').matches)) {
    fail(`dark scheme renders white background (${leftBg}) at viewport edges`);
  }
}

async function pairAndAssertTasks(page, pairingUrl, scheme) {
  await page.fill('textarea, input[type="text"]', pairingUrl);
  await page.getByRole('button', { name: 'Connect' }).click();
  // TasksScreen's initial load state: header is the workspace title,
  // empty workspaces render the "No workspaces yet" empty state.
  const loaded = page.getByText('No workspaces yet').or(page.getByText("Couldn't load the workspace"));
  try {
    await loaded.waitFor({ timeout: 20_000 });
  } catch {
    fail(`tasks screen did not render (${scheme})`);
    return;
  }
  await page.screenshot({ path: join(SMOKE_DIR, `tasks-${scheme}.png`), fullPage: true });
  console.log(`wrote tasks-${scheme}.png`);
}

async function run() {
  buildWeb();
  mkdirSync(SMOKE_DIR, { recursive: true });

  const server = await startStaticServer();
  const url = `http://127.0.0.1:${PORT}/`;
  const browser = await chromium.launch();

  let harness = null;
  try {
    for (const scheme of ['light', 'dark']) {
      const context = await browser.newContext({ colorScheme: scheme, ignoreHTTPSErrors: true });
      const page = await context.newPage();
      trackErrors(page, scheme);
      await assertPairingScreen(page, url);
      await page.screenshot({ path: join(SMOKE_DIR, `pairing-${scheme}.png`), fullPage: true });
      console.log(`wrote pairing-${scheme}.png`);

      if (FULL) {
        if (!harness) harness = await startHarness();
        await pairAndAssertTasks(page, harness.pairingUrl, scheme);
      }
      await context.close();
    }
  } finally {
    await browser.close();
    server.close();
    harness?.cleanup();
  }

  if (pageErrors.length > 0) fail(`page/console errors during run:\n  ${pageErrors.join('\n  ')}`);
  if (failures.length > 0) {
    console.error(`\nsmoke:web failed (${failures.length} assertion(s))`);
    process.exit(1);
  }
  console.log(FULL ? 'smoke:web:full passed' : 'smoke:web passed');
}

run().catch((e) => {
  console.error(e);
  process.exit(1);
});
