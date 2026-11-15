import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash, generateKeyPairSync, sign as cryptoSign } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { daemonArchiveNames, desktopAssetNames, signedVersionOf, verify, verifyMinisign } from "./verify-release-artifacts.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cli = join(scriptDir, "verify-release-artifacts.mjs");
const REPO = "spacingmind/smind";

// Real minisign signatures, made here with a throwaway Ed25519 key in the format `tauri signer sign`
// writes (prehashed "ED": the signature is over BLAKE2b-512 of the data, the global signature over
// the signature and the trusted comment), so the verifier's cryptographic check runs for real.
function newKey() {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  return { privateKey, raw: publicKey.export({ format: "der", type: "spki" }).subarray(-32), keyId: Buffer.from("0123456789abcdef", "hex") };
}
const TEST_KEY = newKey();
const OTHER_KEY = { ...newKey(), keyId: Buffer.from("fedcba9876543210", "hex") };

function tauriPubkey(key = TEST_KEY) {
  const bin = Buffer.concat([Buffer.from("Ed"), key.keyId, key.raw]);
  return Buffer.from(`untrusted comment: minisign public key: TEST\n${bin.toString("base64")}\n`).toString("base64");
}

function minisignText(data, comment, key = TEST_KEY) {
  const hash = createHash("blake2b512").update(data).digest();
  const sig = cryptoSign(null, hash, key.privateKey);
  const global = cryptoSign(null, Buffer.concat([sig, Buffer.from(comment)]), key.privateKey);
  return [
    "untrusted comment: signature from tauri secret key",
    Buffer.concat([Buffer.from("ED"), key.keyId, sig]).toString("base64"),
    `trusted comment: ${comment}`,
    global.toString("base64"),
    "",
  ].join("\n");
}

// The .sig file for `data`, whose trusted comment names `file` and the signed `version` (null = no
// version field, like a signature made before the CLI recorded versions).
function fakeSig(data, version, file = "asset", key = TEST_KEY) {
  const comment = `timestamp:1700000000\tfile:${file}` + (version === null ? "" : `\tversion:${version}`);
  return Buffer.from(minisignText(data, comment, key)).toString("base64") + "\n";
}

// What the Tauri CLI writes in the trusted comment's file: field for each updater asset: the
// installer keeps its name, both macOS tarballs are `sMind.app.tar.gz` (checked on the real v0.20.0
// signatures), so the file field cannot tell the two architectures apart: only the cryptographic
// check can.
const sigFileField = (asset) => (asset.endsWith(".app.tar.gz") ? "sMind.app.tar.gz" : asset);

function daemonBinary(version) {
  return Buffer.from(
    `junk\0-ldflags="-X github.com/spacingmind/smind/internal/version.Version=${version} -X x"\0junk`,
    "latin1"
  );
}

function infoPlist(version) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleShortVersionString</key>
	<string>${version}</string>
	<key>CFBundleVersion</key>
	<string>${version}</string>
