// RelayConnection.password.test.ts covers ADR-0031 P13c on mobile against
// fakeRelay.ts (a daemon with a password answering as the spec says:
// password_required / incorrect_password / too_many_attempts with retry_after):
// the password is asked for, held in memory only, sent only to the daemon it
// was typed for, never re-sent once refused, kept through a lockout, and never
// shown in state, errors, logs or storage.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { base64UrlEncode } from '../base64';
import { createDaemonPasswords, DaemonPasswords, MAX_PASSWORD_BYTES, passwordKey } from '../daemonPasswords';
import { identityFromSeed } from '../deviceIdentity';
import { parsePairingURL } from '../pairing';
import { ConnectOptions, RelayConnection } from '../RelayConnection';
import { FakeRelay } from './fakeRelay';

const identity = identityFromSeed(new Uint8Array(32).fill(7));
const open: RelayConnection[] = [];

function pairingUrl(relay: FakeRelay, host = 'host-1'): string {
  const offer = {
    v: 1,
    id: 'daemon-1',
    pk: base64UrlEncode(relay.daemonKeys.publicKey),
    relay: 'https://relay.test',
    sec: base64UrlEncode(new Uint8Array(32).fill(9)),
    ws: host,
  };
  return 'https://spacingmind.sh/pair#offer=' + base64UrlEncode(new TextEncoder().encode(JSON.stringify(offer)));
}

function start(relay: FakeRelay, passwords: DaemonPasswords, over: Partial<ConnectOptions> = {}): RelayConnection {
  const conn = RelayConnection.open(pairingUrl(relay), {
    identity,
    label: 'Test phone',
    net: relay,
    passwords,
    jitter: (ms) => ms,
    reconnect: { baseMs: 5, maxMs: 20, pendingPollMs: 20, pendingPollMaxMs: 40, minStableMs: 2000, resumeProbeMs: 600, handshakeMs: 1500, lockMinMs: 40, lockMaxMs: 3_600_000 },
    ...over,
  });
  open.push(conn);
  return conn;
}

const passwordsSent = (relay: FakeRelay) => relay.helloLog.map((h) => h.hello.password);

afterEach(() => {
  for (const c of open.splice(0)) c.close();
  vi.restoreAllMocks();
});

