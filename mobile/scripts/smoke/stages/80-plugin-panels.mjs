// ADR-0042 PC3: plugin panels (a daemon-validated ui:1 view drawn read-only) at 390 px.
//
// Needs an ISOLATED SMIND_HOME (never ~/.smind): run as
//   SMIND_HOME=$(mktemp -d) SMOKE_OUT=/tmp/<dir> node scripts/smoke-web.mjs --stage plugin-panels
// The stage writes $SMIND_HOME/config.yaml (plugins.enabled), builds the data-only fixture plugin `panel-demo` under
// $SMIND_HOME/fixtures and installs it through the devharness plugin-* commands. No rpc action (that needs a process)
// and no model call: the only run is the fake agent's seeded end_turn.
//
// Frames (light + dark): pc3-detail-panels, pc3-panel-open (+ -full), pc3-panel-form (harness route, see unsupportedFrame), pc3-panel-overcap (an rpc view from the process plugin panel-big), pc3-panel-refresh-before/after,
// pc3-panel-openurl-confirm, pc3-panel-prompt, pc3-panel-disabled.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import * as lib from '../lib.mjs';

const HOME = process.env.SMIND_HOME ?? '';
const PLUGIN = 'panel-demo';
const HOSTILE = '<script>alert(1)</script> ![x](https://evil.example/p.png) [bad](javascript:alert(1)) [ok](https://example.com/docs)';
const PROMPT_TEXT = 'Why did the suite slow down?';

function w(path, content) {
  mkdirSync(join(path, '..'), { recursive: true });
  writeFileSync(path, content);
}

const TONES = ['success', 'warning', 'danger', 'muted'];

function nodesView() {
  return {
    ui: 1,
    root: {
      type: 'stack',
      gap: 'md',
      children: [
        { type: 'heading', text: 'Everything panel', level: 1 },
        { type: 'heading', text: 'Second level', level: 2 },
        { type: 'text', text: 'Plain text, a little longer so it has to wrap on a phone-width screen without getting clipped.' },
        { type: 'markdown', text: HOSTILE },
        { type: 'row', wrap: true, gap: 'sm', children: TONES.map((t) => ({ type: 'badge', text: t, tone: t })) },
        {
          type: 'list',
          items: [
            { title: 'Re-run suite', subtitle: 'pytest -q', icon: 'PlayCircle', tone: 'success', badge: 'fast', action: { kind: 'prompt', text: 'Re-run the suite' } },
            { title: 'Plain row', subtitle: 'no action' },
          ],
        },
        { type: 'keyValue', items: [{ key: 'Branch', value: 'main' }, { key: 'Coverage', value: '82%', tone: 'success' }, { key: 'Flaky', value: '3', tone: 'warning' }] },
        {
          type: 'table',
          columns: [{ key: 'file', label: 'File' }, { key: 'lines', label: 'Lines', align: 'end' }, { key: 'ok', label: 'OK' }],
          rows: [{ file: 'a.go', lines: 120, ok: true }, { file: 'b.go', lines: 0, ok: false }, { file: 'c.go', lines: null, ok: null }],
        },
        { type: 'code', text: 'func main() {\n\tfmt.Println("hello")\n}' },
        { type: 'diff', text: '--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,2 @@\n-old\n+new' },
        { type: 'progress', value: 0.42, label: 'Coverage', tone: 'success' },
        { type: 'empty', title: 'Nothing here', text: 'An empty node.', icon: 'Inbox' },
        { type: 'error', title: 'Something failed', text: 'An error node.' },
        { type: 'button', label: 'Ask a question', action: { kind: 'prompt', text: PROMPT_TEXT } },
        { type: 'button', label: 'Run the declared command', action: { kind: 'command', name: `${PLUGIN}:review` } },
        { type: 'button', label: 'Open the small panel', action: { kind: 'openPanel', panel: 'small' } },
        { type: 'button', label: 'Open docs', action: { kind: 'openUrl', url: 'https://example.com/docs' } },
      ],
    },
  };
}

