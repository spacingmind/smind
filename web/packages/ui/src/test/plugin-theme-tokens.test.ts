import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { ZCODE_TOKENS } from "@/lib/zcode-tokens";
import { isSafeThemeValue, PLUGIN_THEME_TOKENS, usableThemeTokens } from "@/lib/plugin-contrib";

const HERE = dirname(fileURLToPath(import.meta.url));
const INDEX_CSS = readFileSync(join(HERE, "..", "index.css"), "utf-8");
// The daemon's list: the ONE canonical copy (internal/plugins/themetokens.json), embedded by the Go package.
const DAEMON_LIST = JSON.parse(readFileSync(join(HERE, "..", "..", "..", "..", "..", "internal", "plugins", "themetokens.json"), "utf-8")) as string[];

function block(marker: string): string {
  const start = INDEX_CSS.indexOf(marker);
  expect(start, `${marker} not found in index.css`).toBeGreaterThan(-1);
  let depth = 0;
  for (let i = start; i < INDEX_CSS.length; i++) {
    if (INDEX_CSS[i] === "{") depth++;
    if (INDEX_CSS[i] === "}" && --depth === 0) return INDEX_CSS.slice(start, i);
  }
  throw new Error(`${marker} is not closed`);
}

const declared = (b: string) => new Set([...b.matchAll(/^\s+(--[a-z0-9-]+):/gm)].map((m) => m[1]!));
const NON_COLOUR = /^--(ui-font-size|elevation-shadow-|content-|duration-|lucide-)/;
// Stage-1 ZCode tokens are app-internal until a later PR promotes them into the plugin contract (daemon list + PLUGIN_THEME_TOKENS).
const ZCODE_INTERNAL = new Set(ZCODE_TOKENS.filter((t) => t.status === "added").map((t) => t.smind));

describe("plugin theme token list", () => {
  it("TestTheme_TokenListDrift: the TS list, the daemon's list and index.css's themeable tokens are the same set", () => {
    const light = declared(block("\n:root {"));
    const dark = declared(block("\n.dark {"));
    const css = [...light].filter((t) => dark.has(t) && !NON_COLOUR.test(t) && !ZCODE_INTERNAL.has(t)).sort();
    expect([...PLUGIN_THEME_TOKENS].sort(), "lib/plugin-contrib.ts PLUGIN_THEME_TOKENS vs index.css").toEqual(css);
    expect([...DAEMON_LIST].sort(), "internal/plugins/themetokens.json vs index.css").toEqual(css);
  });
});

const HOSTILE = [
  "url(https://evil.example/x.png)",
  "URL(//evil)",
  "var(--background)",
  "#fff; background: url(x)",
  "#fff} body{display:none",
  "{#fff}",
  "@import 'evil.css'",
  "expression(alert(1))",
  "calc(1px + 2px)",
  "color-mix(in oklab, red, blue)",
  "red",
  "rgb(0 0 0) url(x)",
  "#fff\nurl(x)",
  "#fff/*x*/",
  "\\75rl(x)",
  '"#fff"',
  "",
  "#" + "f".repeat(100),
  "#ffff٣٣",
  "＃ffffff",
  "rgb(１ ２ ３)",
  "rgb(1 2 3)",
  "#fff​",
  "#fff‮",
  "#12345",
  "rgb(1 2 3",
  "rgb(url(x) 2 3)",
  "#fff !important",
  "#fff #000",
  "attr(x)",
  "env(safe-area-inset-top)",
];

describe("isSafeThemeValue", () => {
  it.each(HOSTILE)("rejects %j", (value) => {
    expect(isSafeThemeValue(value)).toBe(false);
  });
  it("rejects non-strings", () => {
    expect(isSafeThemeValue(null)).toBe(false);
    expect(isSafeThemeValue(12)).toBe(false);
  });
  it.each(["#fff", "#FFFF", "#112233", "#11223344", "rgb(1 2 3)", "rgba(1, 2, 3, 0.5)", "rgb(1 2 3 / 50%)", "hsl(120 50% 50%)", "hsl(120deg, 50%, 50%)", "oklch(0.7 0.1 200)", "oklch(70% 0.1 200deg / 0.4)", "12px", "0.5rem", "-1px", "50%", "transparent"])(
    "accepts %j",
    (value) => {
      expect(isSafeThemeValue(value)).toBe(true);
    },
  );
});

describe("usableThemeTokens", () => {
  const complete = () => Object.fromEntries(PLUGIN_THEME_TOKENS.map((t) => [t, "#112233"]));
  it("accepts exactly the canonical tokens with safe values", () => {
    expect(usableThemeTokens(complete())).not.toBeNull();
  });
  it("refuses an incomplete, extended or unsafe set", () => {
    const missing = complete();
    delete missing[PLUGIN_THEME_TOKENS[0]!];
    expect(usableThemeTokens(missing)).toBeNull();
    expect(usableThemeTokens({ ...complete(), "--evil": "#fff" })).toBeNull();
    expect(usableThemeTokens({ ...complete(), [PLUGIN_THEME_TOKENS[3]!]: "url(x)" })).toBeNull();
    expect(usableThemeTokens(undefined)).toBeNull();
  });
});
