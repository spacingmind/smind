import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useRunTimeline, buildTimeline } from "@/hooks/use-run-timeline";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { RunSummary } from "@/lib/types";

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

function summary(partial: Partial<RunSummary> & { ID: string }): RunSummary {
  return {
    TaskID: 1,
    ChatID: 10,
    Provider: "claude-native",
    Prompt: `prompt ${partial.ID}`,
    Status: "running",
    StartedAt: "2026-10-11T00:00:00Z",
    FinishedAt: null,
    StopReason: "",
    Err: "",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
    ...partial,
  };
}

/** Resolves the client's pending run.list, wrapped in act. */
function resolveList(client: FakeWsClient, index: number, runs: RunSummary[]): void {
  act(() => {
    client.nth("run.list", index).resolve(runs);
  });
}

describe("useRunTimeline", () => {
  it("submitPrompt does not append a run when the server queued it", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    resolveList(client, 0, []);
    await flush();

    let submitted: Promise<{ kind: "started"; runId: string } | { kind: "queued"; queueItemId: number }> | undefined;
    let resolved: unknown;
    act(() => {
      submitted = result.current.submitPrompt("claude-native", "later, please");
      void submitted!.then((r) => (resolved = r));
    });

    const start = client.calls.find((c) => c.method === "run.start")!;
    expect(start.params).toMatchObject({ whenBusy: "queue" });
    act(() => {
      start.resolve({ queued: true, queueItemId: 7 });
    });
    await flush();

    expect(resolved).toEqual({ kind: "queued", queueItemId: 7 });
    // No run entry, and no run.attach for a run that doesn't exist yet.
    expect(result.current.runs).toEqual([]);
    expect(client.calls.some((c) => c.method === "run.attach")).toBe(false);
  });

  it("keeps detail on the pending permission from a live event and from run.logs", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const detail = { kind: "write" as const, path: "a.sql", replacesExisting: true, largeShrink: true, newLines: 7, existingLines: 240 };
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, events));
    resolveList(client, 0, []);
    await flush();
    act(() => {
      events.emit("run.status", { runId: "run-d", taskId: 1, chatId: 10, status: "running" });
    });
    resolveList(client, 1, [summary({ ID: "run-d", Status: "running" })]);
    await flush();
    await waitFor(() => expect(client.calls.some((c) => c.method === "run.attach")).toBe(true));

    // Live event.
    act(() => {
      client.emit("run.attach", 0, "permission_request", { requestId: "r1", summary: "Write file", options: [], detail });
    });
    expect(result.current.runs?.[0].pendingPermission?.detail).toEqual(detail);

    // Backfilled run.logs-shaped entry replayed through the same attach path
    // (a finished run's run.logs never leaves a pending permission).
    act(() => {
      client.emit("run.attach", 0, "permission_request", { requestId: "r2", summary: "Write file", options: [], detail: { ...detail, path: "b.sql" } });
    });
    expect(result.current.runs?.[0].pendingPermission?.detail?.path).toBe("b.sql");
  });

  it("adopts and streams a server-delivered run from run.status", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, events));
    resolveList(client, 0, []);
    await flush();

    act(() => {
      events.emit("run.status", { runId: "run-x", taskId: 1, chatId: 10, status: "running" });
    });
    resolveList(client, 1, [summary({ ID: "run-x", Status: "running" })]);
    await flush();

    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-x"]);
    await waitFor(() => {
      expect(client.calls.filter((c) => c.method === "run.attach").map((c) => c.params)).toEqual([{ runId: "run-x" }]);
    });
  });

  it("run.status racing the run.start response does not duplicate the run", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, events));
    resolveList(client, 0, []);
    await flush();

    let submitted: Promise<unknown> | undefined;
    act(() => {
      submitted = result.current.submitPrompt("claude-native", "go");
    });

    // run.status for the run lands BEFORE the run.start response.
    act(() => {
      events.emit("run.status", { runId: "run-r", taskId: 1, chatId: 10, status: "running" });
    });
    resolveList(client, 1, [summary({ ID: "run-r", Status: "running" })]);
    await flush();

    const start = client.calls.find((c) => c.method === "run.start")!;
    act(() => {
      start.resolve({ runId: "run-r" });
    });
    await flush();
    await submitted;

    // One entry, one attach -- despite both paths naming run-r.
    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-r"]);
    expect(client.calls.filter((c) => c.method === "run.attach")).toHaveLength(1);
  });


  it("adoption retries when run.list did not yet contain the run", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, events));
    resolveList(client, 0, []);
    await flush();

    act(() => {
      events.emit("run.status", { runId: "run-x", taskId: 1, chatId: 10, status: "running" });
    });
    // The adoption fetch races the server: the run isn't in run.list yet.
    resolveList(client, 1, []);
    await flush();
    expect(result.current.runs).toEqual([]);

    // A later run.status for the same run must not be blocked forever.
    act(() => {
      events.emit("run.status", { runId: "run-x", taskId: 1, chatId: 10, status: "running" });
    });
    resolveList(client, 2, [summary({ ID: "run-x", Status: "running" })]);
    await flush();

    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-x"]);
  });

  it("submitPrompt resolves with the server result when the chat was switched mid-flight", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(
      ({ chatId }) => useRunTimeline(client, 1, chatId),
      { initialProps: { chatId: 10 as number | null } },
    );
    resolveList(client, 0, []);
    await flush();

    let submitted: Promise<unknown> | undefined;
    act(() => {
      submitted = result.current.submitPrompt("claude-native", "switching away");
    });
    rerender({ chatId: 11 });

    const start = client.calls.find((c) => c.method === "run.start")!;
    let resolved: unknown;
    await act(async () => {
      resolved = await start.resolve({ queued: true, queueItemId: 3 }) ?? undefined;
    });
    // Resolve through the actual promise, not the fake's return.
    resolved = await submitted;

    expect(resolved).toEqual({ kind: "queued", queueItemId: 3 });
    // No run appended anywhere -- the new selection starts at null (its
    // own list still in flight) and never sees chat 10's run.
    expect(result.current.runs === null || result.current.runs.length === 0).toBe(true);
  });

  it("initial load racing an adopted run does not duplicate or double-attach it", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, events));

    // The initial run.list is still pending when run.status adopts a run.
    act(() => {
      events.emit("run.status", { runId: "run-a", taskId: 1, chatId: 10, status: "running" });
    });
    resolveList(client, 1, [summary({ ID: "run-a", Status: "running" })]);
    await flush();
    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-a"]);

    // The initial answer now arrives listing both the older run and the
    // adopted one -- only the older is new, nothing is replaced.
    resolveList(client, 0, [summary({ ID: "run-0", StartedAt: "2026-10-10T00:00:00Z", Status: "done" })]);
    await flush();

    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-0", "run-a"]);
    expect(client.calls.filter((c) => c.method === "run.attach").map((c) => c.params)).toEqual([{ runId: "run-a" }]);
  });

  it("retryWithHigherEffort still starts a new run with the next tier", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    resolveList(client, 0, []);
    await flush();

    let retried: Promise<unknown> | undefined;
    act(() => {
      retried = result.current.retryWithHigherEffort({
        id: "run-e",
        provider: "claude-native",
        prompt: "hard problem",
        status: "error",
        startedAt: "2026-10-11T00:00:00Z",
        items: [],
        permissionMode: "",
        autoAccept: false,
        thinkingLevel: "off",
      });
    });

    const starts = client.calls.filter((c) => c.method === "run.start");
    expect(starts[0]!.params).toMatchObject({ prompt: "hard problem", thinkingLevel: "standard" });
    act(() => {
      starts[0]!.resolve({ runId: "run-e2" });
    });
    await flush();
    await retried;

    expect(result.current.runs?.map((r) => r.id)).toEqual(["run-e2"]);
    expect(client.calls.filter((c) => c.method === "run.attach").map((c) => c.params)).toEqual([{ runId: "run-e2" }]);
  });
});

