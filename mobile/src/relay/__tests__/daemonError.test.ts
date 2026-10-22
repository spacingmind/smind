import { describe, expect, it } from 'vitest';
import { Channel } from '../e2ee';
import { DaemonError, RelayConnection } from '../RelayConnection';
import { DaemonKeys, daemonKeysFrom, FakeV2Daemon, memPipe } from './fakeV2Daemon';

// Same seam as RelayConnection.test.ts: a real E2EE channel pair with a minimal fake wsapi peer.
async function pair() {
  const [daemonTransport, mobileTransport] = memPipe();
  const keys: DaemonKeys = daemonKeysFrom();
  const daemon = new FakeV2Daemon(daemonTransport, keys);
  const daemonReady = daemon.handshake();
  const channel = new Channel(mobileTransport);
  await channel.handshake(keys.publicKey);
  await daemonReady;
  const conn = new RelayConnection(
    { daemonId: '', publicKey: keys.publicKey, relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' },
    { close: () => {} },
    channel,
  );
  return { conn, daemon };
}

describe('DaemonError', () => {
  it("carries the daemon's error code and leaves the message unchanged", async () => {
    const { conn, daemon } = await pair();
    const p = conn.call('x');
    const req = JSON.parse(new TextDecoder().decode(await daemon.receive()));
    daemon.send(new TextEncoder().encode(JSON.stringify({ id: req.id, error: { message: 'push is off', code: 'push_disabled' } })));
    const err = await p.then(
      () => null,
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(DaemonError);
    expect((err as DaemonError).code).toBe('push_disabled');
    expect((err as DaemonError).message).toBe('push is off');
  });

  it('has an undefined code when none is given', () => {
    expect(new DaemonError('m').code).toBeUndefined();
  });
});
