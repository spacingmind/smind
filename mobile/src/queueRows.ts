// queueRows.ts holds the pure label logic for the daemon prompt queue's rows
// (ADR-0021 §9), ported from the web composer's queued-prompts.tsx wording
// so both renderers say the same thing. No React here: testable in isolation.

import { ChatQueueItem } from './api';

/** The status word shown before each prompt: QUEUED, NEXT (priority), or NOT SENT (auto-cancelled). */
export function queueRowLabel(item: Pick<ChatQueueItem, 'Status' | 'Priority'>): 'QUEUED' | 'NEXT' | 'NOT SENT' {
  if (item.Status === 'cancelled') return 'NOT SENT';
  if (item.Priority > 0) return 'NEXT';
  return 'QUEUED';
}

/** The cancel button's accessibility label, naming the first 40 characters of the prompt. */
export function cancelAccessibilityLabel(item: Pick<ChatQueueItem, 'Prompt'>): string {
  return `Cancel queued prompt: ${item.Prompt.slice(0, 40)}`;
}
