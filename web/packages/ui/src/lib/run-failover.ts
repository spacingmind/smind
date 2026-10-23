/**
 * The task's failover chain (ADR-0041 F2): a rate-limited run that was
 * retried elsewhere has a successor (`FailoverOf === run.id`), usually in a
 * different chat, so the index is built from the task-wide run list rather
 * than one chat's timeline. Pure.
 */

/** The run.list fields the index reads. */
export interface FailoverRow {
  ID: string;
  ChatID: number;
  Provider: string;
  FailoverOf?: string;
}

export interface FailoverSuccessor {
  runId: string;
  chatId: number;
  provider: string;
}

export interface FailoverIndex {
  /** failed run id -> the run that continues it. */
  successors: ReadonlyMap<string, FailoverSuccessor>;
  /** successor run id -> the failed run it continues. */
  continues: ReadonlyMap<string, string>;
}

export const EMPTY_FAILOVER_INDEX: FailoverIndex = { successors: new Map(), continues: new Map() };

export function buildFailoverIndex(rows: readonly FailoverRow[]): FailoverIndex {
  const successors = new Map<string, FailoverSuccessor>();
  const continues = new Map<string, string>();
  for (const r of rows) {
    if (!r.FailoverOf) continue;
    successors.set(r.FailoverOf, { runId: r.ID, chatId: r.ChatID, provider: r.Provider });
    continues.set(r.ID, r.FailoverOf);
  }
  return { successors, continues };
}
