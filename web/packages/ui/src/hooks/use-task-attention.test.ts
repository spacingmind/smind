import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useTaskAttention } from "@/hooks/use-task-attention";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { RunStatusValue, RunSummary } from "@/lib/types";

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

function run(id: string, taskId: number, status: RunStatusValue, startedAt: string): RunSummary {
  return {
    ID: id,
    TaskID: taskId,
    Provider: "glm",
    Prompt: "do it",
    Status: status,
    StartedAt: startedAt,
    FinishedAt: status === "running" ? null : startedAt,
    StopReason: "",
    Err: "",
  };
}

/**
 * The runStatus half of useTaskAttention's return -- what the sidebar's
 * leading run dot and its container rows' aggregate are drawn from
 * (ui-redesign-parity Item 12). The attention half is exercised end to end
 * through App.test.tsx.
 */
describe("useTaskAttention runStatus", () => {
  it("is empty with no client, and for a task that has never run", async () => {
    const { result: noClient } = renderHook(() => useTaskAttention(null, null, null));
    expect(noClient.current.runStatus.size).toBe(0);

    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([]);
    await flush();
    expect(result.current.runStatus.get(1)).toBeUndefined();
  });

  it("reports each task's latest run, using run.list's newest-first order", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    // internal/runs.Registry.List sorts StartedAt descending, so the older
    // run is second here and must not win.
    client.nth("run.list").resolve([
      run("new", 1, "error", "2024-01-01T00:05:00Z"),
      run("old", 1, "done", "2024-01-01T00:00:00Z"),
    ]);
    await flush();

    expect(result.current.runStatus.get(1)).toBe("error");
  });

  it("lets a still-running run win over a later-started one that already finished", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([
      run("newer", 1, "done", "2024-01-01T00:05:00Z"),
      run("still-going", 1, "running", "2024-01-01T00:00:00Z"),
    ]);
    await flush();
    // The hook checks every running run for an unresolved permission
    // before it publishes; leaving that pending would stall the rollup.
    client.nth("run.logs").resolve({ events: [] });
    await flush();

    expect(result.current.runStatus.get(1)).toBe("running");
  });

  it("follows live run.status transitions without a refetch", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([]);
    await flush();

    stub.fire("run.status", { runId: "r1", taskId: 7, status: "running" });
    expect(result.current.runStatus.get(7)).toBe("running");

    stub.fire("run.status", { runId: "r1", taskId: 7, status: "done" });
    expect(result.current.runStatus.get(7)).toBe("done");

    // A second run for the same task supersedes the first once it starts.
    stub.fire("run.status", { runId: "r2", taskId: 7, status: "running" });
    expect(result.current.runStatus.get(7)).toBe("running");

    expect(client.calls.filter((c) => c.method === "run.list")).toHaveLength(1);
  });

  it("ranks a live event above every run in the snapshot it arrives after", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([run("snapshot", 3, "done", "2024-01-01T00:09:00Z")]);
    await flush();
    expect(result.current.runStatus.get(3)).toBe("done");

    stub.fire("run.status", { runId: "later", taskId: 3, status: "error" });
    expect(result.current.runStatus.get(3)).toBe("error");
  });

  it("ignores a malformed run.status payload rather than recording a bogus status", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([]);
    await flush();

    stub.fire("run.status", { runId: 5, taskId: "x" });
    stub.fire("run.status", null);
    expect(result.current.runStatus.size).toBe(0);
  });
});
