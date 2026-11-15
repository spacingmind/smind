#!/usr/bin/env node
// Verifies an assembled release dist/ directory as one consistent set
// (docs/plans/active/desktop-release-hygiene.md, item 1): source version
// fields, daemon archives, desktop assets, checksums.txt + minisign
// signature, latest.json, and the versions embedded in the sidecar
// binaries. Read-only; pure functions + CLI; no dependencies; tar/unzip
// only through execFileSync, never a shell, never extracted to disk.

import { execFileSync } from "node:child_process";
import { createHash, createPublicKey, verify as cryptoVerify } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import process from "node:process";

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
  "updater-signatures":
    "the three updater .sig files (two macOS app tarballs, the Windows installer) each carry version:<--desktop-version> in the signed trusted comment (the updater's requireSignedVersion) and verify, with the public key committed in tauri.conf.json, over the artifact they sit next to (a .sig copied from another asset fails)",
  "latest-json":
    "latest.json: version equals --desktop-version, exactly the three updater platforms, urls match tag/repo, each asset is in dist with its .sig content as signature, pub_date parses",
  "sidecar-versions":
    "every daemon archive's smind/smind.exe embeds internal/version.Version=<version>; each macOS .app.tar.gz bundles that daemon and its Info.plist CFBundleShortVersionString equals --desktop-version",
};
export const CHECKS = Object.keys(CHECK_DESCRIPTIONS);

