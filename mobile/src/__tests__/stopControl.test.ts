// stopControl.test.ts covers the Stop control's pure view model and the
// never-throwing requestStop against a scripted connection, mirroring
// api.test.ts's fake-transport discipline.

import { describe, expect, it } from 'vitest';
import { RelayConnection } from '../relay/RelayConnection';
import { PairingOffer } from '../relay/pairing';
import { requestStop, stopControlView } from '../stopControl';

const dummyOffer: PairingOffer = {
  daemonId: '',
  publicKey: new Uint8Array(),
  relay: '',
  relayFingerprint: '',
  secret: new Uint8Array(),
  workspaceId: '',
};

function fakeConnection(handler: (method: string, params: unknown) => unknown): RelayConnection {
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = (method: string, params?: unknown) =>
    Promise.resolve().then(() => handler(method, params));
  return conn;
}

describe('stopControlView', () => {
  it('the stop control is hidden without a live run', () => {
    expect(stopControlView(null, false)).toMatchObject({ visible: false });
    expect(stopControlView(null, true)).toMatchObject({ visible: false });
  });

  it('the stop control reads Stop and is enabled while a run is live', () => {
    expect(stopControlView('run-1', false)).toEqual({ visible: true, label: 'Stop', disabled: false });
  });

  it('the stop control reads Stopping… and is disabled while stopping', () => {
    expect(stopControlView('run-1', true)).toEqual({ visible: true, label: 'Stopping…', disabled: true });
  });
});

describe('requestStop', () => {
  it('requestStop returns null when run.stop succeeds', async () => {
    const conn = fakeConnection((m) => {
      if (m === 'run.stop') return {};
      throw new Error(`unexpected method ${m}`);
    });
    expect(await requestStop(conn, 'run-1')).toBeNull();
  });

  it('requestStop returns the error message instead of throwing', async () => {
    const conn = fakeConnection(() => {
      throw new Error('relay: connection is closed');
    });
    expect(await requestStop(conn, 'run-1')).toBe('relay: connection is closed');
  });
});
