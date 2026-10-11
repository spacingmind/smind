import { act } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useRunTimeline } from "@/hooks/use-run-timeline";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { RunLogsResult, RunSummary, RunUsage } from "@/lib/types";

function usage(overrides: Partial<RunUsage> = {}): RunUsage {
  return { inputTokens: 100, outputTokens: 20, source: "claude_result", ...overrides };
}

function summary(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: 1,
    ChatID: 10,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useRunTimeline usage", () => {
  it("run.list usage seeds the run entry", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ usage: usage({ inputTokens: 5 }) })]);
    await flush();

    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 5 }));
  });

  it("a live usage event replaces the run's usage instead of adding to it", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ Status: "running", FinishedAt: null, usage: usage({ inputTokens: 10, outputTokens: 2 }) })]);
    await flush();
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 10, outputTokens: 2 }));

    await act(async () => {
      client.emit("run.attach", 0, "usage", { usage: usage({ inputTokens: 30, outputTokens: 4 }) });
    });
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 30, outputTokens: 4 }));

    await act(async () => {
      client.emit("run.attach", 0, "usage", { usage: usage({ inputTokens: 40, outputTokens: 5 }) });
    });
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 40, outputTokens: 5 }));

    // A usage event still renders no transcript row.
    expect(result.current.runs?.[0]?.items).toHaveLength(0);
  });

  it("the latest usage event of a run.logs backfill wins", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ usage: usage({ inputTokens: 1 }) })]);
    await flush();

    await act(async () => {
      client.nth("run.logs", 0).resolve({
        runId: "run-1",
        status: "done",
        stopReason: "end_turn",
        events: [
          { type: "usage", usage: usage({ inputTokens: 7, outputTokens: 1 }) },
          { type: "usage", usage: usage({ inputTokens: 9, outputTokens: 2 }) },
        ],
      } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 9, outputTokens: 2 })));
  });

  it("a run without a usage row has undefined usage", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary()]);
    await flush();

    await act(async () => {
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", events: [] } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.items).toHaveLength(0));
    expect(result.current.runs?.[0]?.usage).toBeUndefined();
    expect("usage" in (result.current.runs?.[0] ?? {})).toBe(false);
  });
});
