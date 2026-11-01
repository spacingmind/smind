import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { composite, contrastRatio, parseColor, parseThemeMaps, tokenContrast } from "@/lib/contrast";

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
    // Diff row labels (`--d2h-*-label-color` = --diff-added/removed text) sit on the tinted rows: the tint source is
    // `--diff-*-tint` (ZCode's original hue) over --card at the same percentages as the `--d2h-*` mixes in index.css.
    // Light row tints (15% / 20% / 12%) are AA. The 30% word highlight in light and several dark tints are below 4.5
    // (same-hue text on its own tint; dark is ZCode's untouched value, unchanged by the light AA work): the label text is
    // not drawn on a word highlight, and those known cases only guard against getting worse (>= 3).
    for (const [text, tint, pcts] of [
      ["--diff-added", "--diff-added-tint", [0.15, 0.2, 0.3]],
      ["--diff-removed", "--diff-removed-tint", [0.12, 0.3]],
    ] as const) {
      it.each(pcts)(`${text} text on ${tint} over --card at %s`, (pct) => {
        const card = parseColor(vars.get("--card")!, vars);
        const row = composite({ ...parseColor(vars.get(tint)!, vars), a: pct }, card);
        const ratio = contrastRatio(parseColor(vars.get(text)!, vars), row);
        const knownLow = pct === 0.3 || (vars !== light && !(text === "--diff-added" && pct === 0.15));
        expect(ratio).toBeGreaterThanOrEqual(knownLow ? 3 : 4.5);
      });
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
