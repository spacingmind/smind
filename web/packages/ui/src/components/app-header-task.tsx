import { useState } from "react";
import { Icons } from "@/lib/icons";

import { ArchiveTaskDialog } from "@/components/crud-dialogs";
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
import { toast } from "@/components/ui/toast";
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import { formatRelativeTime } from "@/lib/relative-time";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * The header's task identity: folder icon + title. The title is also the
 * trigger for a hover/focus card holding the metadata the old task header
 * block used to print under it (branch, status, last activity, setup, port,
 * worktree), so it stays one click-free glance away without taking a row.
 */
export function HeaderTaskTitle({ task }: { task: Task }) {
  return (
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
        {task.Branch && (
          <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
            <Icons.branch aria-hidden className="size-3 shrink-0" />
            <span data-testid="app-header-task-branch" className="min-w-0 truncate">
              {task.Branch}
            </span>
          </div>
        )}
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
  const { tooltip, onMenuOpenChange } = useMenuTooltip();
  const worktree = task.WorktreePath;
  return (
    <>
      <DropdownMenu onOpenChange={onMenuOpenChange}>
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
        <DropdownMenuContent align="start" data-testid="app-header-task-menu-content">
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
          <DropdownMenuItem data-testid="app-header-archive" onSelect={() => setArchiving(true)}>
            <Icons.archive aria-hidden /> Archive task…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
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
