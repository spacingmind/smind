// smoke-web.mjs (docs/plans/active/mobile-web-smoke-test.md): drives the
// app's Expo web target in headless Chromium via Playwright. This is
// verification tooling, not product code -- it is not part of `npm test`
// and not wired into CI (it needs a browser, and --full needs the Go
// toolchain). Screenshots land in the gitignored mobile/.smoke/ (SMOKE_OUT
// overrides, so before/after runs can sit side by side) and are never
// committed.
//
// Layout: this file is only the orchestrator (build the web export, static
// server, launch Chromium, run stages, print failures, exit code).
//   scripts/smoke/lib.mjs            shared helpers (FROZEN: stage PRs run in
//                                    parallel, so a screen's PR must not edit it)
//   scripts/smoke/stages/NN-name.mjs one file per stage, auto-discovered: every
//                                    *.mjs there, sorted by file name
// A stage's default export:
//   { name, harness, full, requires?, run: async ({ browser, url, harness, lib }) => {} }
//   harness  null = no daemon (the static pairing screen only); otherwise
//            { args?: string[], fakeAgent?: boolean }: the orchestrator starts
//            the Go device harness with args before the stage and cleanup()s it
//            after, even on failure. fakeAgent builds the repo's fake ACP agent
//            once and passes `-fake-agent <bin>`.
//   full     true = only with --full.
//   requires 'password-flag' = skipped (with a console note) unless this tree's
//            devharness has -password.
// To add a stage: drop scripts/smoke/stages/NN-name.mjs exporting that object;
// no orchestrator edit. Each stage creates its own browser context(s)
// (lib.contextViewport(), ignoreHTTPSErrors) and calls lib.trackErrors.
//
// Modes (see package.json's smoke:web / smoke:web:full):
//   default  the non-full stages: static build, assert PairingScreen renders
//            (title, input, Connect), no console/page errors, screenshot
//            light+dark.
//   --full   also the full stages (ADR-0031 P13b and later): start the Go device
//            harness (internal/relay/bridge/devharness: a real relay and a real
//            multi-device daemon hub; its READY line carries the pairing URL and
//            its stdin takes approve/revoke commands), paste the URL into the
//            pairing input, press Connect and walk the device through its
//            states, screenshotting each in both schemes via page.emulateMedia.
//            The harness's relay uses a self-signed cert, hence
//            ignoreHTTPSErrors on the browser context. The web target has no
//            secure store, so each page load is a new device.
//   --stage <name>  (repeatable) run only the named stages, full or not.
// Env: SMOKE_PORT (static server port), SMOKE_VIEWPORT (WxH[@dpr]), SMOKE_OUT.
//
// The build is deterministic (`npx expo export --platform web` + a tiny
// static file server), not the Metro dev server, so the run does not
// depend on dev-server timing.

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, readdirSync } from 'node:fs';
import { createServer } from 'node:http';
import { extname, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { chromium } from 'playwright';
import * as lib from './smoke/lib.mjs';

const DIST = join(lib.MOBILE_DIR, 'dist');
const PORT = Number(process.env.SMOKE_PORT ?? 8931);
const FULL = process.argv.includes('--full');
const ONLY = process.argv.flatMap((a, i, all) => (a === '--stage' && all[i + 1] ? [all[i + 1]] : []));
const STAGES_DIR = new URL('./smoke/stages/', import.meta.url);

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.ico': 'image/x-icon',
  '.json': 'application/json',
  '.png': 'image/png',
};

function buildWeb() {
  execFileSync('npx', ['expo', 'export', '--platform', 'web'], { cwd: lib.MOBILE_DIR, stdio: 'inherit' });
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

async function loadStages() {
  const files = readdirSync(STAGES_DIR).filter((f) => f.endsWith('.mjs')).sort();
  const stages = [];
  for (const f of files) stages.push((await import(pathToFileURL(join(STAGES_DIR.pathname, f)).href)).default);
  for (const name of ONLY) {
    if (!stages.some((s) => s.name === name)) throw new Error(`--stage ${name}: no such stage (have: ${stages.map((s) => s.name).join(', ')})`);
  }
  return stages.filter((s) => (ONLY.length > 0 ? ONLY.includes(s.name) : FULL || !s.full));
}

async function run() {
  const stages = await loadStages();
  buildWeb();
  mkdirSync(lib.SMOKE_DIR, { recursive: true });

  const server = await startStaticServer();
  const url = `http://127.0.0.1:${PORT}/`;
  const browser = await chromium.launch();

  let fakeAgent = null;
  try {
    for (const stage of stages) {
      if (stage.requires === 'password-flag' && !(await lib.harnessHasPasswordFlag())) {
        console.log(`skipping the ${stage.name} stage: this tree's devharness has no -password (ADR-0031 P13c daemon side)`);
        continue;
      }
      let harness = null;
      try {
        if (stage.harness) {
          const args = [...(stage.harness.args ?? [])];
          if (stage.harness.fakeAgent) {
            fakeAgent ??= lib.buildFakeAgent();
            args.unshift('-fake-agent', fakeAgent.path);
          }
          harness = await lib.startHarness(args);
        }
        await stage.run({ browser, url, harness, lib });
      } finally {
        harness?.cleanup();
      }
    }
  } finally {
    await browser.close();
    server.close();
    fakeAgent?.cleanup();
  }

  if (lib.pageErrors.length > 0) lib.fail(`page/console errors during run:\n  ${lib.pageErrors.join('\n  ')}`);
  if (lib.failures.length > 0) {
    console.error(`\nsmoke:web failed (${lib.failures.length} assertion(s))`);
    process.exit(1);
  }
  console.log(FULL ? 'smoke:web:full passed' : 'smoke:web passed');
}

run().catch((e) => {
  console.error(e);
  process.exit(1);
});

