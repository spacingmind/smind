import { useEffect } from "react";

import { prTab, prTabKey, type TabEntry } from "@/components/tab-registry";
import type { TabPlacement } from "@/hooks/use-task-tabs";
import { readPRTabState, writePRTabState } from "@/lib/pr-tab-state";

/**
 * Opens a task's PR tab once, in the background, when the task has a PR (G9): never again after it was opened or
 * closed (closing records the opt-out, see `useTaskTabs`), and never before the task's strip exists -- opening a tab
 * first would stop the default chat from being seeded. It does not take focus from the active tab.
 *
 * `tabs` is the selected task's tabs, null until its layout exists; `prNumber` is null without a PR.
 */
export function useAutoOpenPRTab({
  taskId,
  prNumber,
  tabs,
  openTab,
}: {
  taskId: number | null;
  prNumber: number | null;
  tabs: readonly TabEntry[] | null;
  openTab: (taskId: number, entry: TabEntry, placement: TabPlacement, options: { background: boolean }) => void;
}): void {
  const hasTab = tabs?.some((t) => taskId !== null && t.key === prTabKey(taskId)) ?? false;
  const ready = tabs !== null;
  useEffect(() => {
    if (taskId === null || prNumber === null || !ready || hasTab) return;
    if (readPRTabState(taskId) !== undefined) return;
    writePRTabState(taskId, "auto-opened");
    openTab(taskId, prTab(taskId, prNumber), "prefer", { background: true });
  }, [taskId, prNumber, ready, hasTab, openTab]);
}
