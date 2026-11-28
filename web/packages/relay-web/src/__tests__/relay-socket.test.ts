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
  /** Per-topic registrations made through subscribe(), in order. */
  subTopics: Array<{ topics: string[]; cb: (e: Ev) => void }> = [];
  droppedCbs: Array<(n: number) => void> = [];
  /** events.subscribe answers: the daemon's real RPC, answered here without being recorded in calls. */
  subscribeRpcs: unknown[] = [];
  rejectUnknown = new Set<string>();
  /** Runs inside the events.subscribe call, before its answer exists (the daemon pushing right after it subscribed). */
  onSubscribeRpc: (() => void) | null = null;
  stateCbs: Array<(s: { kind: string }) => void> = [];
  call(method: string, params?: unknown, opts?: { onEvent?: (e: string, p: unknown) => void }) {
    if (method === 'events.subscribe') {
      this.subscribeRpcs.push(params);
      this.onSubscribeRpc?.();
      const topics = (params as { topics: string[] }).topics;
      const bad = topics.find((t) => this.rejectUnknown.has(t));
      const q = (bad ? Promise.reject(new DaemonError(`unknown topic "${bad}"`)) : Promise.resolve({ topics })) as Promise<unknown> & { cancel(): void };
      q.cancel = () => undefined;
      return q;
    }
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
  subscribe(t: string[], cb: (e: Ev) => void) {
    this.subs.push(cb);
    this.subTopics.push({ topics: t, cb });
    return () => {
      this.subs = this.subs.filter((x) => x !== cb);
      this.subTopics = this.subTopics.filter((x) => x.cb !== cb);
    };
  }
  onDropped(cb: (n: number) => void) {
    this.droppedCbs.push(cb);
    return () => {
      this.droppedCbs = this.droppedCbs.filter((x) => x !== cb);
    };
  }
  /** What the daemon would push for topic: every local registration for it fires. */
  push(topic: string, seq: number, payload: unknown) {
    for (const r of [...this.subTopics]) if (r.topics.includes(topic)) r.cb({ topic, seq, payload });
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

  it('relay_socket_relays_the_real_events_subscribe_answer', async () => {
    const { conn, client } = setup();
    conn.rejectUnknown.add('future.topic');
    // What ui/src/hooks/use-daemon-events.ts's subscribeTolerant does (that module
    // imports React and the app's path aliases, so the loop is mirrored here).
    let rest = ['task.created', 'future.topic', 'run.status'];
    for (;;) {
      try {
        await client.call('events.subscribe', { topics: rest });
        break;
      } catch (err) {
        const unknown = /unknown topic "([^"]+)"/.exec((err as Error).message)?.[1];
        if (!unknown || !rest.includes(unknown)) throw err;
        rest = rest.filter((t) => t !== unknown);
      }
    }
    expect(rest).toEqual(['task.created', 'run.status']);
    expect(conn.subscribeRpcs.length).toBe(2); // the rejected one and the retry
    expect(conn.subTopics.map((r) => r.topics)).toEqual([['task.created'], ['run.status']]);

    const got: Array<{ topic: string; seq: number }> = [];
    client.onNotification((n) => got.push({ topic: n.topic, seq: n.seq }));
    conn.push('run.status', 4, {});
    conn.push('future.topic', 5, {});
    await tick();
    expect(got).toEqual([{ topic: 'run.status', seq: 4 }]);
  });

  it('relay_socket_events_subscribe_is_idempotent_per_topic', async () => {
    const { conn, client } = setup();
    await client.call('events.subscribe', { topics: ['t'] });
    await client.call('events.subscribe', { topics: ['t'] });
    await client.call('events.subscribe', { topics: ['t', 'u'] });
    expect(conn.subTopics.map((r) => r.topics)).toEqual([['t'], ['u']]);
    const got: unknown[] = [];
    client.onNotification((n) => got.push(n));
    conn.push('t', 1, { a: 1 });
    await tick();
    expect(got).toEqual([{ topic: 't', seq: 1, payload: { a: 1 } }]);
  });

  it('relay_socket_events_unsubscribe_releases_only_the_named_topics', async () => {
    const { conn, client } = setup();
    await client.call('events.subscribe', { topics: ['a', 'b', 'c'] });
    await client.call('events.unsubscribe', { topics: ['b'] });
    expect(conn.subTopics.map((r) => r.topics[0])).toEqual(['a', 'c']);
    const got: string[] = [];
    client.onNotification((n) => got.push(n.topic));
    conn.push('a', 1, null);
    conn.push('b', 2, null);
    conn.push('c', 3, null);
    await tick();
    expect(got).toEqual(['a', 'c']);
    // a released topic can be subscribed again
    await client.call('events.subscribe', { topics: ['b'] });
    expect(conn.subTopics.map((r) => r.topics[0])).toEqual(['a', 'c', 'b']);
  });

  it('relay_socket_forwards_event_dropped', async () => {
    const { conn, client, sock } = setup();
    const got: Array<{ topic: string; seq: number; payload: unknown }> = [];
    client.onNotification((n) => got.push(n));
    conn.droppedCbs[0](7);
    await tick();
    expect(got).toEqual([{ topic: 'event.dropped', seq: 0, payload: { count: 7 } }]);
    sock.close();
    expect(conn.droppedCbs.length).toBe(0);
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

  it('relay_socket_delivers_an_event_pushed_before_the_subscribe_answer', async () => {
    const { conn, client } = setup();
    const got: Array<{ topic: string; seq: number }> = [];
    client.onNotification((n) => got.push({ topic: n.topic, seq: n.seq }));
    conn.onSubscribeRpc = () => conn.push('t', 1, { first: true });
    await client.call('events.subscribe', { topics: ['t'] });
    await tick();
    expect(got).toEqual([{ topic: 't', seq: 1 }]);

    // and a rejected subscribe still leaves nothing registered
    conn.onSubscribeRpc = null;
    conn.rejectUnknown.add('bad');
    await expect(client.call('events.subscribe', { topics: ['u', 'bad'] })).rejects.toThrow(/unknown topic/);
    expect(conn.subTopics.map((r) => r.topics[0])).toEqual(['t']);
  });
});
