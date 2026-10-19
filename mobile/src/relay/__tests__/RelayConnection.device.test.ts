// RelayConnection.device.test.ts covers ADR-0031 D9 / P13b at the connection
// layer against fakeRelay.ts: the device_hello the connection sends, the
// ids it uses, keepalive on the stream, the dead-stream rule, resume after a
// drop (same ids), a fresh session when the daemon lost the old one, and the
// pending (awaiting approval) / revoked states. Timings are shortened to a
// few milliseconds; the 30 s / 75 s defaults are asserted in keepalive.test.ts.

import { ed25519 } from '@noble/curves/ed25519.js';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { base64Decode, base64UrlEncode } from '../base64';
import { describeState } from '../connectionStatus';
import { helloSignedBytes, identityFromSeed } from '../deviceIdentity';
import { transcriptHash } from '../e2ee';
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
    reconnect: { baseMs: 5, maxMs: 20, pendingPollMs: 20, pendingPollMaxMs: 40, minStableMs: 2000, resumeProbeMs: 600, handshakeMs: 1500 },
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
    expect(hello.v).toBe(2);
    expect(hello.label).toBe('Test phone');
    expect(base64Decode(hello.device_pub)).toEqual(identity.publicKey);

    const [routeKey] = relay.routeKeys();
    const [deviceId, sessionId] = routeKey.split('|');
    expect(deviceId).toBe(identity.deviceId);
    expect(sessionId).not.toMatch(/^m1-default/);
    expect(sessionId.length).toBeGreaterThan(8);

    // The signature binds the session: checked by the fake daemon with the real v2 signed-bytes construction
    // (handshake transcript, route session id and the challenge the daemon sent on this session).
    const logged = relay.helloLog[0];
    expect(logged.session).toBe(sessionId);
    const e2eeHello = relay.lastStream.sent.find((f) => !f.keepalive && f.payload.length > 0)!;
    const ephemeralPub = e2eeHello.payload.slice(-32); // the first non-empty frame carries the E2EE hello (key in the last 32 bytes)
    // th is the handshake's transcript: this daemon's static key, its fresh ephemeral, and the client ephemeral that M1 carried.
    expect(logged.clientEphPub).toEqual(ephemeralPub);
    expect(logged.th).toEqual(transcriptHash(relay.daemonKeys.publicKey, logged.daemonEphPub, ephemeralPub));
    const bytes = (session: string, challenge: Uint8Array) =>
      helloSignedBytes(logged.th, relay.daemonKeys.publicKey, new TextEncoder().encode(session), challenge);
    expect(ed25519.verify(base64Decode(hello.signature), bytes(sessionId, logged.challenge), identity.publicKey)).toBe(true);
    // Not valid for another route's session id, nor for another challenge (a replay into a new route/session fails both ways).
    expect(ed25519.verify(base64Decode(hello.signature), bytes('s-other-route', logged.challenge), identity.publicKey)).toBe(false);
    expect(ed25519.verify(base64Decode(hello.signature), bytes(sessionId, new Uint8Array(32).fill(1)), identity.publicKey)).toBe(false);

    // And the connection is usable.
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('uses a fresh session id (and ephemeral key) for every new pairing, but the same device id', async () => {
    const relay = new FakeRelay();
    const a = start(relay);
    await a.whenConnected();
    a.close(); // a daemon serves one session per device at a time
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

  it('a daemon on E2EE v1 is refused at the handshake: a specific status, no retry, no other route', async () => {
    const relay = new FakeRelay();
    relay.e2eeVersion = 1;
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow(/older E2EE version: update the daemon/);
    expect(conn.state).toMatchObject({ kind: 'failed', versionMismatch: true, e2eePeerVersion: 1 });
    const streams = relay.streams.length;
    const routes = relay.routeKeys();
    await new Promise((r) => setTimeout(r, 250)); // the retry loop would have opened more by now
    expect(relay.streams.length).toBe(streams);
    expect(relay.routeKeys()).toEqual(routes);
    expect(relay.admissions).toBe(1);
    expect(relay.hellos).toHaveLength(0); // no device_hello (nor anything else) was sent to it
    expect(describeState(conn.state)).toMatchObject({ title: 'Daemon is too old for this app', command: 'smind update' });
  });

  it('a daemon on a NEWER E2EE version says to update this app, and stops the same way', async () => {
    const relay = new FakeRelay();
    relay.e2eeVersion = 3;
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow(/update this app/);
    expect(conn.state).toMatchObject({ kind: 'failed', e2eePeerVersion: 3 });
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 250));
    expect(relay.streams.length).toBe(streams);
    expect(describeState(conn.state)).toMatchObject({ title: 'This app is too old for the daemon' });
    expect(describeState(conn.state)?.command).toBeUndefined();
  });

  it('an E2EE version error after a connection had worked is still final (a daemon that downgraded is not retried)', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.e2eeVersion = 1;
    relay.restartDaemon();
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.state).toMatchObject({ kind: 'failed', e2eePeerVersion: 1 }), { timeout: 5000 });
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 250));
    expect(relay.streams.length).toBe(streams);
  }, 15000);

  it('a daemon rejecting the hello signature is a failure', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'bad_hello';
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('rejected');
  });
});

