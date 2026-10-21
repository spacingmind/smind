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

import { spawn, spawnSync, execFileSync } from 'node:child_process';
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
const PORT = Number(process.env.SMOKE_PORT ?? 8931);

// Phone-sized by default (390x844 @2x); SMOKE_VIEWPORT=WxH or WxH@dpr overrides.
function contextViewport() {
  const m = /^(\d+)x(\d+)(?:@(\d+(?:\.\d+)?))?$/.exec(process.env.SMOKE_VIEWPORT ?? '390x844@2');
  if (!m) throw new Error(`bad SMOKE_VIEWPORT ${process.env.SMOKE_VIEWPORT}; want WxH or WxH@dpr`);
  return { viewport: { width: Number(m[1]), height: Number(m[2]) }, deviceScaleFactor: Number(m[3] ?? 2) };
}
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

/** Whether the harness built from this tree takes -password (the daemon side of P13c). */
function harnessHasPasswordFlag() {
  const dir = mkdtempSync(join(tmpdir(), 'smind-smoke-probe-'));
  try {
    const binary = join(dir, 'devharness');
    execFileSync('go', ['build', '-o', binary, './internal/relay/bridge/devharness'], { cwd: REPO_ROOT, stdio: 'inherit' });
    const probe = spawnSync(binary, ['-h'], { encoding: 'utf8' });
    return `${probe.stderr}${probe.stdout}`.includes('-password');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
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

// The native stack's header back button: on web a link named "<previous title>, back" (or "Go back").
/**
 * Transition frames (every mid-animation glitch is a defect): runs `act` (a click that navigates) and screenshots
 * the page at ~0, 90 and 220 ms while the stack animates, in `scheme`. Frames: <name>-t<ms>-<scheme>.png.
 */
async function shootTransition(page, name, scheme, act) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.waitForTimeout(200);
  const done = act();
  for (const t of [0, 90, 220]) {
    if (t > 0) await page.waitForTimeout(t === 90 ? 90 : 130);
    await page.screenshot({ path: join(SMOKE_DIR, `${name}-t${t}-${scheme}.png`) });
  }
  await done;
  console.log(`wrote ${name}-t{0,90,220}-${scheme}.png`);
}

async function goBack(page) {
  await page.getByRole('link', { name: /(, back|^Go back)$/ }).click();
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
 * post-connect states: 'ready' renders the list (here the empty state, since
 * the harness daemon tracks no workspaces) under a header with Disconnect;
 * 'error' renders "Couldn't load the workspace".
 */
async function assertTasks(page) {
  // The header (with Disconnect) is also there while loading and on error, so wait for the content itself:
  // the empty state when ready (the harness daemon tracks no workspaces), or the error text.
  const empty = page.getByText('No workspaces yet', { exact: true });
  const errored = page.getByText("Couldn't load the workspace");
  try {
    await empty.or(errored).first().waitFor({ timeout: 30_000 });
  } catch {
    fail('tasks screen did not render after approval');
    return false;
  }
  if (await errored.isVisible().catch(() => false)) {
    const bodyText = await page.evaluate(() => document.body.innerText).catch(() => '(unknown)');
    fail(`tasks screen rendered the error state: ${bodyText}`);
    return false;
  }
  if (!(await page.getByRole('button', { name: 'Disconnect' }).isVisible().catch(() => false))) {
    fail('tasks screen header has no Disconnect button');
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
 * ADR-0027 D13 on mobile: a run that ended `done` at a turn cap reads "Hit the
 * tool-turn limit · not finished" with a Continue button (a normal run next to
 * it shows neither); Continue starts a follow-up run with the prompt "Continue".
 * The runs are REAL ones, executed by the repo's fake ACP agent through the
 * harness's seed-run command.
 */
async function needsContinue(page, harness) {
  const capped = await harness.cmd('seed-run max_turn_requests Capped task');
  const normal = await harness.cmd('seed-run end_turn Normal task');
  if (!capped.ok || capped.disposition !== 'needs-continue') fail(`seeded capped run is not needs-continue: ${JSON.stringify(capped)}`);
  if (!normal.ok || normal.disposition) fail(`seeded normal run has a disposition: ${JSON.stringify(normal)}`);
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Capped task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('the seeded tasks did not appear on the task list');
    return;
  }

  // A normal run: no footer.
  await page.getByText('Normal task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  if (await page.getByText(/not finished/).isVisible().catch(() => false)) fail('a normal done run shows a not-finished footer');
  if (await page.getByRole('button', { name: 'Continue', exact: true }).isVisible().catch(() => false)) fail('a normal done run shows Continue');
  await shootBothSchemes(page, 'run-normal');
  await goBack(page);

  // The capped run: the warning sentence and Continue.
  await page.getByText('Capped task', { exact: true }).click();
  try {
    await page.getByText('Hit the tool-turn limit · not finished', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    fail('"Hit the tool-turn limit · not finished" did not appear for a run that ended at the turn cap');
    return;
  }
  await shootBothSchemes(page, 'run-needs-continue');

  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  try {
    await page.getByText('user: Continue', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    fail('Continue did not start a follow-up run (no "Continue" user line)');
    return;
  }
  if (await page.getByText('Hit the tool-turn limit · not finished', { exact: true }).isVisible().catch(() => false)) {
    fail('the footer is still shown after Continue was pressed');
  }
  await shootBothSchemes(page, 'run-continued');
}

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
    fail(`seeding failed: ${JSON.stringify([idle, live, stoppable])}`);
    return;
  }
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Queue task', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('the seeded tasks did not appear on the task list');
    return;
  }

  // Idle: no queue, the plain compose placeholder.
  // Push and pop transition frames, light then dark, before the stage's own assertions.
  for (const scheme of ['light', 'dark']) {
    await shootTransition(page, 'push-task', scheme, () => page.getByText('Idle task', { exact: true }).click());
    await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
    await shootTransition(page, 'pop-task', scheme, () => goBack(page));
    await page.getByText('Idle task', { exact: true }).waitFor({ timeout: 10_000 });
  }
  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByText('Idle task', { exact: true }).click();
  await page.getByText(/^run [0-9a-f]{8} ·/).waitFor({ timeout: 20_000 });
  if (await page.getByLabel('Queued prompts').isVisible().catch(() => false)) fail('an idle task shows a queue');
  await shootBothSchemes(page, 'queue-idle');

  // An idle send under whenBusy "queue" starts the run at once; the user's own prompt line shows exactly once, whichever of
  // the run.start response and the run.status event reaches the screen first.
  await page.locator('textarea').fill('hello idle');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  try {
    await page.getByText('user: hello idle', { exact: true }).waitFor({ timeout: 20_000 });
    await page.waitForFunction(() => document.body.innerText.split('assistant: world!').length - 1 >= 2, null, { timeout: 30_000 });
  } catch {
    fail('an idle send did not start a run showing the prompt and its reply');
  }
  if ((await page.getByText('user: hello idle', { exact: true }).count()) !== 1) fail('the sent prompt line is not shown exactly once');
  await goBack(page);

  // Live: two prompts queue on the daemon.
  await page.getByText('Queue task', { exact: true }).click();
  try {
    await page.getByText(/· glm · live$/).waitFor({ timeout: 20_000 });
  } catch {
    fail('the live run did not show as live');
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
    fail('the second queued prompt did not appear');
    return;
  }
  if ((await page.getByRole('button', { name: /^Cancel queued prompt/ }).count()) !== 2) fail('expected two queued rows with a Cancel button');
  if (!(await page.locator('textarea').getAttribute('placeholder'))?.startsWith('Queue a follow-up')) fail('live placeholder is not the queue wording');
  await shootBothSchemes(page, 'queue-live-2-queued');

  // Cancel the first queued prompt.
  await page.getByRole('button', { name: /Cancel queued prompt: second prompt/ }).click();
  try {
    await page.getByText('second prompt', { exact: true }).waitFor({ state: 'detached', timeout: 20_000 });
  } catch {
    fail('the cancelled prompt did not leave the queue');
    return;
  }
  await shootBothSchemes(page, 'queue-after-cancel');

  // Stop the live run: the daemon delivers the remaining prompt as a new run the screen adopts.
  await harness.cmd(`set-scenario ${live.taskId} default`);
  await harness.cmd(`stop-run ${live.runId}`);
  try {
    // The delivered run is the daemon's own: its streamed reply ("Hello, world!" from the fake agent's default scenario)
    // appears after the first run's, and the header meta moves to the new run.
    await page.getByText('assistant: world!', { exact: true }).waitFor({ timeout: 40_000 });
    await page.getByText(/· glm · done$/).waitFor({ timeout: 20_000 });
  } catch {
    fail(`the delivered prompt did not appear in the transcript (daemon-started run not adopted); the page says: ${JSON.stringify(await page.evaluate(() => document.body.innerText).catch(() => '?'))}`);
    return;
  }
  if (await page.getByRole('button', { name: /^Cancel queued prompt/ }).isVisible().catch(() => false)) fail('the delivered prompt is still shown as queued');
  // The prompt of a daemon-delivered run shows as a "user:" line (its streamed events do not carry it), once, before the reply.
  try {
    await page.getByText('user: third prompt', { exact: true }).waitFor({ timeout: 20_000 });
  } catch {
    fail('the delivered queued prompt is not shown as a "user:" line');
  }
  if ((await page.getByText('user: third prompt', { exact: true }).count()) !== 1) fail('the delivered prompt line is not shown exactly once');
  await shootBothSchemes(page, 'queue-after-delivery');

  // Stop: a live run has a Stop button (run.stop, no confirmation); the run ends as stopped and Stop goes away.
  await goBack(page);
  await page.getByText('Stop task', { exact: true }).click();
  try {
    await page.getByText(/· glm · live$/).waitFor({ timeout: 20_000 });
    await page.getByRole('button', { name: 'Stop', exact: true }).waitFor({ timeout: 10_000 });
  } catch {
    fail('a live run has no Stop button');
    return;
  }
  await shootBothSchemes(page, 'stop-live');
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  try {
    await page.getByText(/· glm · stopped$/).waitFor({ timeout: 30_000 });
  } catch {
    fail('Stop did not end the run (the header never read stopped)');
    return;
  }
  if (await page.getByRole('button', { name: /^Stop(ping…)?$/ }).isVisible().catch(() => false)) fail('the Stop button is still shown after the run stopped');
  await shootBothSchemes(page, 'stop-after');

  // The list stays mounted under the task (native stack): returning must reload it (useFocusEffect), so a task the
  // daemon gained meanwhile shows up (the old hand-rolled stack remounted the list on every return).
  const late = await harness.cmd('seed-run end_turn Late task');
  if (!late.ok) fail(`seeding the late task failed: ${JSON.stringify(late)}`);
  await goBack(page);
  try {
    await page.getByText('Late task', { exact: true }).waitFor({ timeout: 15_000 });
  } catch {
    fail('the task list did not reload after returning from a task (a task created meanwhile is missing)');
  }
  await shootBothSchemes(page, 'list-after-stop');
}

/**
 * ADR-0031 P13c: a daemon with a password. Connect -> "Daemon password
 * required" -> a wrong password -> "Incorrect password" -> more wrong ones until
 * the daemon rate-limits this device -> the correct password -> "Too many
 * attempts" with a countdown (the correct password is kept and tried again by
 * itself) -> the task list. The password only ever goes into the prompt's field.
 */
async function daemonPassword(page, harness, password) {
  const submit = async (value) => {
    await page.locator('input[type="password"]').fill(value);
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
  };
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  try {
    await page.getByText('Daemon password required', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('"Daemon password required" did not appear for a daemon with a password');
    return;
  }
  if ((await page.locator('input[type="password"]').count()) !== 1) fail('the prompt has no masked (type=password) field');
  await shootBothSchemes(page, 'password-required');

  await submit('not the password');
  try {
    await page.getByText('Incorrect password', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('"Incorrect password" did not appear after a wrong password');
    return;
  }
  await shootBothSchemes(page, 'password-incorrect');

  // The 6th wrong attempt for one device id starts the daemon's delay; the next hello is rate limited.
  for (let i = 0; i < 5; i++) {
    await submit(`still wrong ${i}`);
    await page.getByText('Incorrect password', { exact: true }).waitFor({ timeout: 30_000 });
  }
  await submit(password);
  try {
    await page.getByText('Too many attempts', { exact: true }).waitFor({ timeout: 30_000 });
  } catch {
    fail('"Too many attempts" did not appear once the daemon rate-limited this device');
    return;
  }
  await shootBothSchemes(page, 'password-locked');
  if (await assertTasks(page)) await shootBothSchemes(page, 'password-tasks');
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
    // The status screen's native header repeats the card title: either match proves the screen.
    await page.getByText('This device was revoked', { exact: true }).first().waitFor({ timeout: 30_000 });
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
  let protectedDaemon = null;
  let seeded = null;
  let queued = null;
  try {
    for (const scheme of ['light', 'dark']) {
      const context = await browser.newContext({ ...contextViewport(), colorScheme: scheme, ignoreHTTPSErrors: true });
      const page = await context.newPage();
      trackErrors(page, scheme);
      await assertPairingScreen(page, url);
      await page.screenshot({ path: join(SMOKE_DIR, `pairing-${scheme}.png`), fullPage: true });
      console.log(`wrote pairing-${scheme}.png`);
      await context.close();
    }

    if (FULL) {
      harness = await startHarness();
      const context = await browser.newContext({ ...contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
      const page = await context.newPage();
      trackErrors(page, 'full');
      await page.goto(url, { waitUntil: 'load' });
      await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
      await deviceLifecycle(page, harness);
      await context.close();

      outdated = await startHarness(['-outdated-daemon']);
      const oldContext = await browser.newContext({ ...contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
      const oldPage = await oldContext.newPage();
      trackErrors(oldPage, 'outdated');
      await oldPage.goto(url, { waitUntil: 'load' });
      await oldPage.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
      await outdatedDaemon(oldPage, outdated);
      await oldContext.close();

      // ADR-0027 D13: the not-finished footer on a real capped run (the repo's fake ACP agent).
      const agentDir = mkdtempSync(join(tmpdir(), 'smind-smoke-fakeagent-'));
      const fakeAgent = join(agentDir, 'fakeagent');
      execFileSync('go', ['build', '-o', fakeAgent, './internal/taskrunner/fakeagent'], { cwd: REPO_ROOT, stdio: 'inherit' });
      try {
        seeded = await startHarness(['-fake-agent', fakeAgent, '-window-max', '1']);
        const runContext = await browser.newContext({ ...contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
        const runPage = await runContext.newPage();
        trackErrors(runPage, 'run-footer');
        await runPage.goto(url, { waitUntil: 'load' });
        await runPage.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
        await needsContinue(runPage, seeded);
        await runContext.close();

        queued = await startHarness(['-fake-agent', fakeAgent, '-window-max', '1']);
        const queueContext = await browser.newContext({ ...contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
        const queuePage = await queueContext.newPage();
        trackErrors(queuePage, 'queue');
        await queuePage.goto(url, { waitUntil: 'load' });
        await queuePage.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
        await promptQueue(queuePage, queued);
        await queueContext.close();
      } finally {
        rmSync(agentDir, { recursive: true, force: true });
      }

      if (await harnessHasPasswordFlag()) {
        const secret = 'correct horse battery staple';
        protectedDaemon = await startHarness(['-password', secret, '-window-max', '1']);
        const pwContext = await browser.newContext({ ...contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
        const pwPage = await pwContext.newPage();
        trackErrors(pwPage, 'password');
        await pwPage.goto(url, { waitUntil: 'load' });
        await pwPage.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
        await daemonPassword(pwPage, protectedDaemon, secret);
        await pwContext.close();
      } else {
        console.log('skipping the daemon-password stage: this tree\'s devharness has no -password (ADR-0031 P13c daemon side)');
      }
    }
  } finally {
    await browser.close();
    server.close();
    harness?.cleanup();
    outdated?.cleanup();
    protectedDaemon?.cleanup();
    seeded?.cleanup();
    queued?.cleanup();
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
