import { describe, expect, it } from "vitest";

const SOURCES = import.meta.glob<string>(["/src/components/inbox/*.tsx", "/src/hooks/use-inbox.ts", "/src/lib/inbox-types.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
});

describe("inbox safety walk (ADR-0045: the inbox never approves, merges or auto-allows)", () => {
  // Comments may say what the inbox does NOT do; only code is checked.
  const code = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  const files = Object.entries(SOURCES)
    .filter(([path]) => !/\.test\./.test(path))
    .map(([path, source]): [string, string] => [path, code(source)]);

  it("finds the inbox sources", () => {
    expect(files.length).toBeGreaterThanOrEqual(4);
  });

  it.each(files)("%s has no approve-all, merge or always-allow control", (_path, source) => {
    expect(source).not.toMatch(/approveAll|approve-all|approve all|allowAll|alwaysAllow|always allow|mergeNow|"inbox\.(approve|merge)/i);
    expect(source).not.toMatch(/"(pr|task)\.merge"|\.merge\(/);
  });

  it("the only permission mutation is run.respondPermission, one request at a time", () => {
    const hook = files.find(([p]) => p.endsWith("use-inbox.ts"))?.[1] ?? "";
    expect(hook.match(/call\("run\.respondPermission"/g)).toHaveLength(1);
    expect(hook).not.toMatch(/keys\.map\(.*respondPermission|for \(const .* of .*respondPermission/s);
  });
});
