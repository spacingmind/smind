import { useCallback, useEffect, useMemo, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { EMPTY_DIFF_STAT, parseDiffStat, type DiffStat } from "@/lib/diff-stat";
import type { RunStatusEventPayload, Task, TaskDiffResult } from "@/lib/types";

export interface TaskDiffState {
  /** The task's whole base→worktree diff, or null until task.diff resolves. "" for a task with no changes. */
  diff: string | null;
  /** files / +lines / −lines, derived from `diff` -- all zeroes until it lands. */
  stat: DiffStat;
  error: string | null;
  loading: boolean;
  refresh: () => void;
}

/**
 * The task's whole diff plus its stat (ui-redesign-parity plan, Item 19).
 *
 * One fetch serves both of the item's needs: the whole-diff view renders
 * `diff`, and the stat the item asks to surface *outside* the pane is
 * derived from the same text -- so the sidebar (Item 12) or the composer
 * can consume this hook without the diff pane being mounted at all, and
 * without a new daemon RPC.
 *
 * Refreshes on the same signal the rest of the diff surface uses: a
 * *terminal* run.status for this task, i.e. the agent just stopped
 * changing files.
 */
export function useTaskDiff(
  client: { call<T>(method: string, params?: unknown): Promise<T> } | null,
  task: Task | null,
  events?: DaemonEvents | null,
): TaskDiffState {
  const [diff, setDiff] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const taskId = task?.ID ?? null;

  const refresh = useCallback(() => {
    if (!client || taskId === null) return;
    setLoading(true);
    setError(null);
    client
      .call<TaskDiffResult>("task.diff", { taskId })
      .then((result) => {
        setDiff(result?.diff ?? "");
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [client, taskId]);

  useEffect(() => {
    setDiff(null);
    setError(null);
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!events || taskId === null) return;
    return events.subscribe("run.status", (payload) => {
      const p = payload as Partial<RunStatusEventPayload>;
      if (p.taskId !== taskId) return;
      if (p.status !== "done" && p.status !== "error" && p.status !== "stopped") return;
      refresh();
    });
  }, [events, refresh, taskId]);

  // Re-parsing a large diff on every unrelated render (a keystroke in the
  // commit box, say) is the one part of this that isn't free.
  const stat = useMemo(() => (diff === null ? EMPTY_DIFF_STAT : parseDiffStat(diff)), [diff]);

  return { diff, stat, error, loading, refresh };
}
