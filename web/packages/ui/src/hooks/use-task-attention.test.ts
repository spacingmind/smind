import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useTaskAttention } from "@/hooks/use-task-attention";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { RunStatusValue, RunSummary } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

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

const isoFromNow = (msAgo: number): string => new Date(Date.now() - msAgo).toISOString();

function run(id: string, taskId: number, status: RunStatusValue, startedAt: string, chatId = taskId, finishedAt?: string): RunSummary {
  return {
    ID: id,
    TaskID: taskId,
    ChatID: chatId,
    Provider: "glm",
    Prompt: "do it",
    Status: status,
    StartedAt: startedAt,
    FinishedAt: status === "running" ? null : (finishedAt ?? isoFromNow(0)),
    StopReason: "",
    Err: "",
    PermissionMode: "", AutoAccept: false,
    ThinkingLevel: "",
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

  it("event.dropped re-derives run status from a fresh run.list, the same as a reconnect would", async () => {
    // ADR 0005: the event queue is per-connection, so a drop can just as
    // well have swallowed a run.status this hook would otherwise never
    // correct on its own.
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list", 0).resolve([run("r1", 1, "running", "2024-01-01T00:00:00Z")]);
    await flush();
    client.nth("run.logs", 0).resolve({ events: [] });
    await flush();
    expect(result.current.runStatus.get(1)).toBe("running");

    stub.fire("event.dropped", { count: 1 });
    await flush();

    expect(client.calls.filter((c) => c.method === "run.list")).toHaveLength(2);
    client.nth("run.list", 1).resolve([run("r1", 1, "error", "2024-01-01T00:00:00Z")]);
    await flush();

    expect(result.current.runStatus.get(1)).toBe("error");
  });
});

describe("useTaskAttention loaded", () => {
  it("task-attention-loaded-after-first-run-list: false before the first run.list resolves, true after, false again after a client change", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(({ c }) => useTaskAttention(c, null, null), { initialProps: { c: client as unknown as WsClientLike } });
    expect(result.current.loaded).toBe(false);

    client.nth("run.list").resolve([run("r1", 1, "running", "2024-01-01T00:00:00Z")]);
    await flush();
    client.nth("run.logs").resolve({ events: [] });
    await flush();
    expect(result.current.loaded).toBe(true);

    const next = new FakeWsClient();
    rerender({ c: next as unknown as WsClientLike });
    expect(result.current.loaded).toBe(false);

    next.nth("run.list").resolve([]);
    await flush();
    expect(result.current.loaded).toBe(true);
  });
});

describe("useTaskAttention finished recency window", () => {
  it("task-attention-finished-recent-run-dots: a run finished 1h ago gets the finished dot", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([run("r1", 1, "done", "2024-01-01T00:00:00Z", 1, isoFromNow(60 * 60 * 1000))]);
    await flush();

    expect(result.current.attention.get(1)).toEqual(new Set(["finished"]));
  });

  it("task-attention-finished-old-run-quiet: a run finished 29 days ago dots nothing but still reports runStatus", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([run("r1", 1, "done", "2024-01-01T00:00:00Z", 1, isoFromNow(29 * 24 * 60 * 60 * 1000))]);
    await flush();

    expect(result.current.attention.get(1)).toBeUndefined();
    expect(result.current.runStatus.get(1)).toBe("done");
  });

  it("task-attention-finished-unrecorded-finish-ages-from-start: a terminal run with no FinishedAt ages from StartedAt", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    const old = { ...run("r1", 1, "done", isoFromNow(29 * 24 * 60 * 60 * 1000)), FinishedAt: null };
    const fresh = { ...run("r2", 2, "done", isoFromNow(60 * 1000)), FinishedAt: null };
    client.nth("run.list").resolve([old, fresh]);
    await flush();

    expect(result.current.attention.get(1)).toBeUndefined();
    expect(result.current.attention.get(2)).toEqual(new Set(["finished"]));
  });

  it("task-attention-error-old-run-still-attention: an error run finished 29 days ago still gets the error dot", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([run("r1", 1, "error", "2024-01-01T00:00:00Z", 1, isoFromNow(29 * 24 * 60 * 60 * 1000))]);
    await flush();

    expect(result.current.attention.get(1)).toEqual(new Set(["error"]));
  });

  it("task-attention-live-finish-dots: a live terminal run.status for a non-selected task still dots finished", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([]);
    await flush();

    stub.fire("run.status", { runId: "r1", taskId: 7, status: "running" });
    stub.fire("run.status", { runId: "r1", taskId: 7, status: "done" });

    expect(result.current.attention.get(7)).toEqual(new Set(["finished"]));
    expect(result.current.runStatus.get(7)).toBe("done");
  });
});

