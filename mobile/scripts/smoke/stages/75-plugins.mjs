// ADR-0042 PA.5: the read-only Plugins list, detail, composer `/` commands and plugin themes at 390 px.
//
// Needs an ISOLATED SMIND_HOME (never ~/.smind): run as
//   SMIND_HOME=$(mktemp -d) SMOKE_OUT=/tmp/<dir> node scripts/smoke-web.mjs --stage plugins
// The stage writes $SMIND_HOME/config.yaml (plugins.enabled), builds its fixture plugins under $SMIND_HOME/fixtures,
// installs them through the devharness plugin-* commands (a real plugins.Manager on the daemon's own store: the RPCs
// are local-only, so a phone could never do it) and flips the global switch to see plugin.updated refetch live.
// No model is called: the only run is the fake agent's seeded end_turn.
//
// Frames (light + dark): plugins-empty, plugins-list, plugin-detail-{tools,live,broken}, plugins-themes, plugins-themed,
// plugins-off (after a LIVE flip, no navigation), composer-slash, composer-inserted, composer-themed; sequences of 10
// frames ~40 ms apart: seq-slash-open (with the composer's boxes per frame in seq-slash-open-<scheme>.json) and
// seq-list-to-detail.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import * as lib from '../lib.mjs';

const HOME = process.env.SMIND_HOME ?? '';

function w(path, content) {
  mkdirSync(join(path, '..'), { recursive: true });
  writeFileSync(path, content);
}

const TOKENS = JSON.parse(readFileSync(join(lib.REPO_ROOT, 'internal', 'plugins', 'themetokens.json'), 'utf8'));

/** A complete token set: every canonical token `fill`, with the ones mobile maps overridden (some as oklch on purpose). */
function themeSet(fill, over) {
  return Object.fromEntries(TOKENS.map((t) => [t, over[t] ?? fill]));
}