</dict>
</plist>
`;
}

function inStage(prefix, fn) {
  const stage = mkdtempSync(join(tmpdir(), prefix));
  try {
    return fn(stage);
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
}

function buildDaemonTar(archive, version) {
  inStage("vrf-tar-", (stage) => {
    writeFileSync(join(stage, "smind"), daemonBinary(version));
    execFileSync("tar", ["-czf", archive, "-C", stage, "smind"]);
  });
}

function buildDaemonZip(archive, version) {
  inStage("vrf-zip-", (stage) => {
    writeFileSync(join(stage, "smind.exe"), daemonBinary(version));
    execFileSync("zip", ["-qj", archive, join(stage, "smind.exe")]);
  });
}

function buildAppTar(archive, { appVersion, daemonVersion }) {
  inStage("vrf-app-", (stage) => {
    mkdirSync(join(stage, "sMind.app/Contents/MacOS"), { recursive: true });
    writeFileSync(join(stage, "sMind.app/Contents/Info.plist"), infoPlist(appVersion));
    writeFileSync(join(stage, "sMind.app/Contents/MacOS/smind"), daemonBinary(daemonVersion));
    execFileSync("tar", ["-czf", archive, "-C", stage, "sMind.app"]);
  });
}

function writeChecksums(dist) {
  const names = readdirSync(dist)
    .filter((n) => [".tar.gz", ".zip", ".exe", ".msi", ".dmg", ".sig"].some((ext) => n.endsWith(ext)))
    .sort();
  const text =
    names.map((n) => `${createHash("sha256").update(readFileSync(join(dist, n))).digest("hex")}  ${n}`).join("\n") + "\n";
  writeFileSync(join(dist, "checksums.txt"), text);
}

// Builds a complete GOOD release: a fake repo tree under root/src and the
// full artifact set under root/dist, all describing {version, desktopVersion}.
function makeRelease(root, { version = "1.2.3", desktopVersion = version } = {}) {
  const src = join(root, "src");
  const dist = join(root, "dist");
  mkdirSync(join(src, "desktop/src-tauri"), { recursive: true });
  mkdirSync(join(src, "desktop/daemon-client"), { recursive: true });
  mkdirSync(dist);
  writeFileSync(join(src, ".release-please-manifest.json"), JSON.stringify({ ".": desktopVersion }, null, 2) + "\n");
  writeFileSync(join(src, "desktop/package.json"), JSON.stringify({ version: desktopVersion }, null, 2) + "\n");
  writeFileSync(
    join(src, "desktop/src-tauri/tauri.conf.json"),
    JSON.stringify({ version: desktopVersion, plugins: { updater: { pubkey: tauriPubkey() } } }, null, 2) + "\n"
  );
  for (const rel of ["desktop/src-tauri/Cargo.toml", "desktop/daemon-client/Cargo.toml"]) {
    writeFileSync(join(src, rel), `[package]\nname = "x"\nversion = "${desktopVersion}" # x-release-please-version\n`);
  }

  for (const name of daemonArchiveNames(version)) {
    if (name.endsWith(".zip")) buildDaemonZip(join(dist, name), version);
    else buildDaemonTar(join(dist, name), version);
  }
  for (const name of desktopAssetNames(desktopVersion)) {
    if (name.endsWith(".sig")) continue; // signed below, over the artifact's bytes
    else if (name.endsWith(".app.tar.gz")) {
      buildAppTar(join(dist, name), { appVersion: desktopVersion, daemonVersion: version });
    } else writeFileSync(join(dist, name), `dummy ${name}\n`);
  }

  for (const name of desktopAssetNames(desktopVersion)) {
    if (!name.endsWith(".sig")) continue;
    const asset = name.replace(/\.sig$/, "");
    writeFileSync(join(dist, name), fakeSig(readFileSync(join(dist, asset)), desktopVersion, sigFileField(asset)));
  }

  writeChecksums(dist);
  const tag = `v${version}`;
  writeFileSync(
    join(dist, "checksums.txt.minisig"),
    "untrusted comment: fixture signature\n" +
      "Zml4dHVyZS1maXhlZC1maXh0dXJl\n" +
      `trusted comment: timestamp:1\tfile:checksums.txt\tversion:${version}\n` +
      "Zml4dHVyZS1zZWNvbmQtZml4dHVyZQ==\n"
  );
  const asset = (n) => `https://github.com/${REPO}/releases/download/${tag}/${encodeURIComponent(n)}`;
  const arm64 = `smind-desktop-${desktopVersion}-macos-arm64.app.tar.gz`;
  const x64 = `smind-desktop-${desktopVersion}-macos-x86_64.app.tar.gz`;
  const exe = `sMind_${desktopVersion}_x64-setup.exe`;
  writeFileSync(
    join(dist, "latest.json"),
    JSON.stringify(
      {
        version: desktopVersion,
        notes: "",
        pub_date: "2026-11-14T00:00:00Z",
        platforms: {
          "darwin-aarch64": { signature: readFileSync(join(dist, `${arm64}.sig`), "utf8").trim(), url: asset(arm64) },
          "darwin-x86_64": { signature: readFileSync(join(dist, `${x64}.sig`), "utf8").trim(), url: asset(x64) },
          "windows-x86_64": { signature: readFileSync(join(dist, `${exe}.sig`), "utf8").trim(), url: asset(exe) },
        },
      },
      null,
      2
    ) + "\n"
  );
  return { root, src, dist, version, desktopVersion, tag, repo: REPO };
}

