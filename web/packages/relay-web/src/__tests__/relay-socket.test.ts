import { describe, expect, it } from 'vitest';
import {
  CallAbortedError,
  ConnectionClosedError,
  RpcError,
  WsClient,
} from '../../../ui/src/lib/ws-client';
import { DaemonError } from '@smind/relay-client/src/RelayConnection';
import { RelaySocket, type RelayConnectionLike } from '../relay-socket';

type Ev = { topic: string; seq?: number; payload: unknown };

class FakeConn implements RelayConnectionLike {
  state = { kind: 'connected' };
  calls: Array<{
    method: string;
    params: unknown;
    onEvent?: (e: string, p: unknown) => void;
    resolve(v: unknown): void;
    reject(e: unknown): void;
    cancelled: boolean;
  }> = [];
  subs: Array<(e: Ev) => void> = [];
  stateCbs: Array<(s: { kind: string }) => void> = [];
  call(method: string, params?: unknown, opts?: { onEvent?: (e: string, p: unknown) => void }) {
    let resolve!: (v: unknown) => void;
    let reject!: (e: unknown) => void;
    const p = new Promise<unknown>((a, b) => {
      resolve = a;
      reject = b;
    }) as Promise<unknown> & { cancel(): void };
    const rec = { method, params, onEvent: opts?.onEvent, resolve, reject, cancelled: false };
    p.cancel = () => {
      rec.cancelled = true;
    };
    this.calls.push(rec);
    return p;
  }
  subscribe(_t: string[], cb: (e: Ev) => void) {
    this.subs.push(cb);
    return () => {
      this.subs = this.subs.filter((x) => x !== cb);
    };
  }
  onState(cb: (s: { kind: string }) => void) {
    this.stateCbs.push(cb);
    return () => {
      this.stateCbs = this.stateCbs.filter((x) => x !== cb);
    };
  }
  setState(kind: string) {
    this.state = { kind };
    for (const cb of [...this.stateCbs]) cb(this.state);
  }
}

const tick = () => new Promise((r) => setTimeout(r, 0));

function setup() {
  const conn = new FakeConn();
  const sock = new RelaySocket(conn);
  const client = new WsClient(sock);
  return { conn, sock, client };
}

describe('RelaySocket', () => {
  it('relay_socket_is_a_socketlike_for_wsclient', async () => {
    const { conn, client } = setup();

    const p = client.call('task.list', {});
    await tick();
    expect(conn.calls[0].method).toBe('task.list');
    conn.calls[0].resolve({ tasks: [1] });
    expect(await p).toEqual({ tasks: [1] });

    const events: Array<[string, unknown]> = [];
    const sp = client.callStream('task.run', {}, (e, x) => events.push([e, x]));
    await tick();
    conn.calls[1].onEvent!('chunk', { n: 1 });
    conn.calls[1].onEvent!('chunk', { n: 2 });
    await tick();
    expect(events).toEqual([
      ['chunk', { n: 1 }],
      ['chunk', { n: 2 }],
    ]);
    conn.calls[1].resolve('done');
    expect(await sp).toBe('done');

    const ep = client.call('x.fail');
    await tick();
    conn.calls[2].reject(new DaemonError('nope', 'conflict', { k: 1 }));
    const err = (await ep.catch((e) => e)) as RpcError;
    expect(err).toBeInstanceOf(RpcError);
    expect(err.code).toBe('conflict');
    expect(err.data).toEqual({ k: 1 });

    const got: unknown[] = [];
    client.onNotification((n) => got.push(n));
    await client.call('events.subscribe', { topics: ['t'] });
    conn.subs[0]({ topic: 't', seq: 3, payload: { a: 1 } });
    await tick();
    expect(got).toEqual([{ topic: 't', seq: 3, payload: { a: 1 } }]);
    await client.call('events.unsubscribe', { topics: ['t'] });
    expect(conn.subs.length).toBe(0);
  });

  it('abort_cancels_the_in_flight_call', async () => {
    const { conn, client } = setup();
    const ac = new AbortController();
    const p = client.call('slow', {}, { signal: ac.signal });
    await tick();
    ac.abort();
    await expect(p).rejects.toBeInstanceOf(CallAbortedError);
    expect(conn.calls[0].cancelled).toBe(true);
  });

  it('reconnecting_closes_the_socket_once', async () => {
    const { conn, client } = setup();
    let closes = 0;
    client.onClose(() => closes++);
    const p = client.call('slow', {});
    await tick();
    conn.setState('reconnecting');
    conn.setState('connected');
    conn.setState('reconnecting');
    // WsClient fails an in-flight call with its connection-closed message
    // (an RpcError); a call made after the close is a ConnectionClosedError.
    await expect(p).rejects.toThrow(/connection closed/);
    await expect(client.call('late')).rejects.toBeInstanceOf(ConnectionClosedError);
    expect(closes).toBe(1);
    expect(conn.stateCbs.length).toBe(0);
  });

  it('send_after_close_throws_and_not_connected_closes_early', async () => {
    const { sock } = setup();
    sock.close();
    expect(() => sock.send('{}')).toThrow();

    const conn = new FakeConn();
    conn.state = { kind: 'connecting' };
    const s2 = new RelaySocket(conn);
    let closed = false;
    s2.addEventListener('close', () => (closed = true));
    expect(closed).toBe(false);
    await tick();
    expect(closed).toBe(true);
  });
});
