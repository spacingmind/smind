// Pins how release-please.yml uses scripts/ci-green-gate.mjs
// (docs/plans/active/desktop-release-hygiene.md, item 2, AC7). A static read
// of the workflow text: no YAML dependency, job blocks are split on their
// two-space-indented key.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const workflow = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", ".github/workflows/release-please.yml"), "utf8");

function job(name) {
  const lines = workflow.split("\n");
  const start = lines.findIndex((l) => l === `  ${name}:`);
  assert.notEqual(start, -1, `job ${name} not found`);
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^  [A-Za-z0-9_-]+:\s*$/.test(lines[i])) {
      end = i;
      break;
    }
  }
  return lines.slice(start, end).join("\n");
}

// The top-level `&&` terms of a job's multi-line `if: |` block ("a && b && (c || d)" -> [a, b, (c || d)]).
function ifTerms(block) {
  const lines = block.split("\n");
  const start = lines.findIndex((l) => l === "    if: |");
  assert.notEqual(start, -1, "job has a multi-line if: |");
  const body = [];
  for (let i = start + 1; i < lines.length && /^      \S/.test(lines[i]); i++) body.push(lines[i].trim());
  return body.join(" ").split(" && ").map((t) => t.trim());
}

test("gate-ci-workflow-job-name", () => {
  // the gate looks for the job named `ci`: it must stay a plain job with no name: override
  const ci = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", ".github/workflows/ci.yml"), "utf8");
  const lines = ci.split("\n");
  const start = lines.findIndex((l) => l === "  ci:");
  assert.notEqual(start, -1, "ci.yml has a job `ci`");
  const next = lines.findIndex((l, i) => i > start && /^  [A-Za-z0-9_-]+:\s*$/.test(l));
  const body = lines.slice(start + 1, next === -1 ? undefined : next).join("\n");
  assert.doesNotMatch(body, /^    name:/m);
  assert.doesNotMatch(body, /^    (strategy|uses):/m, "a matrix or reusable-workflow job would be named differently");
});

test("gate-workflow-wiring", () => {
  const publish = job("publish");
  const needs = publish.match(/^    needs: \[([^\]]*)\]/m);
  assert.ok(needs, "publish has a needs list");
  assert.ok(needs[1].split(",").map((n) => n.trim()).includes("ci-gate"), "publish needs ci-gate");
  // On a real release the gate's success is mandatory; on a dry run it is not. The clause must be a
  // top-level AND term of publish's own `if:` (a comment or an `|| always()` must not satisfy this).
  const terms = ifTerms(publish);
  assert.equal(terms[0], "always()");
  assert.ok(
    terms.includes("(needs.release-please.outputs.release_created != 'true' || needs.ci-gate.result == 'success')"),
    `publish if terms: ${JSON.stringify(terms)}`
  );
  assert.ok(terms.includes("needs.build-binaries.result == 'success'"), "the build gates are still ANDed in");

  const gate = job("ci-gate");
  assert.equal(ifTerms(gate)[0], "always()", "ci-gate runs even when release-please is skipped (dispatch)");
  assert.match(gate, /needs: release-please/);
  assert.match(gate, /release_created == 'true' \|\| github\.event_name == 'workflow_dispatch'/);
  assert.match(gate, /actions: read/);
  assert.doesNotMatch(gate, /contents: write|secrets\./, "the gate holds no write permission and no secret");
  // The commit under release is the push's own sha; a real release also pins the tag.
  assert.match(gate, /SHA: \$\{\{ github\.sha \}\}/);
  assert.match(gate, /--sha "\$SHA" --tag "\$RELEASE_TAG"/);
  assert.match(gate, /--dry-run/);
  // A dry run must never be able to gate a real release open or shut by accident:
  // the real invocation has no --dry-run.
  const real = gate.split("\n").find((l) => l.includes('--tag "$RELEASE_TAG"'));
  assert.doesNotMatch(real, /--dry-run/);
});

// --- structure pins (review of #888): the gate must not be neutered by a small edit ---

const stripComments = (text) => text.split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
const gateJob = () => stripComments(job("ci-gate"));
const publishJob = () => stripComments(job("publish"));

