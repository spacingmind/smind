import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { parseThemeMaps, tokenContrast } from "@/lib/contrast";

const { light, dark } = parseThemeMaps(readFileSync("src/index.css", "utf8"));
const themes = [
  ["light", light],
  ["dark", dark],
] as const;

const OPAQUE_SURFACES = ["--background", "--sidebar", "--card", "--panel", "--input-bg"];
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
    it("--warning-text on --warning-surface over --background >= 4.5", () => {
      expect(tokenContrast(vars, "--warning-text", "--warning-surface", "--background")).toBeGreaterThanOrEqual(4.5);
    });

    // Fill/glyph (dots, icons, borders; never the only carrier of meaning). Light `--warning` is
    // ZCode's own #e07b00 (pinned by test/zcode-tokens.test.ts): 2.82:1 on --background and 3.00:1
    // on white, a known sub-3:1 exception, so light only guards against getting worse.
    it.each(["--background", "--card"])("--warning (fill/glyph) on %s >= 3 (light: >= 2.8)", (surface) => {
      expect(tokenContrast(vars, "--warning", surface)).toBeGreaterThanOrEqual(vars === light ? 2.8 : 3);
    });
  });
});
