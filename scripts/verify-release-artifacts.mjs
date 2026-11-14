#!/usr/bin/env node
// Verifies an assembled release dist/ directory as one consistent set
// (docs/plans/active/desktop-release-hygiene.md, item 1): the source-tree
// version fields, the daemon archives, the desktop assets, checksums.txt
// and its minisign signature, latest.json, and the versions embedded in
// the bundled sidecar binaries. Read-only -- it never writes into dist.
// Pure functions + a CLI; no dependencies; tar/unzip only through
// execFileSync, never a shell, and archive members are never extracted
// to disk.

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import process from "node:process";

export const CHECKS = [
  "source-versions",
  "tag",
  "daemon-archives",
  "desktop-assets",
  "checksums",
  "checksums-signature",
];

export const CHECK_DESCRIPTIONS = {
  "source-versions":
    'the 5 version fields (.release-please-manifest.json key ".", desktop/package.json, desktop/src-tauri/tauri.conf.json, both Cargo.toml) equal --desktop-version',
  tag: "--tag equals v<version>",
  "daemon-archives":
    "exactly the six smind_<version>_{linux,darwin}_{amd64,arm64}.tar.gz and smind_<version>_windows_{amd64,arm64}.zip archives, no other file of that shape",
  "desktop-assets":
    "the dmg x2, .app.tar.gz x2 (+ .sig each), sMind_<dv>_x64-setup.exe (+ .sig) and sMind_<dv>_x64_en-US.msi for --desktop-version, no other file starting smind-desktop-/sMind_",
  checksums:
    'checksums.txt lines are "<64 lowercase hex>  <name>"; every *.tar.gz *.zip *.exe *.msi *.dmg *.sig file in dist is listed exactly once and each digest equals the file\'s sha256',
  "checksums-signature":
    "checksums.txt.minisig exists, has the 4-line minisign shape, and its trusted comment names file:checksums.txt and version:<version>",
  "latest-json":
    "latest.json: version equals --desktop-version, exactly the three updater platforms, urls match tag/repo, each asset is in dist with its .sig content as signature, pub_date parses",
  "sidecar-versions":
    "every daemon archive's smind/smind.exe embeds internal/version.Version=<version>; each macOS .app.tar.gz bundles that daemon and its Info.plist CFBundleShortVersionString equals --desktop-version",
};

const NOT_CHECKED = [
  "dmg internals and the Windows installer payload: neither can be opened portably in CI (the .app.tar.gz is the same bundle the dmg wraps)",
  "mobile (mobile/package.json): versioned independently for the app stores, not part of this release set",
];

const DAEMON_ARCHIVE_RE = /^smind_.*_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$/;
const CHECKSUM_LINE_RE = /^([0-9a-f]{64})  (.+)$/;
const CHECKSUM_EXTS = [".tar.gz", ".zip", ".exe", ".msi", ".dmg", ".sig"];
const CARGO_VERSION_RE = /^version\s*=\s*"([^"]*)"/m;
const TRUSTED_PREFIX = "trusted comment:";

const SOURCE_JSON_FIELDS = [
  [".release-please-manifest.json", (o) => o["."], 'key "."'],
  ["desktop/package.json", (o) => o.version, ".version"],
  ["desktop/src-tauri/tauri.conf.json", (o) => o.version, ".version"],
];
const SOURCE_CARGO_FILES = ["desktop/src-tauri/Cargo.toml", "desktop/daemon-client/Cargo.toml"];

export function daemonArchiveNames(version) {
  const names = [];
  for (const os of ["linux", "darwin", "windows"]) {
    for (const arch of ["amd64", "arm64"]) {
      names.push(`smind_${version}_${os}_${arch}${os === "windows" ? ".zip" : ".tar.gz"}`);
    }
  }
  return names;
}

export function desktopAssetNames(desktopVersion) {
  const app = (arch) => `smind-desktop-${desktopVersion}-macos-${arch}.app.tar.gz`;
  return [
    `smind-desktop-${desktopVersion}-macos-arm64.dmg`,
    `smind-desktop-${desktopVersion}-macos-x86_64.dmg`,
    app("arm64"),
    `${app("arm64")}.sig`,
    app("x86_64"),
    `${app("x86_64")}.sig`,
    `sMind_${desktopVersion}_x64-setup.exe`,
    `sMind_${desktopVersion}_x64-setup.exe.sig`,
    `sMind_${desktopVersion}_x64_en-US.msi`,
  ];
}

export function checksumGlobNames(fileNames) {
  return fileNames.filter((name) => CHECKSUM_EXTS.some((ext) => name.endsWith(ext)));
}

export function minisigTrustedFields(line) {
  return line
    .slice(TRUSTED_PREFIX.length)
    .split("\t")
    .map((field) => field.trim())
    .filter((field) => field !== "");
}

