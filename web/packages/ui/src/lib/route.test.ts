import { describe, expect, it } from "vitest";

import { formatRoute, parseRoute, type Route } from "@/lib/route";

describe("parseRoute", () => {
  it("parses a base-kind tab", () => {
    expect(parseRoute("#/workspace/1/task/42/diff")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "diff" },
    });
  });

  it("parses a file tab, decoding and rejoining its path", () => {
    expect(parseRoute("#/workspace/1/task/42/file/src/app.ts")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "file", path: "src/app.ts" },
    });
    expect(parseRoute("#/workspace/1/task/42/file/a%20b/c")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "file", path: "a b/c" },
    });
  });

  it("accepts a leading hash or a bare path", () => {
    expect(parseRoute("/workspace/1/task/42/diff")).toEqual(parseRoute("#/workspace/1/task/42/diff"));
  });

  it("returns null for anything malformed rather than throwing", () => {
    for (const hash of [
      "",
      "#",
      "#/",
      "#/workspace/1/task/42",
      "#/workspace/1/task/42/",
      "#/workspace/abc/task/42/diff",
      "#/workspace/1/task/abc/diff",
      "#/workspace/1/task/42/not-a-kind",
      "#/workspace/1/task/42/diff/extra",
      "#/workspace/1/task/42/file",
      "#/not/even/close",
    ]) {
      expect(parseRoute(hash)).toBeNull();
    }
  });
});

describe("formatRoute", () => {
  it("round-trips a base-kind route", () => {
    const route: Route = { workspaceId: 1, taskId: 42, tab: { kind: "terminal" } };
    expect(parseRoute(formatRoute(route))).toEqual(route);
  });

  it("round-trips a file route, including a path with spaces and slashes", () => {
    const route: Route = { workspaceId: 1, taskId: 42, tab: { kind: "file", path: "src/a b/c.ts" } };
    expect(parseRoute(formatRoute(route))).toEqual(route);
  });

  it("encodes each path segment rather than the path as one opaque blob", () => {
    const route: Route = { workspaceId: 1, taskId: 42, tab: { kind: "file", path: "a/b" } };
    // Two segments, not one percent-encoded slash -- readable in a real URL bar.
    expect(formatRoute(route)).toBe("#/workspace/1/task/42/file/a/b");
  });
});
