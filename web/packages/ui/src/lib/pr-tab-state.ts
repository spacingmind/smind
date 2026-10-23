import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/**
 * Whether a task's PR tab has been dealt with (G9): "auto-opened" once it opened by itself, "opted-out" once the user
 * closed it. Either way it is not auto-opened again, so a reload after a close does not bring it back. Persisted per
 * task id; the manual "Pull request" entry still opens it.
 */
export type PRTabState = "auto-opened" | "opted-out";

const MAX_TASKS = 200;

function readAll(): Record<string, PRTabState> {
  return readStored(
    STORAGE_KEYS.prTabState,
    {},
    (v): v is Record<string, PRTabState> =>
      typeof v === "object" && v !== null && !Array.isArray(v) && Object.values(v).every((s) => s === "auto-opened" || s === "opted-out"),
  );
}

export function readPRTabState(taskId: number): PRTabState | undefined {
  return readAll()[String(taskId)];
}

/** Records the state; an opt-out is never downgraded back to "auto-opened". */
export function writePRTabState(taskId: number, state: PRTabState): void {
  const all = readAll();
  const key = String(taskId);
  if (all[key] === "opted-out" && state === "auto-opened") return;
  delete all[key];
  const entries = Object.entries({ ...all, [key]: state });
  writeStored(STORAGE_KEYS.prTabState, Object.fromEntries(entries.slice(-MAX_TASKS)));
}

/** The task id behind a `${taskId}:pr` tab key, else null. */
export function prTabTaskId(key: string): number | null {
  const m = /^(\d+):pr$/.exec(key);
  return m ? Number(m[1]) : null;
}
