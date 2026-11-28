#!/usr/bin/env node
/**
 * Builds the sMind Astryx theme (src/theme/smind-theme.ts) into the checked-in
 * stylesheet src/theme/smind-theme.css at BUILD TIME (ADR-0064, Wave 0 AC4).
 * The app never compiles a theme in the browser.
 *
 *   node scripts/build-smind-theme.mjs [--out <file.css>]
 *
 * Uses the documented route, `astryx theme build <file> --out <css>` from
 * @astryxdesign/cli. The CLI only writes inside the working directory and
 * stamps its own --out path into the header, so this script builds into a
 * scratch folder under node_modules/.cache, rewrites that header line to a
 * fixed command, and copies only the .css to the requested path. The result
 * is the same bytes on every run, wherever --out points.
 */
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const uiRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = "src/theme/smind-theme.ts";
const defaultOut = join(uiRoot, "src/theme/smind-theme.css");
const fixedCommand = `astryx theme build ${source} --out src/theme/smind-theme.css`;

function parseOut(argv) {
  const i = argv.indexOf("--out");
  if (i === -1) return defaultOut;
  const value = argv[i + 1];
  if (!value) throw new Error("--out needs a file path");
  return resolve(process.cwd(), value);
}

function astryxBin() {
  const require = createRequire(join(uiRoot, "package.json"));
  // The package's exports map hides package.json, so find the folder by lookup path.
  const dir = require
    .resolve.paths("@astryxdesign/cli")
    .map((p) => join(p, "@astryxdesign/cli"))
    .find((d) => existsSync(join(d, "package.json")));
  if (!dir) throw new Error("@astryxdesign/cli is not installed");
  const pkgPath = join(dir, "package.json");
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
  const bin = typeof pkg.bin === "string" ? pkg.bin : (pkg.bin.astryx ?? Object.values(pkg.bin)[0]);
  return join(dirname(pkgPath), bin);
}

export function buildSmindTheme(outPath = defaultOut) {
  const cache = join(uiRoot, "node_modules/.cache");
  mkdirSync(cache, { recursive: true });
  const stage = mkdtempSync(join(cache, "smind-theme-"));
  try {
    const staged = join(stage, "smind-theme.css");
    execFileSync(
      process.execPath,
      [astryxBin(), "theme", "build", source, "--out", staged.slice(uiRoot.length + 1)],
      { cwd: uiRoot, stdio: ["ignore", "ignore", "inherit"] },
    );
    const css = readFileSync(staged, "utf8").replace(/^ \* Command: .*$/m, ` * Command: ${fixedCommand}`);
    mkdirSync(dirname(outPath), { recursive: true });
    writeFileSync(outPath, css);
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  buildSmindTheme(parseOut(process.argv.slice(2)));
}
