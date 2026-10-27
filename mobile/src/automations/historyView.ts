// historyView.ts: the run-history rows (automation.audit) as plain data, and the paging rule. Reasons, outcomes and
// notes are daemon-provided strings (some echo signal text), drawn only through <Text>.

import { whenText, StatusTone } from './listView';
import { AutomationAuditRow } from './types';

const DECISIONS: Record<string, { label: string; tone: StatusTone | 'destructive' }> = {
  fire: { label: 'Fired', tone: 'success' },
  deduped: { label: 'Deduped', tone: 'neutral' },
  capped: { label: 'Capped', tone: 'warning' },
  paused: { label: 'Paused', tone: 'warning' },
  missed: { label: 'Missed', tone: 'warning' },
  disabled_kill_switch: { label: 'Kill switch', tone: 'warning' },
  failed: { label: 'Failed', tone: 'destructive' },
};

/** The decision chip text: a known decision gets a short label, an unknown one is shown as the daemon sent it. */
export function decisionChip(decision: string): { label: string; tone: StatusTone | 'destructive' } {
  return DECISIONS[decision] ?? { label: decision || 'Unknown', tone: 'neutral' };
}

export interface AuditRowView {
  id: number;
  when: string;
  decision: string;
  tone: StatusTone | 'destructive';
  reason: string | null;
  outcome: string | null;
  /** 'Task 12' for a fire that created a task, else null. */
  task: string | null;
  taskId: number | null;
  /** 'Manual', 'Catch-up' markers, joined; null when neither. */
  origin: string | null;
}

export function auditRowView(r: AutomationAuditRow): AuditRowView {
  const chip = decisionChip(r.decision);
  const origin = [r.manual ? 'Manual' : '', r.catchup ? 'Catch-up' : ''].filter(Boolean).join(' · ');
  return {
    id: r.id,
    when: whenText(r.createdAt),
    decision: chip.label,
    tone: chip.tone,
    reason: r.reason ? r.reason : null,
    outcome: r.outcome ? r.outcome : null,
    task: r.taskId !== null && r.taskId !== undefined ? `Task ${r.taskId}` : null,
    taskId: r.taskId ?? null,
    origin: origin ? origin : null,
  };
}

export interface HistoryPages {
  rows: AutomationAuditRow[];
  /** True while the last page was full, so another page may exist. */
  more: boolean;
}

/** The `before` cursor for the next page: the id of the oldest row shown (rows come newest first); 0 = the top. */
export function nextBefore(rows: readonly AutomationAuditRow[]): number {
  return rows.length === 0 ? 0 : rows[rows.length - 1].id;
}

/** Appends a page to what is shown (a repeated id is dropped) and decides whether to offer "Load more". */
export function appendPage(shown: HistoryPages, page: readonly AutomationAuditRow[], limit: number): HistoryPages {
  const seen = new Set(shown.rows.map((r) => r.id));
  return { rows: [...shown.rows, ...page.filter((r) => !seen.has(r.id))], more: page.length >= limit };
}

export const HISTORY_EMPTY_TEXT = 'This automation has not run yet.';
