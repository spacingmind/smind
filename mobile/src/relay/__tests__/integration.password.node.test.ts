// integration.password.node.test.ts is ADR-0031 P13c's end-to-end test on mobile:
// the real TypeScript relay client against a real Go relay and daemon hub that
// requires a daemon password (internal/relay/bridge/devharness -password):
// password required, wrong, right, and not required, the rate limit, and the
// proof that a device that fails the password is never recorded as pending.
//
// The daemon side arrives with PR #360; against a tree without it the harness
// has no -password flag and this file skips itself.
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { createDaemonPasswords } from '../daemonPasswords';
import { ConnectOptions, RelayConnection } from '../RelayConnection';
import { DevHarness, harnessSupportsPassword, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

// A failed `go build` (or a missing `go`) throws here and fails the file: only a harness that really lacks the flag
// (a tree without the daemon side, PR #360) skips. SMIND_REQUIRE_PASSWORD_HARNESS=1 turns even that into a failure.
const supported = harnessSupportsPassword();
const required = process.env.SMIND_REQUIRE_PASSWORD_HARNESS === '1';

it.runIf(required && !supported)('the harness has -password (SMIND_REQUIRE_PASSWORD_HARNESS=1)', () => {
  throw new Error('devharness has no -password flag: the daemon side of P13c (PR #360) is missing from this tree');
});

const PASSWORD = 'correct horse battery staple';
const connections: RelayConnection[] = [];
let withPassword: DevHarness;
let withoutPassword: DevHarness;

function open(harness: DevHarness, seed: number, over: Partial<ConnectOptions> = {}): RelayConnection {
  const base = testConnectOptions(seed, over);
  // lockMinMs is lowered from the app's 5 s so the rate-limit wait fits a test; the daemon's own retry_after (2 s) still applies.
  const conn = RelayConnection.open(harness.pairingUrl, {
    ...base,
    reconnect: { ...base.reconnect, lockMinMs: 100 },
    passwords: createDaemonPasswords(),
    ...over,
  });
  connections.push(conn);
  return conn;
}

const registry = async (harness: DevHarness) => (await harness.devices()).map((d) => ({ label: d.label, state: d.state }));

describe(`daemon password (P13c) against a real relay and daemon hub`, () => {
  beforeAll(async () => {
    requireSelfSignedHarnessTrust();
    withPassword = await DevHarness.start(10, false, PASSWORD); // a pairing window, so a device that gets in is approved at once
    withoutPassword = await DevHarness.start(10);
  }, 120_000);

  afterAll(() => {
    for (const c of connections) c.close();
    withPassword?.stop();
    withoutPassword?.stop();
  });

  it('a daemon with a password asks for it; the device is NOT recorded while it has none', { timeout: 60_000 }, async () => {
    const conn = open(withPassword, 41);
    await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'required' }), { timeout: 20_000 });
    expect(await registry(withPassword)).toEqual([]); // not pending, not anything: it learned nothing and left no trace
  });

  it('a wrong password is refused ("incorrect"), still leaves no registry entry, and the right one connects', { timeout: 60_000 }, async () => {
    const conn = open(withPassword, 42);
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'), { timeout: 20_000 });
    conn.submitPassword('not the password');
    await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'incorrect' }), { timeout: 20_000 });
    expect((await registry(withPassword)).filter((d) => d.label === 'node test device 42')).toEqual([]);

    conn.submitPassword(PASSWORD);
    await conn.whenConnected();
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
    expect((await registry(withPassword)).filter((d) => d.label === 'node test device 42')).toEqual([{ label: 'node test device 42', state: 'approved' }]);
  });

  it('an approved device needs the password in every hello: a fresh session after a drop still sends it, no second prompt', { timeout: 60_000 }, async () => {
    // The end-to-end watchdog is shortened (defaults 15 s / 90 s): after the daemon restarts the phone's stream stays up.
    const conn = open(withPassword, 43, { liveness: { pendingStaleMs: 1_500, idleStaleMs: 3_000, checkMs: 250 } });
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'), { timeout: 20_000 });
    conn.submitPassword(PASSWORD);
    await conn.whenConnected();
    const epoch = conn.epoch;
    const states: string[] = [];
    conn.onState((s) => states.push(s.kind));
    await withPassword.mustCmd('daemon-restart');
    await vi.waitFor(() => expect(conn.epoch).toBeGreaterThan(epoch), { timeout: 60_000 });
    expect(states).not.toContain('password');
    await expect(conn.call('workspace.list')).resolves.toEqual([]);
  });

  it('a daemon without a password ignores one the person typed earlier: it still connects', { timeout: 60_000 }, async () => {
    const passwords = createDaemonPasswords();
    const probe = open(withoutPassword, 44, { passwords });
    await probe.whenConnected(); // not asked for anything
    expect(probe.state.kind).toBe('connected');
    const typed = open(withoutPassword, 45, { passwords });
    typed.submitPassword('typed for some other daemon'); // held for this daemon this run
    await typed.whenConnected();
    expect(typed.state.kind).toBe('connected');
  });

  it('rate limiting: after repeated wrong passwords the daemon says "too many attempts", the client keeps the correct password and connects after the wait', { timeout: 120_000 }, async () => {
    const conn = open(withPassword, 46);
    await vi.waitFor(() => expect(conn.state.kind).toBe('password'), { timeout: 20_000 });
    // 5 free wrong attempts for this device id; the 6th wrong one starts the delay (2 s, then 4 s ...).
    for (let i = 0; i < 6; i++) {
      conn.submitPassword(`wrong ${i}`);
      await vi.waitFor(() => expect(conn.state).toEqual({ kind: 'password', reason: 'incorrect' }), { timeout: 20_000 });
    }
    // The next hello is rate limited BEFORE the daemon looks at the password: it must say so and keep the (correct) one it was given.
    conn.submitPassword(PASSWORD);
    await vi.waitFor(() => expect(conn.state.kind).toBe('locked'), { timeout: 20_000 });
    if (conn.state.kind !== 'locked') throw new Error('unreachable');
    expect(conn.state.retryAfter).toBeGreaterThanOrEqual(1);
    await conn.whenConnected(); // after the wait, the same password, once: accepted
    expect(conn.state.kind).toBe('connected');
  });
});
