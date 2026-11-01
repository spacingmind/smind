import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { parseThemeMaps, tokenContrast } from "@/lib/contrast";

const { light, dark } = parseThemeMaps(readFileSync("src/index.css", "utf8"));
const themes = [
  ["light", light],
  ["dark", dark],
] as const;

const OPAQUE_SURFACES = ["--background", "--sidebar", "--card", "--panel", "--input-bg"];
// Status text (sidebar diffstat, git panel counts, chips) also lands on these.
const STATUS_TEXT_SURFACES = [...OPAQUE_SURFACES, "--muted", "--accent", "--sidebar-accent"];
const SUCCESS_TEXT = ["--success", "--diff-added", "--git-added", "--git-untracked"];
const DESTRUCTIVE_TEXT = ["--destructive", "--diff-removed", "--git-deleted"];
const TRANSLUCENT_SURFACES = ["--surface", "--hover", "--selected"];
const TRANSLUCENT_BASES = ["--background", "--sidebar"];

describe("contrast AA regression", () => {
  describe.each(themes)("%s", (_name, vars) => {
    for (const fg of ["--muted-foreground", "--foreground-muted"]) {
      it.each(OPAQUE_SURFACES)(`${fg} on %s >= 4.5`, (surface) => {
        expect(tokenContrast(vars, fg, surface)).toBeGreaterThanOrEqual(4.5);
      });
      for (const surface of TRANSLUCENT_SURFACES) {
        it.each(TRANSLUCENT_BASES)(`${fg} on ${surface} over %s >= 4.5`, (base) => {
          expect(tokenContrast(vars, fg, surface, base)).toBeGreaterThanOrEqual(4.5);
        });
      }
    }

    it.each(OPAQUE_SURFACES)("--warning-text on %s >= 4.5", (surface) => {
      expect(tokenContrast(vars, "--warning-text", surface)).toBeGreaterThanOrEqual(4.5);
    });
    it.each(TRANSLUCENT_BASES)("--warning-text on --warning-surface over %s >= 4.5", (base) => {
      expect(tokenContrast(vars, "--warning-text", "--warning-surface", base)).toBeGreaterThanOrEqual(4.5);
    });

    // Status text: `--success`/`--diff-added`/`--git-added`/`--git-untracked` and
    // `--destructive`/`--diff-removed`/`--git-deleted` are used as 10px text (sidebar task
    // diffstat, git panel). `--git-modified`/`--git-descendant` are dots/glyphs only (>= 3 below).
    for (const [group, surfaceToken] of [
      [SUCCESS_TEXT, "--success-surface"],
      [DESTRUCTIVE_TEXT, "--destructive-surface"],
    ] as const) {
      for (const fg of group) {
        it.each(STATUS_TEXT_SURFACES)(`${fg} text on %s >= 4.5`, (surface) => {
          expect(tokenContrast(vars, fg, surface)).toBeGreaterThanOrEqual(4.5);
        });
        for (const surface of TRANSLUCENT_SURFACES) {
          it.each(TRANSLUCENT_BASES)(`${fg} text on ${surface} over %s >= 4.5`, (base) => {
            expect(tokenContrast(vars, fg, surface, base)).toBeGreaterThanOrEqual(4.5);
          });
        }
        it.each(TRANSLUCENT_BASES)(`${fg} text on ${surfaceToken} over %s >= 4.5`, (base) => {
          expect(tokenContrast(vars, fg, surfaceToken, base)).toBeGreaterThanOrEqual(4.5);
        });
      }
    }
    it.each(["--git-modified", "--git-descendant"])("%s (glyph only) on --background >= 3 (light: >= 2.8)", (fg) => {
      expect(tokenContrast(vars, fg, "--background")).toBeGreaterThanOrEqual(vars === light ? 2.8 : 3);
    });

    // Fill/glyph (dots, icons, borders; never the only carrier of meaning). Light `--warning` is
    // ZCode's own #e07b00 (pinned by test/zcode-tokens.test.ts): 2.82:1 on --background and 3.00:1
    // on white, a known sub-3:1 exception, so light only guards against getting worse.
    it.each(["--background", "--card"])("--warning (fill/glyph) on %s >= 3 (light: >= 2.8)", (surface) => {
      expect(tokenContrast(vars, "--warning", surface)).toBeGreaterThanOrEqual(vars === light ? 2.8 : 3);
    });
  });
});
