import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RetryStartError, retryAnchor, retrySource, useRetryChat } from "@/hooks/use-retry-chat";
import type { RunEntry } from "@/hooks/use-run-timeline";
import type { Chat } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Refactor the parser", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHILD: Chat = { ...CHAT, ID: 11, Title: "Retry of Refactor the parser" };

function entry(id: string, overrides: Partial<RunEntry> = {}): RunEntry {
  return {
    id,
    provider: "claude-native",
    prompt: `prompt ${id}`,
    status: "done",
    startedAt: "2024-01-01T00:00:00Z",
    finishedAt: "2024-01-01T00:00:02Z",
    items: [],
    permissionMode: "",
    autoAccept: false,
    thinkingLevel: "",
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

describe("retryAnchor", () => {
  it("picks the nearest earlier done run", () => {
    const runs = [entry("run-1"), entry("run-2"), entry("run-3")];
    expect(retryAnchor(runs, runs[2])).toBe(runs[1]);
  });

  it("skips errored and stopped runs", () => {
    const runs = [entry("run-1"), entry("run-2", { status: "error" }), entry("run-3", { status: "stopped" }), entry("run-4")];
    expect(retryAnchor(runs, runs[3])).toBe(runs[0]);
  });

  it("skips child_event rows", () => {
    const runs = [entry("run-1"), entry("run-2", { origin: "child_event" }), entry("run-3")];
    expect(retryAnchor(runs, runs[2])).toBe(runs[0]);
  });

  it("is null for the first run", () => {
    const runs = [entry("run-1"), entry("run-2")];
    expect(retryAnchor(runs, runs[0])).toBeNull();
  });
});

describe("useRetryChat", () => {
  it("forks at the anchor with kind retry and starts the edited prompt in the child", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRetryChat(client));
    const runs = [entry("run-1"), entry("run-2")];
    let promise: Promise<Chat> | undefined;
    await act(async () => {
      promise = result.current.retry({
        taskId: 1,
        chat: CHAT,
        runs,
        run: runs[1],
        prompt: "edited prompt",
        mode: "native",
        permission: { permissionMode: "default", autoAccept: false },
      });
      await Promise.resolve();
    });
    expect(client.nth("chat.fork", 0).params).toEqual({ chatId: CHAT.ID, atRunId: "run-1", mode: "native", kind: "retry" });
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ taskId: 1, chatId: CHILD.ID, provider: "claude-native", prompt: "edited prompt", whenBusy: "queue", permissionMode: "default" });
    client.nth("run.start", 0).resolve({ runId: "run-new" });
    await act(async () => {
      await expect(promise).resolves.toBe(CHILD);
    });
  });

  it("without an anchor starts the edited prompt in a fresh chat.create", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRetryChat(client));
    const runs = [entry("run-1")];
    let promise: Promise<Chat> | undefined;
    await act(async () => {
      promise = result.current.retry({
        taskId: 1,
        chat: CHAT,
        runs,
        run: runs[0],
        prompt: "edited",
        mode: "native",
        permission: { permissionMode: "", autoAccept: false },
      });
      await Promise.resolve();
    });
    expect(client.calls.filter((c) => c.method === "chat.fork")).toHaveLength(0);
    expect(client.nth("chat.create", 0).params).toEqual({ taskId: 1 });
    client.nth("chat.create", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ chatId: CHILD.ID, prompt: "edited" });
    client.nth("run.start", 0).resolve({ runId: "run-new" });
    await act(async () => {
      await expect(promise).resolves.toBe(CHILD);
    });
  });

  it("a run.start failure after the chat exists carries the chat on the error", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRetryChat(client));
    const runs = [entry("run-1"), entry("run-2")];
    let promise: Promise<Chat> | undefined;
    await act(async () => {
      promise = result.current.retry({
        taskId: 1,
        chat: CHAT,
        runs,
        run: runs[1],
        prompt: "edited",
        mode: "native",
        permission: { permissionMode: "", autoAccept: false },
      });
      await Promise.resolve();
    });
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    client.nth("run.start", 0).reject(new Error("daemon busy"));
    let thrown: unknown;
    await act(async () => {
      thrown = await promise!.catch((err: unknown) => err);
    });
    expect(thrown).toBeInstanceOf(RetryStartError);
    expect((thrown as Error).message).toBe("The new chat was created but the prompt could not be started: daemon busy");
    expect((thrown as RetryStartError).chat).toBe(CHILD);
  });

  it("re-sends the run's reuse attachments on the new prompt", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRetryChat(client));
    const runs = [
      entry("run-1"),
      entry("run-2", {
        attachments: [{ id: "a1", kind: "image", name: "shot.png", size: 10 }],
      }),
    ];
    await act(async () => {
      void result.current.retry({
        taskId: 1,
        chat: CHAT,
        runs,
        run: runs[1],
        prompt: "edited",
        mode: "native",
        permission: { permissionMode: "", autoAccept: false },
      });
      await Promise.resolve();
    });
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ attachments: [{ kind: "reuse", runId: "run-2", id: "a1" }] });
  });

  it("rejects without a client", async () => {
    const { result } = renderHook(() => useRetryChat(null));
    await act(async () => {
      await expect(
        result.current.retry({
          taskId: 1,
          chat: CHAT,
          runs: [entry("run-1")],
          run: entry("run-1"),
          prompt: "x",
          mode: "native",
          permission: { permissionMode: "", autoAccept: false },
        }),
      ).rejects.toThrow("no daemon connection");
    });
  });
});

describe("retrySource", () => {
  const FORKED: Chat = { ...CHAT, ID: 12, fork: { parentChatId: 7, forkedAtRunId: "parent-run-3", kind: "fork", mode: "native", lossy: false } };
  it("is the chat's own earlier done run when there is one", () => {
    const runs = [entry("own-1"), entry("own-2")];
    expect(retrySource(FORKED, runs, runs[1])).toEqual({ chatId: 12, runId: "own-1" });
  });
  it("in a forked chat's first own turn it is the parent at the fork point, so the inherited history survives", () => {
    const runs = [entry("own-1")];
    expect(retrySource(FORKED, runs, runs[0])).toEqual({ chatId: 7, runId: "parent-run-3" });
  });
  it("is null for the first turn of a chat that is not a fork", () => {
    const runs = [entry("run-1")];
    expect(retrySource(CHAT, runs, runs[0])).toBeNull();
  });
});

describe("useRetryChat in a forked chat", () => {
  it("forks the parent at the fork point with kind retry", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRetryChat(client));
    const chat: Chat = { ...CHAT, ID: 12, fork: { parentChatId: 7, forkedAtRunId: "parent-run-3", kind: "fork", mode: "native", lossy: false } };
    const runs = [entry("own-1")];
    await act(async () => {
      void result.current.retry({ taskId: 1, chat, runs, run: runs[0], prompt: "edited", mode: "native", permission: { permissionMode: "", autoAccept: false } });
      await Promise.resolve();
    });
    expect(client.nth("chat.fork", 0).params).toEqual({ chatId: 7, atRunId: "parent-run-3", mode: "native", kind: "retry" });
  });
});
