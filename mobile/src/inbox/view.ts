// view.ts turns inbox.list into what the Inbox screen draws, as plain data, so grouping, actions and the snooze cap
// are unit-testable without rendering. Titles and summaries are daemon-provided: drawn through <Text>, never
// interpreted. A permission can only be ANSWERED: it never gets Snooze or Resolve.

import { IconName } from '../ui/icons';
import { InboxItem, InboxKind } from './types';

export const INBOX_EMPTY_TITLE = 'Nothing needs you';
export const INBOX_EMPTY_TEXT = 'Permission requests, validator verdicts and stalled tasks, pairing requests and host links show up here.';

/** waiting: a permission asks for you; warning: a verdict that needs a decision (never a success tint); neutral: the rest. */
export type InboxTone = 'waiting' | 'warning' | 'neutral';
export type InboxAction = 'seen' | 'snooze' | 'resolve' | 'open' | 'hosts';

export const SNOOZE_CAP_MS = 24 * 60 * 60 * 1000;
/** Kept just under the daemon's 24 h limit so a slightly fast phone clock is never refused. */
const SNOOZE_SAFETY_MS = 60 * 1000;

export interface SnoozeOption {
  id: '1h' | '4h' | 'tomorrow';
  label: string;
  until: string;
}

export interface InboxRowView {
  key: string;
  kind: InboxKind;
  taskId: number | null;
  hostId: string | null;
  icon: IconName;
  label: string;
  title: string;
  summary: string;
  time: string;
  tone: InboxTone;
  unseen: boolean;
  actions: InboxAction[];
}

export interface InboxSection {
  kind: InboxKind;
  title: string;
  rows: InboxRowView[];
}

export type InboxView = { kind: 'empty'; title: string; text: string } | { kind: 'list'; sections: InboxSection[] };

const KIND_META: Record<InboxKind, { icon: IconName; label: string; section: string }> = {
  permission: { icon: 'lock', label: 'Permission', section: 'Needs you now' },
  host_pair: { icon: 'devices', label: 'Pairing', section: 'Host pairing' },
  host_refused: { icon: 'warning', label: 'Refused', section: 'Hosts refused' },
  host_down: { icon: 'link', label: 'Unreachable', section: 'Hosts unreachable' },
  verdict: { icon: 'check', label: 'Verdict', section: 'Validator verdicts' },
  stall: { icon: 'warning', label: 'Stalled', section: 'Stalled' },
};
/** Host rows come right after permissions: a pairing waits on this human, refusals need a look, a down link can wait. */
const KIND_ORDER: InboxKind[] = ['permission', 'host_pair', 'host_refused', 'host_down', 'verdict', 'stall'];
const HOST_KINDS: readonly InboxKind[] = ['host_pair', 'host_refused', 'host_down'];

/** "just now", "5m ago", "3h ago", "2d ago" from an RFC 3339 instant; unparseable input is shown as it came. */
export function relativeTime(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

/** Row actions: Mark seen only while unseen; Snooze/Resolve never for a permission (or a "perm:" key); Open always. Host rows open the Hosts screen instead of a task. */
export function inboxActions(item: Pick<InboxItem, 'key' | 'kind' | 'state' | 'taskId' | 'links'>): InboxAction[] {
  const out: InboxAction[] = [];
  if (item.state === 'new') out.push('seen');
  if (item.kind !== 'permission' && !item.key.startsWith('perm:')) out.push('snooze', 'resolve');
  if (HOST_KINDS.includes(item.kind)) out.push('hosts');
  if ((item.taskId ?? item.links.taskId) !== undefined) out.push('open');
  return out;
}

/** waiting: a permission or pairing asks for you; warning: a refusal or a verdict that needs a decision (never a success tint); neutral: the rest. */
function rowTone(item: Pick<InboxItem, 'kind' | 'attention'>): InboxTone {
  if (item.kind === 'permission' || item.kind === 'host_pair') return 'waiting';
  if (item.kind === 'host_refused') return 'warning';
  if (item.kind === 'host_down') return 'neutral';
  return item.attention === 'high' ? 'warning' : 'neutral';
}

export function inboxRow(item: InboxItem, now: number): InboxRowView {
  const meta = KIND_META[item.kind];
  return {
    key: item.key,
    kind: item.kind,
    taskId: item.taskId ?? item.links?.taskId ?? null,
    hostId: item.links?.hostId ?? null,
    // A verdict that needs a decision (fail, blocked, scope violation) must not wear the green check of a pass.
    icon: item.kind === 'verdict' && item.attention === 'high' ? 'warning' : meta.icon,
    label: item.kind === 'verdict' ? (item.attention === 'high' ? 'Needs a decision' : 'Passed') : meta.label,
    title: item.title,
    summary: item.summary,
    time: relativeTime(item.createdAt, now),
    tone: rowTone(item),
    unseen: item.state === 'new',
    actions: inboxActions({ ...item, links: item.links ?? {} }),
  };
}

/** The list grouped by kind in a fixed order; the daemon's order (attention, newest first) is kept inside a group. */
export function inboxView(items: InboxItem[], now: number): InboxView {
  const sections: InboxSection[] = [];
  for (const kind of KIND_ORDER) {
    const rows = items.filter((i) => i.kind === kind).map((i) => inboxRow(i, now));
    if (rows.length > 0) sections.push({ kind, title: KIND_META[kind].section, rows });
  }
  if (sections.length === 0) return { kind: 'empty', title: INBOX_EMPTY_TITLE, text: INBOX_EMPTY_TEXT };
  return { kind: 'list', sections };
}

/** 1 h, 4 h and "until tomorrow" (09:00 local), every one of them capped below 24 h from `now`. */
export function snoozeOptions(now: Date): SnoozeOption[] {
  const cap = now.getTime() + SNOOZE_CAP_MS - SNOOZE_SAFETY_MS;
  const at = (ms: number) => new Date(Math.min(ms, cap)).toISOString();
  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(9, 0, 0, 0);
  return [
    { id: '1h', label: 'Snooze for 1 hour', until: at(now.getTime() + 3600_000) },
    { id: '4h', label: 'Snooze for 4 hours', until: at(now.getTime() + 4 * 3600_000) },
    { id: 'tomorrow', label: 'Snooze until tomorrow', until: at(tomorrow.getTime()) },
  ];
}

/** The badge's accessibility label. */
export function inboxBadgeLabel(unseen: number): string {
  if (unseen <= 0) return 'Inbox, nothing new';
  return `Inbox, ${unseen} new ${unseen === 1 ? 'item' : 'items'}`;
}

/** The polite announcement when the unseen count changes; null when there is nothing to say (first load, decrease to the same). */
export function inboxAnnouncement(prev: number | null, next: number): string | null {
  if (prev === null || next === prev) return null;
  if (next === 0) return 'Inbox is clear';
  return `${next} new inbox ${next === 1 ? 'item' : 'items'}`;
}
