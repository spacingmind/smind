// Static pins on .github/workflows/desktop-macos-signed.yml (docs/plans/active/
// desktop-notarization-prep.md, AC6): the signing workflow can never run on a pull request, never
// hands a secret to anything but the one step that needs it, and cannot publish.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const workflow = readFileSync(join(root, ".github/workflows/desktop-macos-signed.yml"), "utf8");
const lines = workflow.split("\n");
const code = lines.filter((l) => !/^\s*#/.test(l)); // comments never count

// The lines inside `run: |` blocks (indented deeper than the `run:` key until it dedents).
function runBlocks() {
  const out = [];
  for (let i = 0; i < code.length; i++) {
    const m = code[i].match(/^(\s*)run:\s*\|\s*$/);
    if (!m) continue;
    const indent = m[1].length;
    for (let j = i + 1; j < code.length; j++) {
      const l = code[j];
      if (l.trim() !== "" && l.match(/^\s*/)[0].length <= indent) break;
      out.push(l);
    }
  }
  return out;
}

test("workflow-is-dispatch-only-and-gated", () => {
  const on = code.slice(code.findIndex((l) => l === "on:") + 1).join("\n").split(/\npermissions:/)[0];
  assert.match(on, /^\s{2}workflow_dispatch:/m);
  for (const trigger of ["pull_request", "pull_request_target", "push", "schedule", "workflow_run", "release", "workflow_call", "issue_comment"]) {
    assert.doesNotMatch(on, new RegExp(`^\\s{2}${trigger}:`, "m"), `${trigger} must never trigger the signing workflow`);
  }
  // top-level permissions: contents read and nothing else
  const perms = workflow.match(/^permissions:\n((?:\s{2}\S.*\n)+)/m);
  assert.ok(perms, "top-level permissions block");
  assert.deepEqual(perms[1].trim().split("\n").map((l) => l.trim()), ["contents: read"]);
  // the master switch is a repository variable checked in a gate job; the signing job needs it, runs
  // only from develop (the Environment's branch policy is the enforcing control; this is the cheap
  // second one) and is protected by the Environment
  assert.match(workflow, /vars\.MACOS_SIGNING_ENABLED/);
  const sign = workflow.slice(workflow.indexOf("\n  sign:"));
  assert.match(sign, /needs: gate/);
  assert.match(sign, /if: needs\.gate\.outputs\.enabled == 'true' && github\.ref == 'refs\/heads\/develop'/);
  assert.match(sign, /environment: macos-signing/);
  // The gate job has no environment, so it cannot read the Environment's secrets: a secret check
  // there would always be false and the workflow could never run (found in review). The existence
  // of the six secrets is checked by sign-notarize.sh inside the signing job.
  const gate = workflow.slice(workflow.indexOf("\n  gate:"), workflow.indexOf("\n  sign:"));
  assert.doesNotMatch(gate, /secrets\./, "the gate job cannot see Environment secrets; do not test them there");
  assert.doesNotMatch(gate, /environment:/, "the gate holds no secrets and needs no approval");
  // nothing here can publish: no release upload/creation, no write permission anywhere
  assert.doesNotMatch(code.join("\n"), /gh release (upload|create|edit)|contents:\s*write|packages:\s*write|id-token:\s*write/);
});

test("workflow-secrets-only-in-env-never-in-run", () => {
  // every mention of the secrets context is an `env:` mapping entry
  for (const l of code) {
    if (!l.includes("secrets.")) continue;
    assert.match(l, /^\s+[A-Z][A-Z0-9_]*: \$\{\{ secrets\.[A-Z0-9_]+( != '')? \}\}\s*$/, `secret used outside an env entry: ${l}`);
  }
  // no expression of any kind inside a run script: inputs and secrets travel through env only
  for (const l of runBlocks()) {
    assert.doesNotMatch(l, /\$\{\{/, `an expression inside a run script: ${l}`);
  }
  // the real secrets (not the gate's booleans) appear in exactly one step: the signing one
  const stepsWithSecrets = workflow.split(/\n\s+- /).filter((s) => /secrets\.SMIND_[A-Z0-9_]+ \}\}/.test(s) && !/!= ''/.test(s));
  assert.equal(stepsWithSecrets.length, 1, "the secrets are exposed to exactly one step");
  assert.match(stepsWithSecrets[0], /scripts\/macos-sign\/sign-notarize\.sh/);
  // no xtrace anywhere in the workflow
  assert.doesNotMatch(code.join("\n"), /set -x|set -o xtrace|bash -x/);
});

// Whatever job reads the secrets must be in the Environment that holds them: a job without it sees
// empty secrets (a silent failure), and the Environment is also where the approval and the branch
// policy live.
test("workflow-job-with-secrets-has-the-environment", () => {
  const jobs = code.join("\n").split(/\n  (?=[a-z][a-z-]*:\n)/).slice(1);
  const withSecrets = jobs.filter((j) => /secrets\.SMIND_/.test(j));
  assert.equal(withSecrets.length, 1, "exactly one job reads secrets");
  assert.match(withSecrets[0], /\n    environment: macos-signing\n/);
});

test("workflow-actions-are-pinned", () => {
  const uses = code.filter((l) => /^\s*-?\s*uses:/.test(l));
  assert.ok(uses.length >= 2, "checkout and upload-artifact at least");
  for (const l of uses) {
    assert.match(l, /uses: [\w.-]+\/[\w.-]+@[0-9a-f]{40}( #.*)?$/, `an action is not pinned to a full commit SHA: ${l}`);
  }
});

test("workflow-signing-job-publishes-nothing-and-takes-no-fork-input", () => {
  // the tag input is validated before use and reaches the shell through env only
  // validated strictly by check-release.sh (a stable vX.Y.Z), before any download
  assert.match(readFileSync(join(root, "scripts/macos-sign/check-release.sh"), "utf8"), /\^v\[0-9\]\+\\\.\[0-9\]\+\\\.\[0-9\]\+\$/);
  assert.match(workflow, /TAG: \$\{\{ inputs\.tag \}\}/);
  assert.match(workflow, /retention-days: 7/);
});

// The job signs only an app that is provably ours: a published stable release on develop, its tarball
// listed in a checksums.txt signed with the committed updater key. All of that runs BEFORE the signing
// step, in steps that have no secret in their environment, and the tarball is unpacked only after it.
test("workflow-asset-is-proven-before-signing", () => {
  const sign = code.join("\n").slice(code.join("\n").indexOf("\n  sign:"));
  const steps = sign.split(/\n      - /).slice(1);
  const at = (needle) => steps.findIndex((s) => s.includes(needle));
  const check = at("scripts/macos-sign/check-release.sh");
  const download = at("gh release download");
  const prove = at("scripts/macos-sign/provenance.mjs");
  const unpack = at("tar -xzf");
  const signing = at("scripts/macos-sign/sign-notarize.sh");
  for (const [n, v] of Object.entries({ check, download, prove, unpack, signing })) assert.notEqual(v, -1, `step ${n} missing`);
  assert.ok(check < download && download < prove && prove < unpack && unpack < signing, `wrong order: ${[check, download, prove, unpack, signing]}`);
  // checksums and their signature come from the release, and every step before the signing one is secret-free
  assert.match(steps[download], /--pattern checksums\.txt --pattern checksums\.txt\.minisig/);
  for (let i = 0; i < signing; i++) assert.doesNotMatch(steps[i], /secrets\./, `step ${i} before signing reads a secret`);
  // full history and tags, so the tag can be shown to sit on develop
  assert.match(sign, /fetch-depth: 0/);
  // the tarball is unpacked from the proven file name, not from a glob
  assert.match(steps[unpack], /tar -xzf "work\/in\/\$ASSET"/);
  assert.match(steps[prove], /--asset "\$ASSET"/);
});

// Only the signing workflow may use the Environment or the Apple secrets: another workflow doing so would
// run unreviewed code with them.
test("workflow-no-other-workflow-uses-the-environment-or-the-secrets", () => {
  const dir = join(root, ".github/workflows");
  const others = readdirSync(dir).filter((f) => /\.ya?ml$/.test(f) && f !== "desktop-macos-signed.yml");
  assert.ok(others.length > 5, "the other workflows were not found");
  for (const f of others) {
    const text = readFileSync(join(dir, f), "utf8");
    assert.doesNotMatch(text, /macos-signing/, `${f} references the macos-signing Environment`);
    assert.doesNotMatch(text, /SMIND_MACOS_|SMIND_NOTARY_/, `${f} references the Apple signing secrets`);
  }
});

test("codeowners-cover-the-signing-paths", () => {
  const owners = readFileSync(join(root, ".github/CODEOWNERS"), "utf8")
    .split("\n")
    .filter((l) => l.trim() !== "" && !l.startsWith("#"))
    .map((l) => l.split(/\s+/)[0]);
  for (const path of [
    "/.github/workflows/",
    "/.github/CODEOWNERS",
    "/scripts/macos-sign/",
    "/scripts/macos-sign.test.mjs",
    "/scripts/macos-sign-workflow.test.mjs",
    "/desktop/src-tauri/entitlements/",
  ]) {
    assert.ok(owners.includes(path), `${path} is not covered by CODEOWNERS`);
  }
  for (const l of readFileSync(join(root, ".github/CODEOWNERS"), "utf8").split("\n").filter((l) => l.trim() !== "" && !l.startsWith("#"))) {
    assert.match(l, /^\S+\s+@\S+$/, `a CODEOWNERS line without an owner: ${l}`);
  }
});
