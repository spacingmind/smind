import { useTaskGit } from "@/hooks/use-task-git";
import type { Task } from "@/lib/types";

/**
 * The PR tab's content (G9). A placeholder for now: the panel (header, checks, reviews, threads, Send to chat) is built on
 * top of `usePRDetails` / `useSendPRToChat` by the next change, which replaces this body. `active` is whether the tab is the
 * one in front -- the value to pass as `focused` to `usePRDetails`.
 */
export function PRPanelPane({ task }: { task: Task; active: boolean }) {
  const git = useTaskGit(task.ID);
  return (
    <div data-testid="pr-panel" className="p-4 text-ui-base text-muted-foreground">
      {git?.pr ? `PR #${git.pr.number}` : "Pull request"}
    </div>
  );
}