function buildFixtures(root) {
  const dusk = {
    light: themeSet('#3b2f63', {
      '--background': '#f3edff', '--card': '#ffffff', '--surface': 'rgba(59, 47, 99, 0.06)', '--accent': '#e0d4ff', '--header': '#ebe2ff',
      '--popover': '#ffffff', '--border': 'rgba(59, 47, 99, 0.16)', '--border-hover': 'rgba(59, 47, 99, 0.28)', '--foreground': 'oklch(0.28 0.06 290)',
      '--foreground-subtle': 'rgba(59, 47, 99, 0.7)', '--foreground-subtlest': 'rgba(59, 47, 99, 0.45)', '--primary': '#5b3fd0', '--primary-foreground': '#ffffff',
      '--secondary': '#e6dcff', '--success': '#1e7a46', '--destructive': '#c42b5b', '--warning': '#a85f00', '--input-bg': '#ffffff', '--input-border': 'rgba(59, 47, 99, 0.2)',
      '--input-border-focused': '#5b3fd0', '--interaction-confirmation-surface': '#e2f5ea', '--interaction-confirmation-foreground': '#1e7a46',
      '--diff-addition': '#1e7a46', '--diff-deletion': '#c42b5b',
    }),
    dark: themeSet('#d9ccff', {
      '--background': '#14101f', '--card': 'oklch(0.26 0.04 295)', '--surface': 'rgba(217, 204, 255, 0.07)', '--accent': '#2a1f4d', '--header': '#1c1630',
      '--popover': 'oklch(0.26 0.04 295)', '--border': 'rgba(217, 204, 255, 0.14)', '--border-hover': 'rgba(217, 204, 255, 0.24)', '--foreground': 'oklch(0.92 0.03 295)',
      '--foreground-subtle': 'rgba(217, 204, 255, 0.7)', '--foreground-subtlest': 'rgba(217, 204, 255, 0.4)', '--primary': '#b794f6', '--primary-foreground': '#14101f',
      '--secondary': '#33285c', '--success': '#5fd58e', '--destructive': '#ff6b8e', '--warning': '#ffb04a', '--input-bg': '#241b3d', '--input-border': 'rgba(217, 204, 255, 0.16)',
      '--input-border-focused': '#b794f6', '--interaction-confirmation-surface': 'rgba(95, 213, 142, 0.16)', '--interaction-confirmation-foreground': '#8fe5b0',
      '--diff-addition': '#5fd58e', '--diff-deletion': '#ff6b8e',
    }),
  };
  const mf = (dir, m) => w(join(root, dir, 'smind-plugin.json'), JSON.stringify({ api: 1, engines: { smind: '>=0.1.0' }, ...m }, null, 1));
  const cmds = (dir, list) => list.forEach(([name, text]) => w(join(root, dir, 'commands', `${name}.md`), text));
  mf('acme-demo', {
    id: 'acme-demo', name: 'Acme demo', version: '1.0.0', description: 'Three prompt commands and a Dusk theme',
    contributes: {
      commands: ['review', 'plan', 'tests'].map((n) => ({ name: n, description: { review: 'Review the staged diff', plan: 'Plan before coding', tests: 'Add tests for the change' }[n], prompt: `commands/${n}.md` })),
      themes: [{ id: 'dusk', label: 'Dusk', file: 'themes/dusk.json' }],
    },
  });
  cmds('acme-demo', [['review', 'Review the staged diff for bugs.'], ['plan', 'Before changing any code, write a short plan: files to touch, the order, and how you will verify it.'], ['tests', 'Add tests for the change.']]);
  w(join(root, 'acme-demo', 'themes', 'dusk.json'), JSON.stringify(dusk, null, 1));
  const tools = (ver, extraMcp) => ({
    id: 'acme-tools', name: 'Acme tools', version: ver,
    description: '<b>Tools</b> [click](javascript:alert(1)) ![x](https://evil.example/p.png) <script>alert(1)</script>',
    contributes: {
      mcpServers: [{ name: 'files', transport: 'stdio', command: ['node', 'mcp.js'] }, ...(extraMcp ? [{ name: 'extra', transport: 'stdio', command: ['node', 'extra.js'] }] : [])],
      skills: ['skills/audit'], profiles: ['profiles/reviewer.json', 'profiles/boss.json'],
      commands: [{ name: 'review', description: 'Tools review (same name as acme-demo)', prompt: 'commands/review.md' }, { name: 'ship', description: 'Ship checklist', prompt: 'commands/ship.md' }],
    },
  });
  for (const [dir, ver, extra] of [['acme-tools', '2.1.0', false], ['acme-tools-v2', '2.2.0', true]]) {
    mf(dir, tools(ver, extra));
    w(join(root, dir, 'skills', 'audit', 'SKILL.md'), '---\nname: audit\ndescription: "Use when auditing"\n---\nAudit it.\n');
    w(join(root, dir, 'profiles', 'reviewer.json'), '{"name":"reviewer","provider":"claude-native","instructions":"Review."}');
    w(join(root, dir, 'profiles', 'boss.json'), '{"name":"boss","provider":"claude-native","permissionMode":"bypassPermissions","orchestrationTools":true,"childPolicy":"profiles","childProfiles":["reviewer"]}');
    cmds(dir, [['review', 'Review with the tools plugin.'], ['ship', 'Run the ship checklist: tests, lint, changelog.']]);
  }
  mf('live-notes', { id: 'live-notes', name: 'Live notes', version: '0.3.0', description: 'A linked working copy', contributes: { commands: [{ name: 'notes', description: 'Summarise into notes', prompt: 'commands/notes.md' }] } });
  cmds('live-notes', [['notes', 'Summarise this session into notes.']]);
  const manifestBroken = { id: 'broken-one', name: 'Broken one', version: '0.1.0', description: 'Fails on reload', contributes: { commands: [{ name: 'b', description: 'b', prompt: 'commands/b.md' }] } };
  mf('broken-one', manifestBroken);
  cmds('broken-one', [['b', 'b']]);
  mf('sage', { id: 'sage', name: 'Sage', version: '1.0.0', description: 'A light-only theme', contributes: { themes: [{ id: 'sage', label: 'Sage', file: 'themes/sage.json' }] } });
  w(join(root, 'sage', 'themes', 'sage.json'), JSON.stringify({ light: themeSet('#2f3b2f', { '--background': '#eef4e6', '--card': '#fbfdf7' }) }, null, 1));
  return root;
}