describe("useTaskAttention rateLimit (ADR-0041 R3)", () => {
  const limited = (id: string, taskId: number, startedAt: string): RunSummary => ({
    ...run(id, taskId, "error", startedAt),
    FailureKind: "rate_limited",
    FailureWindow: "5h",
    FailureResetAt: "2030-01-01T17:35:00Z",
  });

  it("latest run rate limited exposes rateLimit and keeps status error", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useTaskAttention(client, null, null));
    client.nth("run.list").resolve([limited("new", 1, "2024-01-01T00:05:00Z"), run("old", 1, "done", "2024-01-01T00:00:00Z")]);
    await flush();

    expect(result.current.runStatus.get(1)).toBe("error");
    expect(result.current.rateLimit.get(1)).toEqual({ window: "5h", resetAt: "2030-01-01T17:35:00Z" });
    // The attention rollup still sees an error.
    expect(result.current.attention.get(1)?.has("error")).toBe(true);
  });

  it("a later successful run clears it", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([limited("a", 1, "2024-01-01T00:00:00Z")]);
    await flush();
    expect(result.current.rateLimit.has(1)).toBe(true);

    // run.list rows: a newer done run wins.
    stub.fire("run.status", { runId: "b", taskId: 1, chatId: 1, status: "done" });
    expect(result.current.runStatus.get(1)).toBe("done");
    expect(result.current.rateLimit.has(1)).toBe(false);

    // And a live terminal event carrying the failure sets it again, replacing a prior value.
    stub.fire("run.status", { runId: "c", taskId: 1, chatId: 1, status: "error", failureKind: "rate_limited", window: "weekly", resetAt: "2030-02-01T00:00:00Z" });
    expect(result.current.rateLimit.get(1)).toEqual({ window: "weekly", resetAt: "2030-02-01T00:00:00Z" });
    stub.fire("run.status", { runId: "c", taskId: 1, chatId: 1, status: "running" });
    expect(result.current.rateLimit.has(1)).toBe(false);
  });

  it("plain error exposes none", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([run("e", 1, "error", "2024-01-01T00:00:00Z"), { ...run("x", 2, "error", "2024-01-01T00:00:00Z"), FailureKind: "" }]);
    await flush();
    expect(result.current.runStatus.get(1)).toBe("error");
    expect(result.current.rateLimit.size).toBe(0);
    stub.fire("run.status", { runId: "y", taskId: 3, chatId: 3, status: "error", failureKind: "other" });
    expect(result.current.rateLimit.size).toBe(0);
  });
});

describe("useTaskAttention failure classes (run failure classes)", () => {
  const failed = (id: string, kind: string, label: string): RunSummary => ({
    ...run(id, 1, "error", "2024-01-01T00:05:00Z"),
    FailureKind: kind,
    FailureAction: { kind: "x", label },
  });

  it("exposes the class and the action label for auth, model and transient failures, keeping status error", async () => {
    for (const [kind, label] of [["auth_expired", "Sign in again to GLM"], ["model_not_in_plan", "Pick another model"], ["transient_429", "Retry"]]) {
      const client = new FakeWsClient();
      const { result } = renderHook(() => useTaskAttention(client, null, null));
      client.nth("run.list").resolve([failed("r", kind, label)]);
      await flush();
      expect(result.current.runStatus.get(1)).toBe("error");
      expect(result.current.rateLimit.get(1)).toEqual({ kind, label });
    }
  });

  it("a live terminal event sets it and a later status clears it", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    const { result } = renderHook(() => useTaskAttention(client, null, stub.events));
    client.nth("run.list").resolve([]);
    await flush();
    stub.fire("run.status", { runId: "c", taskId: 1, chatId: 1, status: "error", failureKind: "auth_expired", action: { kind: "sign_in", label: "Sign in again" } });
    expect(result.current.rateLimit.get(1)).toEqual({ kind: "auth_expired", label: "Sign in again" });
    stub.fire("run.status", { runId: "c", taskId: 1, chatId: 1, status: "running" });
    expect(result.current.rateLimit.has(1)).toBe(false);
  });
});
