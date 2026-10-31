import { create } from "zustand";

import type { RunConfigState } from "@/components/composer/run-config-toolbar";

const PENDING_SEND_TTL_MS = 30_000;

interface PendingSendEntry {
  taskId: number;
  config: RunConfigState;
  at: number;
}

interface PendingSendStore {
  entry: PendingSendEntry | null;
  /** The home landing hands a first prompt to a new task's composer; the composer takes it once. */
  set: (taskId: number, config: RunConfigState) => void;
  /** Returns the config and clears the entry atomically; null for another task (entry kept) or an expired entry (cleared). */
  take: (taskId: number) => RunConfigState | null;
  /** Same checks as take without clearing. */
  peek: (taskId: number) => boolean;
}

export const usePendingSendStore = create<PendingSendStore>((set, get) => ({
  entry: null,
  set: (taskId, config) => set({ entry: { taskId, config, at: Date.now() } }),
  take: (taskId) => {
    const { entry } = get();
    if (!entry || entry.taskId !== taskId) return null;
    set({ entry: null });
    return Date.now() - entry.at <= PENDING_SEND_TTL_MS ? entry.config : null;
  },
  peek: (taskId) => {
    const { entry } = get();
    return entry !== null && entry.taskId === taskId && Date.now() - entry.at <= PENDING_SEND_TTL_MS;
  },
}));
