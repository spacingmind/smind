import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useTaskLineage } from "@/hooks/use-task-lineage";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task, TaskTreeResult } from "@/lib/types";
import type { TaskTreeNode } from "@/lib/task-lineage";

/** Flushes pending microtasks, wrapped in `act` so React commits before assertions. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** A minimal DaemonEvents fake: topic -> listeners, plus an emit helper. */
function fakeEvents(): DaemonEvents & { emit(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    emit(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

function task(partial: Partial<Task> & { ID: number }): Task {
  return {
    WorkspaceID: 1,
    SpaceID: null,
    Title: `task ${partial.ID}`,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2026-10-11T00:00:00Z",
    UpdatedAt: "2026-10-11T00:00:00Z",
    ArchivedAt: null,
    ...partial,
  };
}

function node(partial: Partial<TaskTreeNode> & { id: number }): TaskTreeNode {
  return {
    parentId: null,
    workspaceId: 1,
    title: `node ${partial.id}`,
    depth: 0,
    taskStatus: "active",
    tier: null,
    childCount: 0,
    run: { state: "idle", runningRunIds: [], pendingPermissions: 0 },
    counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    usage: {
      own: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      subtree: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      archivedDescendants: 0,
    },
    ...partial,
  };
}

describe("useTaskLineage", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("loads the parent via task.get and the tree via task.tree", async () => {
    const client = new FakeWsClient();
    const self = task({ ID: 2, ParentTaskID: 1 });
    const { result } = renderHook(() => useTaskLineage(client, self, null));
    expect(client.calls.map((c) => c.method)).toEqual(["task.get", "task.tree"]);

    act(() => {
      client.nth("task.get").resolve(task({ ID: 1, Title: "Parent" }));
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 }), node({ id: 3, parentId: 2 })], truncated: false } satisfies TaskTreeResult);
    });
    await flush();

    expect(result.current.parent?.task.Title).toBe("Parent");
    expect(result.current.parent?.archived).toBe(false);
    expect(result.current.node?.id).toBe(2);
    expect(result.current.children.map((c) => c.id)).toEqual([3]);
  });

  it("marks an archived parent", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskLineage(client, task({ ID: 2, ParentTaskID: 1 }), null));
    act(() => {
      client.nth("task.get").resolve(task({ ID: 1, ArchivedAt: "2026-10-11T00:00:00Z" }));
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false });
    });
    await flush();
    expect(result.current.parent?.archived).toBe(true);
  });

  it("does not call task.get for a root task", () => {
    const client = new FakeWsClient();
    renderHook(() => useTaskLineage(client, task({ ID: 1 }), null));
    expect(client.calls.map((c) => c.method)).toEqual(["task.tree"]);
  });

  it("refetches (debounced) on task.created and applies the fresh tree", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useTaskLineage(client, task({ ID: 2 }), events));
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 }), node({ id: 3, parentId: 2 })], truncated: false });
    });
    await flush();
    expect(result.current.children.map((c) => c.id)).toEqual([3]);

    act(() => {
      events.emit("task.created", { task: task({ ID: 4, ParentTaskID: 2 }) });
    });
    vi.advanceTimersByTime(200);
    expect(client.calls.filter((c) => c.method === "task.tree")).toHaveLength(2);
    act(() => {
      client.nth("task.tree", 1).resolve({
        nodes: [node({ id: 2 }), node({ id: 3, parentId: 2 }), node({ id: 4, parentId: 2 })],
        truncated: false,
      });
    });
    await flush();
    expect(result.current.children.map((c) => c.id)).toEqual([3, 4]);
  });

  it("a failing task.tree keeps the previous value and does not throw", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = renderHook(() => useTaskLineage(client, task({ ID: 2 }), events));
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false });
    });
    await flush();
    expect(result.current.node?.id).toBe(2);

    // A daemon restart that no longer serves task.tree: the debounced
    // refetch's rejection must leave the last good value in place.
    act(() => {
      events.emit("task.created", {});
    });
    vi.advanceTimersByTime(200);
    await act(async () => {
      client.nth("task.tree", 1).reject(new Error("method not found: task.tree"));
    });
    expect(result.current.node?.id).toBe(2);
    consoleError.mockRestore();
  });

  it("refetches on event.dropped", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    renderHook(() => useTaskLineage(client, task({ ID: 2 }), events));
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false });
    });
    await flush();

    act(() => {
      events.emit("event.dropped", {});
    });
    vi.advanceTimersByTime(200);
    expect(client.calls.filter((c) => c.method === "task.tree")).toHaveLength(2);
  });
});
