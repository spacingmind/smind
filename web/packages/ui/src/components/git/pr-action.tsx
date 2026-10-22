import { useId, useState } from "react";

import { CreatePrDialog } from "@/components/git/create-pr-dialog";
import { describePrError } from "@/components/git/pr-error";
import { PrErrorLine } from "@/components/git/pr-error-line";
import { Button } from "@/components/ui/button";
import { useUpdatePr } from "@/hooks/use-pr-actions";
import { useTaskGit } from "@/hooks/use-task-git";
import { Icons } from "@/lib/icons";

/**
 * The diff header's pull-request control (ADR-0039 §10): **Create PR…** (opens the preview dialog) when the task
 * has no open PR, **Update PR** and **View PR** when it has. A disabled action keeps the daemon's reason sentence
 * as its tooltip and, below `md` where hover doesn't exist, as a visible muted line. The new PR shows up through
 * the `task.git` event (the PR chip), so success leaves no message behind.
 */
export function PrAction({ taskId, workspaceId = null }: { taskId: number; workspaceId?: number | null }) {
  const git = useTaskGit(taskId);
  const [dialogOpen, setDialogOpen] = useState(false);
  const update = useUpdatePr();
  const reasonId = useId();

  const pr = git?.pr?.state === "open" ? git.pr : undefined;
  const action = pr ? git?.actions?.updatePr : git?.actions?.createPr;
  // No git state yet (still loading): disabled, with nothing to explain.
  const disabled = !action?.enabled || update.isPending;
  const reason = action && !action.enabled ? action.reason : undefined;
  const url = pr?.url ?? "";
  const viewable = url.startsWith("https://");
  const error = update.error ? describePrError(update.error, "update") : null;

  return (
    <div className="flex flex-col items-end gap-1" data-testid="pr-action">
      <div className="flex items-center gap-2" title={reason}>
        {pr ? (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              aria-describedby={reason ? reasonId : undefined}
              onClick={() => update.mutate({ taskId })}
              data-testid="update-pr-button"
            >
              {update.isPending ? "Updating…" : "Update PR"}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!viewable}
              onClick={() => window.open(url, "_blank", "noopener,noreferrer")}
              data-testid="view-pr-button"
            >
              View PR
              <Icons.externalLink aria-hidden className="size-3" />
            </Button>
          </>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
            aria-describedby={reason ? reasonId : undefined}
            onClick={() => setDialogOpen(true)}
            data-testid="create-pr-button"
          >
            Create PR…
          </Button>
        )}
      </div>
      {reason && (
        <p id={reasonId} className="max-w-64 text-right text-ui-sm text-foreground-muted md:sr-only" data-testid="pr-action-reason">
          {reason}
        </p>
      )}
      {error && <PrErrorLine error={error} testId="pr-error" className="max-w-80 text-right" />}
      <CreatePrDialog taskId={taskId} workspaceId={workspaceId} open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  );
}
