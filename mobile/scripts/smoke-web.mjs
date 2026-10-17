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
//   --full   Items 2 + ADR-0031 P13b: also start the Go device harness
//            (internal/relay/bridge/devharness: a real relay and a real
//            multi-device daemon hub; built and spawned the same way as
//            mobile/src/relay/__tests__/devHarness.ts; its READY line carries
//            the pairing URL and its stdin takes approve/revoke commands),
//            paste the URL into the pairing input, press Connect and walk the
//            device through its states, screenshotting each in both schemes
//            via page.emulateMedia: "Waiting for approval" (the daemon has
//            not approved this device), the task list (approved), and "This
//            device was revoked". The harness's relay uses a self-signed
//            cert, hence ignoreHTTPSErrors on the browser context. The web
//            target has no secure store, so each page load is a new device.
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

async function startHarness(extraArgs = []) {
  // Same build/spawn + "READY <url>" handshake as devHarness.ts, minus the
  // NODE_TLS_REJECT_UNAUTHORIZED override: the browser context uses
  // ignoreHTTPSErrors for the harness's self-signed cert instead. stdin takes
  // one command per line (approve/revoke/list...), each answered by one JSON
  // line on stdout.
  const tmpDir = mkdtempSync(join(tmpdir(), 'smind-smoke-harness-'));
  const binary = join(tmpDir, 'devharness');
  execFileSync('go', ['build', '-o', binary, './internal/relay/bridge/devharness'], {
    cwd: REPO_ROOT,
    stdio: 'inherit',
  });
  const proc = spawn(binary, extraArgs, { stdio: ['pipe', 'pipe', 'pipe'] });
  proc.stderr.on('data', (c) => process.stderr.write(`[harness] ${c}`));
  const lines = [];
  const waiters = [];
  let buf = '';
  proc.stdout.on('data', (chunk) => {
    buf += chunk.toString('utf8');
    let i;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i);
      buf = buf.slice(i + 1);
      const w = waiters.shift();
      if (w) w(line);
      else lines.push(line);
    }
  });
  const nextLine = (ms) =>
    new Promise((resolveLine, reject) => {
      const queued = lines.shift();
      if (queued !== undefined) return resolveLine(queued);
      const timer = setTimeout(() => reject(new Error('harness: no output in time')), ms);
      waiters.push((l) => {
        clearTimeout(timer);
        resolveLine(l);
      });
    });
  const exited = new Promise((_, reject) => proc.on('exit', (code) => reject(new Error(`harness exited early with code ${code}`))));
  let pairingUrl = '';
  for (;;) {
    const line = await Promise.race([nextLine(20_000), exited]);
    if (line.startsWith('READY ')) {
      pairingUrl = line.slice('READY '.length).trim();
      break;
    }
  }
  const cmd = async (line) => {
    proc.stdin.write(line + '\n');
    for (;;) {
      const raw = await nextLine(30_000);
      if (raw.startsWith('{')) return JSON.parse(raw);
    }
  };
  return {
    pairingUrl,
    cmd,
    cleanup: () => {
      // SIGKILL, not the default SIGTERM: server.Run installs its own
      // signal handler, which disables the Go runtime's default terminate-on-
      // SIGTERM behaviour, so a plain kill can leave the harness process
      // alive (confirmed by hand with the first harness).
      proc.kill('SIGKILL');
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

/** Screenshots the current state in both color schemes (one live connection; page.emulateMedia flips the theme). */
async function shootBothSchemes(page, name) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.waitForTimeout(200); // theme re-render after the media-query flip
    await page.screenshot({ path: join(SMOKE_DIR, `${name}-${scheme}.png`), fullPage: true });
    console.log(`wrote ${name}-${scheme}.png`);
  }
  await page.emulateMedia({ colorScheme: 'light' });
}

/**
 * Waits for the task list after the device was approved. TasksScreen's
 * post-connect states: 'ready' always renders a Disconnect button; 'error'
 * renders "Couldn't load the workspace". When ready, the harness daemon
 * tracks no workspaces, so the empty state -- "No workspaces yet" -- must
 * render.
 */
