import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawnSync } from "node:child_process";

import { buildLatestJson } from "./gen-latest-json.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cli = join(scriptDir, "gen-latest-json.mjs");

// A .sig file the shape the Tauri CLI writes: base64 of the 4-line minisign
// signature text, whose trusted comment carries version:<version> (version
// null = a signature WITHOUT a signed version).
function fakeSig(version, file = "asset") {
  const lines = [
    "untrusted comment: test signature",
    "RWQ" + "A".repeat(20),
    "trusted comment: timestamp:1700000000\tfile:" + file + (version === null ? "" : "\tversion:" + version),
    "GLOBALSIG" + "B".repeat(20),
    "",
  ];
  return Buffer.from(lines.join("\n")).toString("base64") + "\n";
}

function files() {
  return {
    "darwin-aarch64": { asset: "smind-desktop-1.2.3-macos-arm64.app.tar.gz", signature: fakeSig("1.2.3") },
    "darwin-x86_64": { asset: "smind-desktop-1.2.3-macos-x86_64.app.tar.gz", signature: fakeSig("1.2.3") },
    "windows-x86_64": { asset: "sMind_1.2.3_x64-setup.exe", signature: fakeSig("1.2.3") },
  };
}

const base = {
  version: "1.2.3",
  tag: "v1.2.3",
  repo: "spacingmind/smind",
  notes: "release notes",
  pubDate: "2026-10-11T00:00:00Z",
};

test("latest-json-all-platforms", () => {
  const m = buildLatestJson({ ...base, files: files() });
  assert.equal(m.version, "1.2.3");
  assert.equal(m.notes, "release notes");
  assert.equal(m.pub_date, "2026-10-11T00:00:00Z");
  assert.deepEqual(Object.keys(m.platforms).sort(), [
    "darwin-aarch64",
    "darwin-x86_64",
    "windows-x86_64",
  ]);
  for (const p of Object.values(m.platforms)) {
    assert.ok(p.signature.length > 0);
    assert.ok(p.url.startsWith("https://github.com/"));
  }
});

test("latest-json-urls-per-arch", () => {
  const m = buildLatestJson({ ...base, files: files() });
  assert.match(m.platforms["darwin-aarch64"].url, /macos-arm64\.app\.tar\.gz$/);
  assert.match(m.platforms["darwin-x86_64"].url, /macos-x86_64\.app\.tar\.gz$/);
  assert.doesNotMatch(m.platforms["darwin-aarch64"].url, /x86_64\.app\.tar\.gz/);
  assert.doesNotMatch(m.platforms["darwin-x86_64"].url, /arm64\.app\.tar\.gz/);
  for (const url of Object.values(m.platforms).map((p) => p.url)) {
    assert.ok(url.includes("spacingmind/smind/releases/download/v1.2.3/"), url);
  }
});

test("latest-json-signature-is-file-content-trimmed", () => {
  const f = files();
  f["darwin-aarch64"].signature = "  " + fakeSig("1.2.3").trim() + " \n";
  const m = buildLatestJson({ ...base, files: f });
  assert.equal(m.platforms["darwin-aarch64"].signature, fakeSig("1.2.3").trim());
});

test("latest-json-missing-sig-fails", () => {
  const f = files();
  delete f["windows-x86_64"].signature;
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    /windows-x86_64/
  );
  const g = files();
  g["darwin-x86_64"].signature = "   ";
  assert.throws(
    () => buildLatestJson({ ...base, files: g }),
    /darwin-x86_64/
  );
});

test("latest-json-missing-platform-fails", () => {
  const f = files();
  delete f["darwin-x86_64"];
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    /darwin-x86_64/
  );
});

test("latest-json-rejects-prerelease-version", () => {
  assert.throws(
    () => buildLatestJson({ ...base, version: "0.11.0-rc.1", files: files() }),
    /0\.11\.0-rc\.1/
  );
  assert.throws(
    () => buildLatestJson({ ...base, version: "0.11.0+abc", files: files() }),
    /0\.11\.0\+abc/
  );
  const m = buildLatestJson({ ...base, version: "v1.2.3", files: files() });
  assert.equal(m.version, "1.2.3");
});

test("latest-json-url-encodes-asset-names", () => {
  const f = files();
  f["windows-x86_64"].asset = "smind 1.2.3 x64-setup.exe";
  const m = buildLatestJson({ ...base, files: f });
  assert.equal(
    m.platforms["windows-x86_64"].url,
    "https://github.com/spacingmind/smind/releases/download/v1.2.3/smind%201.2.3%20x64-setup.exe"
  );
});

function fixtureDir(version = "1.2.3") {
  const dir = mkdtempSync(join(tmpdir(), "gen-latest-json-"));
  writeFileSync(join(dir, `smind-desktop-${version}-macos-arm64.app.tar.gz`), "a");
  writeFileSync(join(dir, `smind-desktop-${version}-macos-arm64.app.tar.gz.sig`), fakeSig(version));
  writeFileSync(join(dir, `smind-desktop-${version}-macos-x86_64.app.tar.gz`), "a");
  writeFileSync(join(dir, `smind-desktop-${version}-macos-x86_64.app.tar.gz.sig`), fakeSig(version));
  writeFileSync(join(dir, `sMind_${version}_x64-setup.exe`), "a");
  writeFileSync(join(dir, `sMind_${version}_x64-setup.exe.sig`), fakeSig(version));
  return dir;
}

