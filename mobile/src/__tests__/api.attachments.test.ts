// api.attachments.test.ts pins ADR-0038's api.ts additions against a fake
// RelayConnection: run.start carries attachments only when non-empty, and the
// daemon.info / run.attachment wrappers call the right methods.

import { describe, expect, it } from 'vitest';
import { AttachmentPayload, fetchDaemonInfo, fetchRunAttachment, startRun, startRunWhenBusy } from '../api';
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

function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

const img: AttachmentPayload = { kind: 'image', name: 'a.png', mimeType: 'image/png', data: 'AAAA' };

describe('run.start attachments', () => {
  it('run.start omits attachments when none', async () => {
    const { conn, calls } = fakeConnection(() => ({ runId: 'r1' }));
    await startRun(conn, 1, 'glm', 'hi');
    await startRun(conn, 1, 'glm', 'hi', []);
    expect(calls).toEqual([
      ['run.start', { taskId: 1, provider: 'glm', prompt: 'hi' }],
      ['run.start', { taskId: 1, provider: 'glm', prompt: 'hi' }],
    ]);
    expect(Object.keys(calls[1][1] as object)).not.toContain('attachments');
  });

  it('run.start sends attachments when present', async () => {
    const { conn, calls } = fakeConnection(() => ({ runId: 'r1' }));
    await expect(startRun(conn, 1, 'glm', 'hi', [img])).resolves.toBe('r1');
    expect(calls).toEqual([['run.start', { taskId: 1, provider: 'glm', prompt: 'hi', attachments: [img] }]]);
  });

  it('startRunWhenBusy sends attachments', async () => {
    const { conn, calls } = fakeConnection(() => ({ runId: 'r2' }));
    await startRunWhenBusy(conn, { taskId: 1, provider: 'glm', prompt: 'hi', whenBusy: 'queue', attachments: [img] });
    await startRunWhenBusy(conn, { taskId: 1, provider: 'glm', prompt: 'hi', whenBusy: 'queue', attachments: [] });
    expect(calls[0][1]).toEqual({ taskId: 1, provider: 'glm', prompt: 'hi', whenBusy: 'queue', attachments: [img] });
    expect(calls[1][1]).toEqual({ taskId: 1, provider: 'glm', prompt: 'hi', whenBusy: 'queue' });
  });
});

describe('daemon.info and run.attachment', () => {
  it('fetchDaemonInfo returns attachments', async () => {
    const attachments = { maxCount: 10, maxImageBytes: 1, maxFileBytes: 2, maxTotalBytes: 3, maxMessageBytes: 4, images: ['image/png'], kinds: ['image'] };
    const { conn, calls } = fakeConnection(() => ({ version: 'x', attachments }));
    const info = await fetchDaemonInfo(conn);
    expect(info.attachments).toEqual(attachments);
    expect(calls).toEqual([['daemon.info', undefined]]);
  });

  it('fetchRunAttachment calls run.attachment with runId and id', async () => {
    const reply = { name: 'a.png', mimeType: 'image/png', size: 3, data: 'AAAA' };
    const { conn, calls } = fakeConnection(() => reply);
    await expect(fetchRunAttachment(conn, 'run-1', 'att-2')).resolves.toEqual(reply);
    expect(calls).toEqual([['run.attachment', { runId: 'run-1', id: 'att-2' }]]);
  });
});
