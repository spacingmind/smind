import { useEffect, useState } from "react";
import { Icons } from "@/lib/icons";

import { ArchiveTaskDialog } from "@/components/crud-dialogs";
import { ArchiveFinishedDialog, StopTreeDialog } from "@/components/tree-action-dialogs";
import { GitBranchLine } from "@/components/git/git-branch-line";
import { GitTokens } from "@/components/git/git-tokens";
import { PrChip } from "@/components/git/pr-chip";
import { SyncMenuItems } from "@/components/git/sync-actions";
import { SyncDialog } from "@/components/git/sync-dialog";
import type { SyncMode } from "@/components/git/sync-model";
import { PortChip, SetupBadge } from "@/components/task-setup";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { ignoreSelect, MENU_CLOSE_INSTANT, reachableDisabledProps } from "@/lib/menu-item-disabled";
import { toast } from "@/components/ui/toast";
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import { useTaskGit } from "@/hooks/use-task-git";
import { formatRelativeTime } from "@/lib/relative-time";
import { availabilityFromTaskTree, type TreeActionAvailability } from "@/lib/task-tree-actions";
import type { TaskTreeResult } from "@/lib/task-lineage";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * The header's task identity: folder icon + title. The title is also the
 * trigger for a hover/focus card holding the metadata the old task header
 * block used to print under it (branch, status, last activity, setup, port,
 * worktree), so it stays one click-free glance away without taking a row.
 */
export function HeaderTaskTitle({ task }: { task: Task }) {
  const git = useTaskGit(task.ID);
  return (
    <>
      <HoverCard openDelay={400} closeDelay={100}>
        <HoverCardTrigger asChild>
          <button
            type="button"
            data-testid="app-header-title"
            className="flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 text-ui-base font-medium text-foreground outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Icons.folder aria-hidden className="size-4 shrink-0 text-foreground-muted" />
            <span className="min-w-0 truncate">{task.Title}</span>
          </button>
        </HoverCardTrigger>
        <HoverCardContent data-testid="app-header-task-card" side="bottom" align="start" className="flex flex-col gap-1.5">
          <p className="text-ui-base font-medium text-foreground [overflow-wrap:anywhere]">{task.Title}</p>
          {(git?.branch?.name ?? task.Branch) && (
            <GitBranchLine
              branch={(git?.branch?.name ?? task.Branch)!}
              base={git?.base?.known ? git.base.ref : undefined}
              testId="app-header-task-branch"
            />
          )}
          {git && <GitTokens git={git} />}
          {task.WorktreePath && (
            <p data-testid="app-header-task-worktree" className="truncate font-mono text-ui-xs text-muted-foreground" title={task.WorktreePath}>
              {task.WorktreePath}
            </p>
          )}
          <p className="text-ui-sm text-muted-foreground">
            Last activity <span data-testid="app-header-task-activity">{formatRelativeTime(task.UpdatedAt)}</span>
          </p>
          <div className="flex items-center gap-2 text-ui-sm text-muted-foreground">
            <span data-testid="app-header-task-status" className="uppercase">
              {task.Status}
            </span>
            <SetupBadge status={task.SetupStatus} />
            <PortChip port={task.Port} />
          </div>
        </HoverCardContent>
      </HoverCard>
      {git?.pr && <PrChip pr={git.pr} />}
    </>
  );
}

function copy(text: string, what: string): void {
  void navigator.clipboard
    ?.writeText(text)
    .then(() => toast({ variant: "success", title: `${what} copied` }))
    .catch(() => toast({ variant: "error", title: `Couldn't copy ${what.toLowerCase()}` }));
}

/**
 * The header ⋯ menu for the selected task. Pin / Mark unread / Move to space
 * are sidebar-owned state and stay in the sidebar's menu; this one carries
 * the actions that need only the task itself. Archive reuses the sidebar's
 * confirm dialog -- the task list follows the lifecycle events it causes.
 */