function withRelease(opts, fn) {
  const root = mkdtempSync(join(tmpdir(), "vrf-rel-"));
  try {
    fn(makeRelease(root, opts));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

function verifyOf(fx, overrides = {}) {
  return verify({
    dir: fx.dist,
    version: fx.version,
    tag: fx.tag,
    repo: fx.repo,
    desktopVersion: fx.desktopVersion,
    sourceRoot: fx.src,
    ...overrides,
  });
}

function expectFailure(failures, check, needle) {
  const hit = failures.find((f) => {
    if (f.check !== check) return false;
    if (needle === undefined) return true;
    return needle instanceof RegExp ? needle.test(f.message) : f.message.includes(needle);
  });
  assert.ok(
    hit,
    `expected a "${check}" failure${needle ? ` mentioning ${needle}` : ""}, got ${JSON.stringify(failures)}`
  );
  return hit;
}

function setSourceVersion(fx, rel, bad) {
  const path = join(fx.src, rel);
  if (rel.endsWith(".json")) {
    const doc = JSON.parse(readFileSync(path, "utf8"));
    if (rel === ".release-please-manifest.json") doc["."] = bad;
    else doc.version = bad;
    writeFileSync(path, JSON.stringify(doc, null, 2) + "\n");
  } else {
    writeFileSync(path, readFileSync(path, "utf8").replace(/^version = "[^"]*"/m, `version = "${bad}"`));
  }
}

function rewriteChecksums(fx, mutateLines) {
  const path = join(fx.dist, "checksums.txt");
  const lines = readFileSync(path, "utf8").split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  const out = mutateLines(lines);
  writeFileSync(path, out.join("\n") + "\n");
}

function rewriteMinisig(fx, trustedComment) {
  writeFileSync(
    join(fx.dist, "checksums.txt.minisig"),
    "untrusted comment: fixture signature\n" +
      "Zml4dHVyZS1maXhlZC1maXh0dXJl\n" +
      `${trustedComment}\n` +
      "Zml4dHVyZS1zZWNvbmQtZml4dHVyZQ==\n"
  );
}

test("verify-good-set-passes", () => {
  withRelease({}, (fx) => {
    assert.deepEqual(verifyOf(fx), []);
  });
});

const SOURCE_FILES = [
  ".release-please-manifest.json",
  "desktop/package.json",
  "desktop/src-tauri/tauri.conf.json",
  "desktop/src-tauri/Cargo.toml",
  "desktop/daemon-client/Cargo.toml",
];

for (const rel of SOURCE_FILES) {
  test(`verify-source-version-mismatch-${rel}`, () => {
    withRelease({}, (fx) => {
      setSourceVersion(fx, rel, "0.0.1-bad");
      expectFailure(verifyOf(fx), "source-versions", rel);
    });
  });
}

test("verify-tag-mismatch", () => {
  withRelease({}, (fx) => {
    expectFailure(verifyOf(fx, { tag: "v0.0.1" }), "tag", /expected v1\.2\.3, found v0\.0\.1/);
  });
});

test("verify-missing-daemon-archive", () => {
  withRelease({}, (fx) => {
    const name = daemonArchiveNames(fx.version)[0];
    rmSync(join(fx.dist, name));
    expectFailure(verifyOf(fx), "daemon-archives", name);
  });
});

test("verify-stray-daemon-archive-other-version", () => {
  withRelease({}, (fx) => {
    const stray = `smind_0.19.0_linux_amd64.tar.gz`;
    renameSync(join(fx.dist, `smind_${fx.version}_darwin_amd64.tar.gz`), join(fx.dist, stray));
    expectFailure(verifyOf(fx), "daemon-archives", stray);
  });
});

for (const name of desktopAssetNames("1.2.3")) {
  test(`verify-missing-desktop-asset-${name}`, () => {
    withRelease({}, (fx) => {
      rmSync(join(fx.dist, name));
      expectFailure(verifyOf(fx), "desktop-assets", name);
    });
  });
}

test("verify-desktop-asset-wrong-version", () => {
  withRelease({}, (fx) => {
    renameSync(
      join(fx.dist, "smind-desktop-1.2.3-macos-arm64.dmg"),
      join(fx.dist, "smind-desktop-0.0.1-macos-arm64.dmg")
    );
    const failures = verifyOf(fx);
    expectFailure(failures, "desktop-assets", "smind-desktop-1.2.3-macos-arm64.dmg");
    expectFailure(failures, "desktop-assets", "smind-desktop-0.0.1-macos-arm64.dmg");
  });
});

test("verify-checksum-wrong-digest", () => {
  withRelease({}, (fx) => {
    rewriteChecksums(fx, (lines) => {
      lines[0] = `${"0".repeat(64)}  ${lines[0].split("  ")[1]}`;
      return lines;
    });
    expectFailure(verifyOf(fx), "checksums", /digest for/);
  });
});

test("verify-checksum-missing-entry", () => {
  withRelease({}, (fx) => {
    rewriteChecksums(fx, (lines) => lines.slice(1));
    expectFailure(verifyOf(fx), "checksums", "missing an entry for");
  });
});

test("verify-checksum-extra-entry", () => {
  withRelease({}, (fx) => {
    rewriteChecksums(fx, (lines) => [...lines, `${"a".repeat(64)}  ghost.zip`]);
    expectFailure(verifyOf(fx), "checksums", "ghost.zip");
  });
});

test("verify-checksum-duplicate-entry", () => {
  withRelease({}, (fx) => {
    rewriteChecksums(fx, (lines) => [...lines, lines[0]]);
    expectFailure(verifyOf(fx), "checksums", "more than once");
  });
});

test("verify-checksum-bad-line", () => {
  withRelease({}, (fx) => {
    rewriteChecksums(fx, (lines) => {
      lines[0] = "definitely not a checksum line";
      return lines;
    });
    expectFailure(verifyOf(fx), "checksums", "line 1");
  });
});

test("verify-minisig-missing", () => {
  withRelease({}, (fx) => {
    rmSync(join(fx.dist, "checksums.txt.minisig"));
    expectFailure(verifyOf(fx), "checksums-signature", "checksums.txt.minisig");
  });
});

test("verify-minisig-wrong-version", () => {
  withRelease({}, (fx) => {
    rewriteMinisig(fx, "trusted comment: timestamp:1\tfile:checksums.txt\tversion:0.0.1");
    expectFailure(verifyOf(fx), "checksums-signature", /expected version:1\.2\.3, found version:0\.0\.1/);
  });
});

test("verify-minisig-wrong-file", () => {
  withRelease({}, (fx) => {
    rewriteMinisig(fx, "trusted comment: timestamp:1\tfile:other.txt\tversion:1.2.3");
    expectFailure(verifyOf(fx), "checksums-signature", "file:checksums.txt");
  });
});

// A valid signature (by the committed test key) over the asset's current bytes, for `version`.
function signedBy(fx, asset, version, key = TEST_KEY) {
  return fakeSig(readFileSync(join(fx.dist, asset)), version, sigFileField(asset), key);
}

function replaceUpdaterSig(fx, platform, asset, signature) {
  writeFileSync(join(fx.dist, `${asset}.sig`), signature);
  writeChecksums(fx.dist);
  rewriteLatest(fx, (m) => {
    m.platforms[platform].signature = signature.trim();
  });
}

test("verify-updater-signature-without-version", () => {
  withRelease({}, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-arm64.app.tar.gz`;
    replaceUpdaterSig(fx, "darwin-aarch64", asset, signedBy(fx, asset, null));
    const failures = verifyOf(fx);
    expectFailure(failures, "updater-signatures", asset);
    assert.deepEqual(failures.map((f) => f.check), ["updater-signatures"]);
  });
});

test("verify-updater-signature-wrong-version", () => {
  withRelease({ version: "0.20.0" }, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-x86_64.app.tar.gz`;
    replaceUpdaterSig(fx, "darwin-x86_64", asset, signedBy(fx, asset, "0.19.0"));
    expectFailure(verifyOf(fx), "updater-signatures", /expected 0\.20\.0, found 0\.19\.0/);
  });
});

function rawSig(lines) {
  return Buffer.from(lines.join("\n")).toString("base64") + "\n";
}

// the updater plugin reads exactly line 3, `trusted comment: `, tab-split, untrimmed
test("verify-updater-signature-version-field-the-plugin-would-not-read", () => {
  withRelease({}, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-arm64.app.tar.gz`;
    const sig = rawSig(["untrusted comment: x", "RWQAAAA", `trusted comment: timestamp:1\tfile:${asset}\t version:${fx.desktopVersion}`, "GLOBAL", ""]);
    replaceUpdaterSig(fx, "darwin-aarch64", asset, sig);
    expectFailure(verifyOf(fx), "updater-signatures", asset);
  });
});

test("verify-updater-signature-reads-only-the-third-line", () => {
  withRelease({}, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-x86_64.app.tar.gz`;
    // a trusted-comment-looking line BEFORE the real third line (versionless)
    const sig = rawSig(["untrusted comment: x", `trusted comment: timestamp:1\tversion:${fx.desktopVersion}`, `trusted comment: timestamp:1\tfile:${asset}`, "GLOBAL", ""]);
    replaceUpdaterSig(fx, "darwin-x86_64", asset, sig);
    expectFailure(verifyOf(fx), "updater-signatures", asset);
  });
});

test("verify-updater-signature-v-prefix-accepted", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    replaceUpdaterSig(fx, "windows-x86_64", asset, signedBy(fx, asset, `v${fx.desktopVersion}`));
    assert.deepEqual(verifyOf(fx).filter((f) => f.check === "updater-signatures"), []);
  });
});

