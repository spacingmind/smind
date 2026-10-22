import { describe, expect, it } from "vitest";

const SOURCES = import.meta.glob<string>("/src/components/learning/probe-*.tsx", { query: "?raw", import: "default", eager: true });
const CARD_SOURCES = Object.entries(SOURCES).filter(([path]) => !/\.test\.tsx$/.test(path));

describe("probe card sources", () => {
  it("finds the card's source files", () => {
    expect(CARD_SOURCES.map(([p]) => p.split("/").pop())).toEqual(
      expect.arrayContaining(["probe-card.tsx", "probe-arm-rows.tsx", "probe-contrast-table.tsx", "probe-diff.tsx", "probe-verdict-chip.tsx"]),
    );
  });

  it("reads server state only through the shared hooks", () => {
    const card = SOURCES["/src/components/learning/probe-card.tsx"] ?? "";
    expect(card).toMatch(/from "@\/hooks\/use-learning"/);
    for (const [path, source] of CARD_SOURCES) {
      expect(source, path).not.toMatch(/\bfetch\s*\(/);
      expect(source, path).not.toMatch(/client\.call|\.call\s*[<(]/);
      expect(source, path).not.toMatch(/useDaemonConnection|useQueryClient|new WsClient/);
    }
  });

  it("uses the shared primitives rather than its own pill, spinner and segmented control", () => {
    const card = SOURCES["/src/components/learning/probe-card.tsx"] ?? "";
    expect(card).toContain("@/components/ui/segmented-control");
    expect(card).toContain("@/components/ui/inline-spinner");
    expect(card).toContain("@/components/ui/loading-rows");
    expect(SOURCES["/src/components/learning/probe-verdict-chip.tsx"]).toContain("@/components/ui/status-badge");
  });
});