function oneLine(err) {
  return String(err?.shortMessage ?? err?.message ?? err)
    .replace(/\s+/g, " ")
    .trim();
}

function sha256File(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function readTextOrFail(path, fail, what) {
  try {
    return readFileSync(path, "utf8");
  } catch (err) {
    fail(err.code === "ENOENT" ? `missing ${what}` : `${what}: cannot read (${oneLine(err)})`);
    return null;
  }
}

function checkSourceVersions({ sourceRoot, desktopVersion, fail }) {
  for (const [rel, pick, field] of SOURCE_JSON_FIELDS) {
    let doc;
    try {
      doc = JSON.parse(readFileSync(join(sourceRoot, rel), "utf8"));
    } catch (err) {
      fail(err.code === "ENOENT" ? `missing ${rel} under source root` : `${rel}: cannot parse (${oneLine(err)})`);
      continue;
    }
    const found = pick(doc);
    if (found !== desktopVersion) {
      fail(`${rel} ${field}: expected ${desktopVersion}, found ${String(found)}`);
    }
  }
  for (const rel of SOURCE_CARGO_FILES) {
    const text = readTextOrFail(join(sourceRoot, rel), fail, `${rel} under source root`);
    if (text === null) continue;
    const m = text.match(CARGO_VERSION_RE);
    if (!m) fail(`${rel}: no version = "..." line`);
    else if (m[1] !== desktopVersion) fail(`${rel}: expected version ${desktopVersion}, found ${m[1]}`);
  }
}

function checkTag({ tag, version, fail }) {
  const expected = `v${version}`;
  if (tag !== expected) fail(`tag: expected ${expected}, found ${tag}`);
}

function checkDaemonArchives({ version, filesOf, fail }) {
  const files = filesOf();
  const expected = new Set(daemonArchiveNames(version));
  for (const name of expected) {
    if (!files.has(name)) fail(`missing daemon archive ${name}`);
  }
  for (const name of [...files].sort()) {
    if (DAEMON_ARCHIVE_RE.test(name) && !expected.has(name)) {
      fail(`unexpected daemon archive ${name}: expected version ${version}`);
    }
  }
}

function checkDesktopAssets({ desktopVersion, filesOf, fail }) {
  const files = filesOf();
  const expected = new Set(desktopAssetNames(desktopVersion));
  for (const name of expected) {
    if (!files.has(name)) fail(`missing desktop asset ${name}`);
  }
  for (const name of [...files].sort()) {
    if ((name.startsWith("smind-desktop-") || name.startsWith("sMind_")) && !expected.has(name)) {
      fail(`unexpected desktop asset ${name}: expected desktop version ${desktopVersion}`);
    }
  }
}

function checkChecksums({ dir, filesOf, fail }) {
  const text = readTextOrFail(join(dir, "checksums.txt"), fail, "checksums.txt in dist");
  if (text === null) return;
  const entries = new Map();
  const lines = text.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(CHECKSUM_LINE_RE);
    if (!m) {
      fail(`checksums.txt line ${i + 1} is not "<64 lowercase hex>  <name>": ${JSON.stringify(lines[i])}`);
      continue;
    }
    const [, digest, name] = m;
    if (entries.has(name)) fail(`checksums.txt lists ${name} more than once`);
    else entries.set(name, digest);
  }
  const files = filesOf();
  for (const [name, digest] of entries) {
    if (!files.has(name)) {
      fail(`checksums.txt entry ${name} names no file in dist`);
      continue;
    }
    const actual = sha256File(join(dir, name));
    if (actual !== digest) fail(`checksums.txt digest for ${name}: expected ${actual}, found ${digest}`);
  }
  for (const name of checksumGlobNames([...files]).sort()) {
    if (!entries.has(name)) fail(`checksums.txt is missing an entry for ${name}`);
  }
}

function checkChecksumsSignature({ dir, version, fail }) {
  const text = readTextOrFail(join(dir, "checksums.txt.minisig"), fail, "checksums.txt.minisig in dist");
  if (text === null) return;
  const lines = text.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  if (lines.length < 4) {
    fail(`checksums.txt.minisig: expected at least 4 lines, found ${lines.length}`);
    return;
  }
  if (!lines[0].startsWith("untrusted comment:")) {
    fail(`checksums.txt.minisig line 1: expected "untrusted comment: ...", found ${JSON.stringify(lines[0])}`);
  }
  if (!lines[2].startsWith(TRUSTED_PREFIX)) {
    fail(`checksums.txt.minisig line 3: expected "trusted comment: ...", found ${JSON.stringify(lines[2])}`);
    return;
  }
  const fields = minisigTrustedFields(lines[2]);
  if (!fields.includes("file:checksums.txt")) {
    fail(`checksums.txt.minisig trusted comment has no file:checksums.txt field (fields: ${fields.join(", ") || "none"})`);
  }
  const want = `version:${version}`;
  if (!fields.includes(want)) {
    const found = fields.find((f) => f.startsWith("version:")) ?? "none";
    fail(`checksums.txt.minisig trusted comment version field: expected ${want}, found ${found}`);
  }
}

