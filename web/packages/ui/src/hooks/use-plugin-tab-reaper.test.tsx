import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

import { pluginTab, type TabEntry } from "@/components/tab-registry";
import { usePluginTabReaper } from "@/hooks/use-plugin-tab-reaper";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { PluginPanel, PluginPanelsResult } from "@/lib/plugin-panel-types";
import { DEFAULT_PANE_ID, type TaskLayout } from "@/lib/split-tree";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

/** A one-pane layout holding exactly the given tabs -- the minimum the reaper walks. */
function layoutWith(tabs: TabEntry[]): TaskLayout {
  return { root: { kind: "pane", pane: { id: DEFAULT_PANE_ID, tabs, activeKey: tabs[0]?.key ?? null } }, focusedPaneId: DEFAULT_PANE_ID };
}

const PANEL: PluginPanel = { plugin: "acme", id: "usage", title: "Acme usage", where: "task", ui: 1 };

/** The daemon-provider wrapper the reaper's query hooks need. */
function withClient(client: FakeWsClient | null, queryClient = createQueryClient()) {
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

describe("usePluginTabReaper", () => {
  it("TestPanelClosesOnDisable_TabRestorable", async () => {
    // (1) The panel is in the daemon's answer: the tab stays open.
    const key = pluginTab(1, PANEL).key;
    const queryClient = createQueryClient();
    const client = new FakeWsClient();
    const closeTab = vi.fn();
    const tabsByTask = new Map([[1, layoutWith([pluginTab(1, PANEL)])]]);
    const { rerender } = renderHook(() => usePluginTabReaper({ tabsByTask, closeTab }), {
      wrapper: withClient(client, queryClient),
    });
    client.nth("plugin.panels", 0).resolve({ panels: [PANEL], sidebar: [] } satisfies PluginPanelsResult);
    await flush();
    expect(closeTab).not.toHaveBeenCalled();

    // (2) The plugin lost the panel (disabled, removed, reloaded without it): the tab closes once.
    act(() => {
      queryClient.setQueryData(queryKeys.pluginPanels(), { panels: [], sidebar: [] } satisfies PluginPanelsResult);
    });
    await flush();
    expect(closeTab).toHaveBeenCalledTimes(1);
    expect(closeTab).toHaveBeenCalledWith(1, key);

    // A re-render with the same data closes nothing further.
    rerender();
    await flush();
    expect(closeTab).toHaveBeenCalledTimes(1);
  });

  it("never closes while the answer is pending, errored, or there is no client", async () => {
    const closeTab = vi.fn();
    const client = new FakeWsClient();
    const queryClient = createQueryClient();
    // Pending: the fake client leaves plugin.panels unresolved.
    renderHook(() => usePluginTabReaper({ tabsByTask: new Map([[1, layoutWith([pluginTab(1, PANEL)])]]), closeTab }), {
      wrapper: withClient(client, queryClient),
    });
    await flush();
    expect(closeTab).not.toHaveBeenCalled();

    // Errored: a daemon without the RPC answers with an error.
    client.nth("plugin.panels", 0).reject(new Error("unknown method"));
    await flush();
    expect(closeTab).not.toHaveBeenCalled();

    // No client at all: the query is disabled before the first connect.
    renderHook(() => usePluginTabReaper({ tabsByTask: new Map([[1, layoutWith([pluginTab(1, PANEL)])]]), closeTab }), {
      wrapper: withClient(null),
    });
    await flush();
    expect(closeTab).not.toHaveBeenCalled();
  });
});
