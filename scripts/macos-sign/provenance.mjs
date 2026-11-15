#!/usr/bin/env node
// Proves that the macOS app a signing job is about to sign came out of OUR release process, before
// any secret is in the environment (docs/plans/active/desktop-notarization-prep.md, ADR-0060).
//
// Anyone with write access can create a release with an asset of the right name; signing "whatever the
// tag has" would put the Developer ID on it. This script instead requires:
//   1. the release's checksums.txt carries a valid minisign signature (the signature the release job
//      makes with the updater key: `sign-checksums.sh`) by the PUBLIC key committed in
//      desktop/src-tauri/tauri.conf.json, whose signed comment names `file:checksums.txt` and
//      `version:<the tag's version>` (the same rule as internal/selfupdate);
//   2. the asset's SHA-256 is listed, under that exact file name, in that verified checksums.txt.
// It reads files only: no network, no secret. Exit 0 = provenance proven; 1 = refused (one reason line).
//
// usage: node provenance.mjs --dir <dir> --asset <file name> --tag <vX.Y.Z> [--conf <tauri.conf.json>]
//   <dir> holds the asset, checksums.txt and checksums.txt.minisig as downloaded from the release.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import { verifyMinisign } from "../verify-release-artifacts.mjs";

const here = dirname(fileURLToPath(import.meta.url));

// Returns null when the asset is proven to belong to the release, else the reason.
export function provenanceFailure({ asset, assetBytes, checksumsText, minisigText, pubkey, tag }) {
  if (typeof pubkey !== "string" || pubkey === "") return "no updater public key to verify against";
  if (!/^v\d+\.\d+\.\d+$/.test(tag)) return `the tag ${JSON.stringify(tag)} is not a stable release tag`;
  if (asset !== basename(asset) || asset === "") return "the asset name must be a plain file name";
  const why = verifyMinisign(Buffer.from(checksumsText, "utf8"), minisigText, pubkey, { plain: true });
  if (why !== null) return `checksums.txt does not verify with the committed updater public key: ${why}`;
  // the signed comment names the file and the version it was signed for
  const comment = (minisigText.split(/\r?\n/)[2] ?? "").replace(/^trusted comment: /, "");
  const fields = comment.split("\t");
  if (!fields.includes("file:checksums.txt")) return "the checksums signature is not for checksums.txt";
  if (!fields.includes(`version:${tag.slice(1)}`)) return `the checksums signature is not for version ${tag.slice(1)}`;
  const digest = createHash("sha256").update(assetBytes).digest("hex");
  const listed = checksumsText
    .split(/\r?\n/)
    .map((l) => l.match(/^([0-9a-f]{64})  (.+)$/))
    .filter((m) => m !== null && m[2] === asset);
  if (listed.length !== 1) return `${asset} is listed ${listed.length} times in the verified checksums.txt (expected exactly once)`;
  if (listed[0][1] !== digest) return `${asset} does not match its entry in the verified checksums.txt`;
  return null;
}

function main(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 2) args[argv[i].replace(/^--/, "")] = argv[i + 1];
  for (const k of ["dir", "asset", "tag"]) {
    if (!args[k]) {
      process.stderr.write(`provenance: missing --${k}\n`);
      return 2;
    }
  }
  const conf = args.conf ?? join(here, "../../desktop/src-tauri/tauri.conf.json");
  let reason;
  try {
    reason = provenanceFailure({
      asset: args.asset,
      assetBytes: readFileSync(join(args.dir, args.asset)),
      checksumsText: readFileSync(join(args.dir, "checksums.txt"), "utf8"),
      minisigText: readFileSync(join(args.dir, "checksums.txt.minisig"), "utf8"),
      pubkey: JSON.parse(readFileSync(conf, "utf8"))?.plugins?.updater?.pubkey,
      tag: args.tag,
    });
  } catch (err) {
    reason = `cannot read the release files: ${String(err.message ?? err).split("\n")[0]}`;
  }
  if (reason !== null) {
    process.stderr.write(`provenance: REFUSED: ${reason}\n`);
    return 1;
  }
  process.stdout.write(`provenance: ${args.asset} is listed in a checksums.txt signed by the committed updater key for ${args.tag}\n`);
  return 0;
}

if (process.argv[1] && process.argv[1].endsWith("provenance.mjs")) process.exitCode = main(process.argv.slice(2));
