import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useChatQueue } from "@/hooks/use-chat-queue";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { ChatQueueItem } from "@/lib/types";

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

function item(partial: Partial<ChatQueueItem> & { ID: number }): ChatQueueItem {
  return {
    ChatID: 10,
    Prompt: `prompt ${partial.ID}`,
    RunConfig: "{}",
    Source: "human",
    FromTaskID: null,
    FromChatID: null,
    Priority: 0,
    Status: "queued",
    RunID: null,
    CancelReason: "",
    CreatedAt: "2026-10-11T00:00:00Z",
    UpdatedAt: "2026-10-11T00:00:00Z",
    ...partial,
  };
}

/** Resolves the client's pending chat.queueList with items, wrapped in act. */
function resolveList(client: FakeWsClient, index: number, items: ChatQueueItem[]): void {
  act(() => {
    client.nth("chat.queueList", index).resolve({ items });
  });
}

describe("useChatQueue", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("loads initial items from chat.queueList (reload restores the queue)", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    expect(client.calls.map((c) => c.method)).toContain("chat.queueList");

    resolveList(client, 0, [item({ ID: 1 }), item({ ID: 2 })]);
    await flush();

    expect(result.current.items.map((i) => i.ID)).toEqual([1, 2]);
  });

  it("replaces the list from a chat.queueUpdated snapshot for its chat", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useChatQueue(client, events, 10));
    resolveList(client, 0, [item({ ID: 1 })]);
    await flush();

    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [item({ ID: 1 }), item({ ID: 3 })] });
    });
    expect(result.current.items.map((i) => i.ID)).toEqual([1, 3]);

    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [item({ ID: 3 })] });
    });
    expect(result.current.items.map((i) => i.ID)).toEqual([3]);
  });

  it("ignores chat.queueUpdated for another chat", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useChatQueue(client, events, 10));
    resolveList(client, 0, [item({ ID: 1 })]);
    await flush();

    act(() => {
      events.emit("chat.queueUpdated", { chatId: 99, items: [item({ ID: 7, ChatID: 99 })] });
    });
    expect(result.current.items.map((i) => i.ID)).toEqual([1]);
  });

  it("orders by priority desc then id", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    resolveList(client, 0, [
      item({ ID: 5 }),
      item({ ID: 2, Priority: 1 }),
      item({ ID: 1, Priority: 1 }),
      item({ ID: 4 }),
    ]);
    await flush();

    expect(result.current.items.map((i) => i.ID)).toEqual([1, 2, 4, 5]);
  });

  it("hides delivered items", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    resolveList(client, 0, [
      item({ ID: 1 }),
      item({ ID: 2, Status: "delivered", RunID: "run-1" }),
    ]);
    await flush();

    expect(result.current.items.map((i) => i.ID)).toEqual([1]);
  });

  it("shows an auto-cancelled item with its reason for 10s then drops it", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    resolveList(client, 0, [
      item({ ID: 1, Status: "cancelled", CancelReason: "delivery failed: queue is full" }),
    ]);
    await flush();

    expect(result.current.items).toHaveLength(1);
    expect(result.current.items[0]!.CancelReason).toBe("delivery failed: queue is full");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(result.current.items).toHaveLength(0);
  });

  it("does not show a user-cancelled item", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    resolveList(client, 0, [item({ ID: 1, Status: "cancelled", CancelReason: "cancelled by user" })]);
    await flush();

    expect(result.current.items).toHaveLength(0);
  });

  it("ignores a stale chat.queueList response after a chat switch", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(({ chatId }) => useChatQueue(client, null, chatId), {
      initialProps: { chatId: 10 as number | null },
    });

    // Switch before the first fetch answers; the old chat's response
    // must not leak into the new chat's (still empty) view.
    rerender({ chatId: 11 });
    resolveList(client, 0, [item({ ID: 1 })]);
    await flush();
    expect(result.current.items).toHaveLength(0);

    resolveList(client, 1, [item({ ID: 9, ChatID: 11 })]);
    await flush();
    expect(result.current.items.map((i) => i.ID)).toEqual([9]);
  });

  it("ignores a stale queueList response from refresh() after a chat switch", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(({ chatId }) => useChatQueue(client, null, chatId), {
      initialProps: { chatId: 10 as number | null },
    });

    // refresh() while on chat 10, then switch to 11 before it answers.
    act(() => {
      result.current.refresh();
    });
    rerender({ chatId: 11 });
    await flush();

    // The stale call (chat 10's) resolves last and must be ignored;
    // chat 11's own fetch says empty.
    resolveList(client, 0, [item({ ID: 1 })]);
    resolveList(client, 1, []);
    await flush();
    expect(result.current.items).toHaveLength(0);
  });

  it("a queueList response older than a newer chat.queueUpdated snapshot is ignored", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useChatQueue(client, events, 10));

    // A refresh is in flight when a newer snapshot lands with the truth.
    act(() => {
      result.current.refresh();
    });
    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [item({ ID: 3 })] });
    });
    resolveList(client, 1, []);
    await flush();

    // The older, emptier queueList answer must not overwrite item 3.
    expect(result.current.items.map((i) => i.ID)).toEqual([3]);
  });

  it("refetches on a new client and on event.dropped", async () => {
    const client1 = new FakeWsClient();
    const client2 = new FakeWsClient();
    const events = fakeEvents();
    const { rerender } = renderHook(({ client }) => useChatQueue(client, events, 10), {
      initialProps: { client: client1 as FakeWsClient | null },
    });
    resolveList(client1, 0, []);
    await flush();

    rerender({ client: client2 });
    await flush();
    expect(client2.calls.some((c) => c.method === "chat.queueList")).toBe(true);

    act(() => {
      events.emit("event.dropped", {});
    });
    expect(client2.calls.filter((c) => c.method === "chat.queueList")).toHaveLength(2);
  });

  it("cancel calls chat.queueCancel and leaves the row until the snapshot", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { result } = renderHook(() => useChatQueue(client, events, 10));
    resolveList(client, 0, [item({ ID: 1 })]);
    await flush();

    let cancelled: Promise<void> | undefined;
    act(() => {
      cancelled = result.current.cancel(1);
    });
    const call = client.calls.find((c) => c.method === "chat.queueCancel");
    expect(call?.params).toEqual({ itemId: 1 });
    // The queued response resolves but no snapshot has arrived: the row
    // stays -- removal is the snapshot's job, never optimistic.
    act(() => {
      call!.resolve({});
    });
    await flush();
    expect(result.current.items.map((i) => i.ID)).toEqual([1]);

    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [] });
    });
    expect(result.current.items).toHaveLength(0);
    await cancelled;
  });

  it("cancel error refetches the list and surfaces no error", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useChatQueue(client, null, 10));
    resolveList(client, 0, [item({ ID: 1 })]);
    await flush();

    let cancelled: Promise<void> | undefined;
    act(() => {
      cancelled = result.current.cancel(1);
    });
    const cancelCall = client.calls.find((c) => c.method === "chat.queueCancel")!;
    act(() => {
      cancelCall.reject(new Error("not queued"));
    });
    await flush();
    await cancelled;

    // The refetch (second chat.queueList) becomes the truth: the item
    // was actually delivered, so it's gone -- and nothing threw.
    resolveList(client, 1, [item({ ID: 1, Status: "delivered", RunID: "run-1" })]);
    await flush();
    expect(result.current.items).toHaveLength(0);
  });
});
