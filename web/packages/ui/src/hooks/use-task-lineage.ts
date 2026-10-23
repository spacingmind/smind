import { useCallback, useEffect, useRef, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { childrenOf, type TaskTreeResult, type TaskTreeNode } from "@/lib/task-lineage";
import type { Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Bursts of task/run events coalesce into one refetch (a strip of pills doesn't need per-event RPCs). */
const REFETCH_DEBOUNCE_MS = 150;

export interface TaskLineage {
  /** The parent task (null for a root or until task.get resolves), plus whether it's archived (an archived parent is an inert breadcrumb). */
  parent: { task: Task; archived: boolean } | null;
  /** The queried task's own tree node (null until task.tree resolves, or forever on a daemon without it). */
  node: TaskTreeNode | null;
  /** The task's live direct children, in the tree's sibling order. */
  children: TaskTreeNode[];
  /** Every node task.tree returned (the task and its whole subtree), so a pill can tell a rate-limited descendant from a failed one (ADR-0041); absent in hand-built lineages. */
  nodes?: TaskTreeNode[];
}

/**
 * Where a task sits in the hierarchy (task-hierarchy-tiers PR-U2): its
 * parent (breadcrumb) via task.get and its subtree via task.tree -- nodes
 * with parentId === task.ID are its live direct children. Refetches
 * (debounced) on task/run/permission lifecycle events, on a client change
 * (reconnect) and on event.dropped (events may have been missed while
 * disconnected). A failing call never throws: console.error and keep the
 * previous value, so a daemon without task.tree simply shows no strip.
 */
export function useTaskLineage(
  client: WsClientLike | null,
  task: Task,
  events: DaemonEvents | null | undefined,
): TaskLineage {
  const [parent, setParent] = useState<TaskLineage["parent"]>(null);
  const [tree, setTree] = useState<TaskTreeResult | null>(null);

  // The identity-relevant fields only: callers hand down a task object
  // whose identity changes on every parent render (useLiveTask's snapshot
  // included), which would re-run the fetch effect and reset the lineage
  // on each of those. The task object itself is read through a ref so the
  // fetch always sees the current ParentTaskID without joining the deps.
  const taskRef = useRef(task);
  taskRef.current = task;

  // Stale-response guards: the task/client a request was issued for. A
  // late answer for a superseded selection must never clobber the new one.
  const taskIdRef = useRef(task.ID);
  const clientRef = useRef<WsClientLike | null>(client);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchAll = useCallback(() => {
    const current = taskRef.current;
    if (!client) return;
    const reqTaskId = current.ID;
    const reqClient = client;
    if (current.ParentTaskID !== null && current.ParentTaskID !== undefined) {
      client
        .call<Task>("task.get", { id: current.ParentTaskID })
        .then((got) => {
          if (taskIdRef.current !== reqTaskId || clientRef.current !== reqClient) return;
          setParent({ task: got, archived: got.ArchivedAt != null });
        })
        .catch((err) => console.error("task.get (parent) failed, keeping previous parent", err));
    }
    client
      .call<TaskTreeResult>("task.tree", { taskId: current.ID })
      .then((result) => {
        if (taskIdRef.current !== reqTaskId || clientRef.current !== reqClient) return;
        setTree(result);
      })
      .catch((err) => console.error("task.tree failed, keeping previous lineage", err));
  }, [client]);

  const parentId = task.ParentTaskID ?? null;
  useEffect(() => {
    taskIdRef.current = task.ID;
    clientRef.current = client;
    setParent(null);
    setTree(null);
    fetchAll();

    if (!events) return;
    // One debounced refetch for every event kind that can move the strip:
    // task lifecycle (created/updated/archived/deleted in the subtree),
    // run.status (a child's pill state), permission.pending (waiting on
    // you), and event.dropped (anything missed while disconnected).
    const schedule = () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        fetchAll();
      }, REFETCH_DEBOUNCE_MS);
    };
    const offs = [
      "task.created",
      "task.updated",
      "task.archived",
      "task.deleted",
      "run.status",
      "permission.pending",
      "event.dropped",
    ].map((topic) => events.subscribe(topic, schedule));
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      for (const off of offs) off();
    };
  }, [client, events, task.ID, parentId, fetchAll]);

  const node = tree?.nodes[0] ?? null;
  return { parent, node, children: tree ? childrenOf(tree.nodes, task.ID) : [], ...(tree ? { nodes: tree.nodes } : {}) };
}
