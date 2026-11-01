import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { REFUSED_METHODS, RULES } from "./rules";

const SRC = path.resolve(__dirname, "..");

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "demo" || e.name === "test" || e.name === "__fixtures__") continue;
      sourceFiles(p, out);
    } else if (/\.(ts|tsx)$/.test(e.name) && !/\.test\./.test(e.name)) out.push(p);
  }
  return out;
}

/** Methods the UI passes as a string literal to `call` / `callStream` (a local wrapper named call counts too). */
function uiMethods(): Set<string> {
  const re = /\bcall(?:Stream)?(?:<[^()]*?>)?\(\s*"([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)"/g;
  const found = new Set<string>();
  for (const f of sourceFiles(SRC)) for (const m of readFileSync(f, "utf8").matchAll(re)) found.add(m[1]);
  return found;
}

describe("demo coverage guard", () => {
  it("D-03 every method string the UI passes to call/callStream has a rule", () => {
    const methods = uiMethods();
    // the scan itself must keep working: the UI calls well over a hundred distinct methods
    expect(methods.size).toBeGreaterThan(120);
    const missing = [...methods].filter((m) => !Object.hasOwn(RULES, m)).sort();
    expect(missing, `add a rule (read/scripted/local) or list the method in REFUSED_METHODS: ${missing.join(", ")}`).toEqual([]);
  });

  it("every refused method is refused, and none is listed twice", () => {
    for (const m of REFUSED_METHODS) expect(RULES[m]?.kind, m).toBe("refused");
    expect(new Set(REFUSED_METHODS).size).toBe(REFUSED_METHODS.length);
  });

  it("every rule is one of the four documented kinds", () => {
    for (const [method, rule] of Object.entries(RULES)) expect(["read", "scripted", "local", "refused"], method).toContain(rule.kind);
  });
});
