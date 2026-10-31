import { useEffect } from "react";

import { usePluginPanelsQuery } from "@/hooks/use-plugin-panels";
import { pluginTabsToClose } from "@/lib/plugin-tabs";
import { collectAllTabs, type TaskLayout } from "@/lib/split-tree";

/**
 * Closes every open plugin tab whose panel is no longer in the daemon's
 * `plugin.panels` answer (a disabled/removed/reloaded plugin, ADR-0042) --
 * across every task's layout, since a panel's tabs can be open in tasks the
 * user is not looking at. It acts ONLY on a successful answer: while the
 * query is pending (a client still connecting) or failed (a daemon without
 * the RPC), closing would throw away restorable tabs on a guess, so the
 * persisted layouts keep them instead. The effect is keyed on the query
 * data and the layouts, so each close happens once per data change, not
 * once per render.
 */
export function usePluginTabReaper({
  tabsByTask,
  closeTab,
}: {
  tabsByTask: ReadonlyMap<number, TaskLayout>;
  closeTab: (taskId: number, key: string) => void;
}): void {
  const query = usePluginPanelsQuery();
  const panels = query.data?.panels;
  useEffect(() => {
    if (!query.isSuccess || !panels) return;
    for (const [taskId, layout] of tabsByTask) {
      for (const tab of pluginTabsToClose(collectAllTabs(layout.root), panels)) {
        closeTab(taskId, tab.key);
      }
    }
  }, [query.isSuccess, panels, tabsByTask, closeTab]);
}

/** Renders nothing: mounts {@link usePluginTabReaper} inside the daemon provider (see PRTabAutoOpen's own note). */
export function PluginTabReaper({
  tabsByTask,
  closeTab,
}: {
  tabsByTask: ReadonlyMap<number, TaskLayout>;
  closeTab: (taskId: number, key: string) => void;
}) {
  usePluginTabReaper({ tabsByTask, closeTab });
  return null;
}