describe('a daemon with a password', () => {
  it('asks for it (password required), stops retrying, and connects once it is submitted', async () => {
    const relay = new FakeRelay();
    relay.password = 's3cret pass';
    const conn = start(relay, createDaemonPasswords());

    await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'required' }), { timeout: 2000 });
    // The automatic retry loop is stopped: no new hello, no new stream while the prompt is up.
    const hellos = relay.helloLog.length;
    const streams = relay.streams.length;
    await new Promise((r) => setTimeout(r, 250));
    expect(relay.helloLog.length).toBe(hellos);
    expect(relay.streams.length).toBe(streams);
    expect(passwordsSent(relay)).toEqual([undefined]); // nothing typed yet, nothing sent

    conn.submitPassword('s3cret pass');
    await conn.whenConnected();
    expect(passwordsSent(relay)).toEqual([undefined, 's3cret pass']);
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('a submit made synchronously from a state listener is not lost', async () => {
    const relay = new FakeRelay();
    relay.password = 'auto';
    const conn = start(relay, createDaemonPasswords());
    conn.onState((s) => {
      if (s.kind === 'password') conn.submitPassword('auto'); // answers the prompt before setState even returns
    });
    await conn.whenConnected();
    expect(passwordsSent(relay)).toEqual([undefined, 'auto']);
  });

  it('a wrong password is refused, forgotten, never re-sent, and the prompt says so; the right one then connects', async () => {
    const relay = new FakeRelay();
    relay.password = 'right-one';
    const passwords = createDaemonPasswords();
    const conn = start(relay, passwords);
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'));

    conn.submitPassword('wrong-one');
    await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'incorrect' }), { timeout: 2000 });
    expect(passwords.get(passwordKey(parsePairingURL(pairingUrl(relay))))).toBeUndefined(); // forgotten
    const sentWhenRefused = passwordsSent(relay).length;
    await new Promise((r) => setTimeout(r, 250)); // and no automatic retry with it
    expect(passwordsSent(relay).length).toBe(sentWhenRefused);

    conn.submitPassword('right-one');
    await conn.whenConnected();
    expect(passwordsSent(relay)).toEqual([undefined, 'wrong-one', 'right-one']);
    expect(passwordsSent(relay).filter((p) => p === 'wrong-one')).toHaveLength(1); // exactly once
    expect(relay.wrongPasswords).toBe(1);
  });

  it('forgets exactly the rejected value: a newer one typed while the reply was in flight is kept and used', async () => {
    const relay = new FakeRelay();
    relay.password = 'newer-is-right';
    let release: () => void = () => {};
    const passwords = createDaemonPasswords();
    const conn = start(relay, passwords);
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'));

    relay.gate = new Promise<void>((resolve) => (release = resolve)); // the daemon is slow to answer the next hello
    conn.submitPassword('older-wrong');
    await vi.waitFor(() => expect(relay.hellos.map((h) => h.password)).toContain('older-wrong')); // the hello is at the daemon, its answer is held
    conn.submitPassword('newer-is-right'); // typed while the reply to 'older-wrong' is in flight
    release();
    await conn.whenConnected(); // the newer one is used without asking again
    expect(passwordsSent(relay)).toEqual([undefined, 'older-wrong', 'newer-is-right']);
  });

  it('holds the password for the app run: a drop, or a daemon restart, reconnects without asking again', async () => {
    const relay = new FakeRelay();
    relay.password = 'hold-me';
    const conn = start(relay, createDaemonPasswords());
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'));
    conn.submitPassword('hold-me');
    await conn.whenConnected();
    const epoch = conn.epoch;
    const seen: string[] = [];
    conn.onState((s) => seen.push(s.kind));

    relay.restartDaemon(); // the daemon lost the session: a fresh session, a fresh hello
    relay.dropCurrentStream();
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 5000 });
    expect(seen).not.toContain('password');
    expect(passwordsSent(relay).at(-1)).toBe('hold-me');
  });

  it('a lockout (too_many_attempts) keeps the password and tries it once more after the wait', async () => {
    const relay = new FakeRelay();
    relay.password = 'keep-me';
    relay.lockNextHellos = 1; // the daemon rate-limits the first hello, before it looks at the password
    relay.retryAfter = 1; // raised to lockMinMs (40 ms here, 5 s in the app)
    const passwords = createDaemonPasswords();
    passwords.set(passwordKey(parsePairingURL(pairingUrl(relay))), 'keep-me');
    const conn = start(relay, passwords);
    await vi.waitFor(() => expect(conn.state.kind).toBe('locked'), { timeout: 2000 });
    if (conn.state.kind !== 'locked') throw new Error('unreachable');
    expect(conn.state.until).toBeGreaterThan(Date.now() - 1);
    await conn.whenConnected(); // after the wait, the SAME password, once more, and it was right
    expect(passwordsSent(relay)).toEqual(['keep-me', 'keep-me']);
    expect(relay.wrongPasswords).toBe(0);
    expect(passwords.get(passwordKey(parsePairingURL(pairingUrl(relay))))).toBe('keep-me'); // never forgotten by a lockout
  });

  it('clamps what a daemon says about how long to wait', async () => {
    const relay = new FakeRelay();
    relay.password = 'x';
    relay.lockNextHellos = 1;
    relay.retryAfter = 999_999; // a hostile or buggy retry_after
    const passwords = createDaemonPasswords();
    passwords.set(passwordKey(parsePairingURL(pairingUrl(relay))), 'x');
    const conn = start(relay, passwords, { reconnect: { lockMinMs: 5_000, lockMaxMs: 3_600_000 } });
    await vi.waitFor(() => expect(conn.state.kind).toBe('locked'), { timeout: 3000 });
    expect(conn.state).toMatchObject({ kind: 'locked', retryAfter: 3600 });

    const relay2 = new FakeRelay();
    relay2.password = 'x';
    relay2.lockNextHellos = 1;
    relay2.retryAfter = 1; // "retry in 1 s" is raised to the 5 s floor
    const passwords2 = createDaemonPasswords();
    passwords2.set(passwordKey(parsePairingURL(pairingUrl(relay2))), 'x');
    const conn2 = start(relay2, passwords2, { reconnect: { lockMinMs: 5_000, lockMaxMs: 3_600_000 } });
    await vi.waitFor(() => expect(conn2.state.kind).toBe('locked'), { timeout: 3000 });
    expect(conn2.state).toMatchObject({ kind: 'locked', retryAfter: 5 });
  });

  it('sends a password only to the daemon it was typed for (keyed by relay, host id and daemon key)', async () => {
    const relayA = new FakeRelay();
    relayA.password = 'for-daemon-a';
    const passwords = createDaemonPasswords();
    const a = start(relayA, passwords);
    await vi.waitFor(() => expect(a.state.kind).toBe('password'));
    a.submitPassword('for-daemon-a');
    await a.whenConnected();
    a.close();

    // Same relay address and host id, DIFFERENT daemon key (the link was re-issued by another daemon).
    const relayB = new FakeRelay();
    relayB.password = 'for-daemon-a';
    const b = start(relayB, passwords);
    await vi.waitFor(() => expect(b.state).toEqual({ kind: 'password', reason: 'required' }));
    expect(passwordsSent(relayB)).toEqual([undefined]); // nothing leaked to the other daemon

    // Different host id on the same daemon key: also empty.
    const c = RelayConnection.open(pairingUrl(relayA, 'host-2'), { identity, label: 'x', net: relayA, passwords, jitter: (ms) => ms });
    open.push(c);
    await vi.waitFor(() => expect(c.state.kind).toBe('password'));
  });

  it('a daemon without a password never gets one, and a pre-P13c daemon that ignores the field still connects', async () => {
    const plain = new FakeRelay(); // no password on the daemon
    const connPlain = start(plain, createDaemonPasswords());
    await connPlain.whenConnected();
    expect(passwordsSent(plain)).toEqual([undefined]);

    // The person typed a password earlier this run for this daemon; a daemon that has none (or predates P13c) ignores it.
    const old = new FakeRelay();
    const passwords = createDaemonPasswords();
    passwords.set(passwordKey(parsePairingURL(pairingUrl(old))), 'typed-earlier');
    const connOld = start(old, passwords);
    await connOld.whenConnected();
    expect(passwordsSent(old)).toEqual(['typed-earlier']);
  });

  it('refuses to submit a password the daemon could never accept (empty, or over 128 bytes) without keeping it', async () => {
    const relay = new FakeRelay();
    relay.password = 'p';
    const passwords = createDaemonPasswords();
    const conn = start(relay, passwords);
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'));
    expect(() => conn.submitPassword('')).toThrow('Enter the daemon password');
    expect(() => conn.submitPassword('é'.repeat(MAX_PASSWORD_BYTES / 2 + 1))).toThrow('at most 128 bytes');
    expect(passwords.get(passwordKey(parsePairingURL(pairingUrl(relay))))).toBeUndefined();
    expect(relay.wrongPasswords).toBe(0); // no wrong attempt was spent on it
    conn.submitPassword('é'.repeat(MAX_PASSWORD_BYTES / 2)); // exactly 128 bytes is fine to send
  });

  it('the password reaches nothing but the encrypted hello: not state, errors, console, nor the stored identity', async () => {
    const logs: unknown[][] = [];
    for (const m of ['log', 'info', 'warn', 'error', 'debug'] as const) vi.spyOn(console, m).mockImplementation((...a) => void logs.push(a));
    const relay = new FakeRelay();
    relay.password = 'TOP-SECRET-phrase';
    const passwords = createDaemonPasswords();
    const conn = start(relay, passwords);
    const states: unknown[] = [];
    conn.onState((s) => states.push(s));
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'));
    conn.submitPassword('WRONG-secret-phrase');
    await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'incorrect' }));
    conn.submitPassword('TOP-SECRET-phrase');
    await conn.whenConnected();

    const everything = JSON.stringify({ states, state: conn.state, offer: conn.offer, logs, store: passwords, identity });
    for (const secret of ['TOP-SECRET-phrase', 'WRONG-secret-phrase']) expect(everything).not.toContain(secret);
    // The frames on the wire carry it only inside E2EE ciphertext: no frame payload contains it in the clear.
    for (const s of relay.streams) {
      for (const f of s.sent) {
        const text = new TextDecoder('latin1').decode(f.payload);
        expect(text).not.toContain('TOP-SECRET-phrase');
      }
    }
    expect(passwordsSent(relay)).toContain('TOP-SECRET-phrase'); // and the daemon (after decrypting) did get it
  });
});
