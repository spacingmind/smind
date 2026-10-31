/**
 * Before/after WCAG contrast of the tokens the AA pass changed.
 * Run from web/packages/ui: `bun run scripts/contrast-table.ts`.
 * "before" is `src/index.css` on origin/develop; "after" is the working tree.
 */
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

import { parseThemeMaps, tokenContrast } from "../src/lib/contrast";

const before = parseThemeMaps(execFileSync("git", ["show", "origin/develop:web/packages/ui/src/index.css"], { encoding: "utf8" }));
const after = parseThemeMaps(readFileSync("src/index.css", "utf8"));

interface Pair {
  name: string;
  /** token before / after (orange text used `--warning` before `--warning-text` existed) */
  fgBefore: string;
  fgAfter: string;
  surface: string;
  base?: string;
}

const pairs: Pair[] = [
  ...["--background", "--sidebar", "--card", "--panel", "--input-bg"].map((s) => ({ name: "--muted-foreground", fgBefore: "--muted-foreground", fgAfter: "--muted-foreground", surface: s })),
  ...["--surface", "--hover", "--selected"].flatMap((s) =>
    ["--background", "--sidebar"].map((b) => ({ name: "--muted-foreground", fgBefore: "--muted-foreground", fgAfter: "--muted-foreground", surface: s, base: b })),
  ),
  ...["--background", "--sidebar", "--card", "--panel"].map((s) => ({ name: "--warning-text (was --warning)", fgBefore: "--warning", fgAfter: "--warning-text", surface: s })),
  { name: "--warning-text (was --warning)", fgBefore: "--warning", fgAfter: "--warning-text", surface: "--warning-surface", base: "--background" },
  { name: "--warning-text (was --warning)", fgBefore: "--warning", fgAfter: "--warning-text", surface: "--warning-surface", base: "--sidebar" },
];

const r = (n: number) => n.toFixed(2);
const ratio = (vars: Map<string, string>, fg: string, p: Pair) => tokenContrast(vars, fg, p.surface, p.base);

const rows = ["| pair | surface | light before | light after | dark before | dark after |", "|---|---|---|---|---|---|"];
for (const p of pairs) {
  const surface = p.base ? `${p.surface} over ${p.base}` : p.surface;
  rows.push(
    `| ${p.name} | ${surface} | ${r(ratio(before.light, p.fgBefore, p))} | ${r(ratio(after.light, p.fgAfter, p))} | ${r(ratio(before.dark, p.fgBefore, p))} | ${r(ratio(after.dark, p.fgAfter, p))} |`,
  );
}
console.log(rows.join("\n"));
