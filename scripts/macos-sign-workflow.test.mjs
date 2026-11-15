// Static pins on .github/workflows/desktop-macos-signed.yml (docs/plans/active/
// desktop-notarization-prep.md, AC6): the signing workflow can never run on a pull request, never
// hands a secret to anything but the one step that needs it, and cannot publish.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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
  // the switch and the secrets are checked in a gate job; the signing job needs it and is protected
  assert.match(workflow, /vars\.MACOS_SIGNING_ENABLED/);
  const sign = workflow.slice(workflow.indexOf("\n  sign:"));
  assert.match(sign, /needs: gate/);
  assert.match(sign, /if: needs\.gate\.outputs\.enabled == 'true'/);
  assert.match(sign, /environment: macos-signing/);
  for (const name of ["SMIND_MACOS_CERT_P12_BASE64", "SMIND_MACOS_CERT_PASSWORD", "SMIND_MACOS_SIGN_IDENTITY", "SMIND_NOTARY_KEY_P8_BASE64", "SMIND_NOTARY_KEY_ID", "SMIND_NOTARY_ISSUER"]) {
    const gate = workflow.slice(workflow.indexOf("\n  gate:"), workflow.indexOf("\n  sign:"));
    assert.match(gate, new RegExp(`secrets\\.${name} != ''`), `the gate checks ${name} exists`);
  }
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

test("workflow-actions-are-pinned", () => {
  const uses = code.filter((l) => /^\s*-?\s*uses:/.test(l));
  assert.ok(uses.length >= 2, "checkout and upload-artifact at least");
  for (const l of uses) {
    assert.match(l, /uses: [\w.-]+\/[\w.-]+@[0-9a-f]{40}( #.*)?$/, `an action is not pinned to a full commit SHA: ${l}`);
  }
});

test("workflow-signing-job-publishes-nothing-and-takes-no-fork-input", () => {
  // the tag input is validated before use and reaches the shell through env only
  assert.match(workflow, /v\[0-9\]\*\.\[0-9\]\*\.\[0-9\]\*\)/);
  assert.match(workflow, /TAG: \$\{\{ inputs\.tag \}\}/);
  assert.match(workflow, /retention-days: 7/);
});
