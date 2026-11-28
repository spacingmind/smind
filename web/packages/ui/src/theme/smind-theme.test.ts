import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { smindColorTokens } from "./smind-theme";
import { themeAttributesFor } from "./theme-attributes";

const uiRoot = resolve(__dirname, "../..");
const cssPath = join(uiRoot, "src/theme/smind-theme.css");

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

describe("smind Astryx theme", () => {
  it("smind_theme_covers_light_and_dark_for_every_token", () => {
    const entries = Object.entries(smindColorTokens);
    expect(entries.length).toBeGreaterThan(20);
    for (const [name, [light, dark]] of entries) {
      expect(light.trim(), `${name} light`).not.toBe("");
      expect(dark.trim(), `${name} dark`).not.toBe("");
    }
    for (const name of ["--color-background-body", "--color-text-primary", "--color-text-secondary"]) {
      const [light, dark] = smindColorTokens[name as keyof typeof smindColorTokens];
      expect(light, name).not.toBe(dark);
    }
    // The built stylesheet carries both values for a token (light-dark()).
    expect(readFileSync(cssPath, "utf8")).toContain("--color-background-body: light-dark(#f8f8f8, #161616)");
  });

  it("smind_theme_stylesheet_is_built_not_compiled_at_runtime", () => {
    expect(existsSync(cssPath)).toBe(true);
    const css = readFileSync(cssPath, "utf8");
    expect(css.length).toBeGreaterThan(0);
    expect(css).toContain("@layer astryx-theme");
    expect(css).toContain('[data-astryx-theme="smind"]');

    const dir = mkdtempSync(join(tmpdir(), "smind-theme-"));
    try {
      const out = join(dir, "smind-theme.css");
      execFileSync(process.execPath, [join(uiRoot, "scripts/build-smind-theme.mjs"), "--out", out], {
        cwd: uiRoot,
        stdio: "pipe",
      });
      expect(readFileSync(out, "utf8")).toBe(css);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }

    // Nothing in src/ calls the runtime theme compiler (the generator lives in scripts/).
    const compiler = /generateThemeCSS|generateThemeRules|compileTheme/;
    const offenders = walk(join(uiRoot, "src"))
      .filter((f) => /\.(ts|tsx|js|jsx|mjs)$/.test(f) && !f.endsWith("smind-theme.test.ts"))
      .filter((f) => compiler.test(readFileSync(f, "utf8")));
    expect(offenders).toEqual([]);
  });

  it("prepaint_script_sets_the_astryx_theme_attributes", () => {
    const html = readFileSync(join(uiRoot, "index.html"), "utf8");
    const scripts = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)].map((m) => m[1]);
    const inline = scripts.join("\n");
    expect(inline).not.toBe("");

    const light = themeAttributesFor("light", true);
    const dark = themeAttributesFor("dark", false);
    expect(light).toEqual({ "data-astryx-theme": "smind", "data-theme": "light" });
    expect(dark).toEqual({ "data-astryx-theme": "smind", "data-theme": "dark" });
    expect(themeAttributesFor("system", true)).toEqual(dark);
    expect(themeAttributesFor("system", false)).toEqual(light);

    for (const name of Object.keys(light)) expect(inline, name).toContain(name);
    for (const value of ["smind", "light", "dark"]) expect(inline, value).toContain(`"${value}"`);
    // The Tailwind `dark` class stays.
    expect(inline).toContain('classList.toggle("dark"');
  });
});
