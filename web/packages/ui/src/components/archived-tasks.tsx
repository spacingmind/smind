import { useId } from "react";

import { describeArchiveError } from "@/components/git/pr-error";
import { Button } from "@/components/ui/button";
import { useRestoreTask } from "@/hooks/use-archive";
import { formatRelativeTime } from "@/lib/relative-time";
import type { Task } from "@/lib/types";

/** The sentence a Restore that can't succeed shows; the daemon's `branch_gone` text is the same one. */
export const BRANCH_GONE_SENTENCE = "The branch no longer exists, so there is nothing to restore.";

/** One archived task: title, when and whether the branch survived, and Restore (disabled, with the reason, when the branch is gone). */
function ArchivedTaskRow({ task }: { task: Task }) {
  const restore = useRestoreTask();
  const reasonId = useId();
  const gone = Boolean(task.BranchDeletedAt);
  const error = restore.error ? describeArchiveError(restore.error, "restore").message : null;
  return (
    <li className="flex flex-col gap-0.5 px-2 py-1" data-testid="archived-task-row" data-task-id={task.ID}>
      <div className="flex min-h-7 items-center gap-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-ui-sm text-foreground" data-testid="archived-task-title">
            {task.Title}
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="xs"
          disabled={gone || restore.isPending}
          title={gone ? BRANCH_GONE_SENTENCE : undefined}
          aria-describedby={gone ? reasonId : undefined}
          onClick={() => restore.mutate({ taskId: task.ID })}
          data-testid="archived-task-restore"
        >
          {restore.isPending ? "Restoring…" : "Restore"}
        </Button>
      </div>
      {/* Its own full-width line: beside the Restore button it was truncated to "branch del…" in the narrow sidebar. */}
      <p className="truncate text-ui-caption text-foreground-subtle" data-testid="archived-task-meta">
        {task.ArchivedAt ? `${formatRelativeTime(task.ArchivedAt)} · ` : ""}
        {gone ? "branch deleted" : "branch kept"}
      </p>
      {gone && (
        <p id={reasonId} className="sr-only" data-testid="archived-task-reason">
          {BRANCH_GONE_SENTENCE}
        </p>
      )}
      {error && (
        <p role="alert" className="text-ui-caption text-destructive" data-testid="archived-task-error">
          {error}
        </p>
      )}
    </li>
  );
}

/** The expanded body of a workspace's Archived row. */
export function ArchivedTaskList({ tasks }: { tasks: Task[] }) {
  return (
    <ul className="flex flex-col" data-testid="archived-task-list">
      {tasks.map((task) => (
        <ArchivedTaskRow key={task.ID} task={task} />
      ))}
    </ul>
  );
}
