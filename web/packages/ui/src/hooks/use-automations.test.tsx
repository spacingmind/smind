import { act } from "react";
import { describe, expect, it } from "vitest";

import { useAutomationActions, useAutomationAudit, useAutomations } from "@/hooks/use-automations";
import { AUTOMATION_PAGE_SIZE } from "@/lib/automation-types";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { auditRow, automation, AutomationDaemon, forbidden } from "@/test/automation-fixtures";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function daemonWith(...automations: ReturnType<typeof automation>[]): AutomationDaemon {
  const d = new AutomationDaemon();
  d.listing = { enabled: true, paused: false, automations };
  return d;
}

describe("useAutomations", () => {
  it("reads automation.list once and shares it", async () => {
    const d = daemonWith(automation());
    const { result } = renderHookWithDaemon(() => ({ a: useAutomations(), b: useAutomations() }), d, { initialProps: null });
    await flush();
    expect(result.current.a.data?.automations).toHaveLength(1);
    expect(result.current.b.data?.enabled).toBe(true);
    expect(d.calls.filter((c) => c.method === "automation.list")).toHaveLength(1);
  });

  it("refetches on automation.fired, automation.paused and automation.updated", async () => {
    const d = daemonWith(automation());
    renderHookWithDaemon(() => useAutomations(), d, { initialProps: null });
    await flush();
    for (const topic of ["automation.fired", "automation.paused", "automation.updated"]) {
      const before = d.calls.filter((c) => c.method === "automation.list").length;
      await act(async () => d.emit(topic, { automationId: 1 }));
      await flush();
      expect(d.calls.filter((c) => c.method === "automation.list").length, topic).toBe(before + 1);
    }
  });
});

describe("useAutomationActions", () => {
  it("enable writes the daemon's answer into the held list", async () => {
    const d = daemonWith(automation({ enabled: false }));
    const { result } = renderHookWithDaemon(() => ({ list: useAutomations(), actions: useAutomationActions() }), d, { initialProps: null });
    await flush();
    await act(async () => void (await result.current.actions.enable(1)));
    expect(result.current.list.data?.automations[0].enabled).toBe(true);
    expect(d.calls.find((c) => c.method === "automation.enable")?.params).toEqual({ id: 1 });
  });

  it("delete removes the row; a forbidden refusal rejects with its code and costs no refetch", async () => {
    const d = daemonWith(automation(), automation({ id: 2, name: "Other" }));
    const { result } = renderHookWithDaemon(() => ({ list: useAutomations(), actions: useAutomationActions() }), d, { initialProps: null });
    await flush();
    await act(async () => void (await result.current.actions.delete(2)));
    expect(result.current.list.data?.automations.map((a) => a.id)).toEqual([1]);

    d.refuse = forbidden("automation.delete is only available on a direct connection, not over the relay");
    const lists = d.calls.filter((c) => c.method === "automation.list").length;
    let code = "";
    await act(async () => {
      await result.current.actions.delete(1).catch((e) => (code = e.code));
    });
    expect(code).toBe("forbidden");
    expect(d.calls.filter((c) => c.method === "automation.list")).toHaveLength(lists);
  });

  it("fire returns the audit row, memoryClear empties the cached memory", async () => {
    const d = daemonWith(automation());
    d.onMutation["automation.fire"] = () => auditRow({ decision: "capped", reason: "2 runs in the last hour" });
    const { result } = renderHookWithDaemon(() => useAutomationActions(), d, { initialProps: null });
    let row: { decision: string } | undefined;
    await act(async () => void (row = await result.current.fire(1)));
    expect(row?.decision).toBe("capped");
    await act(async () => void (await result.current.memoryClear(1)));
    expect(d.calls.some((c) => c.method === "automation.memory.clear")).toBe(true);
  });
});

describe("useAutomationAudit", () => {
  it("pages with before = the last id, and filters by decision client-side", async () => {
    const d = daemonWith(automation());
    d.audit = Array.from({ length: AUTOMATION_PAGE_SIZE + 5 }, (_, i) => auditRow({ id: i + 1, decision: i % 2 ? "capped" : "fire" }));
    const { result } = renderHookWithDaemon(({ decision }) => useAutomationAudit(1, { decision }), d, { initialProps: { decision: "" } });
    await flush();
    expect(result.current.rows).toHaveLength(AUTOMATION_PAGE_SIZE);
    expect(result.current.hasNextPage).toBe(true);
    await act(async () => void (await result.current.fetchNextPage()));
    expect(result.current.rows).toHaveLength(AUTOMATION_PAGE_SIZE + 5);
    expect(result.current.hasNextPage).toBe(false);
    const audits = d.calls.filter((c) => c.method === "automation.audit");
    expect(audits[0].params).toEqual({ id: 1, limit: AUTOMATION_PAGE_SIZE, before: 0 });
    expect(audits[1].params).toEqual({ id: 1, limit: AUTOMATION_PAGE_SIZE, before: 6 });
  });
});
