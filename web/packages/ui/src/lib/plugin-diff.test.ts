import { describe, expect, it } from "vitest";

import { diffConsent } from "@/lib/plugin-diff";
import { consent } from "@/test/plugin-fixtures";

describe("diffConsent", () => {
  it("reports nothing for an identical declaration", () => {
    expect(diffConsent(consent(), consent())).toEqual([]);
  });

  it("marks what grants or runs more (capability, MCP command, hook, process, escalating agent)", () => {
    const next = consent({
      version: "1.1.0",
      skills: [],
      capabilities: ["events.observe"],
      process: ["node", "main.js"],
      hookEvents: [{ event: "run.done", onError: "open" }],
      mcpServers: [{ name: "docs", transport: "stdio", command: ["npx", "docs"] }],
      profiles: [{ name: "Yolo", provider: "claude-native", escalations: ["auto-accepts every permission request"] }],
    });
    const changes = diffConsent(consent(), next);
    const by = (what: string) => changes.filter((c) => c.what === what);
    expect(by("Version")[0]).toMatchObject({ kind: "changed", text: "1.0.0 → 1.1.0", runs: false });
    expect(by("Capability")[0]).toMatchObject({ kind: "added", runs: true });
    expect(by("Process")[0]).toMatchObject({ kind: "added", text: "node main.js", runs: true });
    expect(by("Hook")[0]).toMatchObject({ kind: "added", runs: true });
    expect(by("MCP server")[0]).toMatchObject({ kind: "changed", runs: true });
    expect(by("Escalating agent")[0]).toMatchObject({ kind: "added", runs: true });
    // the calm agent and the skill that went away are informational only
    expect(by("Agent")[0]).toMatchObject({ kind: "removed", runs: false });
    expect(by("Skill")[0]).toMatchObject({ kind: "removed", runs: false });
  });
});