export function HeaderTaskMenu({
  task,
  client,
  localPathActions,
}: {
  task: Task;
  client: WsClient | null;
  localPathActions: LocalPathActions | null;
}) {
  const [archiving, setArchiving] = useState(false);
  const [syncMode, setSyncMode] = useState<SyncMode | null>(null);
  const [lastSyncMode, setLastSyncMode] = useState<SyncMode>("push");
  const [archiveFinished, setArchiveFinished] = useState(false);
  const [stoppingTree, setStoppingTree] = useState(false);
  const [treeActions, setTreeActions] = useState<TreeActionAvailability | null>(null);
  const { tooltip, onMenuOpenChange } = useMenuTooltip();
  const worktree = task.WorktreePath;

  // PR-H11: the header has no task forest, so the tree actions' availability
  // comes from a task.tree fetch (stale-guarded per task/client). A task
  // whose tree has no descendants offers neither entry, same rule as the
  // sidebar.
  const [treeRefresh, setTreeRefresh] = useState(0);
  // An item that opens a dialog closes the menu instantly (no exit fade under the dialog).
  const [instantClose, setInstantClose] = useState(false);
  // A different task starts with no availability (no leftovers from the last one).
  useEffect(() => {
    setTreeActions(null);
  }, [task.ID]);
  useEffect(() => {
    if (!client) return;
    const reqTaskId = task.ID;
    let cancelled = false;
    client
      .call<TaskTreeResult>("task.tree", { taskId: task.ID })
      .then((result) => {
        if (cancelled || task.ID !== reqTaskId) return;
        setTreeActions(availabilityFromTaskTree(result.nodes ?? []));
      })
      .catch((err) => {
        if (cancelled) return;
        console.error("task.tree failed, header tree actions hidden", err);
      });
    return () => {
      cancelled = true;
    };
    // treeRefresh bumps whenever the menu opens, so the enable/disable state
    // is current when the user looks at it.
  }, [client, task.ID, treeRefresh]);
  return (
    <>
      <DropdownMenu
        onOpenChange={(open) => {
          onMenuOpenChange(open);
          if (open) {
            setTreeRefresh((n) => n + 1);
            setInstantClose(false);
          }
        }}
      >
        <Tooltip {...tooltip}>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Task actions"
                data-testid="app-header-task-menu"
                className="text-foreground-muted hover:text-foreground"
              >
                <Icons.more aria-hidden />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="bottom">Task actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent
          align="start"
          data-testid="app-header-task-menu-content"
          className={instantClose ? MENU_CLOSE_INSTANT : undefined}
        >
          {task.Branch && (
            <DropdownMenuItem data-testid="app-header-copy-branch" onSelect={() => copy(task.Branch!, "Branch name")}>
              <Icons.copy aria-hidden /> Copy branch name
            </DropdownMenuItem>
          )}
          {worktree && (
            <DropdownMenuItem data-testid="app-header-copy-worktree" onSelect={() => copy(worktree, "Worktree path")}>
              <Icons.copy aria-hidden /> Copy worktree path
            </DropdownMenuItem>
          )}
          {worktree && localPathActions && (
            <DropdownMenuItem data-testid="app-header-reveal" onSelect={() => localPathActions.reveal(worktree)}>
              <Icons.folderOpen aria-hidden /> {localPathActions.revealLabel}
            </DropdownMenuItem>
          )}
          {(task.Branch || worktree) && <DropdownMenuSeparator />}
          <SyncMenuItems
            taskId={task.ID}
            onOpen={(mode) => {
              setInstantClose(true);
              setLastSyncMode(mode);
              setSyncMode(mode);
            }}
          />
          {treeActions && (
            <>
              <DropdownMenuItem
                data-testid="app-header-archive-finished"
                {...(!treeActions.archive.enabled ? reachableDisabledProps("app-header-archive-finished-reason") : {})}
                onSelect={
                  treeActions.archive.enabled
                    ? () => {
                        setInstantClose(true);
                        setArchiveFinished(true);
                      }
                    : ignoreSelect
                }
              >
                <Icons.archive aria-hidden /> Archive finished sub-tasks…
                {!treeActions.archive.enabled && (
                  <span id="app-header-archive-finished-reason" className="ml-auto pl-2 text-ui-xs text-muted-foreground">
                    {treeActions.archive.reason}
                  </span>
                )}
              </DropdownMenuItem>
              <DropdownMenuItem
                data-testid="app-header-stop-tree"
                {...(!treeActions.stop.enabled ? reachableDisabledProps("app-header-stop-tree-reason") : {})}
                onSelect={
                  treeActions.stop.enabled
                    ? () => {
                        setInstantClose(true);
                        setStoppingTree(true);
                      }
                    : ignoreSelect
                }
              >
                <Icons.stop aria-hidden /> Stop all in tree…
                {!treeActions.stop.enabled && (
                  <span id="app-header-stop-tree-reason" className="ml-auto pl-2 text-ui-xs text-muted-foreground">
                    {treeActions.stop.reason}
                  </span>
                )}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          )}
          <DropdownMenuItem
            data-testid="app-header-archive"
            onSelect={() => {
              setInstantClose(true);
              setArchiving(true);
            }}
          >
            <Icons.archive aria-hidden /> Archive task…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <SyncDialog taskId={task.ID} mode={lastSyncMode} open={syncMode !== null} onOpenChange={(open) => !open && setSyncMode(null)} />
      {archiveFinished && (
        <ArchiveFinishedDialog
          client={client}
          task={task}
          open
          onOpenChange={(open) => !open && setArchiveFinished(false)}
          onArchived={() => {}}
        />
      )}
      {stoppingTree && (
        <StopTreeDialog
          client={client}
          task={task}
          open
          onOpenChange={(open) => !open && setStoppingTree(false)}
          onStopped={() => {}}
        />
      )}
      {archiving && (
        <ArchiveTaskDialog
          client={client}
          task={task}
          open
          onOpenChange={(open) => !open && setArchiving(false)}
          onArchived={() => {}}
        />
      )}
    </>
  );
}
