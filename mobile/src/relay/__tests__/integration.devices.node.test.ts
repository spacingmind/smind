// integration.devices.node.test.ts is ADR-0031 P13b's end-to-end test: the
// real TypeScript relay client (this package's RelayConnection, unmodified)
// against a real Go relay and a real multi-device daemon hub with a device
// registry (internal/relay/bridge/devharness), together with two Go
// native-gRPC "desktop" peers -- three devices on one relay host at once --
// through approval, a transport drop and resume, a daemon restart (fresh
// session), a relay restart, keepalive against the real relay, and revoke.
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { DataStream, GRPCWebSocketStream } from '../grpcweb';
import { decodeFrame } from '../proto';
import { ConnectOptions, grpcWebRelayNet, RelayConnection, RelayNet } from '../RelayConnection';
import { DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
const connections: RelayConnection[] = [];

/** The real grpc-web network, plus the ability to see every stream and cut the live one (a network drop). */
class RecordingNet implements RelayNet {
  readonly streams: GRPCWebSocketStream[] = [];
  /** keepalive frames the relay echoed back, as decoded by the client's own receive path. */
  echoes = 0;
  admit = grpcWebRelayNet.admit;
  openStream(wsUrl: string, headers: Record<string, string>): DataStream {
    const s = new GRPCWebSocketStream(wsUrl, headers);
    const receive = s.receiveMessage.bind(s);
    s.receiveMessage = (async (decode: (b: Uint8Array) => unknown) =>
      receive((b) => {
        const f = decodeFrame(b);
        if (f.keepalive) this.echoes++;
        return decode(b);
      })) as typeof s.receiveMessage;
    this.streams.push(s);
    return s;
  }
  cutLive(): void {
    this.streams[this.streams.length - 1].close();
  }
}

function open(seed: number, over: Partial<ConnectOptions> = {}): RelayConnection {
  const conn = RelayConnection.open(harness.pairingUrl, testConnectOptions(seed, over));
  connections.push(conn);
  return conn;
}

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  harness = await DevHarness.start(0);
}, 90_000);

afterAll(() => {
  for (const c of connections) c.close();
  harness?.stop();
});

