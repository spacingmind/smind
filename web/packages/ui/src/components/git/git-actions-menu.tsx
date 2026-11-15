import { useId, useState } from "react";

import { ArchiveTaskDialog } from "@/components/crud-dialogs";
import { CreatePrDialog } from "@/components/git/create-pr-dialog";
import { ForgeSettingsLink, isForgeSetupCode } from "@/components/git/forge-settings-link";
import { gitActionEntries, primaryGitAction, type GitActionEntry } from "@/components/git/git-actions-model";
import { describePrError } from "@/components/git/pr-error";
import { PrErrorLine } from "@/components/git/pr-error-line";
import { SyncDialog } from "@/components/git/sync-dialog";
import type { SyncMode } from "@/components/git/sync-model";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useUpdatePr } from "@/hooks/use-pr-actions";
import { useTaskGit } from "@/hooks/use-task-git";
import { Icons } from "@/lib/icons";
import { ignoreSelect, MENU_CLOSE_INSTANT, reachableDisabledProps } from "@/lib/menu-item-disabled";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { cn } from "@/lib/utils";

const ENTRY_ICON: Record<GitActionEntry["id"], (props: { "aria-hidden": true }) => React.ReactNode> = {
  push: (p) => <Icons.push {...p} />,
  update: (p) => <Icons.merge {...p} />,
  "create-pr": (p) => <Icons.pullRequest {...p} />,
  "update-pr": (p) => <Icons.pullRequest {...p} />,
  "view-pr": (p) => <Icons.externalLink {...p} />,
  archive: (p) => <Icons.archive {...p} />,
};

/**
 * The Diff header's one git control (chat-surface-ui decision 8 and 9): a primary button for the next sensible step of the task's
 * state (Publish branch, Update from base, Create PR…, Update PR, Archive…) and, beside it, a menu with every action. A disabled
 * action stays in the menu with the daemon's reason as plain text under its label instead of a greyed button in the row. It replaces
 * the three equal-weight buttons (Push, Update, Create PR) the header used to show; the actions themselves are unchanged (the same
 * confirm dialogs and the same daemon calls).
 */
export function GitActionsMenu({ taskId, workspaceId = null, title = "" }: { taskId: number; workspaceId?: number | null; title?: string }) {
  const git = useTaskGit(taskId);
  const { client } = useDaemonConnection();
  const update = useUpdatePr();
  const [syncMode, setSyncMode] = useState<SyncMode | null>(null);
  const [lastSyncMode, setLastSyncMode] = useState<SyncMode>("push");
  const [prOpen, setPrOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const reasonPrefix = useId();

  const entries = gitActionEntries(git, { updatingPr: update.isPending });
  const primary = primaryGitAction(entries);
  const error = update.error ? describePrError(update.error, "update") : null;
  if (entries.length === 0 || !primary) return null;

  const run = (entry: GitActionEntry) => {
    switch (entry.id) {
      case "push":
      case "update":
        setLastSyncMode(entry.id);
        setSyncMode(entry.id);
        return;
      case "create-pr":
        setPrOpen(true);
        return;
      case "update-pr":
        update.mutate({ taskId });
        return;
      case "view-pr":
        if (entry.url) window.open(entry.url, "_blank", "noopener,noreferrer");
        return;
      case "archive":
        setArchiveOpen(true);
        return;
    }
  };

  return (
    <div className="relative flex items-center" data-testid="git-actions">
      <div className="inline-flex items-stretch" title={primary.enabled ? undefined : primary.reason}>
        <Button
          type="button"
          size="sm"
          className="rounded-r-none"
          disabled={!primary.enabled}
          onClick={() => run(primary)}
          data-testid="git-primary-action"
          data-action={primary.id}
        >
          {primary.label}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              size="icon-sm"
              className="rounded-l-none border-l border-primary-foreground/25"
              aria-label="More git actions"
              data-testid="git-actions-menu-trigger"
            >
              <Icons.chevronDown aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className={cn("min-w-56", MENU_CLOSE_INSTANT)} data-testid="git-actions-menu-content">
            {entries.map((entry) => {
              const blocked = !entry.enabled;
              const reasonId = `${reasonPrefix}-${entry.id}`;
              return (
                <DropdownMenuItem
                  key={entry.id}
                  data-testid={`git-action-${entry.id}`}
                  data-primary={entry.id === primary.id ? "true" : undefined}
                  {...(blocked ? reachableDisabledProps(reasonId) : {})}
                  onSelect={blocked ? ignoreSelect : () => run(entry)}
                >
                  {ENTRY_ICON[entry.id]({ "aria-hidden": true })}
                  <span className="flex min-w-0 flex-col">
                    <span className={cn(entry.id === primary.id && "font-medium")}>{entry.label}</span>
                    {blocked && entry.reason && (
                      <span id={reasonId} className="max-w-64 text-ui-sm font-normal whitespace-normal text-foreground-muted" data-testid={`git-action-${entry.id}-reason`}>
                        {entry.reason}
                      </span>
                    )}
                    {blocked && entry.reason && isForgeSetupCode(entry.code) && <ForgeSettingsLink testId="git-action-settings-link" />}
                  </span>
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {error && (
        // A card anchored under the control, out of flow: an inline error would grow the header and shove the diff down.
        <div
          className="absolute top-full right-0 z-30 mt-2 flex w-80 max-w-[calc(100vw-2rem)] items-start gap-2 rounded-lg border border-input-border bg-background p-3 text-left shadow-md"
          data-testid="pr-error-card"
        >
          <PrErrorLine error={error} testId="pr-error" className="min-w-0 flex-1" />
          <Button type="button" variant="ghost" size="icon-sm" aria-label="Dismiss" data-testid="pr-error-dismiss" onClick={() => update.reset()}>
            <Icons.close aria-hidden />
          </Button>
        </div>
      )}
      <SyncDialog taskId={taskId} mode={lastSyncMode} open={syncMode !== null} onOpenChange={(open) => !open && setSyncMode(null)} />
      <CreatePrDialog taskId={taskId} workspaceId={workspaceId} open={prOpen} onOpenChange={setPrOpen} />
      {archiveOpen && (
        <ArchiveTaskDialog
          client={client}
          task={{ ID: taskId, WorkspaceID: workspaceId ?? 0, Title: title }}
          open
          onOpenChange={setArchiveOpen}
          onArchived={() => {}}
        />
      )}
    </div>
  );
}
