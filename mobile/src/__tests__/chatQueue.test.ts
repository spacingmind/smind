// chatQueue.test.ts covers the ChatQueue store (chatQueue.ts, ported from
// the web composer's use-chat-queue.ts) against a hand-rolled QueueConn
// fake: it records calls, lets each test resolve the chat.queueList answer
// when it chooses, captures the subscribe/onDropped callbacks so the test
// can push events, and uses fake timers for the auto-cancelled TTL.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ChatQueueItem } from '../api';
import { ChatQueue, QueueConn } from '../chatQueue';

function item(partial: Partial<ChatQueueItem> & { ID: number }): ChatQueueItem {
  return {
    ChatID: 5,
    Prompt: 'do it',
    RunConfig: '',
    Source: 'human',
    FromTaskID: null,
    FromChatID: null,
    Priority: 0,
    Status: 'queued',
    RunID: null,
    CancelReason: '',
    CancelKind: '',
    CreatedAt: '',
    UpdatedAt: '',
    ...partial,
  };
}

interface FakeConn extends QueueConn {
  calls: Array<[string, unknown]>;
  answers: Array<(items: ChatQueueItem[]) => void>;
  rejectors: Array<(err: Error) => void>;
  event?: (e: { topic: string; payload: unknown }) => void;
  dropped?: (count: number) => void;
  /** Resolves non-queueList calls; tests set it to control chat.queueCancel. */
  otherCall?: (method: string, params: unknown) => unknown;
}

function fakeConn(): FakeConn {
  const conn: FakeConn = {
    calls: [],
    answers: [],
    rejectors: [],
    call(method, params) {
      conn.calls.push([method, params]);
      if (method === 'chat.queueList') {
        return new Promise((resolve) => {
          conn.answers.push((items) => resolve({ items }));
        });
      }
      return Promise.resolve(conn.otherCall?.(method, params) ?? {});
    },
    subscribe(_topics, onEvent) {
      conn.event = onEvent;
      return () => {
        conn.event = undefined;
      };
    },
    onDropped(cb) {
      conn.dropped = cb;
      return () => {
        conn.dropped = undefined;
      };
    },
  };
  return conn;
}

function snapshot(items: ChatQueueItem[]): unknown {
  return { chatId: 5, items };
}

/** Resolves the oldest pending chat.queueList answer. */
async function answerList(conn: FakeConn, items: ChatQueueItem[], queue = ChatQueue) {
  conn.answers.shift()!(items);
  await Promise.resolve();
  await Promise.resolve();
  void queue;
}

