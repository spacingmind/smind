// RelayConnection.device.test.ts covers ADR-0031 D9 / P13b at the connection
// layer against fakeRelay.ts: the device_hello the connection sends, the
// ids it uses, keepalive on the stream, the dead-stream rule, resume after a
// drop (same ids), a fresh session when the daemon lost the old one, and the
// pending (awaiting approval) / revoked states. Timings are shortened to a
// few milliseconds; the 30 s / 75 s defaults are asserted in keepalive.test.ts.

import { ed25519 } from '@noble/curves/ed25519.js';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { base64Decode, base64UrlEncode } from '../base64';
import { helloSignedBytes, identityFromSeed } from '../deviceIdentity';
import { GrpcStatusError } from '../grpcweb';
import { ConnectionState, ConnectOptions, RelayConnection } from '../RelayConnection';
import { FakeRelay } from './fakeRelay';

const identity = identityFromSeed(new Uint8Array(32).fill(7));
const open: RelayConnection[] = [];

function pairingUrl(relay: FakeRelay): string {
  const offer = {
    v: 1,
    id: 'daemon-1',
    pk: base64UrlEncode(relay.daemonKeys.publicKey),
    relay: 'https://relay.test',
    sec: base64UrlEncode(new Uint8Array(32).fill(9)),
    ws: 'host-1',
  };
  return 'https://spacingmind.sh/pair#offer=' + base64UrlEncode(new TextEncoder().encode(JSON.stringify(offer)));
}

function start(relay: FakeRelay, over: Partial<ConnectOptions> = {}): RelayConnection {
  const conn = RelayConnection.open(pairingUrl(relay), {
    identity,
    label: 'Test phone',
    net: relay,
    jitter: (ms) => ms,
    keepalive: { intervalMs: 40, deadAfterMs: 400, probeTimeoutMs: 150 },
    reconnect: { baseMs: 5, maxMs: 20, pendingPollMs: 20, resumeProbeMs: 300, handshakeMs: 1500 },
    ...over,
  });
  open.push(conn);
  return conn;
}

function states(conn: RelayConnection): ConnectionState[] {
  const seen: ConnectionState[] = [conn.state];
  conn.onState((s) => seen.push(s));
  return seen;
}

const kinds = (s: ConnectionState[]) => s.map((x) => x.kind);

afterEach(() => {
  for (const c of open.splice(0)) c.close();
});

