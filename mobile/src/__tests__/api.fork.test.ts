// api.fork.test.ts exercises the chat-fork calls (ADR-0057 PR 1b) against
// the same scripted fake RelayConnection as api.test.ts: the exact params
// each call sends, and the wire shapes it hands back.

import { describe, expect, it } from 'vitest';
import { forkChat, forkSeed, listChats } from '../api';
import { RelayConnection } from '../relay/RelayConnection';
import { PairingOffer } from '../relay/pairing';

const dummyOffer: PairingOffer = {
  daemonId: '',
  publicKey: new Uint8Array(),
  relay: '',
  relayFingerprint: '',
  secret: new Uint8Array(),
  workspaceId: '',
};

/** A RelayConnection whose call() is fully scripted per test, recording traffic. */
function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

describe('listChats', () => {
  it('sends {taskId, includeArchived: true} and returns the chat entries', async () => {
    const chats = [
      { ID: 5, TaskID: 3, Title: 'main', ArchivedAt: null },
      { ID: 9, TaskID: 3, Title: 'Fork of main', ArchivedAt: null, fork: { parentChatId: 5, forkedAtRunId: 'r2', kind: 'fork', mode: 'seeded', lossy: true } },
    ];
    const { conn, calls } = fakeConnection((method, params) => {
      if (method === 'chat.list') {
        expect(params).toEqual({ taskId: 3, includeArchived: true });
        return chats;
      }
      throw new Error(`unexpected method ${method}`);
    });
    await expect(listChats(conn, 3)).resolves.toEqual(chats);
    expect(calls).toEqual([['chat.list', { taskId: 3, includeArchived: true }]]);
  });
});

describe('forkChat', () => {
  it('sends {chatId, atRunId, mode}', async () => {
    const child = { ID: 9, TaskID: 3, Title: 'Fork of main', ArchivedAt: null, fork: { parentChatId: 5, forkedAtRunId: 'r2', kind: 'fork', mode: 'native', lossy: false } };
    const { conn, calls } = fakeConnection((method, params) => {
      if (method === 'chat.fork') {
        expect(params).toEqual({ chatId: 5, atRunId: 'r2', mode: 'native' });
        return child;
      }
      throw new Error(`unexpected method ${method}`);
    });
    await expect(forkChat(conn, 5, 'r2', 'native')).resolves.toEqual(child);
    expect(calls).toEqual([['chat.fork', { chatId: 5, atRunId: 'r2', mode: 'native' }]]);
  });
});

describe('forkSeed', () => {
  it('sends {chatId} and returns the seed string', async () => {
    const { conn, calls } = fakeConnection((method, params) => {
      if (method === 'chat.forkSeed') {
        expect(params).toEqual({ chatId: 9 });
        return { seed: 'Earlier: the user asked for tests.' };
      }
      throw new Error(`unexpected method ${method}`);
    });
    await expect(forkSeed(conn, 9)).resolves.toBe('Earlier: the user asked for tests.');
    expect(calls).toEqual([['chat.forkSeed', { chatId: 9 }]]);
  });

  it('returns the empty string when the result carries no seed', async () => {
    const { conn } = fakeConnection(() => ({}));
    await expect(forkSeed(conn, 4)).resolves.toBe('');
  });
});
