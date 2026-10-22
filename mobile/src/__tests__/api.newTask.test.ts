// api.newTask.test.ts pins the M6 additions to api.ts against a fake
// RelayConnection: the exact params task.create / run.start / provider.list
// send (no extra fields, empties omitted) and how startModeParams maps the
// four StartMode variants -- the wire contract the screen builds on.

import { describe, expect, it } from 'vitest';
import { createTask, listProviders, startFirstRun, startModeParams } from '../api';
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

/** A RelayConnection whose call() is scripted, recording every {method, params} that goes out. */
function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

describe('listProviders', () => {
  it('unwraps {providers: [...]}', async () => {
    const { conn, calls } = fakeConnection(() => ({ providers: [{ id: 'glm', label: 'GLM' }] }));
    const providers = await listProviders(conn);
    expect(providers).toEqual([{ id: 'glm', label: 'GLM' }]);
    expect(calls).toEqual([['provider.list', undefined]]);
  });
});

describe('startModeParams', () => {
  it('maps the four variants', () => {
    expect(startModeParams({ kind: 'head' })).toEqual({});
    expect(startModeParams({ kind: 'baseRef', ref: 'abc123' })).toEqual({ baseRef: 'abc123' });
    expect(startModeParams({ kind: 'branch', name: 'feat/x' })).toEqual({ branch: 'feat/x' });
    expect(startModeParams({ kind: 'pr', number: 42 })).toEqual({ prNumber: 42 });
  });
});

describe('createTask', () => {
  it('sends exactly {workspaceId, title} for HEAD with no space', async () => {
    const { conn, calls } = fakeConnection(() => ({ ID: 5, WorkspaceID: 3, SpaceID: null, Title: 't', Status: 'created', CreatedAt: '', UpdatedAt: '' }));
    await createTask(conn, { workspaceId: 3, spaceId: null, title: 'Fix bug', start: { kind: 'head' } });
    expect(calls).toEqual([['task.create', { workspaceId: 3, title: 'Fix bug' }]]);
  });

  it('includes spaceId only when set, and the start-mode fields for the other variants', async () => {
    const { conn, calls } = fakeConnection(() => ({ ID: 5, WorkspaceID: 3, SpaceID: 7, Title: 't', Status: 'created', CreatedAt: '', UpdatedAt: '' }));
    await createTask(conn, { workspaceId: 3, spaceId: 7, title: 'A', start: { kind: 'baseRef', ref: 'r' } });
    await createTask(conn, { workspaceId: 3, spaceId: undefined, title: 'B', start: { kind: 'branch', name: 'b' } });
    await createTask(conn, { workspaceId: 3, title: 'C', start: { kind: 'pr', number: 9 } });
    await createTask(conn, { workspaceId: 3, spaceId: null, title: 'D' }); // start omitted -> HEAD
    expect(calls.map(([, p]) => p)).toEqual([
      { workspaceId: 3, spaceId: 7, title: 'A', baseRef: 'r' },
      { workspaceId: 3, title: 'B', branch: 'b' },
      { workspaceId: 3, title: 'C', prNumber: 9 },
      { workspaceId: 3, title: 'D' },
    ]);
  });
});

describe('startFirstRun', () => {
  it('sends taskId/provider/prompt plus model and permissionMode when given, with whenBusy queue', async () => {
    const { conn, calls } = fakeConnection(() => ({ runId: 'run-1' }));
    const result = await startFirstRun(conn, { taskId: 5, provider: 'glm', prompt: 'hi', model: 'glm-4.7', permissionMode: 'acceptEdits' });
    expect(result).toEqual({ kind: 'started', runId: 'run-1' });
    expect(calls).toEqual([['run.start', { taskId: 5, provider: 'glm', prompt: 'hi', whenBusy: 'queue', model: 'glm-4.7', permissionMode: 'acceptEdits' }]]);
  });

  it('omits empty model and permissionMode (still whenBusy queue)', async () => {
    const { conn, calls } = fakeConnection(() => ({ runId: 'run-2' }));
    await startFirstRun(conn, { taskId: 5, provider: 'glm', prompt: 'hi', model: '', permissionMode: '' });
    expect(calls).toEqual([['run.start', { taskId: 5, provider: 'glm', prompt: 'hi', whenBusy: 'queue' }]]);
  });

  it('a queued outcome comes back as {kind: queued, queueItemId}', async () => {
    const { conn, calls } = fakeConnection(() => ({ queued: true, queueItemId: 33 }));
    const result = await startFirstRun(conn, { taskId: 5, provider: 'glm', prompt: 'hi' });
    expect(result).toEqual({ kind: 'queued', queueItemId: 33 });
    expect(calls).toEqual([['run.start', { taskId: 5, provider: 'glm', prompt: 'hi', whenBusy: 'queue' }]]);
  });
});
