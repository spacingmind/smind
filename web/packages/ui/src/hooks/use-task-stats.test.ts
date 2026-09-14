import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useTaskStats } from "@/hooks/use-task-stats";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { TaskStat } from "@/lib/types";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Minimal DaemonEvents stub: records listeners per topic, lets the test fire them. */
function makeEventsStub() {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    events: {
      subscribe(topic: string, listener: (payload: unknown) => void): () => void {
        let set = listeners.get(topic);
        if (!set) {
          set = new Set();
          listeners.set(topic, set);
        }
        set.add(listener);
        return () => set!.delete(listener);
      },
    },
    fire(topic: string, payload: unknown): void {
      act(() => {
        for (const l of listeners.get(topic) ?? []) l(payload);
      });
    },
  };
}

function stat(taskId: number, over: Partial<TaskStat> = {}): TaskStat {
  return { taskId, branch: `branch-${taskId}`, filesChanged: 1, insertions: 2, deletions: 0, ...over };
}

describe("useTaskStats", () => {
  it("fetches one task.stats per workspace and keys the result by task", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskStats(client, null, [1, 2]));

    expect(client.calls.filter((c) => c.method === "task.stats")).toHaveLength(2);
    expect(client.nth("task.stats", 0).params).toEqual({ workspaceId: 1 });
    expect(client.nth("task.stats", 1).params).toEqual({ workspaceId: 2 });

    client.nth("task.stats", 0).resolve({ stats: [stat(10)] });
    client.nth("task.stats", 1).resolve({ stats: [stat(20)] });
    await flush();

    expect(result.current.get(10)?.branch).toBe("branch-10");
    expect(result.current.get(20)?.branch).toBe("branch-20");
  });

  it("fetches nothing at all with no client or no workspaces", async () => {
    const client = new FakeWsClient();
    renderHook(() => useTaskStats(client, null, []));
    expect(client.calls).toHaveLength(0);

    const { result } = renderHook(() => useTaskStats(null, null, [1]));
    expect(result.current.size).toBe(0);
  });

  it("leaves a task absent rather than zeroed when the daemon reports no stat for it", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskStats(client, null, [1]));
    client.nth("task.stats", 0).resolve({ stats: [] });
    await flush();

    // Absent, not {filesChanged: 0} -- "no changes" and "not known" are
    // different claims and the row renders them differently.
    expect(result.current.get(10)).toBeUndefined();
  });

  it("refetches only the finished run's workspace when a run reaches a terminal state", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    renderHook(() => useTaskStats(client, stub.events, [1, 2]));
    client.nth("task.stats", 0).resolve({ stats: [stat(10)] });
    client.nth("task.stats", 1).resolve({ stats: [stat(20)] });
    await flush();
    expect(client.calls.filter((c) => c.method === "task.stats")).toHaveLength(2);

    stub.fire("run.status", { runId: "r1", taskId: 10, status: "done" });
    await flush();

    const calls = client.calls.filter((c) => c.method === "task.stats");
    expect(calls).toHaveLength(3);
    expect(calls[2]!.params).toEqual({ workspaceId: 1 });
  });

  it("does not refetch when a run merely starts -- nothing has changed on disk yet", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    renderHook(() => useTaskStats(client, stub.events, [1]));
    client.nth("task.stats", 0).resolve({ stats: [stat(10)] });
    await flush();

    stub.fire("run.status", { runId: "r1", taskId: 10, status: "running" });
    await flush();

    expect(client.calls.filter((c) => c.method === "task.stats")).toHaveLength(1);
  });

  it("ignores a terminal run for a task it has no workspace mapping for", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    renderHook(() => useTaskStats(client, stub.events, [1]));
    client.nth("task.stats", 0).resolve({ stats: [stat(10)] });
    await flush();

    stub.fire("run.status", { runId: "r1", taskId: 999, status: "done" });
    stub.fire("run.status", null);
    await flush();

    expect(client.calls.filter((c) => c.method === "task.stats")).toHaveLength(1);
  });

  it("drops a task's stale stat when a refetch stops reporting it", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskStats(client, stub.events, [1]));
    client.nth("task.stats", 0).resolve({ stats: [stat(10), stat(11)] });
    await flush();
    expect(result.current.size).toBe(2);

    // Task 11's worktree went away; the refetch simply omits it.
    stub.fire("run.status", { runId: "r1", taskId: 10, status: "done" });
    await flush();
    client.nth("task.stats", 1).resolve({ stats: [stat(10, { filesChanged: 7 })] });
    await flush();

    expect(result.current.get(10)?.filesChanged).toBe(7);
    expect(result.current.get(11)).toBeUndefined();
  });

  it("a failed fetch leaves the map empty rather than throwing", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskStats(client, null, [1]));
    client.nth("task.stats", 0).reject(new Error("boom"));
    await flush();

    expect(result.current.size).toBe(0);
  });
});
