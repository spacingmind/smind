// chatQueue.ts is the pure-TypeScript store for the daemon-side per-chat
// prompt queue (ADR-0021 §9), ported from the web composer's
// use-chat-queue.ts without any React: the screen owns one instance, and
// onChange fires whenever the visible list may have changed. Stale
// chat.queueList answers are guarded by a snapshot sequence, so a late
// answer older than a newer chat.queueUpdated snapshot is ignored.

import { ChatQueueItem } from './api';

/** How long an auto-cancelled item stays visible (the web hook's 10s). */
export const AUTO_CANCELLED_TTL_MS = 10_000;

/** The connection surface ChatQueue needs; RelayConnection satisfies it. */
export interface QueueConn {
  call(method: string, params?: unknown): Promise<unknown>;
  subscribe(topics: string[], onEvent: (e: { topic: string; payload: unknown }) => void): () => void;
  onDropped(cb: (count: number) => void): () => void;
}

/**
 * Queued items first (Priority desc, ID asc), then auto-cancelled items
 * still inside their TTL. Delivered and user-cancelled items are hidden.
 * An auto-cancelled id missing from firstSeen counts as just seen.
 */
export function visibleQueueItems(
  all: ChatQueueItem[],
  now: number,
  firstSeen: ReadonlyMap<number, number>,
): ChatQueueItem[] {
  const queued = all
    .filter((i) => i.Status === 'queued')
    .sort((a, b) => b.Priority - a.Priority || a.ID - b.ID);
  const autoCancelled = all.filter(
    (i) => i.Status === 'cancelled' && i.CancelKind === 'auto' && now - (firstSeen.get(i.ID) ?? now) < AUTO_CANCELLED_TTL_MS,
  );
  return [...queued, ...autoCancelled];
}

export class ChatQueue {
  private all: ChatQueueItem[] = [];
  private firstSeen = new Map<number, number>();
  private timers = new Set<ReturnType<typeof setTimeout>>();
  private seq = 0;
  private stopped = false;
  private unsubscribe?: () => void;
  private unsubscribeDropped?: () => void;

  constructor(
    private readonly conn: QueueConn,
    readonly chatId: number,
    private readonly onChange: () => void,
  ) {}

  start(): void {
    this.unsubscribe = this.conn.subscribe(['chat.queueUpdated'], (e) => {
      const payload = e.payload as { chatId?: number; items?: ChatQueueItem[] };
      if (payload?.chatId !== this.chatId) return;
      this.seq++;
      this.replace(payload.items ?? []);
    });
    this.unsubscribeDropped = this.conn.onDropped(() => void this.refresh());
    void this.refresh();
  }

  stop(): void {
    this.stopped = true;
    this.unsubscribe?.();
    this.unsubscribeDropped?.();
    for (const t of this.timers) clearTimeout(t);
    this.timers.clear();
  }

  async refresh(): Promise<void> {
    const reqSeq = this.seq;
    let items: ChatQueueItem[];
    try {
      const result = (await this.conn.call('chat.queueList', { chatId: this.chatId })) as { items?: ChatQueueItem[] };
      items = result.items ?? [];
    } catch {
      return;
    }
    if (this.stopped || this.seq !== reqSeq) return;
    this.replace(items);
  }

  async cancel(itemId: number): Promise<void> {
    try {
      await this.conn.call('chat.queueCancel', { itemId });
    } catch {
      await this.refresh();
    }
  }

  get items(): ChatQueueItem[] {
    return visibleQueueItems(this.all, Date.now(), this.firstSeen);
  }

  private replace(items: ChatQueueItem[]): void {
    this.all = items;
    const present = new Set(items.filter((i) => i.Status === 'cancelled' && i.CancelKind === 'auto').map((i) => i.ID));
    for (const id of [...this.firstSeen.keys()]) {
      if (!present.has(id)) this.firstSeen.delete(id);
    }
    for (const id of present) {
      if (!this.firstSeen.has(id)) {
        this.firstSeen.set(id, Date.now());
        const t = setTimeout(() => {
          this.timers.delete(t);
          this.onChange();
        }, AUTO_CANCELLED_TTL_MS);
        this.timers.add(t);
      }
    }
    this.onChange();
  }
}