const smallView = (text) => ({ ui: 1, title: 'Small', root: { type: 'stack', children: [{ type: 'heading', text: 'Small panel', level: 2 }, { type: 'text', text }] } });

const BIG_SERVER = `#!/usr/bin/env python3
import json
import sys


def read_message():
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            return None
        line = line.strip()
        if not line:
            break
        key, _, value = line.partition(b":")
        if key.strip().lower() == b"content-length":
            length = int(value.strip())
    if length is None:
        return None
    return json.loads(sys.stdin.buffer.read(length))


def send(message):
    body = json.dumps(message).encode("utf-8")
    sys.stdout.buffer.write(b"Content-Length: %d\\r\\n\\r\\n" % len(body) + body)
    sys.stdout.buffer.flush()


def big_view(params):
    # big: 1500 short nodes (~50 KB, over the 1000-node cap); bytes: 3500 long ones (over the 256 KiB result cap)
    if (params or {}).get("panelId") == "bytes":
        children = [{"type": "text", "text": "row %d %s" % (i, "x" * 60)} for i in range(3500)]
    else:
        children = [{"type": "text", "text": "row %d" % i} for i in range(1500)]
    return {"ui": 1, "root": {"type": "stack", "children": children}}


while True:
    msg = read_message()
    if msg is None:
        break
    method, msg_id = msg.get("method"), msg.get("id")
    if method == "plugin.initialize":
        send({"jsonrpc": "2.0", "id": msg_id, "result": {}})
    elif method == "plugin.shutdown":
        break
    elif method == "rpc.bigView":
        send({"jsonrpc": "2.0", "id": msg_id, "result": big_view(msg.get("params"))})
    elif msg_id is not None:
        send({"jsonrpc": "2.0", "id": msg_id, "error": {"code": -32601, "message": "method not found: %s" % method}})
`;

/** A small process plugin whose bigView rpc answers a valid ui:1 document over the node and byte caps. */
function buildBigFixture(root) {
  const dir = join(root, 'panel-big');
  w(join(dir, 'smind-plugin.json'), JSON.stringify({
    id: 'panel-big', name: 'Panel big', version: '1.0.0', api: 1, engines: { smind: '>=0.15.0' }, description: 'An rpc panel over the view caps',
    process: { command: ['python3', 'server.py'] },
    capabilities: [],
    contributes: {
      rpc: [{ name: 'bigView', description: 'Returns an over-cap ui:1 document.', timeoutMs: 10000 }],
      panels: [
        { id: 'big', title: 'Big', icon: 'Info', where: 'task', view: { rpc: 'bigView' } },
        { id: 'bytes', title: 'Bytes', icon: 'Info', where: 'task', view: { rpc: 'bigView' } },
      ],
    },
  }, null, 1));
  w(join(dir, 'server.py'), BIG_SERVER);
  return dir;
}

function buildFixture(root) {
  const dir = join(root, PLUGIN);
  w(join(dir, 'smind-plugin.json'), JSON.stringify({
    id: PLUGIN, name: 'Panel demo', version: '1.0.0', api: 1, engines: { smind: '>=0.15.0' }, description: 'Data-only panels for the mobile frames',
    contributes: {
      commands: [{ name: 'review', description: 'Review the diff', prompt: 'commands/review.md' }],
      panels: [
        { id: 'nodes', title: 'All nodes', icon: 'Layers', where: 'task', view: { file: 'views/nodes.json' } },
        { id: 'small', title: 'Small panel', icon: 'Info', where: 'sidebar', view: { file: 'views/small.json' } },
      ],
    },
  }, null, 1));
  w(join(dir, 'commands', 'review.md'), 'Review the staged diff for bugs.');
  w(join(dir, 'views', 'nodes.json'), JSON.stringify(nodesView(), null, 1));
  w(join(dir, 'views', 'small.json'), JSON.stringify(smallView('Version one of the small panel.')));
  return dir;
}

async function expectText(page, text, label, timeout = 20_000) {
  try {
    await page.getByText(text).first().waitFor({ timeout });
    return true;
  } catch {
    lib.fail(`${label}: "${text}" did not appear`);
    return false;
  }
}

