import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "@/components/ui/toast";
import type {
  TaskArchiveFinishedResult,
  TaskStopTreeResult,
  TreeActionItem,
} from "@/lib/task-tree-actions";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/*
 * "Archive finished sub-tasks…" and "Stop all in tree…" (task-hierarchy-tiers
 * PR-H11). Both dialogs run the server dry run on open and take their counts
 * from it (one source of truth: UI count == server selection); the menu's
 * enablement is only the pure client heuristic in lib/task-tree-actions.ts.
 *
 * The archive flow follows Paseo's archive-finished model
 * (refs/paseo/packages/app/src/subagents/archive-finished.ts): an explicit
 * action over a finished-only selection, never running work, per-row failure
 * collection shown to the user (never hidden), retry possible on whatever is
 * still eligible.
 */

/** Inline, per-dialog error text -- the daemon's message verbatim. */
function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return <p className="text-ui-base text-destructive">{message}</p>;
}

const MAX_VISIBLE = 8;

/** How long a dry run may take before its dialog opens in a loading state. */
const SLOW_DRY_RUN_MS = 400;

/** Plain-language form of the daemon's skip reasons; an unknown reason is shown as-is. */
function keptReason(reason: string): string {
  switch (reason) {
    case "has unfinished descendants":
      return "they still have unfinished sub-tasks";
    case "descendant failed to archive":
      return "a sub-task could not be archived";
    case "already archived":
      return "already archived";
    default:
      return reason.replace(/`/g, "");
  }
}

/** Capped item list: 8 rows then "and N more". */
function ItemList({ testId, items }: { testId: string; items: TreeActionItem[] }) {
  const visible = items.slice(0, MAX_VISIBLE);
  const rest = items.length - visible.length;
  return (
    <ul data-testid={testId} className="mt-2 list-disc pl-5">
      {visible.map((item, i) => (
        // A task with several running runs appears once per run, so the task id alone is not unique.
        <li key={`${item.id}:${i}`}>{item.title}</li>
      ))}
      {rest > 0 && <li>and {rest} more</li>}
    </ul>
  );
}

/** Stale-dry-run guard shared by both dialogs: a result landing after close or for another task belongs to a different question. */
function useDryRun<T>(
  client: WsClient | null,
  task: Task | null,
  open: boolean,
  method: string,
): { result: T | null; loading: boolean; error: string | null; ready: boolean } {
  const [result, setResult] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The dialog opens only once its content is known (a dry run is a local,
  // millisecond call), so the panel never resizes while it is animating in.
  // If the daemon is slow the dialog opens in its loading state instead.
  const [slow, setSlow] = useState(false);

  useEffect(() => {
    if (!open || !task || !client) return;
    const reqTaskId = task.ID;
    let cancelled = false;
    setResult(null);
    setError(null);
    setLoading(true);
    setSlow(false);
    const slowTimer = setTimeout(() => setSlow(true), SLOW_DRY_RUN_MS);
    client
      .call<T>(method, { taskId: task.ID, dryRun: true })
      .then((r) => {
        if (cancelled || task.ID !== reqTaskId) return;
        setResult(r);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled || task.ID !== reqTaskId) return;
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
      clearTimeout(slowTimer);
    };
    // task.ID, not the task object: the header's live task is a new object on
    // every task.updated, which must not restart a dry run mid-confirm.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, task?.ID, client, method]);

  return { result, loading, error, ready: !loading || slow };
}

export function ArchiveFinishedDialog({
  client,
  task,
  open,
  onOpenChange,
  onArchived,
}: {
  client: WsClient | null;
  task: Task | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onArchived: () => void;
}) {
  const { result, loading, error, ready } = useDryRun<TaskArchiveFinishedResult>(client, task, open, "task.archiveFinished");
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<TaskArchiveFinishedResult | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setOutcome(null);
      setConfirmError(null);
    }
  }, [open]);

  // The dry run stays the source of the selection even after a confirm: the
  // confirm's own result (task.archiveFinished non-dry) is shown only as the
  // outcome.
  const dry = result;
  const eligible = dry?.eligible ?? [];
  const kept = dry?.skipped ?? [];

  return (
    <Dialog open={open && ready} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {outcome
              ? `Archived ${outcome.archived.length} of ${eligible.length} finished sub-tasks`
              : !dry
                ? "Archive finished sub-tasks?"
                : `Archive ${eligible.length === 1 ? "1 finished sub-task" : `${eligible.length} finished sub-tasks`}?`}
          </DialogTitle>
          <DialogDescription asChild>
            <div>
              <span className="font-medium text-foreground">{task?.Title}</span>
              <p className="mt-2">
                Uncommitted work is checkpointed to each task's branch before its worktree is removed. Descendants only
                — this task itself is never archived.
              </p>
              {loading && <p className="mt-2 font-normal text-foreground-muted">Counting finished sub-tasks…</p>}
              {outcome && outcome.failed.length > 0 && (
                <div data-testid="archive-finished-failures" className="mt-2">
                  <p className="font-normal text-destructive">
                    {outcome.failed.length === 1 ? "1 task could not be archived" : `${outcome.failed.length} tasks could not be archived`}:
                  </p>
                  <ul className="mt-1 list-disc pl-5">
                    {outcome.failed.slice(0, MAX_VISIBLE).map((f) => (
                      <li key={f.id}>
                        {f.title} — {f.error}
                      </li>
                    ))}
                    {outcome.failed.length > MAX_VISIBLE && <li>and {outcome.failed.length - MAX_VISIBLE} more</li>}
                  </ul>
                </div>
              )}
              {!loading && !error && !outcome && eligible.length > 0 && (
                <>
                  <p className="mt-2">Finished sub-tasks to archive:</p>
                  <ItemList testId="archive-finished-list" items={eligible} />
                  {(dry?.remaining ?? 0) > 0 && (
                    <p className="mt-1 font-normal text-foreground-muted">
                      and {dry!.remaining} more beyond the 100-task limit
                    </p>
                  )}
                  {kept.length > 0 && (
                    <p data-testid="archive-finished-kept" className="mt-2 font-normal text-foreground-muted">
                      {kept.length === 1 ? "1 finished task is kept" : `${kept.length} finished tasks are kept`}:{" "}
                      {[...new Set(kept.map((k) => keptReason(k.reason)))].join("; ")}.
                    </p>
                  )}
                </>
              )}
              {!loading && !error && !outcome && dry && eligible.length === 0 && (
                <p data-testid="archive-finished-none" className="mt-2 font-normal text-foreground-muted">
                  No finished sub-tasks to archive.
                </p>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>
        <FormError message={confirmError ?? error} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={pending || loading || !!error || eligible.length === 0}
            data-testid="dialog-archive-finished-confirm"
            onClick={async () => {
              if (!task) return;
              setPending(true);
              setConfirmError(null);
              try {
                const done = await client!.call<TaskArchiveFinishedResult>("task.archiveFinished", {
                  taskId: task.ID,
                  // Pinned to what the dry run showed: a task that finished while the dialog was open is not archived unseen.
                  ids: eligible.map((e) => e.id),
                });
                if (done.failed.length > 0) {
                  setOutcome(done);
                } else {
                  toast({ variant: "success", title: `Archived ${done.archived.length} finished sub-task${done.archived.length === 1 ? "" : "s"}` });
                  onArchived();
                  onOpenChange(false);
                }
              } catch (err) {
                setConfirmError(err instanceof Error ? err.message : String(err));
              } finally {
                setPending(false);
              }
            }}
          >
            {pending ? "Archiving…" : outcome ? "Try again" : eligible.length > 0 ? `Archive ${eligible.length}` : "Archive"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function StopTreeDialog({
  client,
  task,
  open,
  onOpenChange,
  onStopped,
}: {
  client: WsClient | null;
  task: Task | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStopped: () => void;
}) {
  const { result, loading, error, ready } = useDryRun<TaskStopTreeResult>(client, task, open, "task.stopTree");
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<TaskStopTreeResult | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setOutcome(null);
      setConfirmError(null);
    }
  }, [open]);

  // The dry run stays the source of the selection: a non-dry task.stopTree
  // result leaves runs/queuedPrompts empty and reports stopped/failed instead.
  const dry = result;
  const runs = dry?.runs ?? [];
  const queued = dry?.queuedPrompts ?? 0;
  const includesSelf = task != null && runs.some((r) => r.id === task.ID);

  return (
    <Dialog open={open && ready} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {outcome
              ? `Stopped ${outcome.stopped.length} of ${runs.length} running tasks`
              : !dry
                ? "Stop running tasks in this tree?"
                : `Stop ${runs.length === 1 ? "1 running task" : `${runs.length} running tasks`} in this tree?`}
          </DialogTitle>
          <DialogDescription asChild>
            <div>
              <span className="font-medium text-foreground">{task?.Title}</span>
              <p className="mt-2">Every run in the tree is stopped and each task's work is left as-is.</p>
              {loading && <p className="mt-2 font-normal text-foreground-muted">Counting running tasks…</p>}
              {outcome && outcome.failed.length > 0 && (
                <div data-testid="stop-tree-failures" className="mt-2">
                  <p className="font-normal text-destructive">
                    {outcome.failed.length === 1 ? "1 task could not be stopped" : `${outcome.failed.length} tasks could not be stopped`}:
                  </p>
                  <ul className="mt-1 list-disc pl-5">
                    {outcome.failed.slice(0, MAX_VISIBLE).map((f) => (
                      <li key={f.id}>
                        {f.title} — {f.error}
                      </li>
                    ))}
                    {outcome.failed.length > MAX_VISIBLE && <li>and {outcome.failed.length - MAX_VISIBLE} more</li>}
                  </ul>
                </div>
              )}
              {!loading && !error && !outcome && runs.length > 0 && (
                <>
                  <p className="mt-2">Tasks that will be stopped:</p>
                  <ItemList testId="stop-tree-list" items={runs} />
                </>
              )}
              {!loading && !error && !outcome && (
                <ul className="mt-2 list-none space-y-1 font-normal text-foreground-muted">
                  {includesSelf && (
                    <li data-testid="stop-tree-includes-self">Includes this task itself — it is running too.</li>
                  )}
                  {queued > 0 && (
                    <li data-testid="stop-tree-queued-note">
                      {queued} queued prompt{queued === 1 ? "" : "s"} will be cancelled.
                    </li>
                  )}
                  {dry && runs.length === 0 && (
                    <li data-testid="stop-tree-none">No running tasks in this tree.</li>
                  )}
                </ul>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>
        <FormError message={confirmError ?? error} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={pending || loading || !!error || runs.length === 0}
            data-testid="dialog-stop-tree-confirm"
            onClick={async () => {
              if (!task) return;
              setPending(true);
              setConfirmError(null);
              try {
                const done = await client!.call<TaskStopTreeResult>("task.stopTree", { taskId: task.ID });
                if (done.failed.length > 0) {
                  setOutcome(done);
                } else {
                  toast({ variant: "success", title: `Stopped ${done.stopped.length} task${done.stopped.length === 1 ? "" : "s"} in the tree` });
                  onStopped();
                  onOpenChange(false);
                }
              } catch (err) {
                setConfirmError(err instanceof Error ? err.message : String(err));
              } finally {
                setPending(false);
              }
            }}
          >
            {pending ? "Stopping…" : outcome ? "Try again" : runs.length > 0 ? `Stop ${runs.length}` : "Stop"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
