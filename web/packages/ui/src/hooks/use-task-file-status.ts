import { useCallback, useEffect, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { RunStatusEventPayload, Task, TaskFile, TaskFilesResult } from "@/lib/types";

export interface TaskFileStatusState {
  /** null until the first task.files call resolves; [] for a task with no changes. */
  files: TaskFile[] | null;
  /** Worktree-relative path -> that file's change kind, for O(1) lookup while rendering a tree row. */
  statusByPath: Map<string, TaskFile["status"]>;
  error: string | null;
  loading: boolean;
  refresh: () => void;
}

/**
 * The task's changed-file list (task.files), shared by every surface that
 * needs to decorate a path with its git status -- the explorer tree's
 * status markers (ui-redesign-parity plan, Item 17) today, the diff stat
 * later (Item 19). Refetches on the same signal diff-viewer-pane.tsx
 * already uses: a *terminal* run.status for this task, meaning the agent
 * just stopped changing files.
 *
 * Deliberately read-only -- it owns no staged/viewed state, so
 * diff-viewer-pane keeps owning its own list (which it mutates in place on
 * stage/unstage) rather than being refactored onto this.
 */
export function useTaskFileStatus(
  client: { call<T>(method: string, params?: unknown): Promise<T> } | null,
  task: Task | null,
  events?: DaemonEvents | null,
): TaskFileStatusState {
  const [files, setFiles] = useState<TaskFile[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const taskId = task?.ID ?? null;

  const refresh = useCallback(() => {
    if (!client || taskId === null) return;
    setLoading(true);
    setError(null);
    client
      .call<TaskFilesResult>("task.files", { taskId })
      .then((result) => {
        setFiles(result?.files ?? []);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [client, taskId]);

  useEffect(() => {
    setFiles(null);
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

  const statusByPath = new Map<string, TaskFile["status"]>();
  for (const f of files ?? []) statusByPath.set(f.path, f.status);

  return { files, statusByPath, error, loading, refresh };
}