describe('ChatQueue', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('loads initial items from chat.queueList (reload restores the queue)', async () => {
    const conn = fakeConn();
    const changes: number[] = [];
    const q = new ChatQueue(conn, 5, () => changes.push(1));
    q.start();
    expect(q.items).toEqual([]);
    await answerList(conn, [item({ ID: 1 }), item({ ID: 2 })]);
    expect(q.items.map((i) => i.ID)).toEqual([1, 2]);
    expect(changes.length).toBeGreaterThan(0);
    q.stop();
  });

  it('replaces the list from a chat.queueUpdated snapshot for its chat', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1 })]);
    conn.event!({ topic: 'chat.queueUpdated', payload: snapshot([item({ ID: 2 }), item({ ID: 3 })]) });
    expect(q.items.map((i) => i.ID)).toEqual([2, 3]);
    q.stop();
  });

  it('ignores chat.queueUpdated for another chat', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1 })]);
    conn.event!({ topic: 'chat.queueUpdated', payload: { chatId: 6, items: [item({ ID: 2, ChatID: 6 })] } });
    expect(q.items.map((i) => i.ID)).toEqual([1]);
    q.stop();
  });

  it('orders by priority desc then id', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [
      item({ ID: 1, Priority: 0 }),
      item({ ID: 3, Priority: 5 }),
      item({ ID: 2, Priority: 5 }),
    ]);
    expect(q.items.map((i) => i.ID)).toEqual([2, 3, 1]);
    q.stop();
  });

  it('hides delivered items', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1, Status: 'delivered' }), item({ ID: 2 })]);
    expect(q.items.map((i) => i.ID)).toEqual([2]);
    q.stop();
  });

  it('shows an auto-cancelled item with its reason for 10s then drops it', async () => {
    const conn = fakeConn();
    const changes: number[] = [];
    const q = new ChatQueue(conn, 5, () => changes.push(1));
    q.start();
    await answerList(conn, [item({ ID: 1, Status: 'cancelled', CancelKind: 'auto', CancelReason: 'superseded' })]);
    expect(q.items.map((i) => i.CancelReason)).toEqual(['superseded']);
    const changesBefore = changes.length;
    vi.advanceTimersByTime(9999);
    expect(q.items).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(q.items).toEqual([]);
    expect(changes.length).toBe(changesBefore + 1);
    q.stop();
  });

  it('does not show a user-cancelled item', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1, Status: 'cancelled', CancelKind: 'user' }), item({ ID: 2 })]);
    expect(q.items.map((i) => i.ID)).toEqual([2]);
    q.stop();
  });

  it('a queueList answer older than a newer chat.queueUpdated snapshot is ignored', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    // The refresh from start() is pending; a newer snapshot arrives first.
    conn.event!({ topic: 'chat.queueUpdated', payload: snapshot([item({ ID: 9 })]) });
    await answerList(conn, [item({ ID: 1 })]); // stale answer
    expect(q.items.map((i) => i.ID)).toEqual([9]);
    q.stop();
  });

  it('a queueList answer after stop() is ignored', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    q.stop();
    await answerList(conn, [item({ ID: 1 })]);
    expect(q.items).toEqual([]);
  });

  it('refetches on event.dropped', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, []);
    expect(conn.calls.filter(([m]) => m === 'chat.queueList')).toHaveLength(1);
    conn.dropped!(3);
    await answerList(conn, [item({ ID: 4 })]);
    expect(q.items.map((i) => i.ID)).toEqual([4]);
    q.stop();
  });

  it('promptForRun finds the prompt of the delivered item for a run id', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1, Status: 'delivered', RunID: 'run-7', Prompt: 'next step' }), item({ ID: 2 })]);
    expect(q.promptForRun('run-7')).toBe('next step');
    q.stop();
  });

  it('promptForRun is undefined for a queued, cancelled or unknown run', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [
      item({ ID: 1, Status: 'queued', RunID: 'run-a', Prompt: 'still queued' }),
      item({ ID: 2, Status: 'cancelled', CancelKind: 'auto', RunID: 'run-b', Prompt: 'gone' }),
      item({ ID: 3, Status: 'delivered', RunID: 'run-c', Prompt: '' }),
    ]);
    expect(q.promptForRun('run-a')).toBeUndefined();
    expect(q.promptForRun('run-b')).toBeUndefined();
    expect(q.promptForRun('nope')).toBeUndefined();
    q.stop();
  });

  it('cancel calls chat.queueCancel with the item id and leaves the row until the snapshot', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1 })]);
    await q.cancel(1);
    expect(conn.calls.some(([m, p]) => m === 'chat.queueCancel' && (p as { itemId: number }).itemId === 1)).toBe(true);
    expect(q.items.map((i) => i.ID)).toEqual([1]); // still visible
    conn.event!({ topic: 'chat.queueUpdated', payload: snapshot([]) });
    expect(q.items).toEqual([]);
    q.stop();
  });

  it('cancel error refetches the list and surfaces no error', async () => {
    const conn = fakeConn();
    const q = new ChatQueue(conn, 5, () => {});
    q.start();
    await answerList(conn, [item({ ID: 1 })]);
    conn.otherCall = () => {
      throw new Error('not queued'); // chat.queueCancel fails; the refresh then refetches
    };
    const pending = q.cancel(1); // its refresh's queueList answer is pending
    conn.answers.shift()!([item({ ID: 2 })]);
    await pending;
    expect(q.items.map((i) => i.ID)).toEqual([2]);
    q.stop();
  });
});
