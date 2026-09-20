// api.test.ts exercises the screen-facing data functions against a fake
// RelayConnection (the plan's Item 2 Test Scenarios: real-data rendering
// from a harness-backed connection is covered by the integration suite;
// here we cover empty-workspace state, call-failure error state, and
// refresh re-using the one connection with no second admit/handshake --
// the fake records every method that goes out).

import { describe, expect, it } from 'vitest';
import { fetchOverview, listWorkspaces } from '../api';
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
function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

describe('listWorkspaces / fetchOverview', () => {
  it('returns workspaces and the per-workspace spaces+tasks from their wire shapes', async () => {
    const { conn, calls } = fakeConnection((method, params) => {
      switch (method) {
        case 'workspace.list':
          return [{ ID: 3, Path: '/repo', Title: 'main', CreatedAt: '', UpdatedAt: '' }];
        case 'space.list':
          if ((params as { workspaceId: number }).workspaceId !== 3) throw new Error('wrong workspace');
          return [{ ID: 7, WorkspaceID: 3, Title: 'bugs', CreatedAt: '', UpdatedAt: '' }];
        case 'task.list':
          return [
            { ID: 11, WorkspaceID: 3, SpaceID: 7, Title: 'fix crash', Status: 'created', CreatedAt: '', UpdatedAt: '' },
            { ID: 12, WorkspaceID: 3, SpaceID: null, Title: 'scratch', Status: 'running', CreatedAt: '', UpdatedAt: '' },
          ];
        default:
          throw new Error(`unexpected method ${method}`);
      }
    });

    const workspaces = await listWorkspaces(conn);
    expect(workspaces.map((w) => w.Title)).toEqual(['main']);

    const { spaces, tasks } = await fetchOverview(conn, 3);
    expect(spaces.map((s) => s.Title)).toEqual(['bugs']);
    expect(tasks.map((t) => t.SpaceID)).toEqual([7, null]);

    expect(calls.map(([m]) => m)).toEqual(['workspace.list', 'space.list', 'task.list']);
  });

  it('refresh re-fetches over the same connection: a second identical load only adds RPC calls, no reconnect path exists', async () => {
    const { conn, calls } = fakeConnection((method) => {
      switch (method) {
        case 'workspace.list':
          return [{ ID: 3, Path: '/repo', Title: 'main', CreatedAt: '', UpdatedAt: '' }];
        case 'space.list':
          return [];
        case 'task.list':
          return [];
        default:
          throw new Error(`unexpected method ${method}`);
      }
    });

    await listWorkspaces(conn); // the initial load's workspace resolution
    await fetchOverview(conn, 3);
    await fetchOverview(conn, 3); // the refresh
    expect(calls.filter(([m]) => m === 'workspace.list')).toHaveLength(1); // resolved once, kept in screen state
    expect(calls.filter(([m]) => m === 'task.list')).toHaveLength(2); // one per load
    expect(calls.filter(([m]) => m === 'space.list')).toHaveLength(2); // one per load
    // Crucially, all of this went out over the SAME conn instance: the
    // fake has no admit/handshake hooks at all, so any implementation
    // that tried to reconnect here would crash instead of pass.
  });

  it('an empty daemon yields empty lists, not an error (empty-workspace state)', async () => {
    const { conn } = fakeConnection((method) => {
      switch (method) {
        case 'workspace.list':
        case 'space.list':
        case 'task.list':
          return [];
        default:
          throw new Error(`unexpected method ${method}`);
      }
    });
    expect(await listWorkspaces(conn)).toEqual([]);
    expect(await fetchOverview(conn, 1)).toEqual({ spaces: [], tasks: [] });
  });

  it('a failed call rejects with the error the screen shows (error + retry state)', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('relay: connection is closed');
    });
    await expect(listWorkspaces(conn)).rejects.toThrow('relay: connection is closed');

    // Retry after failure: the connection object stays usable, so a
    // subsequent call can succeed (retry actually retries).
    let fail = true;
    const { conn: conn2, calls } = fakeConnection((method) => {
      if (method !== 'workspace.list') throw new Error(`unexpected method ${method}`);
      if (fail) throw new Error('relay: connection is closed');
      return [];
    });
    await expect(listWorkspaces(conn2)).rejects.toThrow('relay: connection is closed');
    fail = false;
    await expect(listWorkspaces(conn2)).resolves.toEqual([]);
    expect(calls).toHaveLength(2);
  });
});