/** The panel screen: the document's own rows, never an <img>, a <script> or literal raw HTML. */
async function assertSafe(page, evil, label) {
  // The native header's back icon is the only <img> the app itself draws.
  if ((await page.locator('img:not([src*="back-icon"])').count()) > 0) lib.fail(`${label}: an <img> element is on the page`);
  if ((await page.locator('#root script').count()) > 0) lib.fail(`${label}: a <script> element was rendered`);
  const body = await page.evaluate(() => document.body.innerText);
  if (/<script|<\/script|<img|javascript:|evil\.example|!\[|\]\(/.test(body)) lib.fail(`${label}: hostile markdown markup is visible as text`);
  if (evil.length > 0) lib.fail(`${label}: a request to evil.example was made: ${evil.join(', ')}`);
}

async function openDetail(page, name = 'Panel demo') {
  // The Connection sheet has several Open rows now: reach /plugins by the router's own history entry.
  await page.evaluate(() => {
    window.history.pushState({}, '', '/plugins');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  await page.getByRole('heading', { name: 'Plugins', exact: true }).waitFor({ timeout: 15_000 });
  await page.getByRole('button', { name: new RegExp(`^${name}`) }).click();
  await page.getByRole('heading', { name: 'Plugin', exact: true }).waitFor().catch(() => {});
}

/** goBack once the stack's animation has settled (the header link is "not stable" while a screen is still sliding). */
async function back(page) {
  await page.waitForTimeout(700);
  await lib.goBack(page);
}

/**
 * The devharness' plugin-* commands run on their own manager, which does not publish plugin.updated (a real daemon's
 * does), so after one the screen is nudged with the live global-switch flip, the signal 75-plugins already relies on.
 */
async function nudge(page) {
  w(join(HOME, 'config.yaml'), 'plugins:\n  enabled: false\n');
  await page.waitForTimeout(3500);
  w(join(HOME, 'config.yaml'), 'plugins:\n  enabled: true\n');
  await page.waitForTimeout(3500);
}

async function shoot(page, name) {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.waitForTimeout(600);
  await lib.shootBothSchemes(page, name);
}

async function scenario(page, harness) {
  if (!HOME || !/\/(tmp|var|private|dogfood)\//.test(HOME)) return lib.fail(`SMIND_HOME must be an isolated temp directory, got "${HOME}"`);
  const evil = [];
  page.on('request', (r) => {
    if (/evil\.example/.test(r.url())) evil.push(r.url());
  });
  let dialogs = [];
  let dialogAction = 'dismiss';
  page.on('dialog', async (d) => {
    dialogs.push(d.message());
    if (dialogAction === 'accept') await d.accept();
    else await d.dismiss();
  });
  w(join(HOME, 'config.yaml'), 'plugins:\n  enabled: true\n');
  const fx = buildFixture(join(HOME, 'fixtures'));
  const fxBig = buildBigFixture(join(HOME, 'fixtures'));

  const seeded = await harness.cmd('seed-run end_turn Existing task');
  if (!seeded.ok) return lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  if (!(await expectText(page, 'Existing task', 'connect', 30_000))) return;

  // LINKED, not installed: an installed copy is hash-checked (editing it unloads the plugin: "hash mismatch"), so the
  // refresh frame edits the live directory and reloads.
  for (const line of [`plugin-link ${fx}`, `plugin-enable ${PLUGIN}`]) {
    const r = await harness.cmd(line);
    if (!r.ok) return lib.fail(`${line}: ${JSON.stringify(r)}`);
  }
  // 1. detail with the Panels section
  await openDetail(page);
  await expectText(page, 'Panels', 'detail Panels section');
  await expectText(page, 'All nodes', 'detail panel row');
  await shoot(page, 'pc3-detail-panels');

  // 2. the everything panel
  await page.getByRole('button', { name: 'All nodes' }).click();
  await expectText(page, 'Everything panel', 'nodes panel');
  await page.waitForTimeout(500);
  await expectText(page, 'Re-run suite', 'list row');
  await assertSafe(page, evil, 'nodes');
  if (!(await page.getByText('ok', { exact: true }).first().isVisible().catch(() => false))) lib.fail('markdown link text "ok" is not shown');
  await page.evaluate(() => document.querySelectorAll('*').forEach((e) => { if (e.scrollTop) e.scrollTop = 0; }));
  const hscroll = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  if (hscroll) lib.fail('the nodes panel scrolls horizontally at 390 px');
  await shoot(page, 'pc3-panel-open');
  // scroll the panel's ScrollView to the form and the buttons for the viewport frames
  await page.getByText('Everything panel').first().scrollIntoViewIfNeeded();
  await page.getByText('Open docs').first().scrollIntoViewIfNeeded();

  // 3. openUrl confirm (the dialog is dismissed: nothing opens)
  dialogs = [];
  dialogAction = 'dismiss';
  const popups = [];
  page.context().on('page', (p) => popups.push(p.url()));
  await page.getByRole('button', { name: 'Open docs' }).click();
  await page.waitForTimeout(400);
  if (dialogs.length !== 1 || !dialogs[0].includes('example.com')) lib.fail(`openUrl confirm: expected one dialog naming example.com, got ${JSON.stringify(dialogs)}`);
  else console.log('openUrl dialog:', JSON.stringify(dialogs[0]));
  if (popups.length > 0) lib.fail(`a dismissed openUrl still opened a page: ${popups.join(', ')}`);
  await shoot(page, 'pc3-panel-openurl-confirm');

  // 4. prompt button
  await page.getByRole('button', { name: 'Ask a question' }).click();
  await expectText(page, 'Prompt ready', 'prompt notice', 10_000);
  await page.getByText('Prompt ready').first().scrollIntoViewIfNeeded();
  await shoot(page, 'pc3-panel-prompt');
  await assertSafe(page, evil, 'nodes after actions');

  // 5. the prompt landed in the composer (not sent): open the seeded task
  await page.evaluate(() => {
    window.history.pushState({}, '', '/');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  await page.getByText('Existing task', { exact: true }).click();
  await page.locator('textarea').waitFor({ timeout: 20_000 });
  await page.waitForTimeout(500);
  const draft = await page.locator('textarea').inputValue();
  if (!draft.includes(PROMPT_TEXT)) lib.fail(`the opened task composer does not contain the prompt: ${JSON.stringify(draft)}`);
  if (await page.getByText('Working…').isVisible().catch(() => false)) lib.fail('opening the prompted task started a run');
  await lib.shootBothSchemes(page, 'pc3-panel-prompt-composer');
  await page.locator('textarea').fill('');
  await openDetail(page);

  // 6. refresh on plugin.updated: the small panel, no navigation. The scheme is set BEFORE the panel opens and held
  // through the reload (changing it after the refetch once produced a light frame drawn in the dark theme).
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    writeFileSync(join(fx, 'views', 'small.json'), JSON.stringify(smallView('Version one of the small panel.')));
    await harness.cmd(`plugin-reload ${PLUGIN}`);
    await nudge(page);
    await page.getByRole('button', { name: 'Small panel' }).click();
    await expectText(page, 'Version one of the small panel.', `small panel (${scheme})`);
    await page.waitForTimeout(600);
    await page.screenshot({ path: join(lib.SMOKE_DIR, `pc3-panel-refresh-before-${scheme}.png`), fullPage: true });
    const urlBefore = page.url();
    writeFileSync(join(fx, 'views', 'small.json'), JSON.stringify(smallView('Version two arrived live.')));
    const rl = await harness.cmd(`plugin-reload ${PLUGIN}`);
    console.log('plugin-reload ->', JSON.stringify(rl));
    await nudge(page);
    if (await expectText(page, 'Version two arrived live.', `live refresh after plugin-reload (${scheme})`, 15_000)) {
      if (await page.getByText('Version one of the small panel.').isVisible().catch(() => false)) lib.fail('the old text is still visible after the refresh');
      if (page.url() !== urlBefore) lib.fail('the refresh navigated');
    }
    await page.waitForTimeout(600);
    const bg = await page.evaluate(() => getComputedStyle(document.body).color);
    console.log(`refresh-after ${scheme}: text colour ${bg}`);
    await page.screenshot({ path: join(lib.SMOKE_DIR, `pc3-panel-refresh-after-${scheme}.png`), fullPage: true });
    await back(page);
  }
  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByRole('button', { name: 'Small panel' }).click();

  // 7. disabled while open: the failed state with Retry
  const rd = await harness.cmd(`plugin-disable ${PLUGIN}`);
  if (!rd.ok) lib.fail(`plugin-disable: ${JSON.stringify(rd)}`);
  await nudge(page);
  await expectText(page, 'Retry', 'disabled panel shows Retry', 15_000);
  await shoot(page, 'pc3-panel-disabled');
  await back(page);

  // 8. over-cap: only an RPC view can be refused by the daemon at open time (file views are checked at install).
  for (const line of [`plugin-install ${fxBig}`, 'plugin-enable panel-big']) {
    const r = await harness.cmd(line);
    console.log(line, '->', JSON.stringify(r));
    if (!r.ok) return lib.fail(`${line}: ${JSON.stringify(r)}`);
  }
  await nudge(page);
  await openDetail(page, 'Panel big');
  await page.getByRole('button', { name: 'Big' }).click();
  if (await expectText(page, 'This panel is too large or invalid', 'over-cap panel', 30_000)) {
    const body = await page.evaluate(() => document.body.innerText);
    console.log('over-cap body:', JSON.stringify(body));
    if (!/view_invalid at \$\.root[^\n]*more than 1000 nodes/.test(body)) lib.fail("the daemon's path message is not visible under the over-cap title");
  }
  await shoot(page, 'pc3-panel-overcap');
  // The same rpc answering over the byte cap is refused by the host BEFORE validation (its own words, not view_invalid).
  await back(page);
  await page.getByRole('button', { name: 'Bytes' }).click();
  if (await expectText(page, /over the \d+ byte cap/, 'byte-cap panel', 30_000)) await shoot(page, 'pc3-panel-overcap-bytes');
  await unsupportedFrame(page);
  console.log('plugin-panels stage done');
}

/**
 * The daemon never sends an unknown node type, so the placeholder cannot come live: a throwaway web route
 * (mobile/app/pc3-frames.tsx, untracked, embeds the unknown-node-type fixture) draws it. Skipped when absent.
 */
async function unsupportedFrame(page) {
  if (!existsSync(join(lib.MOBILE_DIR, 'app', 'pc3-frames.tsx'))) return console.log('skipping pc3-unsupported-node: no mobile/app/pc3-frames.tsx');
  // Inside the paired session (without a connection the root gate redirects to pairing); the static server only
  // serves index.html, so the client route is reached by history.
  await page.evaluate(() => {
    window.history.pushState({}, '', '/pc3-frames');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  if (await expectText(page, 'Unsupported in this app version', 'unsupported-node placeholder', 15_000)) await shoot(page, 'pc3-unsupported-node');
  else await page.screenshot({ path: join(lib.SMOKE_DIR, 'pc3-unsupported-node-DEBUG.png') });
  // A form's submit must be an rpc and an rpc must be declared (needs a process), so a data-only panel cannot hold a form:
  // the form frame comes from the same throwaway route.
  await page.evaluate(() => {
    window.history.pushState({}, '', '/pc3-frames?v=form');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  if (await expectText(page, 'File report', 'form frame', 15_000)) await shoot(page, 'pc3-panel-form');
}

export default {
  name: 'plugin-panels',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    if (!HOME || !existsSync(HOME)) {
      console.log('skipping the plugin-panels stage: set SMIND_HOME to an isolated temp directory (it installs plugins; never ~/.smind)');
      return;
    }
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'plugin-panels');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await scenario(page, harness);
    await context.close();
  },
};