// The lines of the `run: |` block that follows `marker` (a line of the step), dedented.
function runBlock(block, marker) {
  const lines = block.split("\n");
  const at = lines.findIndex((l) => l.includes(marker));
  assert.notEqual(at, -1, `step ${marker} not found`);
  const run = lines.findIndex((l, i) => i > at && l === "        run: |");
  assert.notEqual(run, -1, "step has a run block");
  const out = [];
  for (let i = run + 1; i < lines.length && (lines[i] === "" || lines[i].startsWith("          ")); i++) out.push(lines[i].trim());
  return out.filter((l) => l !== "");
}

test("gate-wiring-no-escape-hatches", () => {
  for (const [name, block] of [["ci-gate", gateJob()], ["publish", publishJob()]]) {
    assert.doesNotMatch(block, /continue-on-error/, `${name}: continue-on-error would let a red gate pass`);
    assert.doesNotMatch(block, /\|\|\s*(true|:|exit 0)\b/, `${name}: '|| true' swallows a failure`);
    assert.doesNotMatch(block, /set \+e/, `${name}: set +e swallows a failure`);
  }
  // no step of the gate may be skipped by a step-level condition
  assert.doesNotMatch(gateJob(), /^ {8}if:|^ {6}- if:/m, "a step-level if could skip the gate");
});

test("gate-wiring-invocation-shape", () => {
  const gate = gateJob();
  // The whole script of the step, exactly: the real invocation (with the tag, without --dry-run) is the
  // then-branch of IS_RELEASE = true, the dry run only the else-branch, and nothing after the fi.
  assert.deepEqual(runBlock(gate, "name: Require CI green on the release commit"), [
    "set -euo pipefail",
    'if [ "$IS_RELEASE" = "true" ]; then',
    'node scripts/ci-green-gate.mjs --repo "$REPO" --sha "$SHA" --tag "$RELEASE_TAG"',
    "else",
    'node scripts/ci-green-gate.mjs --repo "$REPO" --sha "$SHA" --dry-run',
    "fi",
  ]);
  // where the inputs come from
  assert.match(gate, /^ {10}IS_RELEASE: \$\{\{ needs\.release-please\.outputs\.release_created \}\}$/m);
  assert.match(gate, /^ {10}RELEASE_TAG: \$\{\{ needs\.release-please\.outputs\.tag_name \}\}$/m);
  assert.match(gate, /^ {10}SHA: \$\{\{ github\.sha \}\}$/m);
  assert.equal((gate.match(/ci-green-gate\.mjs/g) ?? []).length, 2, "the script runs in exactly the two branches");
});

test("gate-wiring-permissions-exact", () => {
  const lines = gateJob().split("\n");
  const at = lines.findIndex((l) => l === "    permissions:");
  assert.notEqual(at, -1, "ci-gate declares its permissions");
  const perms = [];
  for (let i = at + 1; i < lines.length && /^ {6}\S/.test(lines[i]); i++) perms.push(lines[i].trim());
  assert.deepEqual(perms.sort(), ["actions: read", "contents: read"]);
});

test("gate-wiring-publish-needs-superset", () => {
  const needsOf = (block) => new Set(block.match(/^ {4}needs: \[?([^\]\n]*)\]?$/m)?.[1].split(",").map((n) => n.trim()).filter(Boolean));
  const gateNeeds = needsOf(gateJob());
  const publishNeeds = needsOf(publishJob());
  assert.ok(gateNeeds.size > 0 && publishNeeds.size > 0);
  for (const n of [...gateNeeds, "ci-gate"]) assert.ok(publishNeeds.has(n), `publish must need ${n}`);
});

test("gate-wiring-release-uploads-only-in-publish", () => {
  const jobs = [...stripComments(workflow).matchAll(/^ {2}([A-Za-z0-9_-]+):\s*$/gm)].map((m) => m[1]);
  assert.ok(jobs.includes("publish") && jobs.includes("ci-gate"));
  for (const name of jobs) {
    if (name === "publish") continue;
    assert.doesNotMatch(stripComments(job(name)), /gh release (upload|create|edit)/, `${name} must not publish release assets`);
  }
  assert.match(publishJob(), /gh release upload/);
});
