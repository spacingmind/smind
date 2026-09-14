import { useCallback, useEffect, useRef, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type { RunLogEvent, RunLogsResult, RunStatusValue, RunSummary } from "@/lib/types";

/** Why a task currently has the sidebar's attention marker. */
export type AttentionReason = "error" | "finished" | "permission";

/** Task ids that currently warrant an attention dot, with the reasons why. */
export type TaskAttention = Map<number, Set<AttentionReason>>;

/** Stable "nothing known yet" value, so a client-less mount doesn't hand consumers a fresh object every render. */
const EMPTY_SIGNAL: TaskSignal = { attention: new Map(), runStatus: new Map() };

/**
 * The status of each task's latest run -- "running" if any of the task's
 * runs is still going, otherwise the terminal status of the most recently
 * started one. Tasks that have never run are absent rather than carrying
 * a fifth "idle" value: "no run yet" is not a run state, and the sidebar
 * row draws no dot for it.
 */
export type TaskRunStatus = ReadonlyMap<number, RunStatusValue>;

/** Both per-task signals this hook derives from the one run bookkeeping it keeps. */
export interface TaskSignal {
  attention: TaskAttention;
  runStatus: TaskRunStatus;
}

/**
 * The run fields the badge logic needs -- what run.status payloads and
 * RunSummary share -- plus a recency `order`, higher being more recent, so
 * "the task's latest run" is answerable without StartedAt (which
 * run.status payloads don't carry). run.list returns newest-first
 * (internal/runs.Registry.List sorts StartedAt descending), so its rows
 * take order 0, -1, -2...; every live event afterwards takes a positive
 * counter, putting it above the whole snapshot.
 */
interface RunLike extends Pick<RunSummary, "ID" | "TaskID" | "Status"> {
  order: number;
}

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
 * Derives the sidebar's per-task attention badges from two paths:
 *
 * - Live (when `events` is non-null): run.status and permission.pending
 *   notifications update badges immediately, no refetch -- a run
 *   finishing while another task is selected badges its task on arrival.
 * - Resync (every client change): run.list for all runs, then run.logs
 *   for each still-running run to detect an unresolved permission
 *   request. Delivery is live-only (ADR 0005), so this pass is what
 *   re-derives truth after a reconnect.
 *
 * On selectedTaskId change: snapshot that task's terminal run ids as
 * "seen", which is what clears its error/finished badges -- the dot means
 * "something ended or needs you that you haven't looked at yet".
 */
export function useTaskAttention(
  client: WsClientLike | null,
  selectedTaskId: number | null,
  events: DaemonEvents | null,
): TaskSignal {
  const [signal, setSignal] = useState<TaskSignal>(EMPTY_SIGNAL);

  // Latest-known run summaries and permission-pending task ids, kept in
  // refs so the selection-change effect can re-derive badges without
  // refetching.
  const runsRef = useRef<RunLike[] | null>(null);
  const pendingPermissionTasksRef = useRef<Set<number>>(new Set());
  /** Per task, the terminal run ids already observed at selection time. */
  const seenRef = useRef<Map<number, Set<string>>>(new Map());
  /** Monotonic counter handing every live run.status an `order` above the whole run.list snapshot's. */
  const orderRef = useRef(0);

  const recompute = useCallback(() => {
    const runs = runsRef.current;
    if (!runs) return;

    const attention: TaskAttention = new Map();
    const add = (taskId: number, reason: AttentionReason) => {
      const set = attention.get(taskId) ?? new Set<AttentionReason>();
      set.add(reason);
      attention.set(taskId, set);
    };

    // Latest run per task, for the sidebar's run-status dot: a run still
    // going always wins, so a task with one run finished and another
    // still in flight reads as running; otherwise the most recent by
    // `order`.
    const latest = new Map<number, RunLike>();

    for (const run of runs) {
      if (run.Status !== "running") {
        const seen = seenRef.current.get(run.TaskID);
        if (!seen?.has(run.ID)) {
          add(run.TaskID, run.Status === "error" ? "error" : "finished");
        }
      }
      const held = latest.get(run.TaskID);
      if (!held || (held.Status !== "running" && (run.Status === "running" || run.order > held.order))) {
        latest.set(run.TaskID, run);
      }
    }
    for (const taskId of pendingPermissionTasksRef.current) {
      add(taskId, "permission");
    }

    setSignal({
      attention,
      runStatus: new Map([...latest].map(([taskId, run]) => [taskId, run.Status])),
    });
  }, []);

  useEffect(() => {
    if (!client) {
      runsRef.current = null;
      pendingPermissionTasksRef.current = new Set();
      orderRef.current = 0;
      setSignal(EMPTY_SIGNAL);
      return;
    }

    let cancelled = false;

    client
      .call<RunSummary[]>("run.list")
      .then(async (rawRuns) => {
        if (cancelled) return;
        // Newest-first from the daemon, so index 0 is the most recent:
        // order 0, -1, -2... leaves every later live event above it.
        const runs: RunLike[] = (rawRuns ?? []).map((r, index) => ({
          ID: r.ID,
          TaskID: r.TaskID,
          Status: r.Status,
          order: -index,
        }));
        runsRef.current = runs;
        orderRef.current = 0;

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

  // Live path: badge (or clear) as run.status / permission.pending
  // notifications arrive -- no refetch, the notification IS the state
  // transition. Patch the ref-held run list in place; recompute turns
  // it into badges. A terminal run arriving while its task is selected
  // is marked seen directly (the user is watching it), so it never
  // badges; a terminal run for any other task stays unseen and badges.
  useEffect(() => {
    if (!events) return;

    const offRunStatus = events.subscribe("run.status", (payload) => {
      if (typeof payload !== "object" || payload === null) return;
      const p = payload as { runId?: unknown; taskId?: unknown; status?: unknown };
      if (typeof p.runId !== "string" || typeof p.taskId !== "number" || typeof p.status !== "string") return;

      const runs = runsRef.current ?? (runsRef.current = []);
      const idx = runs.findIndex((r) => r.ID === p.runId);
      const order = ++orderRef.current;
      if (idx >= 0) runs[idx] = { ...runs[idx], TaskID: p.taskId, Status: p.status as RunLike["Status"], order };
      else runs.push({ ID: p.runId, TaskID: p.taskId, Status: p.status as RunLike["Status"], order });

      if (p.status === "running") {
        // A (re)started run un-terminalizes: clear its seen marker and
        // any stale permission badge for the task.
        seenRef.current.get(p.taskId)?.delete(p.runId);
        pendingPermissionTasksRef.current.delete(p.taskId);
      } else if (selectedTaskId === p.taskId) {
        let seen = seenRef.current.get(p.taskId);
        if (!seen) {
          seen = new Set();
          seenRef.current.set(p.taskId, seen);
        }
        seen.add(p.runId);
      }
      recompute();
    });

    const offPermission = events.subscribe("permission.pending", (payload) => {
      if (typeof payload !== "object" || payload === null) return;
      const p = payload as { taskId?: unknown };
      if (typeof p.taskId !== "number") return;
      pendingPermissionTasksRef.current.add(p.taskId);
      recompute();
    });

    return () => {
      offRunStatus();
      offPermission();
    };
  }, [events, selectedTaskId, recompute]);

  useEffect(() => {
    if (selectedTaskId === null) return;
    const runs = runsRef.current ?? [];
    const terminal = new Set(runs.filter((r) => r.TaskID === selectedTaskId && r.Status !== "running").map((r) => r.ID));
    seenRef.current.set(selectedTaskId, terminal);
    recompute();
  }, [selectedTaskId, recompute]);

  return signal;
}
