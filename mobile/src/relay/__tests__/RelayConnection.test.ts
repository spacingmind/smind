// RelayConnection.test.ts exercises RelayConnection's RPC layer over a
// real E2EE Channel pair (an in-memory pipe), with a fake wsapi peer on
// the daemon side speaking the exact wire shapes internal/wsapi/conn.go
// and events.go define: {id, method, params} in; {id, result} /
// {id, error} / {id, event, params} / {event: {topic, seq, payload}}
// back. This is the plan's Item 1 Test Scenario: id-matching under
// out-of-order responses, close-then-call, error isolation, and pushed
// event dispatch.

import { describe, expect, it, vi } from 'vitest';
import { Channel, generateKeyPair, RawTransport, Role } from '../e2ee';
import { CallOptions, RelayConnection, RelayEvent } from '../RelayConnection';

function pipe(): [RawTransport, RawTransport] {
  const queueA: Uint8Array[] = [];
  const queueB: Uint8Array[] = [];
  const waitersA: Array<(v: Uint8Array) => void> = [];
  const waitersB: Array<(v: Uint8Array) => void> = [];

  function makeSide(
    sendQueue: Uint8Array[],
    sendWaiters: Array<(v: Uint8Array) => void>,
    recvQueue: Uint8Array[],
    recvWaiters: Array<(v: Uint8Array) => void>,
  ): RawTransport {
    return {
      send(bytes: Uint8Array) {
        const waiter = sendWaiters.shift();
        if (waiter) waiter(bytes);
        else sendQueue.push(bytes);
      },
      receiveChunk(): Promise<Uint8Array> {
        const chunk = recvQueue.shift();
        if (chunk) return Promise.resolve(chunk);
        return new Promise((resolve) => recvWaiters.push(resolve));
      },
    };
  }

  return [makeSide(queueA, waitersA, queueB, waitersB), makeSide(queueB, waitersB, queueA, waitersA)];
}

interface WireRequest {
  id?: string;
  method?: string;
  params?: unknown;
}

/** Minimal fake wsapi daemon peer: handshake, then echo/dispatch per request. */
class FakeDaemon {
  readonly channel: Channel;
  private requests: WireRequest[] = [];

  constructor(transport: RawTransport, daemonKeyPair = generateKeyPair()) {
    this.channel = new Channel(transport, daemonKeyPair, Role.Daemon);
  }

  async start() {
    await this.channel.handshake();
    void this.loop();
  }

  /** The requests the peer has received, in arrival order. */
  received(): readonly WireRequest[] {
    return this.requests;
  }

  /** Resolves once n requests have arrived (for sequencing tests). */
  async waitForRequests(n: number): Promise<WireRequest[]> {
    while (this.requests.length < n) {
      await new Promise((r) => setTimeout(r, 5));
    }
    return this.requests;
  }

  respond(id: string, result: unknown) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, result })));
  }

  respondError(id: string, message: string) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, error: { message } })));
  }

  emitRequestEvent(id: string, event: string, params: unknown) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, event, params })));
  }

  pushNotification(topic: string, payload: unknown, seq = 1) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ event: { topic, seq, payload } })));
  }

  end() {
    // There's no transport-level close in the pipe; the peer just stops.
  }

  private async loop() {
    const decoder = new TextDecoder();
    for (;;) {
      const bytes = await this.channel.receive();
      this.requests.push(JSON.parse(decoder.decode(bytes)));
    }
  }
}

