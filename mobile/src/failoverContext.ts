// failoverContext.ts: the pure lookups behind the footer's ADR-0041
// failover lines -- which queued item is the held retry of a run, and
// which run is its failover successor. Free of react-native imports.

import { ChatQueueItem, RunSummary } from './api';

/**
 * When the held retry of a run with `prompt` starts: the NotBefore of the
 * earliest still-queued item carrying that prompt, or null when there is none.
 */
export function findHeldUntil(items: readonly ChatQueueItem[], prompt: string): Date | null {
  let best: Date | null = null;
  for (const item of items) {
    if (item.Status !== 'queued' || !item.NotBefore || item.Prompt !== prompt) continue;
    const at = new Date(item.NotBefore);
    if (Number.isNaN(at.getTime())) continue;
    if (best === null || at.getTime() < best.getTime()) best = at;
  }
  return best;
}

/** The provider and chat of the run that continues `runId` (its FailoverOf), or null. */
export function findSuccessor(runs: readonly RunSummary[], runId: string): { provider: string; chatId: number } | null {
  const next = runs.find((r) => r.FailoverOf === runId && r.ChatID !== undefined);
  return next ? { provider: next.Provider, chatId: next.ChatID as number } : null;
}