test("latest-json-cli-writes-file", () => {
  const dir = fixtureDir();
  try {
    const out = join(dir, "latest.json");
    const r = spawnSync(process.execPath, [
      cli,
      "--dir", dir,
      "--version", "1.2.3",
      "--tag", "v1.2.3",
      "--repo", "spacingmind/smind",
      "--notes-file", join(dir, "..", "nope.md"),
      "--out", out,
    ]);
    assert.equal(r.status, 1, "expected failure for missing notes file");
    assert.ok(!existsSync(out), "no output file on failure");

    const r2 = execFileSync(process.execPath, [
      cli,
      "--dir", dir,
      "--version", "v1.2.3",
      "--tag", "v1.2.3",
      "--repo", "spacingmind/smind",
      "--out", out,
    ], { stdio: "pipe" });
    const m = JSON.parse(readFileSync(out, "utf8"));
    assert.equal(m.version, "1.2.3");
    assert.equal(m.platforms["darwin-aarch64"].signature, fakeSig("1.2.3").trim());
    assert.match(m.platforms["windows-x86_64"].url, /sMind_1\.2\.3_x64-setup\.exe$/);

    // Missing sig: non-zero exit, no output file.
    rmSync(join(dir, "sMind_1.2.3_x64-setup.exe.sig"));
    const out2 = join(dir, "latest2.json");
    const r3 = spawnSync(process.execPath, [
      cli,
      "--dir", dir,
      "--version", "1.2.3",
      "--tag", "v1.2.3",
      "--repo", "spacingmind/smind",
      "--out", out2,
    ]);
    assert.notEqual(r3.status, 0);
    assert.ok(r3.stderr.toString().includes(".sig"));
    assert.ok(!existsSync(out2), "no output file on missing sig");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("latest-json-asset-version-mismatch-fails", () => {
  // Fixtures built for 1.2.3, manifest requested for 1.2.4: the CLI must
  // refuse and write no output, or clients would loop on "updates".
  const dir = fixtureDir("1.2.3");
  try {
    const out = join(dir, "latest.json");
    const r = spawnSync(process.execPath, [
      cli,
      "--dir", dir,
      "--version", "1.2.4",
      "--tag", "v1.2.4",
      "--repo", "spacingmind/smind",
      "--out", out,
    ]);
    assert.notEqual(r.status, 0);
    assert.match(r.stderr.toString(), /does not contain the requested version "1\.2\.4"/);
    assert.ok(!existsSync(out), "no output file on version mismatch");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("latest-json-refuses-a-signature-without-a-signed-version", () => {
  const f = files();
  f["windows-x86_64"].signature = fakeSig(null);
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    (err) => err.message.includes("windows-x86_64") && err.message.includes("signed version")
  );
});

test("latest-json-refuses-a-signed-version-that-differs", () => {
  const f = files();
  f["darwin-x86_64"].signature = fakeSig("1.2.2");
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    (err) => err.message.includes("darwin-x86_64") && err.message.includes("1.2.2") && err.message.includes("1.2.3")
  );
});

test("latest-json-accepts-v-prefixed-signed-version", () => {
  const f = files();
  f["darwin-aarch64"].signature = fakeSig("v1.2.3");
  const m = buildLatestJson({ ...base, files: f });
  assert.equal(m.platforms["darwin-aarch64"].signature, fakeSig("v1.2.3").trim());
});

test("latest-json-refuses-an-undecodable-signature", () => {
  const f = files();
  f["darwin-aarch64"].signature = "!!!not-base64!!!";
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    (err) => err.message.includes("darwin-aarch64") && err.message.includes("base64")
  );
});

test("latest-json-refuses-a-signature-without-a-trusted-comment", () => {
  const f = files();
  f["windows-x86_64"].signature =
    Buffer.from("untrusted comment: x\nRWQ" + "A".repeat(20) + "\n").toString("base64") + "\n";
  assert.throws(
    () => buildLatestJson({ ...base, files: f }),
    (err) => err.message.includes("windows-x86_64") && err.message.includes("trusted comment")
  );
});

test("latest-json-cli-refuses-and-writes-nothing-for-a-versionless-signature", () => {
  const dir = fixtureDir();
  try {
    writeFileSync(join(dir, "smind-desktop-1.2.3-macos-arm64.app.tar.gz.sig"), fakeSig(null));
    const out = join(dir, "latest.json");
    const r = spawnSync(process.execPath, [
      cli,
      "--dir", dir,
      "--version", "1.2.3",
      "--tag", "v1.2.3",
      "--repo", "spacingmind/smind",
      "--out", out,
    ]);
    assert.equal(r.status, 1);
    assert.match(r.stderr.toString(), /signed version/);
    assert.ok(!existsSync(out), "no output file for a versionless signature");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
