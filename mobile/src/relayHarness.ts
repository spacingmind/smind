// relayHarness.ts is the in-memory E2EE pipe (the client Channel and the test-only v2
// responder, handshaked back-to-back) the RelayConnection unit tests and attachLifecycle.test.ts
// use -- shared here so Milestone 3's follow-up tests can script a
// realistic fake daemon (respond to RPCs, emit request-scoped events)
// against a real RelayConnection without any network.

import { Channel } from './relay/e2ee';
import { daemonKeysFrom, FakeV2Daemon, memPipe } from './relay/__tests__/fakeV2Daemon';
import { RelayConnection } from './relay/RelayConnection';

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
  readonly channel: FakeV2Daemon;
  private requests: WireRequest[] = [];

  constructor(channel: FakeV2Daemon) {
    this.channel = channel;
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

  /** A pushed eventNotification (no request id) -- e.g. permission.pending, delivered to every subscriber of the topic. */
  pushNotification(topic: string, payload: unknown, seq = 1) {
    this.channel.send(new TextEncoder().encode(JSON.stringify({ event: { topic, seq, payload } })));
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
  const [daemonTransport, mobileTransport] = memPipe();
  const daemonKeyPair = daemonKeysFrom();
  const daemon = new FakeDaemon(new FakeV2Daemon(daemonTransport, daemonKeyPair));
  const daemonReady = daemon.start();
  const mobileChannel = new Channel(mobileTransport);
  await mobileChannel.handshake(daemonKeyPair.publicKey);
  await daemonReady;
  const conn = new RelayConnection(
    { daemonId: '', publicKey: daemonKeyPair.publicKey, relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' },
    { close: () => {} },
    mobileChannel,
  );
  return { conn, daemon };
}
