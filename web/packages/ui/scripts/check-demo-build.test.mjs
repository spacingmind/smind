import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import { BUDGET, checkDemoBuild, checkProductionBuild } from "./check-demo-build.mjs";
import { cspMetaTag, headersFile, inlineScriptHashes } from "./demo-csp.mjs";

const BOOT = "(function(){var x=1;})();";

/** Writes a minimal but valid demo dist into a temp dir; `over` replaces files by relative path. */
function makeDist(over = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "demo-dist-"));
  mkdirSync(path.join(dir, "assets"));
  const bareHtml = `<!doctype html><html><head></head><body><script>${BOOT}</script><script type="module" crossorigin src="./assets/index-a.js"></script><link rel="stylesheet" href="./assets/index-a.css"></body></html>`;
  const hashes = inlineScriptHashes(bareHtml);
  const html = bareHtml.replace("<head>", `<head>${cspMetaTag(hashes)}`);
  const files = {
    "index.html": html,
    "_headers": headersFile(hashes),
    "assets/index-a.js": "console.log('entry');",
    "assets/index-a.css": "body{margin:0}",
    "assets/connect-b.js": "export const code='demo_unsupported';",
    ...over,
  };
  for (const [name, text] of Object.entries(files)) {
    if (text === null) continue;
    mkdirSync(path.dirname(path.join(dir, name)), { recursive: true });
    writeFileSync(path.join(dir, name), text);
  }
  return dir;
}

function withDist(over, fn) {
  const dir = makeDist(over);
  try {
    return fn(dir);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("G-03 passes on a clean synthetic dist", () => {
  withDist({}, (dir) => assert.deepEqual(checkDemoBuild(dir).errors, []));
});

for (const [name, snippet] of [
  ["/api/token", "fetch('/api/token')"],
  ["new WebSocket(", "new WebSocket(u)"],
  ["ws://", "var u='ws://example.test/x'"],
  ["localhost", "var u='http://localhost:9999/x'"],
  ["127.0.0.1", "var u='http://127.0.0.1:4648'"],
  ["relay.spacingmind", "var u='relay.spacingmind.com'"],
  ["smind.bearer", "var p='smind.bearer.'"],
]) {
  test(`G-01 fails when dist-demo contains ${name}`, () => {
    withDist({ "assets/index-a.js": `console.log('entry');${snippet}` }, (dir) => {
      const { errors } = checkDemoBuild(dir);
      assert.ok(errors.length > 0, `expected an error for ${name}`);
    });
  });
}

test("G-01 tolerates the two documented form placeholders and plain comparisons with localhost", () => {
  withDist({ "assets/index-a.js": `x==="localhost"||x==="127.0.0.1";var a="http://localhost:8080",b="http://localhost:54545/callback?code=";` }, (dir) => {
    assert.deepEqual(checkDemoBuild(dir).errors, []);
  });
});

test("G-02 fails over the gzip budget for demo code, initial JS and initial CSS", () => {
  const noise = () => Array.from({ length: 4000 }, (_, i) => `v${i}=${Math.random()};`).join("");
  withDist({ "assets/connect-b.js": `export const code='demo_unsupported';${noise()}` }, (dir) => {
    const { errors } = checkDemoBuild(dir, { budget: { ...BUDGET, demoChunkGzip: 100 } });
    assert.ok(errors.some((e) => e.startsWith("demo code is")), errors.join("\n"));
  });
  withDist({ "assets/index-a.js": noise() }, (dir) => {
    const { errors } = checkDemoBuild(dir, { budget: { ...BUDGET, initialJsGzip: 100 } });
    assert.ok(errors.some((e) => e.startsWith("initial JS is")), errors.join("\n"));
  });
  withDist({ "assets/index-a.css": noise() }, (dir) => {
    const { errors } = checkDemoBuild(dir, { budget: { ...BUDGET, initialCssGzip: 100 } });
    assert.ok(errors.some((e) => e.startsWith("initial CSS is")), errors.join("\n"));
  });
});

test("G-02 fails when the scripted daemon is in the entry chunk instead of a lazy chunk", () => {
  withDist({ "assets/index-a.js": "var c='demo_unsupported';", "assets/connect-b.js": null }, (dir) => {
    const { errors } = checkDemoBuild(dir);
    assert.ok(errors.some((e) => e.includes("lazily imported")), errors.join("\n"));
  });
});

test("G-04 the CSP script-src hash equals sha256 of the inline bootstrap, and a changed script without a new hash fails", () => {
  withDist({}, (dir) => assert.deepEqual(checkDemoBuild(dir).errors, []));
  const stale = makeDist();
  try {
    const bareHtml = `<!doctype html><html><head></head><body><script>${BOOT.replace("x=1", "x=2")}</script><script type="module" src="./assets/index-a.js"></script><link rel="stylesheet" href="./assets/index-a.css"></body></html>`;
    // the meta still carries the hash of the OLD script
    writeFileSync(path.join(stale, "index.html"), bareHtml.replace("<head>", `<head>${cspMetaTag(inlineScriptHashes(`<script>${BOOT}</script>`))}`));
    const { errors } = checkDemoBuild(stale);
    assert.ok(errors.some((e) => e.includes("missing the hash of the inline script")), errors.join("\n"));
  } finally {
    rmSync(stale, { recursive: true, force: true });
  }
});

test("the CSP must keep connect-src 'none' and no unsafe script sources", () => {
  withDist({ "_headers": null }, (dir) => assert.ok(checkDemoBuild(dir).errors.includes("_headers is missing")));
  const weak = makeDist();
  try {
    const hashes = inlineScriptHashes(`<script>${BOOT}</script>`);
    const html = `<html><head>${cspMetaTag(hashes).replace("connect-src 'none'", "connect-src *")}</head><body><script>${BOOT}</script><script type="module" src="./assets/index-a.js"></script></body></html>`;
    writeFileSync(path.join(weak, "index.html"), html);
    assert.ok(checkDemoBuild(weak).errors.some((e) => e.includes("connect-src 'none'")));
  } finally {
    rmSync(weak, { recursive: true, force: true });
  }
});

test("G-05 a production build with no demo code passes; one with the banner or the scripted daemon fails", () => {
  withDist({}, (dir) => {
    // the synthetic demo dist DOES contain the scripted daemon: the production check must flag it
    assert.ok(checkProductionBuild(dir).errors.length > 0);
  });
  withDist({ "assets/connect-b.js": "export const x=1;" }, (dir) => assert.deepEqual(checkProductionBuild(dir).errors, []));
  withDist({ "assets/connect-b.js": "export const x='Demo — not connected';" }, (dir) => assert.ok(checkProductionBuild(dir).errors.length > 0));
});