describe('device identity on the wire', () => {
  it('sends a device_hello that the daemon verifies, under a key-derived device id and a per-pairing session id', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();

    expect(relay.hellos).toHaveLength(1);
    const hello = relay.hellos[0];
    expect(hello.v).toBe(1);
    expect(hello.label).toBe('Test phone');
    expect(base64Decode(hello.device_pub)).toEqual(identity.publicKey);

    const [routeKey] = relay.routeKeys();
    const [deviceId, sessionId] = routeKey.split('|');
    expect(deviceId).toBe(identity.deviceId);
    expect(sessionId).not.toMatch(/^m1-default/);
    expect(sessionId.length).toBeGreaterThan(8);

    // The signature binds the handshake: checked by the fake daemon with the real signed-bytes construction.
    // The first non-empty frame carries the E2EE hello with the client's ephemeral key (last 32 bytes).
    const e2eeHello = relay.lastStream.sent.find((f) => !f.keepalive && f.payload.length > 0)!;
    const ephemeralPub = e2eeHello.payload.slice(-32);
    expect(ed25519.verify(base64Decode(hello.signature), helloSignedBytes(relay.daemonKeys.publicKey, ephemeralPub), identity.publicKey)).toBe(true);

    // And the connection is usable.
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('uses a fresh session id (and ephemeral key) for every new pairing, but the same device id', async () => {
    const relay = new FakeRelay();
    const a = start(relay);
    await a.whenConnected();
    const b = start(relay);
    await b.whenConnected();
    const keys = relay.routeKeys();
    expect(keys).toHaveLength(2);
    expect(keys[0].split('|')[0]).toBe(keys[1].split('|')[0]);
    expect(keys[0].split('|')[1]).not.toBe(keys[1].split('|')[1]);
  });
});

describe('keepalive', () => {
  it('keepalive is sent on the websocket stream (first probe at once, then every interval, with a stable nonce)', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    const stream = relay.lastStream;
    await vi.waitFor(() => expect(stream.sent.filter((f) => f.keepalive).length).toBeGreaterThanOrEqual(3), { timeout: 2000 });
    const probes = stream.sent.filter((f) => f.keepalive);
    expect(new Set(probes.map((p) => p.sequence)).size).toBe(1);
    expect(probes[0].sequence).toBeGreaterThan(0);
    expect(probes.every((p) => p.payload.length === 0 && p.deviceId === identity.deviceId)).toBe(true);
    expect(conn.state.kind).toBe('connected');
  });

  it('a relay without keepalive support never trips the dead-stream rule', async () => {
    const relay = new FakeRelay();
    relay.echoKeepalive = false;
    const conn = start(relay);
    await conn.whenConnected();
    const seen = states(conn);
    await new Promise((r) => setTimeout(r, 900)); // well past deadAfterMs
    expect(kinds(seen)).toEqual(['connected']);
    expect(relay.streams).toHaveLength(1);
  });

  it('dead stream after the silence window reconnects (and resumes the same session)', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    expect(relay.streams).toHaveLength(1);
    const seen = states(conn);

    relay.blackholed.add(relay.lastStream); // nothing is echoed or forwarded on it: a half-open path
    await vi.waitFor(() => expect(relay.streams.length).toBeGreaterThanOrEqual(2), { timeout: 2000 });
    await vi.waitFor(() => expect(conn.state.kind).toBe('connected'), { timeout: 2000 });
    expect(kinds(seen)).toContain('reconnecting');
    // Same route on the new stream: it resumed rather than re-handshaking.
    expect(relay.routeKeys()).toHaveLength(1);
    expect(relay.hellos).toHaveLength(1);
  });
});

describe('reconnect and resume', () => {
  it('resumes after a drop keeping ids, sequence and the E2EE session (no second hello)', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    await conn.call('workspace.list');
    const first = relay.lastStream;
    const lastSeq = Math.max(...first.sent.filter((f) => !f.keepalive).map((f) => f.sequence));
    const epoch = conn.epoch;

    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 2000 });

    expect(relay.streams.length).toBe(2);
    const second = relay.lastStream;
    expect(second.routeKey).toBe(first.routeKey);
    expect(relay.hellos).toHaveLength(1);
    const regs = second.sent.filter((f) => !f.keepalive);
    expect(regs[0].payload.length).toBe(0); // the route registration frame
    expect(regs[0].sequence).toBeGreaterThan(lastSeq); // the frame sequence carries on

    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('rejects an in-flight request when the stream drops (at-most-once) and allows a retry afterwards', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    const inFlight = conn.call('hang');
    await vi.waitFor(() => expect(relay.requests.some((r) => r.method === 'hang')).toBe(true));
    const epoch = conn.epoch;
    relay.dropCurrentStream();
    await expect(inFlight).rejects.toThrow('relay: connection lost');
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 2000 });
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('calls made while reconnecting reject at once instead of hanging', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.admitError = new Error('network down');
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.state.kind).toBe('reconnecting'));
    await expect(conn.call('workspace.list')).rejects.toThrow('not connected (reconnecting)');
    relay.admitError = null;
    await vi.waitFor(() => expect(conn.state.kind).toBe('connected'), { timeout: 2000 });
  });

  it('opens a fresh session (new session id, new hello) when the daemon lost the old one, and re-subscribes', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    conn.subscribe(['task.created'], () => {});
    await vi.waitFor(() => expect(relay.requests.filter((r) => r.method === 'events.subscribe')).toHaveLength(1));
    const epoch = conn.epoch;

    relay.restartDaemon(); // the daemon forgets every session
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 3000 });

    expect(relay.routeKeys()).toHaveLength(2);
    const [oldKey, newKey] = relay.routeKeys();
    expect(newKey.split('|')[0]).toBe(oldKey.split('|')[0]); // same device
    expect(newKey.split('|')[1]).not.toBe(oldKey.split('|')[1]); // new session
    expect(relay.hellos).toHaveLength(2);
    await vi.waitFor(() => expect(relay.requests.filter((r) => r.method === 'events.subscribe').length).toBeGreaterThanOrEqual(2));
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('close() ends the reconnect loop for good', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.admitError = new Error('down');
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.state.kind).toBe('reconnecting'));
    conn.close();
    const admissions = relay.admissions;
    await new Promise((r) => setTimeout(r, 100));
    expect(conn.state.kind).toBe('closed');
    expect(relay.admissions).toBe(admissions);
    await expect(conn.call('workspace.list')).rejects.toThrow('relay: connection is closed');
  });
});