test("verify-updater-signature-undecodable", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    replaceUpdaterSig(fx, "windows-x86_64", asset, "!!!\n");
    expectFailure(verifyOf(fx), "updater-signatures", /base64/);
  });
});

// verify-updater-signature-for-another-artifact: the other macOS architecture's .sig has the same
// version and the same trusted-comment file name, so only the cryptographic check can see that it
// is not this tarball's signature (the client would only fail at install time otherwise).
test("verify-updater-signature-for-another-artifact", () => {
  withRelease({}, (fx) => {
    const arm = `smind-desktop-${fx.desktopVersion}-macos-arm64.app.tar.gz`;
    const x64 = `smind-desktop-${fx.desktopVersion}-macos-x86_64.app.tar.gz`;
    replaceUpdaterSig(fx, "darwin-aarch64", arm, readFileSync(join(fx.dist, `${x64}.sig`), "utf8"));
    const failures = verifyOf(fx);
    expectFailure(failures, "updater-signatures", /does not verify for .*arm64.*signature does not match the artifact/);
    assert.deepEqual(failures.map((f) => f.check), ["updater-signatures"]);
  });
});

test("verify-updater-signature-by-another-key", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    replaceUpdaterSig(fx, "windows-x86_64", asset, signedBy(fx, asset, fx.desktopVersion, OTHER_KEY));
    expectFailure(verifyOf(fx), "updater-signatures", /different key/);
  });
});

