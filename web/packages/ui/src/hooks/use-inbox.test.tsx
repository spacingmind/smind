import { act } from "react";
import { describe, expect, it } from "vitest";

import { useInboxActions, useInboxCount, useInboxList, useInboxRequest } from "@/hooks/use-inbox";
import { InboxDaemon, permissionItem, REQUEST_EVENTS, stallItem, verdictItem } from "@/test/inbox-fixtures";
import { renderHookWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

function daemonWith(...items: ReturnType<typeof permissionItem>[]): InboxDaemon {
  const d = new InboxDaemon();
  d.items = items;
  d.logs["run-1"] = REQUEST_EVENTS;
  return d;
}

describe("inbox hooks", () => {
  it("reads inbox.count once and shares it", async () => {
    const d = daemonWith(permissionItem(), verdictItem());
    const { result } = renderHookWithDaemon(() => ({ a: useInboxCount(), b: useInboxCount() }), d, { initialProps: null });
    await flush();
    expect(result.current.a.data).toEqual({ total: 2, unseen: 2, byKind: { permission: 1, verdict: 1 } });
    expect(d.callsOf("inbox.count")).toHaveLength(1);
  });

  it("refetches the count and the list on inbox.changed", async () => {
    const d = daemonWith(verdictItem());
    renderHookWithDaemon(() => ({ c: useInboxCount(), l: useInboxList() }), d, { initialProps: null });
    await flush();
    await act(async () => d.emit("inbox.changed", {}));
    await flush();
    expect(d.callsOf("inbox.count")).toHaveLength(2);
    expect(d.callsOf("inbox.list")).toHaveLength(2);
  });

  it("snooze and resolve never send a permission key", async () => {
    const d = daemonWith(permissionItem(), stallItem({ state: "new" }));
    const { result } = renderHookWithDaemon(() => useInboxActions(), d, { initialProps: null });
    await act(async () => {
      await result.current.snooze([permissionItem()], 3_600_000);
      await result.current.resolve([permissionItem()]);
    });
    expect(d.callsOf("inbox.snooze")).toHaveLength(0);
    expect(d.callsOf("inbox.resolve")).toHaveLength(0);
    await act(async () => {
      await result.current.resolve([permissionItem(), stallItem({ state: "new" })]);
    });
    expect(d.callsOf("inbox.resolve")[0].params).toEqual({ keys: ["stall:8:1000"] });
  });

  it("the actions refresh the inbox afterwards", async () => {
    const d = daemonWith(verdictItem());
    const { result } = renderHookWithDaemon(() => ({ l: useInboxList(), a: useInboxActions() }), d, { initialProps: null });
    await flush();
    await act(async () => void (await result.current.a.seen(["verdict:7"])));
    await flush();
    expect(result.current.l.data?.[0].state).toBe("seen");
  });

  it("useInboxRequest finds the unresolved request", async () => {
    const d = daemonWith(permissionItem());
    const { result } = renderHookWithDaemon(() => useInboxRequest("run-1", "q1", true), d, { initialProps: null });
    await flush();
    expect(result.current.data?.options.map((o) => o.id)).toEqual(["allow-1", "deny-1"]);
  });

  it("useInboxRequest returns null once the request is answered", async () => {
    const d = daemonWith(permissionItem());
    d.logs["run-1"] = [...REQUEST_EVENTS, { type: "permission_resolved", requestId: "q1", optionId: "allow-1" }];
    const { result } = renderHookWithDaemon(() => useInboxRequest("run-1", "q1", true), d, { initialProps: null });
    await flush();
    expect(result.current.data).toBeNull();
  });
});