function setSwitch(on) {
  w(join(HOME, 'config.yaml'), `plugins:\n  enabled: ${on}\n`);
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

/** The composer's boxes, for the no-shift check. */
async function composerBoxes(page) {
  const box = async (loc) => (await loc.boundingBox().catch(() => null)) ?? null;
  return {
    field: await box(page.locator('textarea').first()),
    send: await box(page.getByRole('button', { name: 'Send', exact: true })),
    attach: await box(page.getByRole('button', { name: 'Attach', exact: true })),
  };
}

/** n frames ~40 ms apart starting as `act` runs; per-frame boxes (if probe) go to <name>-<scheme>.json. */
async function sequence(page, name, scheme, act, probe) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.waitForTimeout(250);
  const boxes = [];
  // Frame 0 is BEFORE the action (the idle state); the rest follow it as it runs.
  let done = null;
  for (let i = 0; i < 10; i++) {
    if (i === 1) done = act();
    await page.screenshot({ path: join(lib.SMOKE_DIR, `${name}-${scheme}-f${String(i).padStart(2, '0')}.png`) });
    if (probe) boxes.push(await probe());
    await page.waitForTimeout(30);
  }
  await done;
  if (probe) {
    writeFileSync(join(lib.SMOKE_DIR, `${name}-${scheme}.json`), JSON.stringify(boxes, null, 1));
    const uniq = new Set(boxes.map((b) => JSON.stringify(b)));
    console.log(`${name}-${scheme}: ${boxes.length} frames, ${uniq.size} distinct composer box set(s)`);
    return boxes;
  }
  return null;
}

async function noMutatingControls(page, label) {
  const names = await page.getByRole('button').evaluateAll((els) => els.map((e) => (e.getAttribute('aria-label') ?? e.textContent ?? '').trim()));
  const bad = names.filter((n) => /^(enable|disable|install|remove|uninstall|update|reload|apply|link|delete)\b/i.test(n));
  if (bad.length > 0) lib.fail(`${label}: mutating plugin control(s) on mobile: ${bad.join(', ')}`);
}

async function openPlugins(page) {
  await page.getByRole('button', { name: 'Connection' }).click();
  await page.getByRole('button', { name: 'Open', exact: true }).click();
  await page.getByRole('heading', { name: 'Plugins', exact: true }).waitFor({ timeout: 15_000 }).catch(() => lib.fail('the Plugins screen did not open from the Connection sheet'));
}