describe('review hardening', () => {
  it('close() during the handshake ends the stream instead of leaving an admitted ghost session', async () => {
    const relay = new FakeRelay();
    let release: () => void = () => {};
    relay.gate = new Promise<void>((resolve) => (release = resolve)); // the daemon is slow to answer the hello
    const conn = start(relay);
    await vi.waitFor(() => expect(relay.hellos).toHaveLength(1)); // the hello is on its way, the answer is not
    conn.close();
    expect(relay.streams[0].closed).toBe(true);
    release();
    await new Promise((r) => setTimeout(r, 100));
    expect(conn.state.kind).toBe('closed');
    expect(relay.streams).toHaveLength(1); // and it did not start another one
  });

  it('a daemon that restarted without the stream dropping is noticed (calls hang no longer): fresh session, pending calls rejected', async () => {
    const relay = new FakeRelay();
    const conn = start(relay, { liveness: { pendingStaleMs: 150, idleStaleMs: 5000, checkMs: 30 } });
    await conn.whenConnected();
    const epoch = conn.epoch;
    const inFlight = conn.call('hang'); // never answered, like a request the new daemon never saw
    relay.restartDaemon(); // the relay keeps the phone's stream up and echoing keepalives
    const rejection = expect(inFlight).rejects.toThrow('relay: connection lost');
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
    await rejection;
    expect(relay.routeKeys()).toHaveLength(2); // a fresh session, not a resume of the dead one
    expect(relay.hellos).toHaveLength(2);
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('total silence on an idle session is probed too, and a healthy daemon is left alone', async () => {
    const relay = new FakeRelay();
    const conn = start(relay, { liveness: { pendingStaleMs: 5000, idleStaleMs: 120, checkMs: 30 } });
    await conn.whenConnected();
    const epoch = conn.epoch;
    await vi.waitFor(() => expect(relay.requests.filter((r) => r.method === 'workspace.list').length).toBeGreaterThanOrEqual(2), { timeout: 3000 });
    await new Promise((r) => setTimeout(r, 900)); // longer than the probe's own timeout (600 ms): a false drop would show by now
    expect(conn.epoch).toBe(epoch); // answered: no reconnect
    relay.restartDaemon();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
  });

  it('a transient error while pending does not turn into a final failure', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'pending';
    const conn = start(relay);
    await vi.waitFor(() => expect(conn.state.kind).toBe('pending'), { timeout: 2000 });
    relay.admitError = new Error('network blip');
    await new Promise((r) => setTimeout(r, 200));
    expect(['pending', 'reconnecting']).toContain(conn.state.kind);
    relay.admitError = null;
    relay.policy = () => 'approved';
    await conn.whenConnected();
  });

  it('a daemon with no room (too_many_devices) is retried with backoff, even on the first connect', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'too_many';
    const conn = start(relay);
    await vi.waitFor(() => expect(relay.hellos.length).toBeGreaterThanOrEqual(2), { timeout: 3000 });
    expect(conn.state.kind).toBe('reconnecting');
    relay.policy = () => 'approved';
    await conn.whenConnected();
  });

  it('a relay that is full (status 8 on the stream) is retried too', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    relay.restartDaemon();
    relay.dropCurrentStream(new GrpcStatusError('OpenData', 8, 'relay: too many sessions for this relay host'));
    await vi.waitFor(() => expect(conn.state.kind).toBe('reconnecting'));
    await vi.waitFor(() => expect(conn.state.kind).toBe('connected'), { timeout: 5000 });
  });

  it('a relay that refuses the route when the first connect opens it (status 8) is retried, not failed', async () => {
    const relay = new FakeRelay();
    relay.killNextStream = new GrpcStatusError('OpenData', 8, 'relay: too many sessions for this relay host');
    const conn = start(relay);
    const seen = states(conn);
    await conn.whenConnected(); // the second attempt gets through
    expect(kinds(seen)).toContain('reconnecting');
    expect(kinds(seen)).not.toContain('failed');
  });

  it('keeps probing from the OLDEST unanswered request: tapping again on a frozen session does not postpone the check', async () => {
    const relay = new FakeRelay();
    const conn = start(relay, { liveness: { pendingStaleMs: 200, idleStaleMs: 5000, checkMs: 30 } });
    await conn.whenConnected();
    const epoch = conn.epoch;
    relay.restartDaemon();
    const calls: Promise<unknown>[] = [];
    for (let i = 0; i < 12; i++) {
      calls.push(conn.call('hang').catch(() => {}));
      await new Promise((r) => setTimeout(r, 40)); // a new request every 40 ms, well inside the stale window
    }
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
    await Promise.all(calls);
  });

  it('a path that connects and dies at once backs off instead of reconnecting in a tight loop', async () => {
    const relay = new FakeRelay();
    const conn = start(relay, { reconnect: { baseMs: 80, maxMs: 400, minStableMs: 10_000, resumeProbeMs: 600, handshakeMs: 1500 } });
    await conn.whenConnected();
    const before = relay.streams.length;
    relay.dropCurrentStream();
    await new Promise((r) => setTimeout(r, 50)); // inside the backoff (baseMs 80): no new stream yet
    expect(relay.streams.length).toBe(before);
    await vi.waitFor(() => expect(relay.streams.length).toBeGreaterThan(before), { timeout: 3000 });
  });

  it('a daemon that never answers device_hello (the real behaviour of one that predates it) fails with the update hint', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'silent';
    const conn = start(relay, { reconnect: { handshakeMs: 150 } });
    await expect(conn.whenConnected()).rejects.toThrow('may be older than this app');
  });
});

