import { afterEach, describe, expect, it, vi } from 'vitest';
import { IDBFactory } from 'fake-indexeddb';
import { base64UrlEncode } from '@smind/relay-client/src/base64';
import { deviceIdFor } from '@smind/relay-client/src/deviceIdentity';
import { RelayConnection } from '@smind/relay-client/src/RelayConnection';
import { FakeRelay } from '@smind/relay-client/src/__tests__/fakeRelay';
import { WsClient } from '../../../ui/src/lib/ws-client';
import { RelayController, RelayStateError, type ControllerState } from '../controller';
import { NotPairedError, RepairRequiredError } from '../identity-manager';
import { openRelayStore } from '../store';

const SECRET = new Uint8Array(32).map((_, i) => 0xa0 + (i % 16));

function offerFor(relay: FakeRelay) {
  const payload = base64UrlEncode(
    new TextEncoder().encode(
      JSON.stringify({
        v: 1,
        id: 'daemon-1',
        pk: base64UrlEncode(relay.daemonKeys.publicKey),
        relay: 'https://relay.test',
        sec: base64UrlEncode(SECRET),
        ws: 'host-1',
      }),
    ),
  );
  return { url: 'https://app.test/#offer=' + payload, payload };
}

const hex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
const b64 = (b: Uint8Array) => Buffer.from(b).toString('base64');

const timings = {
  jitter: (ms: number) => ms,
  keepalive: { intervalMs: 40, deadAfterMs: 400, probeTimeoutMs: 150 },
  reconnect: { baseMs: 5, maxMs: 20, pendingPollMs: 20, pendingPollMaxMs: 40, minStableMs: 2000, resumeProbeMs: 600, handshakeMs: 1500 },
};