async function scenario(page, harness) {
  if (!HOME || !/\/(tmp|var|private)\//.test(HOME)) return lib.fail(`SMIND_HOME must be an isolated temp directory, got "${HOME}"`);
  const evil = [];
  page.on('request', (r) => {
    if (/evil\.example/.test(r.url())) evil.push(r.url());
  });
  const fx = buildFixtures(join(HOME, 'fixtures'));
  setSwitch(true);

  const seeded = await harness.cmd('seed-run end_turn Existing task');
  if (!seeded.ok) return lib.fail(`seeding failed: ${JSON.stringify(seeded)}`);
  await page.fill('textarea, input[type="text"]', harness.pairingUrl);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  if (!(await expectText(page, 'Existing task', 'connect', 30_000))) return;

  // 1. empty (switch on, nothing installed)
  await openPlugins(page);
  await expectText(page, 'No plugins are installed on this daemon.', 'empty state');
  await noMutatingControls(page, 'empty');
  await lib.shootBothSchemes(page, 'plugins-empty');
  await lib.goBack(page);

  // 2. install everything through the real manager
  for (const line of [
    `plugin-install ${fx}/acme-demo`, `plugin-enable acme-demo`,
    `plugin-install ${fx}/acme-tools`, `plugin-enable acme-tools`, `plugin-update acme-tools ${fx}/acme-tools-v2`,
    `plugin-link ${fx}/live-notes`, `plugin-enable live-notes`,
    `plugin-install ${fx}/broken-one`, `plugin-enable broken-one`,
    `plugin-install ${fx}/sage`,
  ]) {
    const r = await harness.cmd(line);
    if (!r.ok) return lib.fail(`${line}: ${JSON.stringify(r)}`);
  }
  // reload re-reads the INSTALLED copy: break that one.
  writeFileSync(join(HOME, 'plugins', 'broken-one', 'smind-plugin.json'), '{ not json');
  const rb = await harness.cmd('plugin-reload broken-one');
  console.log('plugin-reload broken-one ->', JSON.stringify(rb));

  // 3. the list, every status
  await openPlugins(page);
  for (const t of ['Acme demo', 'Acme tools', 'Live notes', 'Sage', 'Error', 'Pending update', 'Enabled', 'Disabled', 'Live directory: changes after consent are not re-reviewed', 'Manage plugins on the computer: Settings -> Plugins']) {
    await expectText(page, t, 'list');
  }
  await noMutatingControls(page, 'list');
  await lib.shootBothSchemes(page, 'plugins-list');

  // 4. list -> detail transition (10 frames), then the detail frames
  for (const scheme of ['light', 'dark']) {
    await sequence(page, 'seq-list-to-detail', scheme, () => page.getByRole('button', { name: /^Acme tools/ }).click());
    await expectText(page, 'Needs your attention', 'detail escalation flag');
    await expectText(page, 'from acme-tools', 'detail owner label');
    await noMutatingControls(page, 'detail');
    const hostile = await page.getByText('<script>alert(1)</script>', { exact: false }).first().isVisible().catch(() => false);
    if (!hostile) lib.fail(`the hostile description is not rendered as literal text (${scheme})`);
    if ((await page.locator('img[src*="evil"], script:not([src])').count()) > 0) lib.fail('plugin text produced an element');
    if (evil.length > 0) lib.fail(`a remote URL from plugin data was requested: ${evil.join(', ')}`);
    await page.screenshot({ path: join(lib.SMOKE_DIR, `plugin-detail-tools-${scheme}.png`), fullPage: true });
    await lib.goBack(page);
    await page.getByRole('button', { name: /^Acme tools/ }).waitFor();
  }
  for (const [name, frame] of [['Live notes', 'live'], ['Broken one', 'broken']]) {
    await page.getByRole('button', { name: new RegExp(`^${name}`) }).click();
    await page.getByRole('heading', { name: 'Plugin', exact: true }).waitFor().catch(() => {});
    await page.waitForTimeout(400);
    await lib.shootBothSchemes(page, `plugin-detail-${frame}`);
    await lib.goBack(page);
  }

  // 5. themes: picker, select Dusk, applied colours, light-only caption
  await page.waitForTimeout(300);
  await lib.shootBothSchemes(page, 'plugins-themes');
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.getByRole('radio', { name: 'Dusk' }).click();
  await page.waitForTimeout(400);
  const duskApplied = await page.evaluate(() => [...document.querySelectorAll('*')].some((e) => getComputedStyle(e).backgroundColor === 'rgb(20, 16, 31)'));
  if (!duskApplied) lib.fail('the Dusk dark background (#14101f) was not applied');
  await lib.shootBothSchemes(page, 'plugins-themed'); // light shows Dusk light, dark shows Dusk dark
  await page.emulateMedia({ colorScheme: 'light' });
  await page.waitForTimeout(300);
  const lightApplied = await page.evaluate(() => [...document.querySelectorAll('*')].some((e) => getComputedStyle(e).backgroundColor === 'rgb(243, 237, 255)'));
  if (!lightApplied) lib.fail('the Dusk light background (#f3edff) was not applied');
  await lib.goBack(page);

  // 6. composer `/`
  await page.getByText('Existing task', { exact: true }).click();
  await page.locator('textarea').waitFor({ timeout: 20_000 });
  const idle = await composerBoxes(page);
  for (const scheme of ['light', 'dark']) {
    await page.locator('textarea').fill('');
    await page.emulateMedia({ colorScheme: scheme });
    await page.waitForTimeout(250);
    const boxes = await sequence(page, 'seq-slash-open', scheme, () => page.locator('textarea').fill('/'), () => composerBoxes(page));
    const open = await composerBoxes(page);
    const moved = [...(boxes ?? []), open].filter((b) => JSON.stringify(b) !== JSON.stringify(idle)).length;
    if (moved > 0) lib.fail(`the composer moved in ${moved} frame(s) when the / list opened (${scheme})`);
    await expectText(page, 'from acme-demo', 'slash list owner suffix');
    await expectText(page, '/acme-tools:review', 'slash collision label');
    await page.screenshot({ path: join(lib.SMOKE_DIR, `composer-slash-${scheme}.png`) });
    await page.locator('textarea').fill('/pl');
    await page.waitForTimeout(150);
    await page.screenshot({ path: join(lib.SMOKE_DIR, `composer-slash-filtered-${scheme}.png`) });
    await page.getByRole('menuitem', { name: '/plan' }).click();
    await page.waitForTimeout(200);
    const value = await page.locator('textarea').inputValue();
    if (!value.startsWith('Before changing any code')) lib.fail(`picking /plan did not insert its prompt (${scheme}): ${JSON.stringify(value)}`);
    if (await page.getByRole('menuitem').count()) lib.fail(`the / list stayed open after picking (${scheme})`);
    await page.screenshot({ path: join(lib.SMOKE_DIR, `composer-inserted-${scheme}.png`) });
    if (await page.getByText('Working…').isVisible().catch(() => false)) lib.fail(`picking a command started a run (${scheme})`);
  }
  await page.locator('textarea').fill('');
  await lib.goBack(page);

  // 7. live refetch: flip the global switch off; the Plugins screen shows the off notice with no navigation, the theme falls back
  await openPlugins(page);
  await expectText(page, 'Acme demo', 'before the flip');
  setSwitch(false);
  await expectText(page, 'Plugins are off on this daemon. Turn them on in config.yaml on the computer.', 'live off notice (plugin.updated)', 15_000);
  await page.waitForTimeout(500);
  const stillDusk = await page.evaluate(() => [...document.querySelectorAll('*')].some((e) => getComputedStyle(e).backgroundColor === 'rgb(243, 237, 255)'));
  if (stillDusk) lib.fail('the plugin theme stayed after the plugins were switched off');
  await noMutatingControls(page, 'off');
  await lib.shootBothSchemes(page, 'plugins-off');
  setSwitch(true);
  await expectText(page, 'Live directory: changes after consent are not re-reviewed', 'live list again after switching on', 15_000);
  await lib.goBack(page);
  // no plugin commands once off, composer unchanged is covered above by the unit test; with plugins back the list is back
  console.log('plugins stage done');
}

export default {
  name: 'plugins',
  harness: { args: ['-window-max', '1'], fakeAgent: true },
  full: true,
  run: async ({ browser, url, harness }) => {
    if (!HOME || !existsSync(HOME)) {
      console.log('skipping the plugins stage: set SMIND_HOME to an isolated temp directory (it installs plugins; never ~/.smind)');
      return;
    }
    const context = await browser.newContext({ ...lib.contextViewport(), colorScheme: 'light', ignoreHTTPSErrors: true });
    const page = await context.newPage();
    lib.trackErrors(page, 'plugins');
    await page.goto(url, { waitUntil: 'load' });
    await page.getByText('smind pairing', { exact: true }).waitFor({ timeout: 20_000 });
    await scenario(page, harness);
    await context.close();
  },
};
