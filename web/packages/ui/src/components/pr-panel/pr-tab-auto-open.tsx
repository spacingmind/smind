import type { TabEntry } from "@/components/tab-registry";
import { useAutoOpenPRTab } from "@/hooks/use-auto-open-pr-tab";
import { useTaskGit } from "@/hooks/use-task-git";
import type { TabPlacement } from "@/hooks/use-task-tabs";

/** Renders nothing: it reads the task's PR from the query cache (so it lives under the daemon provider) and auto-opens the PR tab. */
export function PRTabAutoOpen({
  taskId,
  tabs,
  openTab,
}: {
  taskId: number | null;
  tabs: readonly TabEntry[] | null;
  openTab: (taskId: number, entry: TabEntry, placement: TabPlacement, options: { background: boolean }) => void;
}) {
  const pr = useTaskGit(taskId)?.pr;
  useAutoOpenPRTab({ taskId, prNumber: pr?.number ?? null, tabs, openTab });
  return null;
}