describe('hello v2: the daemon speaks first', () => {
  /** A hello of any kind on the wire: the client must not send one without a valid challenge. */
  const helloSent = (relay: FakeRelay) => relay.streams.some((st) => st.sent.some((f) => !f.keepalive && f.payload.length > 90)) || relay.hellos.length > 0;

  it('a daemon that sends no challenge (older than v2) is reported as outdated, never sent a v1 hello, never retried', async () => {
    const relay = new FakeRelay();
    relay.challengeMode = 'none';
    const conn = start(relay, { reconnect: { challengeMs: 150, handshakeMs: 1500 } });
    await expect(conn.whenConnected()).rejects.toThrow('did not send a device challenge');
    expect(conn.state).toMatchObject({ kind: 'failed', versionMismatch: true });
    expect(relay.hellos).toHaveLength(0); // no hello of any version reached the daemon
    expect(helloSent(relay)).toBe(false);
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 300));
    expect(relay.streams.length).toBe(streams); // and no retry loop
    expect(relay.streams[0].closed).toBe(true);
  });

  it('a daemon that stalls on the challenge AFTER having answered is retried, not called outdated', { timeout: 20_000 }, async () => {
    const relay = new FakeRelay();
    const conn = start(relay, { reconnect: { baseMs: 5, maxMs: 20, resumeProbeMs: 300, handshakeMs: 1500, challengeMs: 150 } });
    await conn.whenConnected();
    const epoch = conn.epoch;
    relay.challengeMode = 'none'; // the new session's challenge never comes (a stalled daemon)
    relay.restartDaemon();
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(relay.streams.length).toBeGreaterThan(3), { timeout: 8000 }); // it keeps trying
    expect(conn.state.kind).not.toBe('failed');
    relay.challengeMode = 'v2';
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 8000 });
  });

  it('a challenge of another version, or one that is not 32 bytes, means different versions: failed, no hello', async () => {
    for (const mode of ['other-version', 'short'] as const) {
      const relay = new FakeRelay();
      relay.challengeMode = mode;
      const conn = start(relay);
      await expect(conn.whenConnected()).rejects.toThrow('different device-identity versions');
      expect(conn.state).toMatchObject({ kind: 'failed', versionMismatch: true });
      expect(relay.hellos).toHaveLength(0);
    }
  });

  it('a daemon that refuses the hello as hello_version_unsupported is a version mismatch too', async () => {
    const relay = new FakeRelay();
    relay.policy = () => 'version';
    const conn = start(relay);
    await expect(conn.whenConnected()).rejects.toThrow('different device-identity versions');
    expect(conn.state).toMatchObject({ kind: 'failed', versionMismatch: true });
  });

  it('every fresh session signs the challenge of THAT session: a hello from one session does not verify in another', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    const epoch = conn.epoch;
    relay.restartDaemon();
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
    expect(relay.helloLog).toHaveLength(2);
    const [a, b] = relay.helloLog;
    expect(a.session).not.toBe(b.session);
    expect(Buffer.from(a.challenge).equals(Buffer.from(b.challenge))).toBe(false); // a fresh challenge per session
    const verifies = (h: typeof a, against: typeof a) =>
      ed25519.verify(
        base64Decode(h.hello.signature),
        helloSignedBytes(against.th, relay.daemonKeys.publicKey, new TextEncoder().encode(against.session), against.challenge),
        identity.publicKey,
      );
    expect(verifies(a, a)).toBe(true);
    expect(verifies(b, b)).toBe(true);
    expect(verifies(a, b)).toBe(false); // replaying session A's hello into session B fails
    expect(verifies(b, a)).toBe(false);
  });

  it('a resumed session sends no new hello (its challenge was answered once, on that session)', async () => {
    const relay = new FakeRelay();
    const conn = start(relay);
    await conn.whenConnected();
    const epoch = conn.epoch;
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
    expect(relay.helloLog).toHaveLength(1);
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
