import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import type { Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts -- same helper other component test files use. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/**
 * Fires the reconnect loop's pending backoff timer, wrapped in `act` --
 * unlike reconnect.test.ts (which asserts on the reconnect module in
 * isolation, so the *only* pending timer is ever its own), a full App
 * render tree has other unrelated pending timers too (React's own
 * scheduler, etc.), so advancing to just "the next" timer can fire one of
 * those instead of the reconnect backoff. Advancing by the full 10s max
 * backoff window fires everything due within it -- safe here because,
 * unlike the backoff-growth test in reconnect.test.ts, every reconnect in
 * this file's tests succeeds on the first attempt, so there's no retry
 * cascade to worry about.
 */
async function advanceReconnectTimer(): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
}

/** Resolves socket's nth pending request for method with result, by inspecting what WsClient actually sent over the wire (App.test.tsx drives real WsClient instances, not FakeWsClient, since it needs genuine new-instance-per-reconnect semantics). */
function respond(socket: FakeSocket, method: string, result: unknown, index = 0): void {
  const matches = socket.sent.filter((e) => e.method === method);
  const env = matches[index];
  if (!env?.id) throw new Error(`no ${method} request #${index} sent yet (have ${matches.length})`);
  socket.emit({ id: env.id, result });
}

/** Drives AppSidebar's workspace.list -> {space.list, task.list} sequence for a single workspace with one task, so a task row exists to click. */
async function resolveSidebar(socket: FakeSocket): Promise<void> {
  await flush();
  respond(socket, "workspace.list", [WORKSPACE]);
  await flush();
  respond(socket, "space.list", []);
  respond(socket, "task.list", [TASK]);
  await flush();
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("App", () => {
  it("an initial connect failure shows the disconnected/error state (regression: don't retry silently)", async () => {
    const connect = vi.fn().mockRejectedValue(new Error("no daemon listening"));
    render(<App connect={connect} />);
    await flush();

    expect(screen.getByText(/disconnected: no daemon listening/i)).toBeInTheDocument();
    expect(connect).toHaveBeenCalledTimes(1);
  });

  it("an unexpected disconnect updates the header away from 'Connected to daemon', and a successful reconnect brings it back", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await flush();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();

    socket1.emitClose();
    await flush();
    expect(screen.queryByText("Connected to daemon")).not.toBeInTheDocument();
    expect(screen.getByText(/reconnecting to daemon/i)).toBeInTheDocument();

    await advanceReconnectTimer();
    await flush();
    expect(connect).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();
  });

  it("after a successful reconnect, AppSidebar re-fetches against the new client (proving it received a different client reference, not the same instance mutated in place)", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await resolveSidebar(socket1);
    expect(screen.getByText("Fix the bug")).toBeInTheDocument();

    expect(socket2.sent.some((e) => e.method === "workspace.list")).toBe(false);

    socket1.emitClose();
    await flush();
    await advanceReconnectTimer();
    await flush();

    // useWorkspaceTree's effect is keyed on the `client` reference -- if
    // AppSidebar had been handed back the *same* WsClient instance (just
    // reconnected in place), no new workspace.list would fire at all,
    // since nothing about the effect's dependency would have changed.
    expect(socket2.sent.some((e) => e.method === "workspace.list")).toBe(true);
    expect(socket1.sent.filter((e) => e.method === "workspace.list")).toHaveLength(1);
  });

  it("a task selected before disconnect remains selected after reconnect (selection state isn't thrown away, only data is refetched)", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await resolveSidebar(socket1);

    const row = screen.getByText("Fix the bug");
    fireEvent.click(row);
    await flush();

    // TaskDetailPane mounts for the selected task and issues its own
    // run.list against the pre-disconnect client.
    respond(socket1, "run.list", []);
    await flush();
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();

    socket1.emitClose();
    await flush();
    await advanceReconnectTimer();
    await flush();

    // The task is still selected/rendered after reconnect, and its pane
    // re-fetched fresh against the new client rather than being unmounted.
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(socket2.sent.some((e) => e.method === "run.list")).toBe(true);
  });
});
