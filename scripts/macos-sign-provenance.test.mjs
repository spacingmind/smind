// The signing job signs only an app that came out of OUR release process (docs/plans/active/
// desktop-notarization-prep.md): the release is published, stable and on develop (check-release.sh),
// and the app tarball is listed in a checksums.txt signed with the committed updater key
// (provenance.mjs). Unit tests with a throwaway key, a temporary git repository and a fake `gh`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync, execFileSync } from "node:child_process";
import { createHash, generateKeyPairSync, sign as cryptoSign } from "node:crypto";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { provenanceFailure } from "./macos-sign/provenance.mjs";
import { verifyMinisign } from "./verify-release-artifacts.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const PROVENANCE = join(root, "scripts/macos-sign/provenance.mjs");
const CHECK = join(root, "scripts/macos-sign/check-release.sh");

// --- real minisign signatures with throwaway keys (prehashed ED, as `tauri signer sign` writes) ---
function newKey(id) {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  return { privateKey, raw: publicKey.export({ format: "der", type: "spki" }).subarray(-32), keyId: Buffer.from(id, "hex") };
}
const KEY = newKey("0123456789abcdef");
const OTHER = newKey("fedcba9876543210");
const pubkey = (k = KEY) =>
  Buffer.from(`untrusted comment: minisign public key: TEST\n${Buffer.concat([Buffer.from("Ed"), k.keyId, k.raw]).toString("base64")}\n`).toString("base64");
function minisig(data, comment, k = KEY) {
  const sig = cryptoSign(null, createHash("blake2b512").update(data).digest(), k.privateKey);
  const global = cryptoSign(null, Buffer.concat([sig, Buffer.from(comment)]), k.privateKey);
  return ["untrusted comment: signature from tauri secret key", Buffer.concat([Buffer.from("ED"), k.keyId, sig]).toString("base64"), `trusted comment: ${comment}`, global.toString("base64"), ""].join("\n");
}

const TAG = "v1.2.3";
const ASSET = "smind-desktop-1.2.3-macos-arm64.app.tar.gz";
const BYTES = Buffer.from("the app tarball bytes");
const sha = (b) => createHash("sha256").update(b).digest("hex");

function release({ checksums, comment = `timestamp:1\tfile:checksums.txt\tversion:1.2.3`, key = KEY, tamper = false } = {}) {
  const text = checksums ?? `${sha(Buffer.from("other"))}  smind_1.2.3_linux_amd64.tar.gz\n${sha(BYTES)}  ${ASSET}\n`;
  const sig = minisig(Buffer.from(text), comment, key);
  return { asset: ASSET, assetBytes: BYTES, checksumsText: tamper ? text + "extra\n" : text, minisigText: sig, pubkey: pubkey(), tag: TAG };
}

test("provenance-accepts-a-release-asset-listed-in-signed-checksums", () => {
  assert.equal(provenanceFailure(release()), null);
});

test("provenance-refuses-an-asset-not-in-the-checksums", () => {
  const r = release({ checksums: `${sha(Buffer.from("other"))}  smind_1.2.3_linux_amd64.tar.gz\n` });
  assert.match(provenanceFailure(r), /listed 0 times/);
});

test("provenance-refuses-an-asset-that-differs-from-its-entry", () => {
  const r = { ...release(), assetBytes: Buffer.from("a malicious tarball") };
  assert.match(provenanceFailure(r), /does not match its entry/);
});

test("provenance-refuses-an-asset-listed-twice", () => {
  const line = `${sha(BYTES)}  ${ASSET}\n`;
  assert.match(provenanceFailure(release({ checksums: line + line })), /listed 2 times/);
});

test("provenance-refuses-checksums-signed-by-another-key", () => {
  assert.match(provenanceFailure(release({ key: OTHER })), /different key|does not verify/);
});

test("provenance-refuses-checksums-changed-after-signing", () => {
  assert.match(provenanceFailure(release({ tamper: true })), /does not match the artifact/);
});

