// Wire shapes of the inbox.* RPCs (ADR-0045 section 5): the daemon's derived Human inbox. Every string is
// daemon-provided and only ever drawn as plain <Text>.

export type InboxKind = 'permission' | 'verdict' | 'stall';
export type InboxState = 'new' | 'seen' | 'snoozed' | 'resolved';
export type InboxAttention = 'high' | 'normal';

export interface InboxLinks {
  taskId?: number;
  runId?: string;
  permissionRequestId?: string;
  validationId?: number;
}

export interface InboxItem {
  key: string;
  kind: InboxKind;
  taskId?: number;
  title: string;
  summary: string;
  /** RFC 3339. */
  createdAt: string;
  state: InboxState;
  snoozedUntil?: string;
  attention: InboxAttention;
  links: InboxLinks;
}

export interface InboxCount {
  total: number;
  unseen: number;
  byKind: Partial<Record<InboxKind, number>>;
}

export const INBOX_CHANGED_TOPIC = 'inbox.changed';
