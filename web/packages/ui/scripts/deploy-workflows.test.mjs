// The shape of the two Cloudflare deploy workflows and how release-please.yml calls them (ADR-0051). Run by the required `ci` job
// (`node --test scripts/*.test.mjs`); the docs workflow's own details are tested in docs-site/scripts/docs-site.test.mjs.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");
const wf = (name) => readFileSync(path.join(ROOT, ".github", "workflows", name), "utf8");
const code = (text) => text.replace(/^\s*#.*$/gm, "");

test("demo-deploy runs only when called or dispatched with a ref, never on a push, a release event or a pull request", () => {
  const w = wf("demo-deploy.yml");
  const on = w.slice(w.indexOf("\non:"), w.indexOf("\npermissions:"));
  assert.match(on, /workflow_call:\n\s+inputs:\n\s+ref:[^]*?required: true/);
  assert.match(on, /workflow_dispatch:\n\s+inputs:\n\s+ref:/);
  for (const trigger of ["push:", "pull_request:", "release:", "schedule:"]) assert.ok(!on.includes(trigger), `no ${trigger} trigger`);
  assert.match(w, /ref: \$\{\{ inputs\.ref \}\}/);
});

test("demo-deploy hardening: read-only token, SHA-pinned actions, pinned wrangler, no-cancel concurrency, timeout", () => {
  const w = wf("demo-deploy.yml");
  assert.match(w, /permissions:\n\s+contents: read/);
  assert.match(w, /concurrency:\n\s+group: demo-deploy\n\s+cancel-in-progress: false/);
  assert.match(w, /timeout-minutes: 15/);
  for (const [, u] of w.matchAll(/uses: (\S+)/g)) assert.match(u, /@[0-9a-f]{40}$/, `${u} is not pinned by SHA`);
  assert.match(w, /bunx wrangler@\d+\.\d+\.\d+ deploy --config wrangler\.demo\.jsonc/);
  assert.match(w, /::notice title=demo-deploy skipped::/);
  // the check, the build and the guard run before the upload
  assert.ok(w.indexOf("build:demo") < w.indexOf("check-demo-build.mjs dist-demo") && w.indexOf("check-demo-build.mjs dist-demo") < w.indexOf("wrangler@"));
});

test("the Cloudflare secrets reach only the check step and the deploy step of each workflow", () => {
  for (const name of ["demo-deploy.yml", "docs-deploy.yml"]) {
    const w = code(wf(name));
    const lines = w.split("\n").filter((l) => /secrets\.CLOUDFLARE_/.test(l));
    assert.equal(lines.length, 4, `${name}: two secrets in two steps`);
    // not in a job-level env: every use sits under a step's own env, after the first step starts
    assert.ok(w.indexOf("secrets.CLOUDFLARE_API_TOKEN") > w.indexOf("steps:"), `${name}: not mapped at job level`);
  }
});

test("release-please deploys the demo and the docs from the new tag, as leaves, only for a created release", () => {
  const w = wf("release-please.yml");
  const doc = (job) => {
    const start = w.indexOf(`\n  ${job}:\n`);
    assert.ok(start >= 0, `${job} job exists`);
    const rest = w.slice(start + 1);
    const next = rest.slice(1).search(/\n {2}[a-z][a-z-]*:\n/);
    return next < 0 ? rest : rest.slice(0, next + 1);
  };
  for (const [job, file] of [["deploy-demo", "demo-deploy.yml"], ["deploy-docs", "docs-deploy.yml"]]) {
    const j = doc(job);
    assert.match(j, /needs: \[release-please, publish\]/);
    assert.match(j, /if: needs\.release-please\.outputs\.release_created == 'true' && needs\.publish\.result == 'success'/);
    assert.match(j, /permissions:\n\s+contents: read/);
    assert.match(j, new RegExp(`uses: \\./\\.github/workflows/${file.replace(".", "\\.")}`));
    assert.match(j, /ref: \$\{\{ needs\.release-please\.outputs\.tag_name \}\}/);
    assert.match(j, /CLOUDFLARE_API_TOKEN: \$\{\{ secrets\.CLOUDFLARE_API_TOKEN \}\}/);
    assert.match(j, /CLOUDFLARE_ACCOUNT_ID: \$\{\{ secrets\.CLOUDFLARE_ACCOUNT_ID \}\}/);
    assert.doesNotMatch(j, /secrets: inherit/);
  }
  // leaves: no other job waits for them, so a failed deploy cannot hold back or undo anything of the release
  for (const [, needs] of w.matchAll(/\n {4}needs: (.*)\n/g)) assert.doesNotMatch(needs, /deploy-demo|deploy-docs/);
});
