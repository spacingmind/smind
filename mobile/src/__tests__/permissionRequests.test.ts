// permissionRequests.test.ts covers Milestone 3 Item 2's Test Scenarios
// at the logic layer, in the codebase's established styles: the board's
// event/reducer behavior directly, and the tap flow (respondToPermission)
// against a scripted fake connection (api.test.ts's discipline) so the
// exact run.respondPermission params are asserted on the recorded wire
// traffic.

import { describe, expect, it } from 'vitest';
import {
  clampRunes,
  DENY_REASON_COUNTER_FROM,
  DENY_REASON_MAX,
  denyReasonCounter,
  denyReasonParam,
  dispatchPermissionTap,
  feedPermissionEvent,
  PermissionBoard,
  respondToPermission,
  runeCount,
} from '../permissionRequests';
import { respondPermission } from '../api';
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

  it('a permission request keeps its detail', () => {
    const board = new PermissionBoard();
    board.requestReceived('run-1', { ...REQ, detail: { policy: { ruleId: 'r1' } } });
    expect(board.list()[0].detail).toEqual({ policy: { ruleId: 'r1' } });
  });

  it('a permission_resolved event resolves an unanswered request regardless of origin (concurrent auto-accept resolution)', () => {
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
      optionId: 'deny', // a timeout landed first server-side
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

describe('deny reason', () => {
  it('denyReasonParam trims and returns the text', () => {
    expect(denyReasonParam('  use bun instead  ')).toBe('use bun instead');
  });

  it('denyReasonParam returns undefined for blank text', () => {
    expect(denyReasonParam('')).toBeUndefined();
    expect(denyReasonParam('   \t\n  ')).toBeUndefined();
  });

  it('denyReasonParam cuts at 500 characters', () => {
    expect(denyReasonParam('x'.repeat(600))).toBe('x'.repeat(500));
    expect(denyReasonParam(` ${'x'.repeat(500)}x `)).toBe('x'.repeat(500));
  });

  it('denyReasonCounter is hidden below DENY_REASON_COUNTER_FROM runes', () => {
    expect(denyReasonCounter('x'.repeat(DENY_REASON_COUNTER_FROM - 1))).toEqual({
      shown: false,
      used: DENY_REASON_COUNTER_FROM - 1,
      max: DENY_REASON_MAX,
      atLimit: false,
    });
  });

  it('denyReasonCounter is shown from DENY_REASON_COUNTER_FROM runes with used and max', () => {
    expect(denyReasonCounter('x'.repeat(DENY_REASON_COUNTER_FROM))).toEqual({
      shown: true,
      used: DENY_REASON_COUNTER_FROM,
      max: DENY_REASON_MAX,
      atLimit: false,
    });
  });

  it('denyReasonCounter flags the limit at the cap', () => {
    expect(denyReasonCounter('x'.repeat(DENY_REASON_MAX))).toEqual({
      shown: true,
      used: DENY_REASON_MAX,
      max: DENY_REASON_MAX,
      atLimit: true,
    });
  });

  it('runeCount counts characters (runes), not UTF-16 units: an emoji is one', () => {
    expect(runeCount('use bun instead')).toBe('use bun instead'.length);
    expect(runeCount('😀')).toBe(1);
    expect(runeCount('😀 no')).toBe(4);
  });

  it('clampRunes accepts 499 x plus an emoji whole, and cuts one more character (never a surrogate half)', () => {
    const atCap = 'x'.repeat(499) + '😀'; // 500 runes, 501 UTF-16 units
    expect(clampRunes(atCap)).toBe(atCap);
    expect(clampRunes(atCap + 'y')).toBe(atCap);
    expect(runeCount(clampRunes(atCap + 'y'))).toBe(DENY_REASON_MAX);
  });

  it('denyReasonParam of 600 emoji returns exactly 500 emoji', () => {
    expect(denyReasonParam('😀'.repeat(600))).toBe('😀'.repeat(DENY_REASON_MAX));
  });

  it('respondPermission sends no reason key without a reason', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    await respondPermission(conn, 'run-9', 'perm-1', 'deny');
    expect(calls).toEqual([['run.respondPermission', { runId: 'run-9', requestId: 'perm-1', optionId: 'deny' }]]);
  });

  it('respondPermission sends the reason when given', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    await respondPermission(conn, 'run-9', 'perm-1', 'deny', 'use bun instead');
    expect(calls).toEqual([
      ['run.respondPermission', { runId: 'run-9', requestId: 'perm-1', optionId: 'deny', reason: 'use bun instead' }],
    ]);
  });

  it('respondToPermission passes the reason through to the wire', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);
    await respondToPermission(conn, board, 'perm-1', 'deny', 'use bun instead');
    expect(calls).toEqual([
      ['run.respondPermission', { runId: 'run-9', requestId: 'perm-1', optionId: 'deny', reason: 'use bun instead' }],
    ]);
  });

  it('respondToPermission without a reason keeps the old params exactly', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new PermissionBoard();
    board.requestReceived('run-9', REQ);
    await respondToPermission(conn, board, 'perm-1', 'allow', denyReasonParam('   '));
    expect(calls).toEqual([['run.respondPermission', { runId: 'run-9', requestId: 'perm-1', optionId: 'allow' }]]);
  });

  it('dispatchPermissionTap: a reject option carries the reason (trimmed/capped), blank stays undefined', () => {
    const calls: unknown[][] = [];
    const tap = (...args: unknown[]) => calls.push(args);
    dispatchPermissionTap(tap, 'perm-1', 'deny', 'reject_once', ' use bun instead ');
    dispatchPermissionTap(tap, 'perm-1', 'deny', 'reject_once', 'x'.repeat(600));
    dispatchPermissionTap(tap, 'perm-1', 'deny', 'reject_once', '   ');
    expect(calls).toEqual([
      ['perm-1', 'deny', 'use bun instead'],
      ['perm-1', 'deny', 'x'.repeat(500)],
      ['perm-1', 'deny', undefined],
    ]);
  });

  it('dispatchPermissionTap: any other option is called with exactly two arguments', () => {
    const calls: unknown[][] = [];
    const tap = (...args: unknown[]) => calls.push(args);
    dispatchPermissionTap(tap, 'perm-1', 'allow', 'allow_once', 'use bun instead');
    dispatchPermissionTap(tap, 'perm-1', 'always', 'allow_always', 'use bun instead');
    dispatchPermissionTap(tap, 'perm-1', 'odd', 'weird', 'use bun instead');
    expect(calls).toEqual([
      ['perm-1', 'allow'],
      ['perm-1', 'always'],
      ['perm-1', 'odd'],
    ]);
  });
});
