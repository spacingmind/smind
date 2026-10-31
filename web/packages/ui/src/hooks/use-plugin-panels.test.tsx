import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";

import { usePluginPanels, usePluginPanelsQuery, usePluginViewQuery } from "@/hooks/use-plugin-panels";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import type { PluginPanel, PluginPanelsResult } from "@/lib/plugin-panel-types";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

const PANELS: PluginPanel[] = [
  { plugin: "acme", id: "usage", title: "Acme usage", where: "task", ui: 1 },
  { plugin: "zeta", id: "board", title: "Board", where: "sidebar", ui: 1 },
];
const SIDEBAR: PluginPanelsResult["sidebar"] = [{ plugin: "zeta", id: "board-entry", title: "Board", opens: "board" }];

/** The daemon-provider wrapper the query hooks need (same harness as use-plugin-tab-reaper.test.tsx). */
function withClient(client: FakeWsClient, queryClient = createQueryClient()) {
  return ({ children }: { children: ReactNode }) => (
    <DaemonQueryProvider queryClient={queryClient} client={client as never} events={null}>
      {children}
    </DaemonQueryProvider>
  );
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

describe("use-plugin-panels", () => {
  it("TestPanelsQuery_ReportsUiLevel", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(() => usePluginPanelsQuery(), { wrapper: withClient(client) });

    // The client's ui level goes on the wire, and the fetch happens exactly once
    // (staleTime Infinity, no refetch on mount or re-render).
    expect(client.nth("plugin.panels", 0).params).toEqual({ ui: 1 });
    client.nth("plugin.panels", 0).resolve({ panels: PANELS, sidebar: SIDEBAR } satisfies PluginPanelsResult);
    await flush();
    expect(result.current.data?.panels).toEqual(PANELS);

    rerender();
    await flush();
    expect(client.calls.filter((c) => c.method === "plugin.panels")).toHaveLength(1);
  });

  it("TestPanelsQuery_OlderDaemonErrorReadsAsNoPanels", async () => {
    const client = new FakeWsClient();
    const { result, rerender } = renderHook(() => usePluginPanels(), { wrapper: withClient(client) });
    client.nth("plugin.panels", 0).reject(new Error("unknown method"));
    await flush();

    // The error reads as "no panels", never as a throw or a failure the UI shows.
    expect(result.current.panels).toEqual([]);
    expect(result.current.sidebar).toEqual([]);

    // One stable empty pair, so consumers' identities survive a re-render.
    const panels = result.current.panels;
    const sidebar = result.current.sidebar;
    rerender();
    await flush();
    expect(result.current.panels).toBe(panels);
    expect(result.current.sidebar).toBe(sidebar);
  });

  it("TestViewQuery_SendsLevelAndContext", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(
      () => usePluginViewQuery({ pluginId: "a", panelId: "p", context: { taskId: 7 } }),
      { wrapper: withClient(client) },
    );

    expect(client.nth("plugin.view", 0).params).toEqual({ pluginId: "a", panelId: "p", ui: 1, context: { taskId: 7 } });
    const view = { type: "text", text: "hello" };
    client.nth("plugin.view", 0).resolve({ pluginId: "a", panelId: "p", view });
    await flush();
    // Only the validated `view` document is the query's data; the rest of the answer is not.
    expect(result.current.data).toBe(view);
  });
});
