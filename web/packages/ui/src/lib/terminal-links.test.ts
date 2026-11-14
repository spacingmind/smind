import { describe, expect, it, vi } from "vitest";

import { activateTerminalLink, findTerminalLinks } from "@/lib/terminal-links";

/** The path matches of a line, for terser assertions. */
function paths(line: string) {
  return findTerminalLinks(line)
    .filter((m): m is Extract<typeof m, { kind: "path" }> => m.kind === "path")
    .map(({ path, line: l, col }) => ({ path, line: l, col }));
}

function urls(line: string): string[] {
  return findTerminalLinks(line)
    .filter((m): m is Extract<typeof m, { kind: "url" }> => m.kind === "url")
    .map((m) => m.url);
}

describe("findTerminalLinks paths", () => {
  it("matches path:line", () => {
    expect(paths("error in src/app.ts:12")).toEqual([{ path: "src/app.ts", line: 12, col: undefined }]);
  });

  it("matches a ./-prefixed path with line and col", () => {
    expect(paths("at ./cmd/smind/main.go:40:7 here")).toEqual([
      { path: "./cmd/smind/main.go", line: 40, col: 7 },
    ]);
  });

  it("matches a plain path with no line", () => {
    expect(paths("see web/packages/ui/src/App.tsx")).toEqual([
      { path: "web/packages/ui/src/App.tsx", line: undefined, col: undefined },
    ]);
  });

  it("matches a path wrapped in parentheses", () => {
    const [match] = paths("(internal/files/files.go:12)");
    expect(match).toEqual({ path: "internal/files/files.go", line: 12, col: undefined });
    // The span excludes the wrapping parens.
    const full = findTerminalLinks("(internal/files/files.go:12)")[0]!;
    expect(full.text).toBe("internal/files/files.go:12");
    expect(full.start).toBe(1);
    expect(full.end).toBe(1 + "internal/files/files.go:12".length);
  });

  it("a token with only a slash-containing part is still a path (a/b)", () => {
    expect(paths("a/b")).toEqual([{ path: "a/b", line: undefined, col: undefined }]);
  });

  it("an extension-only token without a separator matches (app.ts)", () => {
    expect(paths("app.ts")).toEqual([{ path: "app.ts", line: undefined, col: undefined }]);
  });

  it("strips a trailing sentence period from a separator-containing token", () => {
    expect(paths("failed: src/app.ts.")).toEqual([{ path: "src/app.ts", line: undefined, col: undefined }]);
  });

  it("does not match bare versions, IPs, plain words or e.g.", () => {
    expect(paths("1.2.3")).toEqual([]);
    expect(paths("10.0.0.1:8080")).toEqual([]);
    expect(paths("hello")).toEqual([]);
    expect(paths("e.g.")).toEqual([]);
  });
});

describe("findTerminalLinks urls", () => {
  it("matches a plain http URL and a case-insensitive scheme", () => {
    expect(urls("see https://example.com/a and HTTP://example.org/b")).toEqual([
      "https://example.com/a",
      "HTTP://example.org/b",
    ]);
  });

  it("trims trailing sentence punctuation and an unbalanced paren", () => {
    expect(urls("see https://example.com/a).")).toEqual(["https://example.com/a"]);
  });

  it("keeps a balanced paren inside the URL", () => {
    expect(urls("https://en.example.org/wiki/A_(b)")).toEqual(["https://en.example.org/wiki/A_(b)"]);
  });

  it("rejects a URL with userinfo", () => {
    expect(urls("https://user:pw@example.com")).toEqual([]);
  });

  it("rejects a URL over 2048 characters and accepts exactly 2048", () => {
    const long = `https://example.com/${"a".repeat(2048 - "https://example.com/".length)}`;
    expect(long).toHaveLength(2048);
    expect(urls(long)).toEqual([long]);
    const tooLong = `${long}a`;
    expect(urls(tooLong)).toEqual([]);
  });

  it("never linkifies file:, javascript:, data: or other schemes", () => {
    expect(findTerminalLinks("file:///etc/passwd")).toEqual([]);
    expect(findTerminalLinks("javascript:alert(1)")).toEqual([]);
    expect(findTerminalLinks("data:text/html,hi")).toEqual([]);
    expect(findTerminalLinks("ftp://example.com/x")).toEqual([]);
  });
});

describe("findTerminalLinks combined", () => {
  it("a path inside a URL is a URL link, never also a path match", () => {
    const matches = findTerminalLinks("docs: https://example.com/src/app.ts end");
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({ kind: "url", url: "https://example.com/src/app.ts" });
    // Even a rejected URL (userinfo) is blanked for path matching, so its
    // host/authority never leaks out as a path link either.
    expect(findTerminalLinks("https://user:pw@example.com/x.ts")).toEqual([]);
  });

  it("returns matches sorted and non-overlapping, mixing kinds", () => {
    const line = "src/app.ts:12 then https://example.com/a)";
    const matches = findTerminalLinks(line);
    expect(matches.map((m) => m.kind)).toEqual(["path", "url"]);
    expect(matches[0]!.text).toBe("src/app.ts:12");
    expect(matches[1]!.text).toBe("https://example.com/a");
    for (let i = 1; i < matches.length; i++) {
      expect(matches[i]!.start).toBeGreaterThanOrEqual(matches[i - 1]!.end);
    }
    expect(matches[0]!.start).toBe(0);
  });
});

describe("findTerminalLinks on adversarial input", () => {
  it("handles a very long run of closing parens in linear time and links nothing past the cap", () => {
    const started = Date.now();
    const out = findTerminalLinks("http://a" + ")".repeat(100_000));
    expect(Date.now() - started).toBeLessThan(500);
    expect(out.filter((m) => m.kind === "url")).toEqual([]);
  });

  it("still trims an unbalanced closing paren on an ordinary URL", () => {
    const urls = findTerminalLinks("(see https://example.com/a)").filter((m) => m.kind === "url");
    expect(urls.map((m) => (m.kind === "url" ? m.url : ""))).toEqual(["https://example.com/a"]);
  });
});

describe("activateTerminalLink", () => {
  it("runs only with Ctrl or Cmd held", () => {
    const run = vi.fn();
    activateTerminalLink({ ctrlKey: false, metaKey: false }, run);
    expect(run).not.toHaveBeenCalled();
    activateTerminalLink({ ctrlKey: true, metaKey: false }, run);
    activateTerminalLink({ ctrlKey: false, metaKey: true }, run);
    expect(run).toHaveBeenCalledTimes(2);
  });
});