test("provenance-refuses-a-signature-for-another-file-or-version", () => {
  assert.match(provenanceFailure(release({ comment: "timestamp:1\tfile:other.txt\tversion:1.2.3" })), /not for checksums\.txt/);
  assert.match(provenanceFailure(release({ comment: "timestamp:1\tfile:checksums.txt\tversion:9.9.9" })), /not for version 1\.2\.3/);
  assert.match(provenanceFailure(release({ comment: "timestamp:1\tfile:checksums.txt" })), /not for version/);
});

test("provenance-refuses-without-a-key-or-with-a-bad-tag-or-asset-name", () => {
  assert.match(provenanceFailure({ ...release(), pubkey: undefined }), /no updater public key/);
  assert.match(provenanceFailure({ ...release(), tag: "v1.2.3-rc1" }), /not a stable release tag/);
  assert.match(provenanceFailure({ ...release(), tag: "main" }), /not a stable release tag/);
  assert.match(provenanceFailure({ ...release(), asset: "../" + ASSET }), /plain file name/);
});

// Real data (v0.20.0, public): the committed key verifies the real checksums.txt signature, and the
// checks before the digest comparison pass on it (the asset bytes here are not the real tarball).
test("provenance-real-v0.20.0-checksums-pass-up-to-the-digest", () => {
  const dir = join(root, "scripts/testdata/real-v0.20.0");
  const checksumsText = readFileSync(join(dir, "checksums.txt"), "utf8");
  const real = {
    asset: "smind-desktop-0.20.0-macos-arm64.app.tar.gz",
    assetBytes: Buffer.from("not the real tarball"),
    checksumsText,
    minisigText: readFileSync(join(dir, "checksums.txt.minisig"), "utf8"),
    pubkey: readFileSync(join(dir, "updater-pubkey.txt"), "utf8").trim(),
    tag: "v0.20.0",
  };
  assert.equal(verifyMinisign(Buffer.from(checksumsText), real.minisigText, real.pubkey, { plain: true }), null);
  assert.match(provenanceFailure(real), /does not match its entry/, "signature, comment and listing all passed: only the bytes differ");
  assert.match(provenanceFailure({ ...real, tag: "v0.20.1" }), /not for version 0\.20\.1/);
});