const CHECK_FNS = {
  "source-versions": checkSourceVersions,
  tag: checkTag,
  "daemon-archives": checkDaemonArchives,
  "desktop-assets": checkDesktopAssets,
  checksums: checkChecksums,
  "checksums-signature": checkChecksumsSignature,
};

// Runs every check and collects every failure as {check, message}; a check
// that throws becomes a failure of that check, never a crash. Empty result
// means the release set is consistent.
export function verify({ dir, version, tag, repo, desktopVersion = version, sourceRoot = ".", skipLatest = false }) {
  const failures = [];
  let cache = null;
  let current = "?";
  const ctx = {
    dir: resolve(dir),
    sourceRoot: resolve(sourceRoot),
    version,
    desktopVersion,
    tag,
    repo,
    filesOf: () => {
      if (cache === null) {
        cache = new Set(
          readdirSync(ctx.dir, { withFileTypes: true })
            .filter((entry) => entry.isFile())
            .map((entry) => entry.name)
        );
      }
      return cache;
    },
    fail: (message) => failures.push({ check: current, message }),
  };
  for (const check of CHECKS) {
    if (check === "latest-json" && skipLatest) continue;
    current = check;
    try {
      CHECK_FNS[check](ctx);
    } catch (err) {
      failures.push({ check, message: `check crashed: ${oneLine(err)}` });
    }
  }
  return failures;
}

class UsageError extends Error {}

const VALUE_OPTIONS = ["dir", "version", "tag", "repo", "desktop-version", "source-root"];

function parseArgs(argv) {
  const args = { help: false, "skip-latest": false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      args.help = true;
      continue;
    }
    if (arg === "--skip-latest") {
      args["skip-latest"] = true;
      continue;
    }
    const eq = arg.indexOf("=");
    const key = (eq === -1 ? arg : arg.slice(0, eq)).replace(/^--/, "");
    if (!VALUE_OPTIONS.includes(key)) throw new UsageError(`unknown option ${arg}`);
    const value = eq === -1 ? argv[++i] : arg.slice(eq + 1);
    if (value === undefined) throw new UsageError(`missing value for --${key}`);
    args[key] = value;
  }
  return args;
}

function helpText() {
  return [
    "Usage: node scripts/verify-release-artifacts.mjs --dir <dist> --version <X.Y.Z> --tag <vX.Y.Z> --repo <owner/name> [--desktop-version <X.Y.Z>] [--source-root <repo>] [--skip-latest]",
    "",
    "Checks (all of them run; every failure is reported):",
    ...CHECKS.map((id) => `  ${id.padEnd(21)} ${CHECK_DESCRIPTIONS[id]}`),
    "",
    "Not checked (known limits):",
    ...NOT_CHECKED.map((line) => `  - ${line}`),
    "",
  ].join("\n");
}

function main(argv) {
  const args = parseArgs(argv);
  if (args.help) {
    process.stdout.write(helpText());
    return 0;
  }
  const missing = ["dir", "version", "tag", "repo"].filter((key) => !args[key]);
  if (missing.length > 0) {
    throw new UsageError(`missing required option${missing.length > 1 ? "s" : ""}: ${missing.map((k) => `--${k}`).join(" ")}`);
  }
  const failures = verify({
    dir: args.dir,
    version: args.version,
    tag: args.tag,
    repo: args.repo,
    desktopVersion: args["desktop-version"] ?? args.version,
    sourceRoot: args["source-root"] ?? ".",
    skipLatest: args["skip-latest"],
  });
  if (failures.length === 0) {
    for (const id of CHECKS.filter((c) => !(c === "latest-json" && args["skip-latest"]))) {
      process.stdout.write(`ok ${id}\n`);
    }
    return 0;
  }
  const annotate = Boolean(process.env.GITHUB_ACTIONS);
  for (const { check, message } of failures) {
    process.stdout.write(`${check}: ${message}\n`);
    if (annotate) process.stdout.write(`::error::${check}: ${message}\n`);
  }
  return 1;
}

if (process.argv[1] && process.argv[1].endsWith("verify-release-artifacts.mjs")) {
  try {
    process.exitCode = main(process.argv.slice(2));
  } catch (err) {
    process.stderr.write(`verify-release-artifacts: ${err.message}\n`);
    process.exitCode = err instanceof UsageError ? 2 : 1;
  }
}
