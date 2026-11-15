#!/usr/bin/env node
// Generates the Tauri updater's static `latest.json` manifest from the
// release artifacts (see ADR-0023.3). Pure function + CLI; no dependencies.

import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import process from "node:process";

export const PLATFORMS = ["darwin-aarch64", "darwin-x86_64", "windows-x86_64"];

const SEMVER_STABLE = /^\d+\.\d+\.\d+$/;

export function normalizeVersion(version) {
  const v = String(version).replace(/^v/, "");
  if (!SEMVER_STABLE.test(v)) {
    throw new Error(
      `version "${version}" is not a stable semver (X.Y.Z); pre-release or build parts are rejected by the updater channel`
    );
  }
  return v;
}

// The version a minisign signature was made for: the `version:` field of its trusted comment (the
// Tauri CLI writes `timestamp:..\tfile:..\tversion:X.Y.Z`). A .sig file is the base64 of the
// four-line minisign signature text. The updater plugin checks the signature cryptographically at
// install time; this reads the signed comment so a release the updater would refuse
// (`requireSignedVersion`) is stopped before it is published.
export function signedVersionOf(signature) {
  const b64 = String(signature).trim();
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(b64)) {
    throw new Error("not a base64 minisign signature");
  }
  const lines = Buffer.from(b64, "base64").toString("utf8").split("\n");
  const line = lines.find((l) => l.startsWith("trusted comment:"));
  if (line === undefined) {
    throw new Error("the signature has no trusted comment");
  }
  const field = line
    .slice("trusted comment:".length)
    .split("\t")
    .map((f) => f.trim())
    .find((f) => f.startsWith("version:"));
  if (field === undefined) {
    throw new Error(
      "the signature carries no signed version (the updater's requireSignedVersion would refuse it): re-sign it with the Tauri CLI"
    );
  }
  return field.slice("version:".length);
}

export function buildLatestJson({ version, tag, repo, notes, pubDate, files }) {
  const normalized = normalizeVersion(version);
  const platforms = {};
  for (const platform of PLATFORMS) {
    const entry = files[platform];
    if (!entry || !entry.asset || !entry.signature) {
      throw new Error(
        `missing updater artifact for platform "${platform}": expected { asset, signature }`
      );
    }
    const signature = String(entry.signature).trim();
    if (signature === "") {
      throw new Error(
        `empty signature for platform "${platform}": the .sig file content is required`
      );
    }
    let signed;
    try {
      signed = signedVersionOf(signature);
    } catch (err) {
      throw new Error(`signature for platform "${platform}": ${err.message}`);
    }
    if (normalizeVersion(signed) !== normalized) {
      throw new Error(
        `signature for platform "${platform}" was signed for version "${signed}" but the manifest version is "${normalized}": the updater would refuse it (or, without requireSignedVersion, install the wrong release)`
      );
    }
    platforms[platform] = {
      signature,
      url: `https://github.com/${repo}/releases/download/${tag}/${encodeURIComponent(entry.asset)}`,
    };
  }
  return {
    version: normalized,
    notes,
    pub_date: pubDate,
    platforms,
  };
}

// CLI: discover the three platform files in --dir, read their .sig files,
// write the manifest to --out. Any missing or ambiguous file is fatal and
// leaves no output file behind.
function assertVersionIn(name, version, dir) {
  if (!name.includes(version)) {
    throw new Error(
      `asset "${name}" in ${dir} does not contain the requested version "${version}"; stale or wrong-version artifacts would make clients loop on updates`
    );
  }
}

function find(dir, pattern) {
  const matches = readdirSync(dir).filter((f) => f.match(pattern));
  if (matches.length === 0) {
    throw new Error(`no file matching ${pattern} in ${dir}`);
  }
  if (matches.length > 1) {
    throw new Error(`ambiguous: ${matches.length} files match ${pattern} in ${dir}: ${matches.join(", ")}`);
  }
  return matches[0];
}

function readSig(dir, asset) {
  const sig = `${asset}.sig`;
  try {
    return readFileSync(resolve(dir, sig), "utf8");
  } catch {
    throw new Error(`missing signature file ${sig} in ${dir}`);
  }
}

function main(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 2) {
    args[argv[i].replace(/^--/, "")] = argv[i + 1];
  }
  const required = ["dir", "version", "tag", "repo", "out"];
  for (const name of required) {
    if (!args[name]) {
      throw new Error(`missing required --${name}`);
    }
  }
  const notes = args["notes-file"]
    ? readFileSync(args["notes-file"], "utf8")
    : "";
  const pubDate = args["pub-date"] ?? new Date().toISOString();

  const version = normalizeVersion(args.version);
  const assetFor = {
    "darwin-aarch64": find(args.dir, /^.*-macos-arm64\.app\.tar\.gz$/),
    "darwin-x86_64": find(args.dir, /^.*-macos-x86_64\.app\.tar\.gz$/),
    "windows-x86_64": find(args.dir, /^.*-setup\.exe$/),
  };
  for (const asset of Object.values(assetFor)) {
    assertVersionIn(asset, version, args.dir);
  }
  const files = {};
  for (const [platform, asset] of Object.entries(assetFor)) {
    files[platform] = { asset, signature: readSig(args.dir, asset) };
  }

  const manifest = buildLatestJson({
    version: args.version,
    tag: args.tag,
    repo: args.repo,
    notes,
    pubDate,
    files,
  });
  writeFileSync(args.out, JSON.stringify(manifest, null, 2) + "\n");
  process.stderr.write(`wrote ${args.out}\n`);
}

if (process.argv[1] && process.argv[1].endsWith("gen-latest-json.mjs")) {
  try {
    main(process.argv.slice(2));
  } catch (err) {
    console.error(`gen-latest-json: ${err.message}`);
    process.exit(1);
  }
}
