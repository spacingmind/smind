import { useCallback, useEffect, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { RunStatusEventPayload, Task, TaskSearchIndexResult } from "@/lib/types";

export interface TaskSearchIndexState {
  /** null until task.searchIndex resolves for the first time. */
  paths: string[] | null;
  error: string | null;
  loading: boolean;
  refresh: () => void;
}

/**
 * The whole-worktree path list quick-open fuzzy-matches against
 * (ui-redesign-parity plan, Item 18), fetched once via `task.searchIndex`
 * rather than walked directory-by-directory over `file.list` -- see
 * `internal/workspace/search.go`'s doc comment for the measurement that
 * decided this.
 *
 * Refreshes on the same signal every other task-scoped fetch in this
 * plan uses: a *terminal* `run.status` for this task, since a new file
 * the agent just created should be quick-openable without a reload.
 */
export function useTaskSearchIndex(
  client: { call<T>(method: string, params?: unknown): Promise<T> } | null,
  task: Task | null,
  events?: DaemonEvents | null,
): TaskSearchIndexState {
  const [paths, setPaths] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const taskId = task?.ID ?? null;

  const refresh = useCallback(() => {
    if (!client || taskId === null) return;
    setLoading(true);
    setError(null);
    client
      .call<TaskSearchIndexResult>("task.searchIndex", { taskId })
      .then((result) => {
        setPaths(result?.paths ?? []);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [client, taskId]);

  useEffect(() => {
    setPaths(null);
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

  return { paths, error, loading, refresh };
}
