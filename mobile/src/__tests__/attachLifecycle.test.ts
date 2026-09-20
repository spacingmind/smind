// attachLifecycle.test.ts covers Item 3's detach Test Scenario at the
// connection layer, over the same in-memory E2EE pipe the RelayConnection
// unit tests use: cancelling a run.attach-style call sends the daemon's
// task.cancel wire shape, rejects only that call, leaves the connection
// fully usable for further calls, and delivers no further interim events
// to the cancelled call's onEvent (no leaked handler).

import { describe, expect, it } from 'vitest';
import { Channel, generateKeyPair, RawTransport, Role } from '../relay/e2ee';
import { RelayConnection } from '../relay/RelayConnection';

function pipe(): [RawTransport, RawTransport] {
  const queueA: Uint8Array[] = [];
  const queueB: Uint8Array[] = [];
  const waitersA: Array<(v: Uint8Array) => void> = [];
  const waitersB: Array<(v: Uint8Array) => void> = [];
  function makeSide(
    sendQueue: Uint8Array[], sendWaiters: Array<(v: Uint8Array) => void>,
    recvQueue: Uint8Array[], recvWaiters: Array<(v: Uint8Array) => void>,
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

class FakeDaemon {
  readonly channel: Channel;
  private requests: WireRequest[] = [];

  constructor(transport: RawTransport, kp = generateKeyPair()) {
    this.channel = new Channel(transport, kp, Role.Daemon);
  }

  async start() {
    await this.channel.handshake();
    void this.loop();
  }

  received(): readonly WireRequest[] {
    return this.requests;
  }

  async waitForRequests(n: number): Promise<WireRequest[]> {
    while (this.requests.length < n) await new Promise((r) => setTimeout(r, 5));
    return this.requests;
  }

  emitRequestEvent(id: string, event: string, params: unknown) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, event, params })));
  }

  private async loop() {
    const decoder = new TextDecoder();
    for (;;) {
      const bytes = await this.channel.receive();
      this.requests.push(JSON.parse(decoder.decode(bytes)));
    }
  }
}

async function makePair() {
  const [daemonTransport, mobileTransport] = pipe();
  const daemonKeyPair = generateKeyPair();
  const daemon = new FakeDaemon(daemonTransport, daemonKeyPair);
  const daemonReady = daemon.start();
  const mobileChannel = new Channel(mobileTransport, generateKeyPair(), Role.Mobile);
  await mobileChannel.handshake(daemonKeyPair.publicKey);
  await daemonReady;
  const conn = new RelayConnection(
    { daemonId: '', publicKey: daemonKeyPair.publicKey, relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' },
    { close: () => {} },
    mobileChannel,
  );
  return { conn, daemon };
}

describe('call cancellation (run.attach detach)', () => {
  it('cancel() sends task.cancel on the wire, rejects only that call, and stops event delivery to its onEvent', async () => {
    const { conn, daemon } = await makePair();
    const seen: string[] = [];
    const attach = conn.call('run.attach', { runId: 'r1' }, { onEvent: (e) => seen.push(e) });
    const [attachReq] = await daemon.waitForRequests(1);

    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'streaming...' });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual(['chunk']);

    attach.cancel();
    // Attach the rejection expectation before anything else so the
    // cancel-triggered rejection is always handled.
    const rejected = expect(attach).rejects.toThrow('relay: call cancelled');

    const cancelReq = (await daemon.waitForRequests(2))[1];
    expect(cancelReq.method).toBe('task.cancel');
    expect(cancelReq.id).toBeUndefined(); // no envelope id, per conn.go's handleCancel
    expect((cancelReq.params as { id: string }).id).toBe(attachReq.id);
    await rejected;

    // A late interim event for the cancelled id has no handler anymore.
    daemon.emitRequestEvent(attachReq.id!, 'chunk', { text: 'too late' });
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual(['chunk']); // unchanged -- no leaked handler

    // The connection itself is still perfectly usable.
    const followUp = conn.call('workspace.list');
    const followReq = (await daemon.waitForRequests(3))[2];
    daemon.channel.send(new TextEncoder().encode(JSON.stringify({ id: followReq.id, result: [] })));
    await expect(followUp).resolves.toEqual([]);
  });

  it('cancel() after the call resolved is a no-op', async () => {
    const { conn, daemon } = await makePair();
    const p = conn.call('workspace.list');
    const [req] = await daemon.waitForRequests(1);
    daemon.channel.send(new TextEncoder().encode(JSON.stringify({ id: req.id, result: [1, 2] })));
    await expect(p).resolves.toEqual([1, 2]);
    expect(() => p.cancel()).not.toThrow();
    await new Promise((r) => setTimeout(r, 10));
    expect(daemon.received()).toHaveLength(1); // no spurious task.cancel
  });
});