/** A connected RelayConnection<->FakeDaemon pair. */
async function makePair(): Promise<{ conn: RelayConnection; daemon: FakeDaemon }> {
  const [daemonTransport, mobileTransport] = pipe();
  const daemonKeyPair = generateKeyPair();
  const daemon = new FakeDaemon(daemonTransport, daemonKeyPair);
  // Not awaited to completion on its own: the daemon's handshake can only
  // finish once the mobile side (created below) participates, so start it
  // concurrently like Channel's own tests do.
  const daemonReady = daemon.start();

  const mobileKeyPair = generateKeyPair();
  const mobileChannel = new Channel(mobileTransport, mobileKeyPair, Role.Mobile);
  await mobileChannel.handshake(daemonKeyPair.publicKey);
  await daemonReady;

  const conn = new RelayConnection(
    { daemonId: '', publicKey: daemonKeyPair.publicKey, relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' },
    { close: () => {} },
    mobileChannel,
  );
  return { conn, daemon };
}

describe('RelayConnection.call', () => {
  it('matches responses to calls by id, including back-to-back calls answered out of order', async () => {
    const { conn, daemon } = await makePair();
    const p1 = conn.call('a.first');
    const p2 = conn.call('a.second');
    const p3 = conn.call('a.third');

    const reqs = await daemon.waitForRequests(3);
    expect(reqs.map((r) => r.method)).toEqual(['a.first', 'a.second', 'a.third']);
    // Answer in reverse order: id-matching must route each to its own call.
    daemon.respond(reqs[2].id!, 'third-result');
    daemon.respond(reqs[0].id!, 'first-result');
    await expect(p1).resolves.toBe('first-result');
    await expect(p3).resolves.toBe('third-result');
    daemon.respond(reqs[1].id!, 'second-result');
    await expect(p2).resolves.toBe('second-result');
  });

  it('serializes params onto the wire', async () => {
    const { conn, daemon } = await makePair();
    const p = conn.call('task.list', { workspaceId: 7 });
    const [req] = await daemon.waitForRequests(1);
    expect(req.params).toEqual({ workspaceId: 7 });
    daemon.respond(req.id!, [{ ID: 1 }]);
    await expect(p).resolves.toEqual([{ ID: 1 }]);
  });

  it('rejects only the errored call, leaving other in-flight calls intact', async () => {
    const { conn, daemon } = await makePair();
    const ok = conn.call('a.ok');
    const bad = conn.call('a.bad');
    const reqs = await daemon.waitForRequests(2);
    daemon.respondError(reqs[1].id!, 'boom: method not found');
    daemon.respond(reqs[0].id!, 'fine');
    await expect(ok).resolves.toBe('fine');
    await expect(bad).rejects.toThrow('boom: method not found');
  });

  it('routes request-scoped {id, event, params} messages to onEvent before the terminal result', async () => {
    const { conn, daemon } = await makePair();
    const seen: Array<[string, unknown]> = [];
    const opts: CallOptions = { onEvent: (e, p) => seen.push([e, p]) };
    const p = conn.call('run.attach', { runId: 'r1' }, opts);
    const [req] = await daemon.waitForRequests(1);
    daemon.emitRequestEvent(req.id!, 'chunk', { text: 'hello ' });
    daemon.emitRequestEvent(req.id!, 'chunk', { text: 'world' });
    daemon.respond(req.id!, { runId: 'r1', stopReason: 'end_turn' });
    await expect(p).resolves.toEqual({ runId: 'r1', stopReason: 'end_turn' });
    expect(seen).toEqual([
      ['chunk', { text: 'hello ' }],
      ['chunk', { text: 'world' }],
    ]);
  });
});

describe('RelayConnection.subscribe', () => {
  it('subscribes via events.subscribe, dispatches pushed notifications by topic, and unsubscribes', async () => {
    const { conn, daemon } = await makePair();
    const events: RelayEvent[] = [];
    const unsubscribe = conn.subscribe(['task.status'], (ev) => events.push(ev));

    const reqs = await daemon.waitForRequests(1);
    expect(reqs[0].method).toBe('events.subscribe');
    expect(reqs[0].params).toEqual({ topics: ['task.status'] });
    daemon.respond(reqs[0].id!, { topics: ['task.status'] });

    daemon.pushNotification('task.status', { taskId: 3, status: 'running' }, 1);
    await vi.waitFor(() => expect(events).toEqual([{ topic: 'task.status', seq: 1, payload: { taskId: 3, status: 'running' } }]));

    // A topic with no registered callback is dropped.
    daemon.pushNotification('run.status', { runId: 'x', status: 'done' }, 2);
    unsubscribe();

    const unsubReqs = await daemon.waitForRequests(2);
    expect(unsubReqs[1].method).toBe('events.unsubscribe');
    expect(unsubReqs[1].params).toEqual({ topics: ['task.status'] });
    daemon.respond(unsubReqs[1].id!, { topics: [] });

    daemon.pushNotification('task.status', { taskId: 3, status: 'done' }, 3);
    await new Promise((r) => setTimeout(r, 25));
    expect(events).toHaveLength(1); // no callback fired after unsubscribe
  });

  it('coalesces overlapping subscriptions and only unsubscribes topics when the last callback goes', async () => {
    const { conn, daemon } = await makePair();
    const seen1: string[] = [];
    const seen2: string[] = [];
    const unsub1 = conn.subscribe(['task.updated'], () => seen1.push('a'));
    const [sub1] = await daemon.waitForRequests(1);
    daemon.respond(sub1.id!, { topics: ['task.updated'] });

    const unsub2 = conn.subscribe(['task.updated'], () => seen2.push('b'));
    await new Promise((r) => setTimeout(r, 25));
    // Coalesced: the topic is already subscribed server-side, so no second
    // events.subscribe RPC goes out.
    expect(daemon.received()).toHaveLength(1);

    daemon.pushNotification('task.updated', { task: { ID: 1 } }, 1);
    await vi.waitFor(() => {
      expect(seen1).toEqual(['a']);
      expect(seen2).toEqual(['b']);
    });

    unsub1();
    // task.updated still has subscriber 2: no events.unsubscribe fired,
    // and the push still reaches the remaining callback only.
    daemon.pushNotification('task.updated', { task: { ID: 2 } }, 2);
    await vi.waitFor(() => expect(seen2).toEqual(['b', 'b']));
    expect(seen1).toEqual(['a']); // the unsubscribed callback no longer fires
    await new Promise((r) => setTimeout(r, 25));
    expect(daemon.received().every((r) => r.method === 'events.subscribe')).toBe(true);

    unsub2();
    const reqs = await daemon.waitForRequests(2);
    expect(reqs[1].method).toBe('events.unsubscribe');
    expect(reqs[1].params).toEqual({ topics: ['task.updated'] });
  });
});

describe('RelayConnection.close', () => {
  it('rejects calls issued after close with a clear error', async () => {
    const { conn } = await makePair();
    conn.close();
    await expect(conn.call('workspace.list')).rejects.toThrow('relay: connection is closed');
  });

  it('rejects in-flight calls when closed', async () => {
    const { conn, daemon } = await makePair();
    const p = conn.call('slow');
    await daemon.waitForRequests(1);
    conn.close();
    await expect(p).rejects.toThrow('relay: connection is closed');
  });

  it('unsubscribe after close is a no-op (no subscribe RPC fired)', async () => {
    const { conn, daemon } = await makePair();
    conn.close();
    const unsubscribe = conn.subscribe(['task.status'], () => {});
    unsubscribe();
    await new Promise((r) => setTimeout(r, 10));
    expect(daemon.received()).toHaveLength(0);
  });
});
