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

  it("parses chat route with card", () => {
    expect(parseRoute("#/workspace/1/task/42/chat/7?card=req-9")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "chat", chatId: 7, requestId: "req-9" },
    });
  });

  it("ignores unknown query", () => {
    expect(parseRoute("#/workspace/1/task/42/chat/7?foo=bar&card=req-9")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "chat", chatId: 7, requestId: "req-9" },
    });
    // A query on any other route kind never blocks the route itself.
    expect(parseRoute("#/workspace/1/task/42/diff?card=req-9")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "diff" },
    });
    expect(parseRoute("#/workspace/1/task/42/chat/7?foo=bar")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "chat", chatId: 7 },
    });
  });

  it("ignores a card on a non-chat route", () => {
    expect(parseRoute("#/workspace/1/task/42/terminal?card=req-9")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "terminal" },
    });
    expect(parseRoute("#/workspace/1/task/42/file/a%20b/c.ts?card=req-9")).toEqual({
      workspaceId: 1,
      taskId: 42,
      tab: { kind: "file", path: "a b/c.ts" },
    });
  });

  it("an overlong card id is dropped", () => {
    const hash = `#/workspace/1/task/42/chat/7?card=${"x".repeat(201)}`;
    expect(parseRoute(hash)).toEqual({ workspaceId: 1, taskId: 42, tab: { kind: "chat", chatId: 7 } });
    const atLimit = `#/workspace/1/task/42/chat/7?card=${"x".repeat(200)}`;
    expect(parseRoute(atLimit)).toEqual({ workspaceId: 1, taskId: 42, tab: { kind: "chat", chatId: 7, requestId: "x".repeat(200) } });
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

  it("round-trips the request id", () => {
    const route: Route = { workspaceId: 1, taskId: 42, tab: { kind: "chat", chatId: 7, requestId: "req 9&x" } };
    expect(formatRoute(route)).toBe("#/workspace/1/task/42/chat/7?card=req%209%26x");
    expect(parseRoute(formatRoute(route))).toEqual(route);
    const bare: Route = { workspaceId: 1, taskId: 42, tab: { kind: "chat", chatId: 7 } };
    expect(formatRoute(bare)).toBe("#/workspace/1/task/42/chat/7");
  });

  it("encodes each path segment rather than the path as one opaque blob", () => {
    const route: Route = { workspaceId: 1, taskId: 42, tab: { kind: "file", path: "a/b" } };
    // Two segments, not one percent-encoded slash -- readable in a real URL bar.
    expect(formatRoute(route)).toBe("#/workspace/1/task/42/file/a/b");
  });
});