describe("buildTimeline plan items", () => {
  const plan = (entries: unknown) => ({ type: "raw", kind: "plan", payload: { sessionUpdate: "plan", entries } }) as RunLogEvent;

  it("plan raw event becomes a plan item", () => {
    const items = buildTimeline([plan([{ content: "Read the code", status: "in_progress", priority: "high" }, { content: "Fix it" }])]);
    expect(items).toEqual([
      {
        kind: "plan",
        id: "plan-0",
        entries: [
          { content: "Read the code", status: "in_progress", priority: "high" },
          { content: "Fix it", status: "pending", priority: "medium" },
        ],
      },
    ]);
  });

  it("a later plan update replaces the plan in place", () => {
    const v1 = [{ content: "Explore", status: "completed", priority: "high" }];
    const v2 = [
      { content: "Explore", status: "completed", priority: "high" },
      { content: "Write tests", status: "in_progress", priority: "low" },
    ];
    const items = buildTimeline([
      { type: "chunk", text: "on it" } as RunLogEvent,
      plan(v1),
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "ls", status: "success" } as RunLogEvent,
      plan(v2),
    ]);

    expect(items.filter((i) => i.kind === "plan")).toHaveLength(1);
    expect(items[1]).toEqual({ kind: "plan", id: "plan-1", entries: v2 });
  });

  it("invalid plan entries are dropped", () => {
    const items = buildTimeline([plan([{ content: "", status: "pending", priority: "high" }, { content: 7 }, "nope", { content: "Only this one" }])]);
    expect(items).toEqual([
      { kind: "plan", id: "plan-0", entries: [{ content: "Only this one", status: "pending", priority: "medium" }] },
    ]);
  });

  it("plan with no valid entries falls back to unknown", () => {
    const items = buildTimeline([plan([]), plan([{ content: "" }]), { type: "raw", kind: "plan", payload: { sessionUpdate: "plan" } } as RunLogEvent]);
    expect(items.map((i) => i.kind)).toEqual(["unknown", "unknown", "unknown"]);
    expect(items[0]).toMatchObject({ eventType: "raw", rawKind: "plan", rawPayload: { sessionUpdate: "plan", entries: [] } });
  });

  it("other raw kinds stay unknown", () => {
    const items = buildTimeline([{ type: "raw", kind: "some_future_kind", payload: { a: 1 } } as RunLogEvent]);
    expect(items).toEqual([{ kind: "unknown", id: "unknown-0", eventType: "raw", rawKind: "some_future_kind", rawPayload: { a: 1 } }]);
  });
});

// Referenced so vi stays imported even if unused in some configurations.
void vi;
