import { QueryClient, QueryClientProvider, onlineManager, useQuery } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { DaemonNotification, WsClient } from "@/lib/ws-client";
import type { ReactNode } from "react";

/** A client whose `events.subscribe` stays pending until `answerSubscribe()`, and that can push notifications. */
function fakeClient() {
  const order: string[] = [];
  const listeners = new Set<(n: DaemonNotification) => void>();
  let answer: () => void = () => {};
  const client = {
    call: vi.fn((method: string) => {
      order.push(method);
      if (method !== "events.subscribe") return Promise.resolve(null);
      return new Promise((resolve) => {
        answer = () => resolve(null);
      });
    }),
    onNotification: (fn: (n: DaemonNotification) => void) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  } as unknown as WsClient;
  return {
    client,
    order,
    answerSubscribe: () => act(async () => answer()),
    push: (topic: string, payload: unknown) => act(() => listeners.forEach((l) => l({ topic, payload } as DaemonNotification))),
  };
}

describe("useDaemonEvents -> query cache", () => {
  it("account.updated reaches the cache through the real hook", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.accountList(), [{ id: 1, label: "old" }]);
    const daemon = fakeClient();
    renderHook(() => useDaemonEvents(daemon.client, qc));
    daemon.push("account.updated", { account: { id: 1, label: "renamed" } });
    expect(qc.getQueryData<{ label: string }[]>(queryKeys.accountList())?.[0]?.label).toBe("renamed");
  });

  it("a same-daemon reconnect invalidates only after the new events.subscribe is answered", async () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const first = fakeClient();
    const second = fakeClient();
    let current = first.client;
    const { rerender } = renderHook(() => useDaemonEvents(current, qc));
    await first.answerSubscribe();
    // The first connect never invalidates.
    expect(invalidate).not.toHaveBeenCalled();

    current = second.client;
    rerender();
    // Subscribed-but-unanswered: no re-read yet, so no event can fall between the read and the subscription.
    expect(second.order).toEqual(["events.subscribe"]);
    expect(invalidate).not.toHaveBeenCalled();
    await second.answerSubscribe();
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("a same-daemon reconnect drops the cached health answers so they are re-tested", async () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.providerTest("anthropic"), { ok: false, detail: "codex not on PATH" });
    qc.setQueryData(queryKeys.accountList(), [{ id: 1 }]);
    const first = fakeClient();
    const second = fakeClient();
    let current = first.client;
    const { rerender } = renderHook(() => useDaemonEvents(current, qc));
    await first.answerSubscribe();
    expect(qc.getQueryData(queryKeys.providerTest("anthropic"))).toBeDefined();

    current = second.client;
    rerender();
    await second.answerSubscribe();
    expect(qc.getQueryData(queryKeys.providerTest("anthropic"))).toBeUndefined();
    // Everything else is kept (and invalidated), not dropped.
    expect(qc.getQueryData(queryKeys.accountList())).toEqual([{ id: 1 }]);
  });

  it("an inactive query invalidated by a reconnect re-reads on its next mount, through the new client", async () => {
    const qc = createQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    const oldCall = vi.fn().mockResolvedValue(["stale"]);
    const newCall = vi.fn().mockResolvedValue(["fresh"]);
    const useList = (call: typeof oldCall) =>
      useQuery({ queryKey: queryKeys.accountList(), queryFn: () => call() });

    const first = renderHook(() => useList(oldCall), { wrapper });
    await vi.waitFor(() => expect(first.result.current.data).toEqual(["stale"]));
    first.unmount();

    // A reconnect: everything is invalidated while nothing observes the query.
    await act(async () => void qc.invalidateQueries());
    expect(newCall).not.toHaveBeenCalled();

    // Mounting again (Providers opened later) re-reads once, with the *new* client's function.
    const second = renderHook(() => useList(newCall), { wrapper });
    await vi.waitFor(() => expect(second.result.current.data).toEqual(["fresh"]));
    expect(newCall).toHaveBeenCalledTimes(1);

    // And a plain remount of a fresh entry does not re-read.
    second.unmount();
    renderHook(() => useList(newCall), { wrapper });
    expect(newCall).toHaveBeenCalledTimes(1);
  });

  it("a mutation made while the browser reports offline is sent now, not queued for later", async () => {
    // With the library default ("online") this would stay paused and fire when the network returns,
    // outside the window/TTL gates that decide when a usage touch may go out (ADR-0028).
    onlineManager.setOnline(false);
    try {
      const run = vi.fn().mockResolvedValue("sent");
      const result = await createQueryClient().getMutationCache().build(createQueryClient(), { mutationFn: run }).execute(undefined);
      expect(result).toBe("sent");
      expect(run).toHaveBeenCalledTimes(1);

      // Control: the same call on a client with default options pauses.
      const paused = new QueryClient();
      const control = vi.fn().mockResolvedValue("late");
      void paused.getMutationCache().build(paused, { mutationFn: control }).execute(undefined);
      await Promise.resolve();
      expect(control).not.toHaveBeenCalled();
    } finally {
      onlineManager.setOnline(true);
    }
  });
});