describe('approval and revocation', () => {
  it('a device the daemon has not approved is pending, then connects by itself once approved', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'pending';
    const conn = start(relay);
    const seen = states(conn);

    await vi.waitFor(() => expect(conn.state.kind).toBe('pending'), { timeout: 2000 });
    expect(conn.state).toEqual({ kind: 'pending', deviceId: identity.deviceId, label: 'Test phone' });
    // It keeps asking (each ask is a fresh session) rather than giving up.
    await vi.waitFor(() => expect(relay.hellos.length).toBeGreaterThanOrEqual(3), { timeout: 2000 });
    expect(relay.routeKeys().length).toBeGreaterThanOrEqual(3);

    relay.policy = () => 'approved';
    await conn.whenConnected();
    expect(kinds(seen)).toContain('pending');
    expect(conn.state.kind).toBe('connected');
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('a revoked device is revoked and stops trying', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'revoked';
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('revoked');
    expect(conn.state).toEqual({ kind: 'revoked', deviceId: identity.deviceId });
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 100));
    expect(relay.streams.length).toBe(streams);
  });

  it('revocation mid-session (the relay closes the stream with PermissionDenied) ends in revoked', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.dropCurrentStream(new GrpcStatusError('OpenData', 7, 'relay: device revoked'));
    await vi.waitFor(() => expect(conn.state.kind).toBe('revoked'), { timeout: 2000 });
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 100));
    expect(relay.streams.length).toBe(streams);
  });

  it('revocation noticed only at the next hello (daemon restarted, relay forgot) ends in revoked', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.policy = () => 'revoked';
    relay.restartDaemon();
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.state.kind).toBe('revoked'), { timeout: 3000 });
  });

  it('a daemon that predates device identity is a clear failure, not a retry loop', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'legacy-daemon';
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('predates device identity');
    expect(conn.state.kind).toBe('failed');
  });

  it('a daemon rejecting the hello signature is a failure', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'bad_hello';
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('rejected');
  });
});

describe('first connect failures', () => {
  it('an unreachable relay on the first attempt is reported (failed), not retried silently', async () => {
    const relay = new FakeRelay();
    relay.admitError = new Error('grpc-web AdmitChallenge: status 16: unauthenticated');
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('unauthenticated');
    expect(conn.state.kind).toBe('failed');
  });

  it('a daemon whose key is not the offer’s is a failure', async () => {
    const relay = new FakeRelay();
    const url = pairingUrl(relay).replace(/offer=.*/, () => {
      const wrong = base64UrlEncode(new Uint8Array(32).fill(3));
      const offer = { v: 1, id: 'd', pk: wrong, relay: 'https://relay.test', sec: base64UrlEncode(new Uint8Array(32).fill(9)), ws: 'host-1' };
      return 'offer=' + base64UrlEncode(new TextEncoder().encode(JSON.stringify(offer)));
    });
    const conn = RelayConnection.open(url, {
      identity,
      label: 'x',
      net: relay,
      jitter: (ms) => ms,
      reconnect: { handshakeMs: 800 },
    });
    open.push(conn);
    await expect(conn.whenConnected()).rejects.toThrow('not the one in the pairing offer');
  });
});
