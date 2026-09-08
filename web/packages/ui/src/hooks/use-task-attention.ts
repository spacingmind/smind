import { useCallback, useEffect, useRef, useState } from "react";

import type { WsClientLike } from "@/lib/ws-client";
import type { RunLogEvent, RunLogsResult, RunSummary } from "@/lib/types";

/** Why a task currently has the sidebar's attention marker. */
export type AttentionReason = "error" | "finished" | "permission";

/** Task ids that currently warrant an attention dot, with the reasons why. */
export type TaskAttention = Map<number, Set<AttentionReason>>;

/**
 * True if events contain a permission_request whose requestId has no
 * later permission_resolved -- i.e. a permission somebody still has to
 * answer. Order-sensitive by construction: a resolved event only clears
 * a request that was already seen, matching the wire order the daemon
 * emits them in.
 */
function hasUnresolvedPermission(events: RunLogEvent[]): boolean {
  const pending = new Set<string>();
  for (const e of events) {
    if (e.type === "permission_request" && e.requestId) pending.add(e.requestId);
    else if (e.type === "permission_resolved" && e.requestId) pending.delete(e.requestId);
  }
  return pending.size > 0;
}

/**
 * Derives the sidebar's per-task attention badges from data the app
 * already pulls on (re)connect -- deliberately NOT new push/subscribe
 * plumbing (that's the separate wsapi-event-subscription task; the plan
 * accepts refresh/reconnect-driven updates for this pass).
 *
 - On every client (re)connect: run.list for all runs, then run.logs for
 * each still-running run to detect an unresolved permission request.
 * - On selectedTaskId change: snapshot that task's terminal run ids as
 * "seen", which is what clears its error/finished badges -- the dot means
 * "something ended or needs you that you haven't looked at yet".
 */
export function useTaskAttention(client: WsClientLike | null, selectedTaskId: number | null): TaskAttention {
  const [attention, setAttention] = useState<TaskAttention>(new Map());

  // Latest-known run summaries and permission-pending task ids, kept in
  // refs so the selection-change effect can re-derive badges without
  // refetching.
  const runsRef = useRef<RunSummary[] | null>(null);
  const pendingPermissionTasksRef = useRef<Set<number>>(new Set());
  /** Per task, the terminal run ids already observed at selection time. */
  const seenRef = useRef<Map<number, Set<string>>>(new Map());

  const recompute = useCallback(() => {
    const runs = runsRef.current;
    if (!runs) return;

    const next: TaskAttention = new Map();
    const add = (taskId: number, reason: AttentionReason) => {
      const set = next.get(taskId) ?? new Set<AttentionReason>();
      set.add(reason);
      next.set(taskId, set);
    };

    for (const run of runs) {
      if (run.Status !== "running") {
        const seen = seenRef.current.get(run.TaskID);
        if (!seen?.has(run.ID)) {
          add(run.TaskID, run.Status === "error" ? "error" : "finished");
        }
      }
    }
    for (const taskId of pendingPermissionTasksRef.current) {
      add(taskId, "permission");
    }

    setAttention(next);
  }, []);

  useEffect(() => {
    if (!client) {
      runsRef.current = null;
      pendingPermissionTasksRef.current = new Set();
      setAttention(new Map());
      return;
    }

    let cancelled = false;

    client
      .call<RunSummary[]>("run.list")
      .then(async (runs) => {
        if (cancelled) return;
        runsRef.current = runs;

        const pending = new Set<number>();
        await Promise.all(
          runs
            .filter((r) => r.Status === "running")
            .map((r) =>
              client
                .call<RunLogsResult>("run.logs", { runId: r.ID })
                .then((logs) => {
                  if (!cancelled && hasUnresolvedPermission(logs.events)) pending.add(r.TaskID);
                })
                .catch(() => {
                  // Best-effort detection -- a failed run.logs just means
                  // no permission badge from this pass, not an error state.
                }),
            ),
        );
        if (cancelled) return;
        pendingPermissionTasksRef.current = pending;
        recompute();
      })
      .catch(() => {
        // No run data -> no badges; a reconnect re-runs this effect.
      });

    return () => {
      cancelled = true;
    };
  }, [client, recompute]);

  useEffect(() => {
    if (selectedTaskId === null) return;
    const runs = runsRef.current ?? [];
    const terminal = new Set(runs.filter((r) => r.TaskID === selectedTaskId && r.Status !== "running").map((r) => r.ID));
    seenRef.current.set(selectedTaskId, terminal);
    recompute();
  }, [selectedTaskId, recompute]);

  return attention;
}
