// Pins how release-please.yml runs scripts/verify-release-artifacts.mjs
// (docs/plans/active/desktop-release-hygiene.md items 1 and 4): the verifier runs in `publish`
// BEFORE anything is uploaded, on dry runs too, with every check on (only the manifest check is
// skipped the first time, because latest.json does not exist yet), and runs again on latest.json
// before that file is uploaded. A static read of the workflow text, like ci-green-gate-wiring.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { CHECKS } from "./verify-release-artifacts.mjs";

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
  return lines.slice(start, end);
}

// The steps of a job: each starts at a `      - ` line (6 spaces) and runs to the next one.
function steps(jobLines) {
  const out = [];
  for (const l of jobLines) {
    if (/^ {6}- /.test(l)) out.push([l]);
    else if (out.length) out[out.length - 1].push(l);
  }
  return out.map((ls) => ls.filter((l) => !/^\s*#/.test(l)).join("\n"));
}

const publishSteps = () => steps(job("publish"));
const isVerify = (s) => /scripts\/verify-release-artifacts\.mjs/.test(s);
const isUpload = (s) => /upload-artifact@|gh release upload/.test(s);

test("release-verify-runs-before-any-upload-and-is-never-skipped", () => {
  const all = publishSteps();
  const verifySteps = all.filter(isVerify);
  assert.equal(verifySteps.length, 2, "the verifier runs twice in publish: before the uploads, and on latest.json");

  const first = all.findIndex((s) => isVerify(s) && /--skip-latest/.test(s));
  assert.notEqual(first, -1, "the pre-upload verification (without latest.json) exists");
  const firstUpload = all.findIndex(isUpload);
  assert.notEqual(firstUpload, -1);
  assert.ok(first < firstUpload, `the pre-upload verification (step ${first}) must come before the first upload (step ${firstUpload})`);

  // it runs on dry runs too: no condition at all on that step
  assert.doesNotMatch(all[first], /^\s*(- )?if:/m, "the pre-upload verification must not be conditional");
  assert.doesNotMatch(all[first], /continue-on-error|\|\|\s*(true|:)/, "its failure must fail the job");
  // everything but the manifest is checked: no other way to switch a check off is passed
  const flags = [...all[first].matchAll(/ --([a-z-]+)/g)].map((m) => m[1]);
  for (const f of flags) {
    assert.ok(["dir", "version", "tag", "repo", "desktop-version", "skip-latest", "source-root"].includes(f), `unexpected verifier flag --${f}`);
  }
  assert.ok(CHECKS.includes("updater-signatures"), "the verifier has the updater-signatures check");
  assert.ok(CHECKS.includes("latest-json") && CHECKS.includes("sidecar-versions"));
});

test("release-verify-checks-latest-json-before-uploading-it", () => {
  const all = publishSteps();
  const gen = all.find((s) => /gen-latest-json\.mjs/.test(s));
  assert.ok(gen, "the latest.json step exists");
  const run = gen;
  const verifyAt = run.indexOf("verify-release-artifacts.mjs");
  const uploadAt = run.indexOf('gh release upload "$TAG" latest.json');
  assert.ok(verifyAt !== -1 && uploadAt !== -1 && verifyAt < uploadAt, "latest.json is verified before it is uploaded");
  assert.ok(run.indexOf("gen-latest-json.mjs") < verifyAt, "and after it is generated");
  assert.doesNotMatch(run.slice(verifyAt, uploadAt), /--skip-latest/, "the second run checks the manifest too");
  assert.doesNotMatch(run.slice(verifyAt, uploadAt), /\|\|\s*(true|:)/);
});
