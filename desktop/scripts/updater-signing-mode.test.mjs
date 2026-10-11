import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const script = join(dirname(fileURLToPath(import.meta.url)), "updater-signing-mode.sh");

function run(env) {
  return spawnSync("sh", [script], {
    env: { PATH: process.env.PATH, ...env },
    encoding: "utf8",
  });
}

test("signing-mode-signs-when-key-present", () => {
  const r = run({ TAURI_SIGNING_PRIVATE_KEY: "test-key-material" });
  assert.equal(r.status, 0);
  assert.equal(r.stdout.trim(), "sign=true");
});

test("signing-mode-skips-when-absent", () => {
  const r = run({});
  assert.equal(r.status, 0);
  assert.equal(r.stdout.trim(), "sign=false");

  const r2 = run({ REQUIRE_UPDATER_SIGNING: "false" });
  assert.equal(r2.status, 0);
  assert.equal(r2.stdout.trim(), "sign=false");
});

test("signing-mode-fails-when-required-and-absent", () => {
  const r = run({ REQUIRE_UPDATER_SIGNING: "true" });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /require-updater-signing is set but the TAURI_SIGNING_PRIVATE_KEY secret is empty; set it with gh secret set TAURI_SIGNING_PRIVATE_KEY/);
});

test("signing-mode-never-prints-key", () => {
  for (const env of [
    { TAURI_SIGNING_PRIVATE_KEY: "SECRET-KEY-MATERIAL" },
    { TAURI_SIGNING_PRIVATE_KEY: "SECRET-KEY-MATERIAL", REQUIRE_UPDATER_SIGNING: "true" },
    { TAURI_SIGNING_PRIVATE_KEY: "SECRET-KEY-MATERIAL", REQUIRE_UPDATER_SIGNING: "other" },
  ]) {
    const r = run(env);
    assert.ok(!r.stdout.includes("SECRET-KEY-MATERIAL"), `stdout leaked the key: ${r.stdout}`);
    assert.ok(!r.stderr.includes("SECRET-KEY-MATERIAL"), `stderr leaked the key: ${r.stderr}`);
  }
});