async function assertTasks(page) {
  const ready = page.getByRole('button', { name: 'Disconnect' });
  const errored = page.getByText("Couldn't load the workspace");
  const loaded = ready.or(errored).first();
  try {
    await loaded.waitFor({ timeout: 30_000 });
  } catch {
    fail('tasks screen did not render after approval');
    return false;
  }
  if (await errored.isVisible().catch(() => false)) {
    const bodyText = await page.evaluate(() => document.body.innerText).catch(() => '(unknown)');
    fail(`tasks screen rendered the error state: ${bodyText}`);
    return false;
  }
  try {
    await page.getByText('No workspaces yet', { exact: true }).waitFor({ timeout: 5_000 });
  } catch {
    fail('tasks screen empty state ("No workspaces yet") not shown for the task-less harness workspace');
  }
  return true;
}

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
    fail('"Daemon outdated or on a different version" did not appear for a daemon that sends no device challenge');
    return;
  }
  if (!(await page.getByText('smind update', { exact: true }).isVisible().catch(() => false))) fail('outdated card does not show `smind update`');
  await shootBothSchemes(page, 'outdated');
}

/**
 * ADR-0031 P13b's device lifecycle in the browser, against the real relay and
 * daemon hub: Connect -> "Waiting for approval" (with the approve command
 * naming this device) -> `approve` on the daemon -> the task list appears by
 * itself -> `revoke` on the daemon -> "This device was revoked".
 */
async function deviceLifecycle(page, harness) {
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect' }).click();

  try {
    await page.getByText('Waiting for approval', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('"Waiting for approval" did not appear for an unapproved device');
    return;
  }
  const command = await page.locator('text=/smind relay devices approve [a-z2-7]{26}/').first().innerText().catch(() => '');
  const deviceId = command.match(/approve ([a-z2-7]{26})/)?.[1];
  if (!deviceId) {
    fail(`pending card does not show the approve command for this device (saw: ${JSON.stringify(command)})`);
    return;
  }
  const listed = (await harness.cmd('list')).devices?.find((d) => d.id === deviceId);
  if (!listed || listed.state !== 'pending') fail(`daemon does not list the shown device id as pending: ${JSON.stringify(listed)}`);
  await shootBothSchemes(page, 'pending');

  await harness.cmd(`approve ${deviceId}`);
  if (await assertTasks(page)) await shootBothSchemes(page, 'tasks');

  await harness.cmd(`revoke ${deviceId}`);
  try {
    await page.getByText('This device was revoked', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('"This device was revoked" did not appear after the daemon revoked the device');
    return;
  }
  await shootBothSchemes(page, 'revoked');
}

async function run() {
  buildWeb();
  mkdirSync(SMOKE_DIR, { recursive: true });

  const server = await startStaticServer();
  const url = `http://127.0.0.1:${PORT}/`;
  const browser = await chromium.launch();

  let harness = null;
  let outdated = null;
  try {
    for (const scheme of ['light', 'dark']) {
      const context = await browser.newContext({ colorScheme: scheme, ignoreHTTPSErrors: true });
      const page = await context.newPage();
      trackErrors(page, scheme);
      await assertPairingScreen(page, url);
      await page.screenshot({ path: join(SMOKE_DIR, `pairing-${scheme}.png`), fullPage: true });
      console.log(`wrote pairing-${scheme}.png`);
      await context.close();
    }

    if (FULL) {
      harness = await startHarness();
      const context = await browser.newContext({ colorScheme: 'light', ignoreHTTPSErrors: true });
      const page = await context.newPage();
      trackErrors(page, 'full');
      await page.goto(url, { waitUntil: 'load' });
      await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
      await deviceLifecycle(page, harness);
      await context.close();

      outdated = await startHarness(['-outdated-daemon']);
      const oldContext = await browser.newContext({ colorScheme: 'light', ignoreHTTPSErrors: true });
      const oldPage = await oldContext.newPage();
      trackErrors(oldPage, 'outdated');
      await oldPage.goto(url, { waitUntil: 'load' });
      await oldPage.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
      await outdatedDaemon(oldPage, outdated);
      await oldContext.close();
    }
  } finally {
    await browser.close();
    server.close();
    harness?.cleanup();
    outdated?.cleanup();
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
