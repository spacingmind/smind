// permissionRequests.test.ts covers Milestone 3 Item 2's Test Scenarios
// at the logic layer, in the codebase's established styles: the board's
// event/reducer behavior directly, and the tap flow (respondToPermission)
// against a scripted fake connection (api.test.ts's discipline) so the
// exact run.respondPermission params are asserted on the recorded wire
// traffic.

import { describe, expect, it } from 'vitest';
import { feedPermissionEvent, PermissionBoard, respondToPermission } from '../permissionRequests';
import { CloseableStream, RelayConnection } from '../relay/RelayConnection';
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
function fakeConnection(
  handler: (method: string, params: unknown) => unknown,
): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => unknown }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

const REQ = {
  requestId: 'perm-1',
  summary: 'run `npm install` in /repo',
  options: [
    { id: 'allow', label: 'Allow', kind: 'allow' },
    { id: 'deny', label: 'Deny', kind: 'deny' },
    { id: 'always', label: 'Always allow', kind: 'allow_always' },
  ],
};

describe('PermissionBoard (event/reducer behavior)', () => {
  it('a live permission_request tracks pending with one option per button, labels intact', () => {
    const board = new PermissionBoard();
    feedPermissionEvent(board, 'run-1', 'permission_request', REQ);
    expect(board.list()).toEqual([
      {
        runId: 'run-1',
        requestId: 'perm-1',
        summary: 'run `npm install` in /repo',
        options: REQ.options,
        status: 'pending',
        error: null,
      },
    ]);
  });

  it('a duplicate permission_request (attach backfill after the screen already saw it) changes nothing', () => {
    const board = new PermissionBoard();
    board.requestReceived('run-1', REQ);
    board.requestReceived('run-1', REQ);
    expect(board.list()).toHaveLength(1);
  });

  it('a permission_resolved event resolves an unanswered request regardless of origin (concurrent auto-safe resolution)', () => {
    const board = new PermissionBoard();
    board.requestReceived('run-1', REQ);
    expect(board.list()[0].status).toBe('pending');
    feedPermissionEvent(board, 'run-1', 'permission_resolved', {
      requestId: 'perm-1',
      optionId: 'deny',
      reason: 'auto_safe',
    });
    expect(board.list()[0]).toMatchObject({
      status: 'resolved',
      resolvedWith: { optionId: 'deny', by: 'event' },
      error: null,
    });
  });

  it('a resolved event is authoritative over an optimistic tap that chose differently', () => {
    const board = new PermissionBoard();
    board.requestReceived('run-1', REQ);
    board.choose('perm-1', 'allow'); // the tap
    feedPermissionEvent(board, 'run-1', 'permission_resolved', {
      requestId: 'perm-1',
      optionId: 'deny', // an auto-safe timeout landed first server-side
      reason: 'timeout',
    });
    expect(board.list()[0].resolvedWith).toEqual({ optionId: 'deny', by: 'event' });
  });

  it('a request+resolved pair from run.logs history renders directly resolved, never interactive', () => {
    const board = new PermissionBoard();
    board.applyLogEvents('run-1', [
      { type: 'permission_request', ...REQ },
      { type: 'permission_resolved', requestId: 'perm-1', optionId: 'allow', reason: 'human' },
    ]);
    const reqs = board.list();
    expect(reqs).toHaveLength(1);
    expect(reqs[0].status).toBe('resolved');
    expect(reqs[0].resolvedWith).toEqual({ optionId: 'allow', by: 'event' });
  });

  it('a permission_resolved for an unknown requestId is ignored, not crashed on', () => {
    const board = new PermissionBoard();
    feedPermissionEvent(board, 'run-1', 'permission_resolved', { requestId: 'nope', optionId: 'allow' });
    expect(board.list()).toEqual([]);
  });
});

describe('respondToPermission (tap flow, fake connection)', () => {
  it('tapping an option calls run.respondPermission with {runId, requestId, optionId} and resolves optimistically', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);

    await respondToPermission(conn, board, 'perm-1', 'allow');
    expect(calls).toEqual([['run.respondPermission', { runId: 'run-9', requestId: 'perm-1', optionId: 'allow' }]]);
    expect(board.list()[0]).toMatchObject({ status: 'resolved', resolvedWith: { optionId: 'allow', by: 'tap' } });
  });

  it('the optimistic resolution lands before the RPC resolves (buttons disable immediately)', async () => {
    let releaseRpc: ((value: unknown) => void) | null = null;
    const { conn } = fakeConnection(
      () => new Promise((resolve) => { releaseRpc = resolve; }),
    );
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);

    const pending = respondToPermission(conn, board, 'perm-1', 'allow');
    expect(board.list()[0].status).toBe('resolved'); // already, RPC still in flight
    await Promise.resolve(); // let the fake's handler assign releaseRpc
    releaseRpc!({});
    await pending;
  });

  it('a run.respondPermission rejection re-enables the request and shows an inline error', async () => {
    const { conn, calls } = fakeConnection(() => {
      throw new Error('relay: connection is closed');
    });
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);

    await respondToPermission(conn, board, 'perm-1', 'allow');
    expect(calls).toHaveLength(1);
    const req = board.list()[0];
    expect(req.status).toBe('pending'); // rolled back -- buttons re-enabled
    expect(req.resolvedWith).toBeUndefined();
    expect(req.error).toBe('relay: connection is closed');
  });

  it('a rejection after an authoritative event already resolved the request does not roll it back', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('run run-9: permission already resolved');
    });
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);

    const pending = respondToPermission(conn, board, 'perm-1', 'allow');
    // The authoritative resolution lands while the RPC is still in flight.
    board.resolvedEvent({ requestId: 'perm-1', optionId: 'deny', reason: 'auto_safe' });
    await pending;
    expect(board.list()[0]).toMatchObject({
      status: 'resolved',
      resolvedWith: { optionId: 'deny', by: 'event' }, // the event wins, the failure is moot
      error: null,
    });
  });

  it('tapping an already-resolved request is a no-op (no second RPC)', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);
    board.resolvedEvent({ requestId: 'perm-1', optionId: 'deny', reason: 'human' });

    await respondToPermission(conn, board, 'perm-1', 'allow');
    expect(calls).toEqual([]);
    expect(board.list()[0].resolvedWith).toEqual({ optionId: 'deny', by: 'event' });
  });
});