test("provenance-cli-exit-codes-and-no-secret-needed", () => {
  const dir = mkdtempSync(join(tmpdir(), "prov-"));
  try {
    const r = release();
    writeFileSync(join(dir, ASSET), r.assetBytes);
    writeFileSync(join(dir, "checksums.txt"), r.checksumsText);
    writeFileSync(join(dir, "checksums.txt.minisig"), r.minisigText);
    const conf = join(dir, "tauri.conf.json");
    writeFileSync(conf, JSON.stringify({ plugins: { updater: { pubkey: r.pubkey } } }));
    const env = { PATH: process.env.PATH }; // a clean environment: no secret is read
    const ok = spawnSync("node", [PROVENANCE, "--dir", dir, "--asset", ASSET, "--tag", TAG, "--conf", conf], { env, encoding: "utf8" });
    assert.equal(ok.status, 0, ok.stderr);
    writeFileSync(join(dir, ASSET), "swapped");
    const bad = spawnSync("node", [PROVENANCE, "--dir", dir, "--asset", ASSET, "--tag", TAG, "--conf", conf], { env, encoding: "utf8" });
    assert.equal(bad.status, 1);
    assert.match(bad.stderr, /REFUSED/);
    assert.equal(spawnSync("node", [PROVENANCE, "--dir", dir], { env, encoding: "utf8" }).status, 2);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// --- check-release.sh: a temporary git repository and a fake gh ---------------------------------
function repoWithTag({ tagOnDevelop = true, tag = TAG } = {}) {
  const dir = mkdtempSync(join(tmpdir(), "chk-"));
  const git = (...a) => execFileSync("git", ["-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", ...a], { encoding: "utf8" });
  git("init", "-q", "-b", "develop");
  writeFileSync(join(dir, "a"), "1");
  git("add", "-A");
  git("commit", "-qm", "one");
  git("update-ref", "refs/remotes/origin/develop", "HEAD");
  if (!tagOnDevelop) {
    git("checkout", "-q", "-b", "stray");
    writeFileSync(join(dir, "b"), "2");
    git("add", "-A");
    git("commit", "-qm", "stray");
  }
  if (tag) git("tag", tag);
  const bin = join(dir, ".fakebin");
  mkdirSync(bin);
  writeFileSync(join(bin, "gh"), '#!/usr/bin/env bash\necho "$*" >> "$FAKE_GH_LOG"\n[ "${FAKE_GH_FAIL:-}" = 1 ] && exit 1\necho "$FAKE_GH_JSON"\n');
  chmodSync(join(bin, "gh"), 0o755);
  return { dir, bin };
}
function runCheck(r, args, { json = '{"isDraft":false,"isPrerelease":false,"tagName":"v1.2.3"}', fail = false } = {}) {
  const log = join(r.dir, "gh.log");
  const res = spawnSync("bash", [CHECK, ...args], {
    cwd: r.dir,
    env: { PATH: `${r.bin}:${process.env.PATH}`, HOME: r.dir, FAKE_GH_JSON: json, FAKE_GH_LOG: log, ...(fail ? { FAKE_GH_FAIL: "1" } : {}) },
    encoding: "utf8",
  });
  return { ...res, ghCalled: existsSync(log) };
}
function withRepo(opts, fn) {
  const r = repoWithTag(opts);
  try {
    fn(r);
  } finally {
    rmSync(r.dir, { recursive: true, force: true });
  }
}
const ARGS = ["--tag", TAG, "--repo", "o/r"];

test("check-release-accepts-a-published-stable-release-on-develop", () => {
  withRepo({}, (r) => {
    const res = runCheck(r, ARGS);
    assert.equal(res.status, 0, res.stderr);
  });
});

test("check-release-refuses-a-draft-a-prerelease-and-a-tag-name-mismatch", () => {
  withRepo({}, (r) => {
    assert.match(runCheck(r, ARGS, { json: '{"isDraft":true,"isPrerelease":false,"tagName":"v1.2.3"}' }).stderr, /draft/);
    assert.match(runCheck(r, ARGS, { json: '{"isDraft":false,"isPrerelease":true,"tagName":"v1.2.3"}' }).stderr, /prerelease/);
    assert.match(runCheck(r, ARGS, { json: '{"isDraft":false,"isPrerelease":false,"tagName":"v9.9.9"}' }).stderr, /tag name/);
    for (const j of ['{"isDraft":false,"isPrerelease":false,"tagName":"v1.2.3"', "not json", ""]) {
      assert.notEqual(runCheck(r, ARGS, { json: j }).status, 0, `json ${j}`);
    }
  });
});

test("check-release-refuses-a-tag-not-on-develop", () => {
  withRepo({ tagOnDevelop: false }, (r) => {
    const res = runCheck(r, ARGS);
    assert.equal(res.status, 1);
    assert.match(res.stderr, /not on develop/);
  });
});

test("check-release-refuses-a-missing-tag-a-missing-release-and-a-missing-develop", () => {
  withRepo({ tag: null }, (r) => assert.match(runCheck(r, ARGS).stderr, /tag does not exist/));
  withRepo({}, (r) => assert.match(runCheck(r, ARGS, { fail: true }).stderr, /no such release/));
  withRepo({}, (r) => {
    execFileSync("git", ["-C", r.dir, "update-ref", "-d", "refs/remotes/origin/develop"]);
    assert.match(runCheck(r, ARGS).stderr, /origin\/develop/);
  });
});

test("check-release-refuses-a-non-stable-tag-before-asking-github", () => {
  withRepo({}, (r) => {
    for (const bad of ["v1.2.3-rc1", "1.2.3", "main", "v1.2", "v1.2.3;rm"]) {
      const res = runCheck(r, ["--tag", bad, "--repo", "o/r"]);
      assert.equal(res.status, 1, bad);
      assert.equal(res.ghCalled, false, `gh was called for ${bad}`);
    }
    assert.equal(runCheck(r, ["--tag", TAG]).status, 2, "--repo is required");
  });
});