test("verify-updater-signature-altered-trusted-comment", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    // a valid signature, then its signed version rewritten to the right one: the global
    // signature no longer covers the comment
    const old = signedBy(fx, asset, "0.0.1");
    const text = Buffer.from(old.trim(), "base64").toString("utf8").replace("version:0.0.1", `version:${fx.desktopVersion}`);
    replaceUpdaterSig(fx, "windows-x86_64", asset, Buffer.from(text).toString("base64") + "\n");
    expectFailure(verifyOf(fx), "updater-signatures", /not covered by the signature/);
  });
});

// the plugin reads lines with Rust str::lines(), which strips \r: a CRLF .sig is a good signature
test("verify-updater-signature-crlf-is-accepted", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    const lf = Buffer.from(signedBy(fx, asset, fx.desktopVersion).trim(), "base64").toString("utf8");
    replaceUpdaterSig(fx, "windows-x86_64", asset, Buffer.from(lf.replaceAll("\n", "\r\n")).toString("base64") + "\n");
    assert.deepEqual(verifyOf(fx).filter((f) => f.check === "updater-signatures"), []);
  });
});

// trim_start_matches('v') strips EVERY leading v
test("verify-updater-signature-vv-prefix-accepted", () => {
  withRelease({}, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-arm64.app.tar.gz`;
    replaceUpdaterSig(fx, "darwin-aarch64", asset, signedBy(fx, asset, `vv${fx.desktopVersion}`));
    assert.deepEqual(verifyOf(fx).filter((f) => f.check === "updater-signatures"), []);
  });
});

test("verify-updater-signature-needs-the-committed-public-key", () => {
  withRelease({}, (fx) => {
    writeFileSync(join(fx.src, "desktop/src-tauri/tauri.conf.json"), JSON.stringify({ version: fx.desktopVersion }) + "\n");
    expectFailure(verifyOf(fx), "updater-signatures", /no plugins\.updater\.pubkey/);
  });
});

// Real data, v0.20.0 (public release assets, committed under scripts/testdata/real-v0.20.0/): the
// verifier's minisign verification agrees with a real `tauri signer sign` signature, and the real
// updater signatures carry what the scripts expect. The artifacts are too large to commit: the
// checksums.txt and its signature stand in for the cryptographic proof; the three real .sig files
// prove the trusted-comment shape (macOS tarballs both say file:sMind.app.tar.gz).
const real = (n) => join(scriptDir, "testdata/real-v0.20.0", n);

test("verify-real-checksums-signature-verifies-and-tampering-does-not", () => {
  const pub = readFileSync(real("updater-pubkey.txt"), "utf8").trim();
  const data = readFileSync(real("checksums.txt"));
  const sig = readFileSync(real("checksums.txt.minisig"), "utf8");
  assert.equal(verifyMinisign(data, sig, pub, { plain: true }), null);
  assert.match(verifyMinisign(Buffer.concat([data, Buffer.from("x")]), sig, pub, { plain: true }), /does not match the artifact/);
  assert.match(verifyMinisign(data, sig.replace("version:0.20.0", "version:0.19.0"), pub, { plain: true }), /not covered by the signature/);
});

test("verify-real-updater-signatures-parse", () => {
  for (const n of ["sMind_0.20.0_x64-setup.exe.sig", "smind-desktop-0.20.0-macos-arm64.app.tar.gz.sig", "smind-desktop-0.20.0-macos-x86_64.app.tar.gz.sig"]) {
    const sig = readFileSync(real(n), "utf8");
    assert.equal(signedVersionOf(sig), "0.20.0", n);
    // and the same signature with CRLF line endings, as the plugin would read it
    const crlf = Buffer.from(Buffer.from(sig.trim(), "base64").toString("utf8").replaceAll("\n", "\r\n")).toString("base64");
    assert.equal(signedVersionOf(crlf), "0.20.0", `${n} (CRLF)`);
  }
  // the two macOS signatures name the same file: why the file field cannot pin the architecture
  const f = (n) => Buffer.from(readFileSync(real(n), "utf8").trim(), "base64").toString("utf8").split("\n")[2].split("\t")[1];
  assert.equal(f("smind-desktop-0.20.0-macos-arm64.app.tar.gz.sig"), f("smind-desktop-0.20.0-macos-x86_64.app.tar.gz.sig"));
});

function rewriteLatest(fx, mutate) {
  const path = join(fx.dist, "latest.json");
  const manifest = JSON.parse(readFileSync(path, "utf8"));
  mutate(manifest);
  writeFileSync(path, JSON.stringify(manifest, null, 2) + "\n");
}

test("verify-latest-wrong-version", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      m.version = "0.0.1";
    });
    expectFailure(verifyOf(fx), "latest-json", /expected 1\.2\.3, found 0\.0\.1/);
  });
});

test("verify-latest-missing-platform", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      delete m.platforms["darwin-x86_64"];
    });
    expectFailure(verifyOf(fx), "latest-json", "missing platform darwin-x86_64");
  });
});

test("verify-latest-wrong-url", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      m.platforms["windows-x86_64"].url = "https://example.com/nope";
    });
    expectFailure(verifyOf(fx), "latest-json", /windows-x86_64 url: expected https:\/\/github\.com/);
  });
});

test("verify-latest-asset-not-in-dist", () => {
  withRelease({}, (fx) => {
    rmSync(join(fx.dist, "smind-desktop-1.2.3-macos-arm64.app.tar.gz"));
    expectFailure(verifyOf(fx), "latest-json", "asset smind-desktop-1.2.3-macos-arm64.app.tar.gz is not present in dist");
  });
});

test("verify-latest-signature-mismatch", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      m.platforms["darwin-aarch64"].signature = "not-the-sig-file-content";
    });
    expectFailure(verifyOf(fx), "latest-json", /signature: expected the content of/);
  });
});

test("verify-latest-empty-signature", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      m.platforms["darwin-x86_64"].signature = "";
    });
    expectFailure(verifyOf(fx), "latest-json", "signature is empty");
  });
});

test("verify-latest-bad-pub-date", () => {
  withRelease({}, (fx) => {
    rewriteLatest(fx, (m) => {
      m.pub_date = "not-a-timestamp";
    });
    expectFailure(verifyOf(fx), "latest-json", "pub_date");
  });
});

test("verify-skip-latest-ignores-bad-manifest", () => {
  withRelease({}, (fx) => {
    writeFileSync(join(fx.dist, "latest.json"), "{ this is not json\n");
    assert.deepEqual(verifyOf(fx, { skipLatest: true }), []);
  });
});

test("verify-sidecar-version-mismatch-daemon", () => {
  withRelease({}, (fx) => {
    const name = `smind_${fx.version}_windows_amd64.zip`;
    buildDaemonZip(join(fx.dist, name), "0.0.1");
    expectFailure(verifyOf(fx), "sidecar-versions", `${name} smind.exe: embedded daemon version: expected 1.2.3, found 0.0.1`);
  });
});

test("verify-sidecar-version-mismatch-app", () => {
  withRelease({}, (fx) => {
    buildAppTar(join(fx.dist, "smind-desktop-1.2.3-macos-arm64.app.tar.gz"), {
      appVersion: fx.desktopVersion,
      daemonVersion: "0.0.1",
    });
    expectFailure(
      verifyOf(fx),
      "sidecar-versions",
      "smind-desktop-1.2.3-macos-arm64.app.tar.gz sMind.app/Contents/MacOS/smind: embedded daemon version"
    );
  });
});

test("verify-plist-version-mismatch", () => {
  withRelease({}, (fx) => {
    buildAppTar(join(fx.dist, "smind-desktop-1.2.3-macos-x86_64.app.tar.gz"), {
      appVersion: "0.0.1",
      daemonVersion: fx.version,
    });
    expectFailure(verifyOf(fx), "sidecar-versions", "Info.plist CFBundleShortVersionString: expected 1.2.3, found 0.0.1");
  });
});

test("verify-sidecar-prefix-version-not-accepted", () => {
  withRelease({ version: "0.20.0" }, (fx) => {
    buildDaemonTar(join(fx.dist, "smind_0.20.0_linux_amd64.tar.gz"), "0.20.01");
    expectFailure(verifyOf(fx), "sidecar-versions", "embedded daemon version: expected 0.20.0, found 0.20.01");
  });
});

test("verify-reports-every-failure", () => {
  withRelease({}, (fx) => {
    setSourceVersion(fx, "desktop/package.json", "0.0.1");
    rmSync(join(fx.dist, "checksums.txt.minisig"));
    const failures = verifyOf(fx);
    expectFailure(failures, "source-versions", "desktop/package.json");
    expectFailure(failures, "checksums-signature", "checksums.txt.minisig");
  });
});

test("verify-dry-run-dev-version-split", () => {
  withRelease({ version: "0.20.0-dev+abc", desktopVersion: "0.20.0" }, (fx) => {
    assert.deepEqual(verifyOf(fx), []);
  });
});

test("verify-cli-exit-codes", () => {
  withRelease({}, (fx) => {
    const args = (tag) => [
      cli,
      "--dir", fx.dist,
      "--version", fx.version,
      "--tag", tag,
      "--repo", fx.repo,
      "--source-root", fx.src,
    ];
    const good = spawnSync(process.execPath, args(fx.tag));
    assert.equal(good.status, 0, good.stderr.toString());
    assert.match(good.stdout.toString(), /^ok source-versions$/m);
    assert.match(good.stdout.toString(), /^ok latest-json$/m);
    assert.match(good.stdout.toString(), /^ok sidecar-versions$/m);

    const bad = spawnSync(process.execPath, args("v0.0.1"));
    assert.equal(bad.status, 1);
    assert.match(`${bad.stdout}${bad.stderr}`, /^tag: /m);

    const usage = spawnSync(process.execPath, [cli]);
    assert.equal(usage.status, 2);
    assert.match(`${usage.stdout}${usage.stderr}`, /--dir/);
  });
});
