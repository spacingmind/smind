import { describe, expect, it } from "vitest";

import { Icons, pluginIconByName } from "@/lib/icons";

describe("pluginIconByName", () => {
  it("binds a curated plugin-facing name to a real component", () => {
    expect(pluginIconByName("Hand")).not.toBe(Icons.plugin);
  });

  it("falls back to the generic plugin glyph for an unknown name", () => {
    expect(pluginIconByName("NoSuchIconXyz")).toBe(Icons.plugin);
  });

  it("keys deprecated lucide alias spellings to the canonical component", () => {
    expect(pluginIconByName("Wand2")).toBe(pluginIconByName("WandSparkles"));
  });

  it("prototype member names fall back to the generic icon", () => {
    expect(pluginIconByName("constructor")).toBe(Icons.plugin);
    expect(pluginIconByName("toString")).toBe(Icons.plugin);
    expect(pluginIconByName("__proto__")).toBe(Icons.plugin);
    expect(pluginIconByName("hasOwnProperty")).toBe(Icons.plugin);
  });
});
