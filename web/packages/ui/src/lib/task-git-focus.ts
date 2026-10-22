import { useEffect, useRef } from "react";
import { create } from "zustand";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";

/**
 * The ONE task whose git state the daemon keeps fresh (`task.gitFocus`, ADR-0039 §3). Every mounted
 * `useTaskGitFocus(taskId)` pushes an entry; the last mounted wins and unmounting restores the previous
 * one, so two surfaces never fight over it.
 */
interface FocusEntry {
  key: symbol;
  taskId: number;
}

interface TaskGitFocusState {
  stack: FocusEntry[];
  push: (entry: FocusEntry) => void;
  remove: (key: symbol) => void;
}

export const useTaskGitFocusStore = create<TaskGitFocusState>()((set) => ({
  stack: [],
  push: (entry) => set((s) => ({ stack: [...s.stack, entry] })),
  remove: (key) => set((s) => ({ stack: s.stack.filter((e) => e.key !== key) })),
}));

/** The task the daemon should focus right now, or null. */
export function focusedTaskId(): number | null {
  const { stack } = useTaskGitFocusStore.getState();
  return stack.length === 0 ? null : stack[stack.length - 1]!.taskId;
}

interface FocusClient {
  call<T>(method: string, params?: unknown): Promise<T>;
}

// The focus is per connection (the daemon drops it on disconnect), so what was last sent is tracked per client.
// A client not in the map has no focus at the daemon.
const sent = new WeakMap<object, number | null>();

function syncFocus(client: FocusClient): void {
  const want = focusedTaskId();
  if ((sent.get(client) ?? null) === want) return;
  sent.set(client, want);
  client.call("task.gitFocus", { taskId: want }).catch(() => {
    // Not delivered: forget it so the next sync sends again.
    if (sent.get(client) === want) sent.delete(client);
  });
}

/**
 * While mounted with a non-null id, makes `taskId` the daemon's focused task (and, after a reconnect, sends
 * it again on the new connection); clears it (`{taskId: null}`) once no mounted caller wants one.
 */
export function useTaskGitFocus(taskId: number | null): void {
  const { client } = useDaemonConnection();
  const clientRef = useRef(client);
  clientRef.current = client;

  useEffect(() => {
    if (taskId === null) return;
    const key = Symbol("task-git-focus");
    useTaskGitFocusStore.getState().push({ key, taskId });
    if (clientRef.current) syncFocus(clientRef.current);
    return () => {
      useTaskGitFocusStore.getState().remove(key);
      if (clientRef.current) syncFocus(clientRef.current);
    };
  }, [taskId]);

  // A new client (reconnect) has no focus yet.
  useEffect(() => {
    if (client) syncFocus(client);
  }, [client]);
}
