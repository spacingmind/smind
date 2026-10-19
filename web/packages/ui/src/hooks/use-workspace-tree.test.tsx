import { act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { ConnectionClosedError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const WORKSPACE = { ID: 1, Path: "/a", Title: "A", RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" };

async function flush() {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

function mount() {
  const ws = new FakeWsClient();
  const daemon = new FakeUsageDaemon([], ws);
  const view = renderHookWithDaemon(() => useWorkspaceTree(), daemon, { initialProps: undefined });
  return { ws, view };
}

async function load(ws: FakeWsClient) {
  await flush();
  ws.nth("workspace.list").resolve([WORKSPACE]);
  await flush();
  ws.nth("space.list").resolve([]);
  ws.nth("task.list").resolve([]);
  await flush();
}

describe("useWorkspaceTree", () => {
  it("is null until the first fetch lands, then the tree", async () => {
    const { ws, view } = mount();
    expect(view.result.current.workspaces).toBeNull();
    await load(ws);
    expect(view.result.current.workspaces?.map((w) => w.ID)).toEqual([1]);
    expect(view.result.current.error).toBeNull();
  });

  it("a first load that fails shows its message", async () => {
    const { ws, view } = mount();
    await flush();
    ws.nth("workspace.list").reject(new Error("database is locked"));
    await flush();
    expect(view.result.current.error).toBe("database is locked");
  });

  it("a first load cut short by a reconnect (the socket closed under it) is not shown as an error", async () => {
    const { ws, view } = mount();
    await flush();
    ws.nth("workspace.list").reject(new ConnectionClosedError("wsclient: connection closed"));
    await flush();
    expect(view.result.current.error).toBeNull();
    expect(view.result.current.workspaces).toBeNull();
  });

  it("refresh re-reads in the background: the tree stays on screen, a failed re-read is logged and keeps the stale tree", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const { ws, view } = mount();
    await load(ws);
    act(() => view.result.current.refresh());
    await flush();
    // The tree is still there while the re-read is in flight.
    expect(view.result.current.workspaces?.map((w) => w.ID)).toEqual([1]);
    ws.nth("workspace.list", 1).reject(new Error("boom"));
    await flush();
    expect(view.result.current.workspaces?.map((w) => w.ID)).toEqual([1]);
    expect(consoleError).toHaveBeenCalledWith("workspace tree re-read failed", expect.any(Error));
    consoleError.mockRestore();
  });
});
