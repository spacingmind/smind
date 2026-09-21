// relayHarness.ts is the in-memory E2EE pipe (two handshaked Channels
// back-to-back) the RelayConnection unit tests and attachLifecycle.test.ts
// use -- shared here so Milestone 3's follow-up tests can script a
// realistic fake daemon (respond to RPCs, emit request-scoped events)
// against a real RelayConnection without any network.

import { Channel, generateKeyPair, RawTransport, Role } from './relay/e2ee';
import { RelayConnection } from './relay/RelayConnection';

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

export interface WireRequest {
  id?: string;
  method?: string;
  params?: unknown;
}

/**
 * A scripted daemon over a handshaked Channel: records every request,
 * can reply or error a pending call by id, and can emit request-scoped
 * events (run.attach's streamed chunks) or resolve a call outright.
 */
export class FakeDaemon {
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

  reply(id: string, result: unknown) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, result })));
  }

  fail(id: string, message: string) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ id, error: { message } })));
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

/** One admitted (fake) relay session: a RelayConnection paired to a FakeDaemon. */
export async function makeRelayHarness(): Promise<{ conn: RelayConnection; daemon: FakeDaemon }> {
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
