import { act } from "react";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useRunFailoverIndex } from "@/hooks/use-run-failover-index";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";

describe("useRunFailoverIndex", () => {
  it("loads the task-wide run.list and refetches when a run of the task changes status", async () => {
    const client = new FakeWsClient();
    const listeners = new Map<string, (p: unknown) => void>();
    const events = { subscribe: (t: string, l: (p: unknown) => void) => (listeners.set(t, l), () => listeners.delete(t)) } as unknown as DaemonEvents;
    const { result } = renderHook(() => useRunFailoverIndex(client as never, 1, events));
    expect(client.calls.find((c) => c.method === "run.list")?.params).toEqual({ taskId: 1 });
    await act(async () => {
      client.nth("run.list", 0).resolve([{ ID: "r1", ChatID: 10, Provider: "claude-native" }]);
    });
    expect(result.current.successors.size).toBe(0);

    await act(async () => {
      listeners.get("run.status")?.({ taskId: 2 });
    });
    expect(client.calls.filter((c) => c.method === "run.list")).toHaveLength(1);
    await act(async () => {
      listeners.get("run.status")?.({ taskId: 1 });
    });
    await act(async () => {
      client.nth("run.list", 1).resolve([
        { ID: "r1", ChatID: 10, Provider: "claude-native" },
        { ID: "r2", ChatID: 11, Provider: "glm", FailoverOf: "r1" },
      ]);
    });
    expect(result.current.successors.get("r1")).toEqual({ runId: "r2", chatId: 11, provider: "glm" });
    expect(result.current.continues.get("r2")).toBe("r1");
  });
});
