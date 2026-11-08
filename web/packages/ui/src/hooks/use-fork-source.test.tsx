import { act } from "react";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { inheritedUpTo, useForkSource } from "@/hooks/use-fork-source";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Chat, RunSummary, Task } from "@/lib/types";

const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const PARENT: Chat = { ID: 9, TaskID: 1, Title: "Original chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

function forked(overrides: Partial<NonNullable<Chat["fork"]>>): Chat {
  return { ...PARENT, ID: 10, fork: { parentChatId: PARENT.ID, forkedAtRunId: "run-2", kind: "fork", mode: "native", lossy: false, ...overrides } };
}

function summary(id: string): RunSummary {
  return {
    ID: id,
    TaskID: 1,
    ChatID: PARENT.ID,
    Provider: "claude-native",
    Prompt: `prompt ${id}`,
    Status: "done",
    StartedAt: `2024-01-01T00:00:0${id.slice(-1)}Z`,
    FinishedAt: `2024-01-01T00:00:0${id.slice(-1)}Z`,
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("inheritedUpTo", () => {
  it("slices up to and including the fork point", () => {
    const runs = [{ id: "a" }, { id: "b" }, { id: "c" }] as never[];
    expect(inheritedUpTo(runs, "b").map((r) => r.id)).toEqual(["a", "b"]);
    expect(inheritedUpTo(runs, "a").map((r) => r.id)).toEqual(["a"]);
    expect(inheritedUpTo(runs, "missing")).toEqual([]);
  });
});

describe("useForkSource", () => {
  it("a native fork inherits the parent's runs up to the fork point, with the turn number", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useForkSource(client, TASK, forked({})));

    expect(result.current.loading).toBe(true);
    expect(result.current.inherited).toBeNull();
    client.nth("run.list", 0).resolve([summary("run-1"), summary("run-2"), summary("run-3")]);
    await flush();

    expect(result.current.loading).toBe(false);
    expect(result.current.inherited?.map((r) => r.id)).toEqual(["run-1", "run-2"]);
    expect(result.current.turn).toBe(2);
    expect(result.current.seed).toBeNull();
    expect(client.calls.filter((c) => c.method === "chat.forkSeed")).toEqual([]);
  });

  it("a fork point not in the parent's runs inherits nothing and has no turn", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useForkSource(client, TASK, forked({ forkedAtRunId: "run-x" })));

    client.nth("run.list", 0).resolve([summary("run-1")]);
    await flush();

    expect(result.current.inherited).toEqual([]);
    expect(result.current.turn).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it("a seeded fork fetches chat.forkSeed once for its chat id", async () => {
    const client = new FakeWsClient();
    const chat = forked({ mode: "seeded", lossy: true });
    const { result } = renderHook(() => useForkSource(client, TASK, chat));

    client.nth("run.list", 0).resolve([summary("run-1"), summary("run-2")]);
    await flush();
    // The seed is still in flight: the summary view stays in its loading state.
    expect(result.current.loading).toBe(true);
    expect(result.current.seed).toBeNull();

    client.nth("chat.forkSeed", 0).resolve({ seed: "User: do it\nAssistant: done" });
    await flush();

    expect(client.nth("chat.forkSeed", 0).params).toEqual({ chatId: chat.ID });
    expect(result.current.seed).toBe("User: do it\nAssistant: done");
    // The native turns are not the seed's story; the turn number still loads.
    expect(result.current.turn).toBe(2);
    expect(result.current.loading).toBe(false);
  });

  it("a chat that is not a fork loads nothing and calls nothing", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useForkSource(client, TASK, PARENT));
    await flush();

    expect(result.current).toEqual({ inherited: null, turn: null, seed: null, loading: false });
    expect(client.calls).toEqual([]);
  });
});