const DAEMON_ARCHIVE_RE = /^smind_.*_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$/;
const APP_TARBALL_RE = /^smind-desktop-.*\.app\.tar\.gz$/;
const CHECKSUM_LINE_RE = /^([0-9a-f]{64})  (.+)$/;
const CHECKSUM_EXTS = [".tar.gz", ".zip", ".exe", ".msi", ".dmg", ".sig"];
const CARGO_VERSION_RE = /^version\s*=\s*"([^"]*)"/m;
const TRUSTED_PREFIX = "trusted comment:";
const VERSION_MARKER_RE = /internal\/version\.Version=([^ "\n\0]+)/g;
const PLIST_SHORT_VERSION_RE = /<key>CFBundleShortVersionString<\/key>\s*<string>([^<]*)<\/string>/;
const MAX_BUFFER = 256 * 1024 * 1024;

const SOURCE_JSON_FIELDS = [
  [".release-please-manifest.json", (o) => o["."], 'key "."'],
  ["desktop/package.json", (o) => o.version, ".version"],
  ["desktop/src-tauri/tauri.conf.json", (o) => o.version, ".version"],
];
const SOURCE_CARGO_FILES = ["desktop/src-tauri/Cargo.toml", "desktop/daemon-client/Cargo.toml"];

export function daemonArchiveNames(version) {
  return ["linux", "darwin", "windows"].flatMap((os) =>
    ["amd64", "arm64"].map((arch) => `smind_${version}_${os}_${arch}${os === "windows" ? ".zip" : ".tar.gz"}`)
  );
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
  return line.slice(TRUSTED_PREFIX.length).split("\t").map((f) => f.trim()).filter((f) => f !== "");
}

// The updater asset each platform key of latest.json must point at.
export function updaterAssets(desktopVersion) {
  return {
    "darwin-aarch64": `smind-desktop-${desktopVersion}-macos-arm64.app.tar.gz`,
    "darwin-x86_64": `smind-desktop-${desktopVersion}-macos-x86_64.app.tar.gz`,
    "windows-x86_64": `sMind_${desktopVersion}_x64-setup.exe`,
  };
}

// Every -ldflags build-setting version stamp in a Go binary, latin1-decoded.
export function versionMarkers(buffer) {
  return [...buffer.toString("latin1").matchAll(VERSION_MARKER_RE)].map((m) => m[1]);
}

export function plistShortVersion(plistXml) {
  const m = plistXml.match(PLIST_SHORT_VERSION_RE);
  return m ? m[1] : null;
}

function oneLine(err) {
  return String(err?.shortMessage ?? err?.message ?? err).replace(/\s+/g, " ").trim();
}

const trimmedLines = (text) => text.split("\n").filter((line, i, all) => !(line === "" && i === all.length - 1));

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

function readJsonOrFail(path, fail, what) {
  const text = readTextOrFail(path, fail, what);
  if (text === null) return null;
  try {
    return JSON.parse(text);
  } catch (err) {
    fail(`${what}: cannot parse (${oneLine(err)})`);
    return null;
  }
}

function checkSourceVersions({ sourceRoot, desktopVersion, fail }) {
  for (const [rel, pick, field] of SOURCE_JSON_FIELDS) {
    const doc = readJsonOrFail(join(sourceRoot, rel), fail, `${rel} under source root`);
    if (doc === null) continue;
    const found = pick(doc);
    if (found !== desktopVersion) fail(`${rel} ${field}: expected ${desktopVersion}, found ${String(found)}`);
  }
  for (const rel of SOURCE_CARGO_FILES) {
    const text = readTextOrFail(join(sourceRoot, rel), fail, `${rel} under source root`);
    if (text === null) continue;
    const m = text.match(CARGO_VERSION_RE);
    if (!m) fail(`${rel}: no version = "..." line`);
    else if (m[1] !== desktopVersion) fail(`${rel}: expected version ${desktopVersion}, found ${m[1]}`);
  }
}

function checkDaemonArchives({ version, filesOf, fail }) {
  const files = filesOf();
  const expected = new Set(daemonArchiveNames(version));
  for (const name of expected) if (!files.has(name)) fail(`missing daemon archive ${name}`);
  for (const name of [...files].sort()) {
    if (DAEMON_ARCHIVE_RE.test(name) && !expected.has(name)) {
      fail(`unexpected daemon archive ${name}: expected version ${version}`);
    }
  }
}

function checkDesktopAssets({ desktopVersion, filesOf, fail }) {
  const files = filesOf();
  const expected = new Set(desktopAssetNames(desktopVersion));
  for (const name of expected) if (!files.has(name)) fail(`missing desktop asset ${name}`);
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
  trimmedLines(text).forEach((line, i) => {
    const m = line.match(CHECKSUM_LINE_RE);
    if (!m) {
      fail(`checksums.txt line ${i + 1} is not "<64 lowercase hex>  <name>": ${JSON.stringify(line)}`);
      return;
    }
    const [, digest, name] = m;
    if (entries.has(name)) fail(`checksums.txt lists ${name} more than once`);
    else entries.set(name, digest);
  });
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
  const lines = trimmedLines(text);
  if (lines.length < 4) return fail(`checksums.txt.minisig: expected at least 4 lines, found ${lines.length}`);
  if (!lines[0].startsWith("untrusted comment:")) {
    fail(`checksums.txt.minisig line 1: expected "untrusted comment: ...", found ${JSON.stringify(lines[0])}`);
  }
  if (!lines[2].startsWith(TRUSTED_PREFIX)) {
    return fail(`checksums.txt.minisig line 3: expected "trusted comment: ...", found ${JSON.stringify(lines[2])}`);
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

function checkLatestJson({ dir, filesOf, tag, repo, desktopVersion, fail }) {
  const manifest = readJsonOrFail(join(dir, "latest.json"), fail, "latest.json in dist");
  if (manifest === null) return;
  if (manifest?.version !== desktopVersion) fail(`latest.json version: expected ${desktopVersion}, found ${String(manifest?.version)}`);
  const platforms = manifest?.platforms ?? {};
  const assets = updaterAssets(desktopVersion);
  for (const platform of new Set([...Object.keys(assets), ...Object.keys(platforms)])) {
    if (!(platform in platforms)) fail(`latest.json: missing platform ${platform}`);
    if (!(platform in assets)) fail(`latest.json: unexpected platform ${platform}`);
  }
  const files = filesOf();
  for (const [platform, asset] of Object.entries(assets)) {
    const entry = platforms[platform];
    if (!entry) continue; // already reported as a missing platform
    const expectedUrl = `https://github.com/${repo}/releases/download/${tag}/${encodeURIComponent(asset)}`;
    if (entry.url !== expectedUrl) fail(`latest.json ${platform} url: expected ${expectedUrl}, found ${String(entry.url)}`);
    if (!files.has(asset)) {
      fail(`latest.json ${platform}: asset ${asset} is not present in dist`);
      continue;
    }
    if (typeof entry.signature !== "string" || entry.signature.trim() === "") {
      fail(`latest.json ${platform}: signature is empty`);
      continue;
    }
    const sig = readTextOrFail(join(dir, `${asset}.sig`), fail, `${asset}.sig in dist`);
    if (sig !== null && entry.signature !== sig.trim()) {
      fail(`latest.json ${platform} signature: expected the content of ${asset}.sig, found a different value`);
    }
  }
  if (typeof manifest?.pub_date !== "string" || Number.isNaN(Date.parse(manifest.pub_date))) {
    fail(`latest.json pub_date: not a valid timestamp: ${String(manifest?.pub_date)}`);
  }
}

// Extracts one fixed member name into memory. No shell, no disk; the member
// name is always a literal here -- never built from archive content.
function readMember(archive, member, zip) {
  const args = zip ? ["-p", archive, member] : ["-xzOf", archive, member];
  return execFileSync(zip ? "unzip" : "tar", args, { maxBuffer: MAX_BUFFER });
}

function checkEmbeddedVersion({ dir, version, fail }, archive, member, zip) {
  let buffer;
  try {
    buffer = readMember(join(dir, archive), member, zip);
  } catch (err) {
    return fail(`${archive}: cannot extract ${member}: ${oneLine(err)}`);
  }
  const markers = versionMarkers(buffer);
  if (markers.length === 0) fail(`${archive}: no internal/version.Version marker in ${member}`);
  for (const marker of new Set(markers)) {
    if (marker !== version) fail(`${archive} ${member}: embedded daemon version: expected ${version}, found ${marker}`);
  }
}

function checkSidecarVersions(ctx) {
  const { dir, filesOf, desktopVersion, fail } = ctx;
  for (const name of [...filesOf()].sort()) {
    if (!DAEMON_ARCHIVE_RE.test(name)) continue;
    const zip = name.endsWith(".zip");
    checkEmbeddedVersion(ctx, name, zip ? "smind.exe" : "smind", zip);
  }
  for (const name of [...filesOf()].sort()) {
    if (!APP_TARBALL_RE.test(name)) continue;
    let plist;
    try {
      plist = readMember(join(dir, name), "sMind.app/Contents/Info.plist", false).toString("utf8");
    } catch (err) {
      fail(`${name}: cannot extract sMind.app/Contents/Info.plist: ${oneLine(err)}`);
      continue;
    }
    const short = plistShortVersion(plist);
    if (short === null) fail(`${name}: no CFBundleShortVersionString string in sMind.app/Contents/Info.plist`);
    else if (short !== desktopVersion) {
      fail(`${name}: Info.plist CFBundleShortVersionString: expected ${desktopVersion}, found ${short}`);
    }
    checkEmbeddedVersion(ctx, name, "sMind.app/Contents/MacOS/smind", false);
  }
}

// A .sig file is the base64 of a four-line minisign signature text: the untrusted comment, the
// base64 signature (algorithm "ED" = prehashed, key id, 64-byte Ed25519 signature), the trusted
// comment and the base64 global signature over the signature plus the trusted comment. Lines are
// split like the updater plugin does (Rust `str::lines()`: `\n` or `\r\n`). Keep the version
// reading in sync with desktop/scripts/gen-latest-json.mjs.
export function sigLines(signature) {
  const b64 = String(signature).trim();
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(b64)) throw new Error("not a base64 minisign signature");
  return Buffer.from(b64, "base64").toString("utf8").split(/\r?\n/);
}

// The version a signature was made for: the `version:` field of its trusted comment, read exactly as
// the plugin reads it: the THIRD line, starting `trusted comment: `, split on tabs, no trimming.
export function signedVersionOf(signature) {
  const line = sigLines(signature)[2];
  if (line === undefined || !line.startsWith("trusted comment: ")) throw new Error("no trusted comment");
  const field = line
    .slice("trusted comment: ".length)
    .split("\t")
    .find((f) => f.startsWith("version:"));
  if (field === undefined) throw new Error("no signed version in the trusted comment");
  return field.slice("version:".length);
}

const ED25519_SPKI_PREFIX = Buffer.from("302a300506032b6570032100", "hex");

function ed25519Verify(publicKey32, message, signature64) {
  const key = createPublicKey({ key: Buffer.concat([ED25519_SPKI_PREFIX, publicKey32]), format: "der", type: "spki" });
  return cryptoVerify(null, message, key, signature64);
}

// Verifies a minisign signature over `data` with a public key in the form the Tauri config holds
// (base64 of the two-line public key file). Only the prehashed "ED" algorithm is accepted (what
// `tauri signer sign` writes): the Ed25519 signature is over the BLAKE2b-512 of the data, the global
// signature over the signature bytes followed by the trusted comment. Returns null when it verifies,
// else the reason. The same rule as internal/selfupdate/verify.go.
export function verifyMinisign(data, signature, tauriPubkey, { plain = false } = {}) {
  let lines;
  try {
    // a `.sig` file is base64 of the text; a `.minisig` (checksums.txt.minisig) is the text itself
    lines = plain ? String(signature).split(/\r?\n/) : sigLines(signature);
  } catch (err) {
    return oneLine(err);
  }
  const pubLines = Buffer.from(String(tauriPubkey), "base64").toString("utf8").split(/\r?\n/);
  const pub = Buffer.from(pubLines[1] ?? "", "base64");
  if (pub.length !== 42 || pub.subarray(0, 2).toString("latin1") !== "Ed") return "the public key is not a minisign Ed25519 key";
  const sig = Buffer.from(lines[1] ?? "", "base64");
  if (sig.length !== 74) return "the signature line is not 74 bytes";
  if (sig.subarray(0, 2).toString("latin1") !== "ED") return "only the prehashed minisign algorithm (ED) is accepted";
  if (!sig.subarray(2, 10).equals(pub.subarray(2, 10))) return "the signature is by a different key than the committed public key";
  const comment = lines[2] ?? "";
  if (!comment.startsWith("trusted comment: ")) return "no trusted comment";
  const global = Buffer.from(lines[3] ?? "", "base64");
  if (global.length !== 64) return "the global signature is not 64 bytes";
  const hash = createHash("blake2b512").update(data).digest();
  const key = pub.subarray(10);
  if (!ed25519Verify(key, hash, sig.subarray(10))) return "the signature does not match the artifact";
  const signed = Buffer.concat([sig.subarray(10), Buffer.from(comment.slice("trusted comment: ".length), "utf8")]);
  if (!ed25519Verify(key, signed, global)) return "the trusted comment is not covered by the signature";
  return null;
}

function checkUpdaterSignatures({ dir, sourceRoot, desktopVersion, fail }) {
  const conf = readJsonOrFail(join(sourceRoot, "desktop/src-tauri/tauri.conf.json"), fail, "desktop/src-tauri/tauri.conf.json under source root");
  const pubkey = conf?.plugins?.updater?.pubkey;
  if (conf !== null && typeof pubkey !== "string") fail("tauri.conf.json has no plugins.updater.pubkey");
  const files = readdirSync(dir);
  for (const asset of Object.values(updaterAssets(desktopVersion))) {
    const sig = readTextOrFail(join(dir, `${asset}.sig`), fail, `${asset}.sig in dist`);
    if (sig === null) continue;
    let signed;
    try {
      signed = signedVersionOf(sig);
    } catch (err) {
      fail(`${asset}.sig: ${oneLine(err)} (the updater's requireSignedVersion would refuse this release)`);
      continue;
    }
    // the plugin strips every leading `v` before comparing as semver
    if (signed.replace(/^v+/, "") !== desktopVersion) {
      fail(`${asset}.sig signed version: expected ${desktopVersion}, found ${signed}`);
    }
    // The signature must be for THIS artifact: a .sig copied from another asset (the other macOS
    // arch carries the same version and the same trusted-comment file name) is only refused by the
    // client at install time otherwise.
    if (typeof pubkey === "string" && files.includes(asset)) {
      const why = verifyMinisign(readFileSync(join(dir, asset)), sig, pubkey);
      if (why !== null) {
        fail(`${asset}.sig does not verify for ${asset} with the updater public key in tauri.conf.json: ${why}`);
      }
    }
  }
}

const CHECK_FNS = {
  "source-versions": checkSourceVersions,
  tag: ({ tag, version, fail }) => {
    if (tag !== `v${version}`) fail(`tag: expected v${version}, found ${tag}`);
  },
  "daemon-archives": checkDaemonArchives,
  "desktop-assets": checkDesktopAssets,
  checksums: checkChecksums,
  "checksums-signature": checkChecksumsSignature,
  "updater-signatures": checkUpdaterSignatures,
  "latest-json": checkLatestJson,
  "sidecar-versions": checkSidecarVersions,
};

// Runs every check and collects every failure as {check, message}; a check
// that throws becomes a failure of that check, never a crash. Empty result
// means the release set is consistent.
export function verify({ dir, version, tag, repo, desktopVersion = version, sourceRoot = ".", skipLatest = false }) {
  const failures = [];
  let cache = null;
  let current = "?";
  const listFiles = (d) =>
    new Set(readdirSync(d, { withFileTypes: true }).filter((e) => e.isFile()).map((e) => e.name));
  const ctx = {
    dir: resolve(dir),
    sourceRoot: resolve(sourceRoot),
    version,
    desktopVersion,
    tag,
    repo,
    filesOf: () => (cache ??= listFiles(ctx.dir)),
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
    if (arg === "--help" || arg === "-h") args.help = true;
    else if (arg === "--skip-latest") args["skip-latest"] = true;
    else {
      const eq = arg.indexOf("=");
      const key = (eq === -1 ? arg : arg.slice(0, eq)).replace(/^--/, "");
      if (!VALUE_OPTIONS.includes(key)) throw new UsageError(`unknown option ${arg}`);
      const value = eq === -1 ? argv[++i] : arg.slice(eq + 1);
      if (value === undefined) throw new UsageError(`missing value for --${key}`);
      args[key] = value;
    }
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
    "  - dmg internals and the Windows installer payload: neither can be opened portably in CI (the .app.tar.gz is the same bundle the dmg wraps)",
    "  - mobile (mobile/package.json): versioned independently for the app stores, not part of this release set",
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
  if (missing.length > 0) throw new UsageError(`missing required option${missing.length > 1 ? "s" : ""}: ${missing.map((k) => `--${k}`).join(" ")}`);
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
    const ok = CHECKS.filter((c) => !(c === "latest-json" && args["skip-latest"]));
    for (const id of ok) process.stdout.write(`ok ${id}\n`);
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