const controllers: RelayController[] = [];
afterEach(() => {
  for (const c of controllers.splice(0)) c.close();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('controller', () => {
  it('no_secret_reaches_console_or_storage', async () => {
    const written: string[] = [];
    const consoleCalls: string[] = [];
    for (const m of ['log', 'info', 'warn', 'error', 'debug', 'trace'] as const) {
      vi.spyOn(console, m).mockImplementation((...a: unknown[]) => {
        consoleCalls.push(a.map((x) => (x instanceof Uint8Array ? hex(x) : String(x))).join(' '));
      });
    }
    const fakeStorage = () => ({
      getItem: (k: string) => (written.push('get:' + k), null),
      setItem: (k: string, v: string) => void written.push(`set:${k}=${v}`),
      removeItem: () => undefined,
    });
    vi.stubGlobal('localStorage', fakeStorage());
    vi.stubGlobal('sessionStorage', fakeStorage());
    vi.stubGlobal('document', {
      get cookie() {
        return '';
      },
      set cookie(v: string) {
        written.push('cookie=' + v);
      },
    });

    const relay = new FakeRelay();
    const { url, payload } = offerFor(relay);
    const store = await openRelayStore(new IDBFactory());
    const c = new RelayController({ store, net: relay, timings });
    controllers.push(c);
    const seen: ControllerState[] = [];
    c.onState((s) => seen.push(s));

    await c.pair(url, 'laptop');
    await c.whenConnected();
    const sock = await c.socket();
    const client = new WsClient(sock);
    expect(await client.call('workspace.list')).toEqual([]);

    relay.dropCurrentStream();
    await vi.waitFor(() => expect(seen.some((s) => s.kind === 'reconnecting')).toBe(true), { timeout: 3000 });
    await c.whenConnected();
    const sock2 = await c.socket();
    expect(await new WsClient(sock2).call('workspace.list')).toEqual([]);
    c.close();

    const pub = (await store.loadIdentity())!.publicKey;
    const forbidden = [
      base64UrlEncode(SECRET),
      hex(SECRET),
      b64(SECRET),
      payload,
      url,
      base64UrlEncode(pub),
      hex(pub),
      b64(pub),
    ];
    const haystack = [...consoleCalls, ...written].join('\n');
    for (const f of forbidden) expect(haystack).not.toContain(f);

    // Typed errors carry no secret either.
    const errs: Error[] = [new NotPairedError(), new RepairRequiredError(), new RelayStateError({ kind: 'revoked' })];
    for (const e of errs) for (const f of forbidden) expect(e.message).not.toContain(f);
  });

  it('resume_states_and_socket_rejection', async () => {
    const relay = new FakeRelay();
    const store = await openRelayStore(new IDBFactory());
    const c = new RelayController({ store, net: relay, timings });
    controllers.push(c);
    await expect(c.resume()).rejects.toBeInstanceOf(NotPairedError);
    expect(c.state.kind).toBe('unpaired');

    await store.savePairing({ offerUrl: offerFor(relay).url, daemonId: 'd', relay: 'r', relayFingerprint: '', label: 'l', pairedAt: 1 });
    await expect(c.resume()).rejects.toBeInstanceOf(RepairRequiredError);
    expect(c.state.kind).toBe('needs-repair');
    await expect(c.socket()).rejects.toBeInstanceOf(RelayStateError);

    relay.policy = () => 'revoked';
    await c.pair(offerFor(relay).url, 'laptop');
    const err = await c.socket().catch((e) => e);
    expect(err).toBeInstanceOf(RelayStateError);
    expect(err.state.kind).toBe('revoked');
    expect(c.state.kind).toBe('revoked');

    await c.unpair();
    expect(c.state.kind).toBe('unpaired');
    expect(await store.loadPairing()).toBeNull();
    expect(await store.loadIdentity()).toBeNull();
  });

  it('pair_failure_keeps_the_previous_identity_and_pairing', async () => {
    const relay = new FakeRelay();
    const real = await openRelayStore(new IDBFactory());
    let failSave = false;
    const store = {
      ...real,
      savePaired: (i: Parameters<typeof real.savePaired>[0], p: Parameters<typeof real.savePaired>[1]) =>
        failSave ? Promise.reject(new Error('disk full')) : real.savePaired(i, p),
    };
    const c = new RelayController({ store, net: relay, timings });
    controllers.push(c);
    await c.pair(offerFor(relay).url, 'A');
    await c.whenConnected();
    const idA = (await real.loadIdentity())!;
    const pairA = (await real.loadPairing())!;

    failSave = true;
    await expect(c.pair(offerFor(relay).url, 'B')).rejects.toThrow('disk full');
    const idNow = (await real.loadIdentity())!;
    expect(Array.from(idNow.publicKey)).toEqual(Array.from(idA.publicKey));
    expect((await real.loadPairing())!.label).toBe(pairA.label);
    expect(c.state.kind).toBe('connected');
    expect(await new WsClient(await c.socket()).call('workspace.list')).toEqual([]);
  });

  function recorder() {
    const opened: Array<{ conn: RelayConnection; closed: boolean; deviceId: string }> = [];
    const open: typeof RelayConnection.open = (url, opts) => {
      const conn = RelayConnection.open(url, opts);
      const rec = { conn, closed: false, deviceId: opts.identity.deviceId };
      const orig = conn.close.bind(conn);
      conn.close = () => {
        rec.closed = true;
        orig();
      };
      opened.push(rec);
      return conn;
    };
    return { opened, open };
  }

  it('concurrent_pair_calls_leave_one_live_connection', async () => {
    const relay = new FakeRelay();
    const real = await openRelayStore(new IDBFactory());
    const { opened, open } = recorder();
    const c = new RelayController({ store: real, net: relay, timings, open });
    controllers.push(c);
    await Promise.all([c.pair(offerFor(relay).url, 'first'), c.pair(offerFor(relay).url, 'second')]);
    await c.whenConnected();
    expect(opened.length).toBe(2);
    expect(opened.filter((o) => !o.closed).length).toBe(1);
    expect(opened[0].closed).toBe(true);
    expect((await real.loadPairing())!.label).toBe('second');
    expect(deviceIdFor((await real.loadIdentity())!.publicKey)).toBe(opened[1].deviceId);
    expect(await new WsClient(await c.socket()).call('workspace.list')).toEqual([]);
  });

  it('resume_during_pair_waits_for_the_pair', async () => {
    const relay = new FakeRelay();
    const real = await openRelayStore(new IDBFactory());
    const { opened, open } = recorder();
    const c = new RelayController({ store: real, net: relay, timings, open });
    controllers.push(c);
    const seen: string[] = [];
    c.onState((s) => seen.push(s.kind));
    const p = c.pair(offerFor(relay).url, 'p');
    const r = c.resume(); // starts in the same tick: must not see 'unpaired' nor NotPairedError
    expect(c.state.kind).toBe('connecting');
    await Promise.all([p, r]);
    await c.whenConnected();
    expect(seen).not.toContain('unpaired');
    expect(opened.filter((o) => !o.closed).length).toBe(1);
  });

  it('a_throwing_state_listener_does_not_break_the_others', async () => {
    const relay = new FakeRelay();
    const real = await openRelayStore(new IDBFactory());
    const c = new RelayController({ store: real, net: relay, timings });
    controllers.push(c);
    const seen: string[] = [];
    c.onState(() => {
      throw new Error('boom');
    });
    c.onState((s) => seen.push(s.kind));
    await c.pair(offerFor(relay).url, 'l');
    await c.whenConnected();
    expect(seen).toContain('connected');
    expect(await new WsClient(await c.socket()).call('workspace.list')).toEqual([]);
  });

  it('an_unexpected_connection_close_is_reported_as_failed', async () => {
    const relay = new FakeRelay();
    const real = await openRelayStore(new IDBFactory());
    const { opened, open } = recorder();
    const c = new RelayController({ store: real, net: relay, timings, open });
    controllers.push(c);
    await c.pair(offerFor(relay).url, 'l');
    await c.whenConnected();
    opened[0].conn.close(); // not the controller's doing
    expect(c.state).toEqual({ kind: 'failed', message: 'the relay connection closed' });
  });
});
