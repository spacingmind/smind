import { act } from "react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { TaskDetailPane } from "@/components/task-detail";
import { limitedProvidersKey } from "@/lib/rate-limit-providers";
import type { Chat, RunLogsResult, RunSummary, Task } from "@/lib/types";

// The composer toolbar persists its run-config state per task id; these
// tests share TASK_A.ID, so a leftover value from one would leak into the
// next.
afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

const TASK_A: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const CHAT_A: Chat = {
  ID: 10,
  TaskID: TASK_A.ID,
  Title: "Chat",
  Provider: null,
  AgentSession: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

function doneRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK_A.ID,
    ChatID: CHAT_A.ID,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "acceptEdits",
    AutoAccept: false,
    ThinkingLevel: "",
    ...overrides,
  };
}

/** The reset instant rateLimitedRun carries: one hour ahead of now. */
function futureResetAt(): string {
  return new Date(Date.now() + 3600_000).toISOString();
}

/** A run the daemon just classified as rate limited, with the reset still an hour ahead. */
function rateLimitedRun(): RunSummary {
  return doneRun({
    Status: "error",
    StopReason: "",
    Err: "limit",
    Provider: "glm",
    FailureKind: "rate_limited",
    FailureWindow: "5h",
    FailureResetAt: futureResetAt(),
  });
}

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Spies on every QueryClient's invalidateQueries without performing the invalidation. */
function spyOnInvalidate() {
  return vi.spyOn(QueryClient.prototype, "invalidateQueries").mockImplementation(async () => {});
}

/** How many of the spy's calls targeted the provider.list key. */
function providerListInvalidations(spy: ReturnType<typeof spyOnInvalidate>): number {
  return spy.mock.calls.filter(([filters]) => (filters as { queryKey?: readonly string[] } | undefined)?.queryKey?.[0] === "provider.list").length;
}

describe("limitedProvidersKey", () => {
  const now = new Date("2026-10-30T12:00:00Z");
  const reset = "2026-10-30T13:00:00Z";

  it("is the provider of a rate-limited run whose reset is still ahead", () => {
    expect(limitedProvidersKey([{ status: "error", failureKind: "rate_limited", provider: "glm", failureResetAt: reset }], now)).toBe("glm");
  });

  it("is empty without a reset, once the reset has passed, and for a non-rate-limited run", () => {
    expect(limitedProvidersKey([{ status: "error", failureKind: "rate_limited", provider: "glm" }], now)).toBe("");
    expect(limitedProvidersKey([{ status: "error", failureKind: "rate_limited", provider: "glm", failureResetAt: "2026-10-30T11:00:00Z" }], now)).toBe("");
    expect(limitedProvidersKey([{ status: "error", failureKind: "auth", provider: "glm", failureResetAt: reset }], now)).toBe("");
    expect(limitedProvidersKey([{ status: "done", provider: "glm" }], now)).toBe("");
    expect(limitedProvidersKey(null, now)).toBe("");
  });

  it("sorts and joins several limited providers into one stable key", () => {
    expect(
      limitedProvidersKey(
        [
          { status: "error", failureKind: "rate_limited", provider: "glm", failureResetAt: reset },
          { status: "error", failureKind: "rate_limited", provider: "claude-native", failureResetAt: reset },
        ],
        now,
      ),
    ).toBe("claude-native,glm");
  });
});

describe("TaskDetailPane provider.list refresh on rate limit", () => {
  it("invalidates the provider.list query once a rate-limited run with a future reset arrives", async () => {
    const spy = spyOnInvalidate();
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([rateLimitedRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "error",
      failureKind: "rate_limited",
      window: "5h",
      resetAt: futureResetAt(),
      events: [],
    } satisfies RunLogsResult);
    await flush();

    expect(spy).toHaveBeenCalledWith({ queryKey: ["provider.list"] });
    // The key does not change again, so the effect fired once -- not per render.
    expect(providerListInvalidations(spy)).toBe(1);
  });

  it("never invalidates provider.list for a non-limited run", async () => {
    const spy = spyOnInvalidate();
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", events: [] } satisfies RunLogsResult);
    await flush();

    expect(providerListInvalidations(spy)).toBe(0);
  });
});