describe('three devices on one relay host: two Go desktop peers and the mobile TypeScript client', () => {
  const net = new RecordingNet();
  let mobile: RelayConnection;
  let mobileId = '';

  it('an unapproved mobile device is pending, shows the daemon its id, and connects by itself once approved', { timeout: 60_000 }, async () => {
    mobile = open(11, {
      net,
      // Quick probes against the REAL relay (the 30 s / 75 s defaults are unit-tested).
      keepalive: { intervalMs: 300, deadAfterMs: 2_000, probeTimeoutMs: 2_000 },
      // The end-to-end watchdog, shortened (defaults: 15 s with a request waiting, 90 s idle).
      liveness: { pendingStaleMs: 1_500, idleStaleMs: 3_000, checkMs: 250 },
    });
    await vi.waitFor(() => expect(mobile.state.kind).toBe('pending'), { timeout: 20_000 });
    if (mobile.state.kind !== 'pending') throw new Error('unreachable');
    mobileId = mobile.state.deviceId;
    expect(mobileId).toHaveLength(26);

    // The daemon recorded it as pending, under the label the app sent.
    const rows = await harness.devices();
    const row = rows.find((r) => r.id === mobileId)!;
    expect(row).toMatchObject({ state: 'pending', label: 'node test device 11', connected: false });

    await harness.mustCmd(`approve ${mobileId}`);
    await mobile.whenConnected();
    await expect(mobile.call('workspace.list')).resolves.toEqual([]);
  });

  it('two Go desktop peers join while the mobile client stays attached to the same daemon', { timeout: 60_000 }, async () => {
    await harness.mustCmd('window 2');
    expect((await harness.mustCmd('peer-add desk-a')).state).toBe('approved');
    expect((await harness.mustCmd('peer-add desk-b')).state).toBe('approved');

    const rows = await harness.devices();
    expect(rows.filter((r) => r.state === 'approved' && r.connected)).toHaveLength(3);
    expect(mobile.state.kind).toBe('connected');
    await expect(mobile.call('workspace.list')).resolves.toEqual([]);
    expect((await harness.mustCmd('peer-call desk-a')).result_len).toBe(0);
    expect((await harness.mustCmd('peer-call desk-b')).result_len).toBe(0);
  });

  it('keepalive is echoed by the real relay on the mobile stream and keeps it alive across the idle window', { timeout: 30_000 }, async () => {
    const epoch = mobile.epoch;
    await vi.waitFor(() => expect(net.echoes).toBeGreaterThanOrEqual(3), { timeout: 10_000 });
    await new Promise((r) => setTimeout(r, 2_500)); // longer than deadAfterMs
    expect(mobile.epoch).toBe(epoch); // no reconnect: the echoes kept the stream alive
    expect(mobile.state.kind).toBe('connected');
  });

  it('resumes after a transport drop; the other two devices are unaffected', { timeout: 60_000 }, async () => {
    const epoch = mobile.epoch;
    const streams = net.streams.length;
    net.cutLive();
    await vi.waitFor(() => expect(mobile.epoch).toBeGreaterThan(epoch), { timeout: 30_000 });
    expect(net.streams.length).toBe(streams + 1);
    await expect(mobile.call('workspace.list')).resolves.toEqual([]);
    expect((await harness.mustCmd('peer-call desk-a')).result_len).toBe(0);
    expect((await harness.mustCmd('peer-call desk-b')).result_len).toBe(0);
    // Still ONE device row for the mobile client: a resume is not a new device.
    expect((await harness.devices()).filter((r) => r.id === mobileId)).toHaveLength(1);
  });

  it('opens a fresh session and re-subscribes after the daemon restarts (stream stays up)', { timeout: 90_000 }, async () => {
    const seen: string[] = [];
    mobile.subscribe(['workspace.updated', 'task.created'], (e) => seen.push(e.topic));
    const epoch = mobile.epoch;
    await harness.mustCmd('daemon-restart');
    // The daemon lost the session's counters but the relay keeps the phone's stream up and echoing
    // keepalives, so nothing drops: the liveness watchdog notices that the daemon stopped answering and
    // re-handshakes with a new session id (no cut of the stream here, on purpose).
    await vi.waitFor(() => expect(mobile.epoch).toBeGreaterThan(epoch), { timeout: 60_000 });
    await expect(mobile.call('workspace.list')).resolves.toEqual([]);
    expect((await harness.devices()).find((r) => r.id === mobileId)).toMatchObject({ state: 'approved', connected: true });
  });

  it('survives a relay restart', { timeout: 90_000 }, async () => {
    const epoch = mobile.epoch;
    await harness.mustCmd('relay-restart');
    await vi.waitFor(() => expect(mobile.epoch).toBeGreaterThan(epoch), { timeout: 60_000 });
    await expect(mobile.call('workspace.list')).resolves.toEqual([]);

    // The Go peers' native streams died with the relay (the harness peers do not reconnect): bring them
    // back as the same, already approved devices, so the three are attached again.
    for (const label of ['desk-a', 'desk-b']) {
      await vi.waitFor(async () => expect((await harness.cmd(`peer-add ${label}`)).state).toBe('approved'), { timeout: 60_000, interval: 500 });
    }
    expect((await harness.devices()).filter((r) => r.state === 'approved' && r.connected)).toHaveLength(3);
  });

  it('revoking the mobile device refuses only it; the Go desktop peers keep working', { timeout: 60_000 }, async () => {
    await harness.mustCmd(`revoke ${mobileId}`);
    await vi.waitFor(() => expect(mobile.state.kind).toBe('revoked'), { timeout: 30_000 });
    expect(mobile.state).toEqual({ kind: 'revoked', deviceId: mobileId });
    await expect(mobile.call('workspace.list')).rejects.toThrow('not connected (revoked)');
    // The other devices are untouched.
    expect((await harness.mustCmd('peer-call desk-a')).result_len).toBe(0);
    expect((await harness.mustCmd('peer-call desk-b')).result_len).toBe(0);
    expect((await harness.devices()).find((r) => r.id === mobileId)?.state).toBe('revoked');
  });

  it('a revoked device that connects again is refused, and a new identity is a new pending device', { timeout: 60_000 }, async () => {
    const again = open(11); // same key, same device id
    await expect(again.whenConnected()).rejects.toThrow('revoked');
    const fresh = open(12);
    await vi.waitFor(() => expect(fresh.state.kind).toBe('pending'), { timeout: 20_000 });
    expect(fresh.state).not.toEqual({ kind: 'pending', deviceId: mobileId, label: expect.anything() });
  });
});

describe('a daemon from before device_hello v2 (real relay, real E2EE handshake, then silence)', () => {
  it('is reported as outdated: failed with versionMismatch, no hello ever sent, no retry loop', { timeout: 60_000 }, async () => {
    const old = await DevHarness.start(0, true);
    try {
      const net = new RecordingNet();
      const conn = RelayConnection.open(old.pairingUrl, testConnectOptions(31, { net, reconnect: { challengeMs: 1_500, handshakeMs: 15_000 } }));
      connections.push(conn);
      await expect(conn.whenConnected()).rejects.toThrow('did not send a device challenge');
      expect(conn.state).toMatchObject({ kind: 'failed', versionMismatch: true });
      const streams = net.streams.length;
      await new Promise((r) => setTimeout(r, 1_500));
      expect(net.streams.length).toBe(streams); // not retried
    } finally {
      old.stop();
    }
  });
});
