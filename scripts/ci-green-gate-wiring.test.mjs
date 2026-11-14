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

test("gate-workflow-wiring", () => {
  const publish = job("publish");
  const needs = publish.match(/^    needs: \[([^\]]*)\]/m);
  assert.ok(needs, "publish has a needs list");
  assert.ok(needs[1].split(",").map((n) => n.trim()).includes("ci-gate"), "publish needs ci-gate");
  // On a real release the gate's success is mandatory; on a dry run it is not.
  assert.match(publish, /needs\.release-please\.outputs\.release_created != 'true' \|\| needs\.ci-gate\.result == 'success'/);

  const gate = job("ci-gate");
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
