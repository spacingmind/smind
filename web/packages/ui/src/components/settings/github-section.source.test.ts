import { describe, expect, it } from "vitest";

// Raw source, resolved at transform time (no fs access needed).
const SOURCES = import.meta.glob<string>(
  ["/src/components/settings/github-section.tsx", "/src/hooks/use-forge-status.ts"],
  { query: "?raw", import: "default", eager: true },
);

describe("the pasted GitHub token is never persisted", () => {
  it("scans the token-handling files for stores, storage and logging", () => {
    expect(Object.keys(SOURCES)).toHaveLength(2);
    for (const [path, source] of Object.entries(SOURCES)) {
      expect(source, path).not.toMatch(/localStorage|sessionStorage|indexedDB|document\.cookie/);
      expect(source, path).not.toMatch(/from "zustand|from "@\/stores\//);
      expect(source, path).not.toMatch(/console\./);
      expect(source, path).not.toMatch(/persist|setQueryData<[^>]*>\([^)]*token/i);
    }
  });

  it("keeps the token out of mutation variables (the mutation cache would hold them)", () => {
    const hook = Object.entries(SOURCES).find(([p]) => p.endsWith("use-forge-status.ts"))![1];
    expect(hook).toContain("useMutation<ForgeStatusResult, Error, { host?: string }>");
    expect(hook).not.toMatch(/variables|mutate\(\{[^}]*token/);
    expect(hook).toMatch(/gcTime: 0/);
  });
});
