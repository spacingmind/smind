import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { daemonArchiveNames, desktopAssetNames, verify } from "./verify-release-artifacts.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cli = join(scriptDir, "verify-release-artifacts.mjs");
const REPO = "spacingmind/smind";

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
  writeFileSync(join(src, "desktop/src-tauri/tauri.conf.json"), JSON.stringify({ version: desktopVersion }, null, 2) + "\n");
  for (const rel of ["desktop/src-tauri/Cargo.toml", "desktop/daemon-client/Cargo.toml"]) {
    writeFileSync(join(src, rel), `[package]\nname = "x"\nversion = "${desktopVersion}" # x-release-please-version\n`);
  }

  for (const name of daemonArchiveNames(version)) {
    if (name.endsWith(".zip")) buildDaemonZip(join(dist, name), version);
    else buildDaemonTar(join(dist, name), version);
  }
  for (const name of desktopAssetNames(desktopVersion)) {
    if (name.endsWith(".sig")) writeFileSync(join(dist, name), fakeSig(desktopVersion, name.replace(/\.sig$/, "")));
    else if (name.endsWith(".app.tar.gz")) {
      buildAppTar(join(dist, name), { appVersion: desktopVersion, daemonVersion: version });
    } else writeFileSync(join(dist, name), `dummy ${name}\n`);
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
          "darwin-aarch64": { signature: fakeSig(desktopVersion, arm64).trim(), url: asset(arm64) },
          "darwin-x86_64": { signature: fakeSig(desktopVersion, x64).trim(), url: asset(x64) },
          "windows-x86_64": { signature: fakeSig(desktopVersion, exe).trim(), url: asset(exe) },
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
    replaceUpdaterSig(fx, "darwin-aarch64", asset, fakeSig(null, asset));
    const failures = verifyOf(fx);
    expectFailure(failures, "updater-signatures", asset);
    assert.deepEqual(failures.map((f) => f.check), ["updater-signatures"]);
  });
});

test("verify-updater-signature-wrong-version", () => {
  withRelease({ version: "0.20.0" }, (fx) => {
    const asset = `smind-desktop-${fx.desktopVersion}-macos-x86_64.app.tar.gz`;
    replaceUpdaterSig(fx, "darwin-x86_64", asset, fakeSig("0.19.0", asset));
    expectFailure(verifyOf(fx), "updater-signatures", /expected 0\.20\.0, found 0\.19\.0/);
  });
});

test("verify-updater-signature-undecodable", () => {
  withRelease({}, (fx) => {
    const asset = `sMind_${fx.desktopVersion}_x64-setup.exe`;
    replaceUpdaterSig(fx, "windows-x86_64", asset, "!!!\n");
    expectFailure(verifyOf(fx), "updater-signatures", /base64/);
  });
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
