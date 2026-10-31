/** Wire shapes and pure helpers of the Human inbox (`inbox.*`, ADR-0045 section 5). */

import type { FailureAction } from "@/lib/failure-class";

export type InboxKind = "permission" | "run_failed" | "verdict" | "stall" | "host_pair" | "host_down" | "host_refused";
export type InboxState = "new" | "seen" | "snoozed" | "resolved";

export interface InboxItem {
  key: string;
  kind: InboxKind;
  taskId?: number;
  title: string;
  summary: string;
  createdAt: string;
  state: InboxState;
  snoozedUntil?: string;
  attention: "high" | "normal";
  /** A run_failed item's next action (the daemon-built failure action); absent for every other kind. */
  action?: FailureAction;
  links: { taskId?: number; runId?: string; permissionRequestId?: string; validationId?: number; hostId?: string };
}

export interface InboxCounts {
  total: number;
  unseen: number;
  byKind: Partial<Record<InboxKind, number>>;
}

/** The groups of the page, in display order. */
export const INBOX_GROUPS: { kind: InboxKind; label: string }[] = [
  { kind: "permission", label: "Needs you now" },
  { kind: "run_failed", label: "Runs that need you" },
  { kind: "host_pair", label: "Host pairing" },
  { kind: "host_refused", label: "Hosts refused" },
  { kind: "host_down", label: "Hosts unreachable" },
  { kind: "verdict", label: "Validator verdicts" },
  { kind: "stall", label: "Stalled" },
];

export function groupInbox(items: InboxItem[]): { kind: InboxKind; label: string; items: InboxItem[] }[] {
  return INBOX_GROUPS.map((g) => ({ ...g, items: items.filter((i) => i.kind === g.kind) })).filter((g) => g.items.length > 0);
}

/** A permission can only be answered: no snooze, no resolve (the daemon refuses both). */
export function canSnoozeOrResolve(item: Pick<InboxItem, "kind">): boolean {
  return item.kind !== "permission";
}

export const SNOOZE_OPTIONS: { id: string; label: string; ms: number }[] = [
  { id: "1h", label: "1 hour", ms: 60 * 60_000 },
  { id: "4h", label: "4 hours", ms: 4 * 60 * 60_000 },
  { id: "tomorrow", label: "Tomorrow (24 hours)", ms: 24 * 60 * 60_000 - 60_000 },
];

/** RFC 3339 instant `ms` from `now`, kept under the daemon's 24 hour cap. */
export function snoozeUntil(ms: number, now: number = Date.now()): string {
  return new Date(now + Math.min(ms, 24 * 60 * 60_000 - 1000)).toISOString();
}

/** "3 need you" / "Nothing needs you": the polite live-region text for the badge. */
export function inboxAnnouncement(unseen: number): string {
  if (unseen <= 0) return "Inbox is clear";
  return unseen === 1 ? "1 item needs you" : `${unseen} items need you`;
}
