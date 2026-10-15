// fakeRelay.ts is an in-memory stand-in for a relay plus the daemon behind it,
// at the OpenData-stream level (relay.v1.Frame messages), so RelayConnection's
// device identity, keepalive, resume and fresh-session logic can be tested
// deterministically without a socket. It is deliberately a *relay model*, not
// the real one: the real one is exercised against the Go relay and bridge by
// integration.devices.node.test.ts.
//
// What it models of the relay: a route per (device id, session id) derived
// from a stream's first frame; keepalive frames echoed (or swallowed, to play
// a relay that predates keepalive or a dead path); frames buffered for a
// route that has no stream; a stream killed with an error (a network drop, or
// a relay refusal as a gRPC status).
// What it models of the daemon: the E2EE responder per route, the
// relay.device_hello check (signature verified with the real helloSignedBytes),
// the approved/pending/revoked decision, and a tiny request handler.

import { ed25519 } from '@noble/curves/ed25519.js';
import { base64Decode } from '../base64';
import { DeviceHello, deviceIdFor, helloSignedBytes } from '../deviceIdentity';
import { Channel, generateKeyPair, RawTransport, Role } from '../e2ee';
import { DataStream } from '../grpcweb';
import { Frame, decodeFrame, encodeFrame, Direction } from '../proto';
import { RelayNet } from '../RelayConnection';

export type DevicePolicy = 'approved' | 'pending' | 'revoked' | 'legacy-daemon' | 'silent' | 'too_many' | 'bad_hello';

export class FakeStream implements DataStream {
  routeKey: string | null = null;
  readonly sent: Frame[] = [];
  private inbox: Uint8Array[] = [];
  private waiter: (() => void) | null = null;
  private error: Error | null = null;

  constructor(
    private readonly relay: FakeRelay,
    readonly headers: Record<string, string>,
  ) {}

  async ready(): Promise<void> {}

  send(bytes: Uint8Array): void {
    if (this.error) throw this.error;
    const frame = decodeFrame(bytes);
    this.sent.push(frame);
    this.relay.fromDevice(this, frame);
  }

  async receiveMessage<Msg>(decode: (b: Uint8Array) => Msg): Promise<Msg> {
    for (;;) {
      const next = this.inbox.shift();
      if (next) return decode(next);
      if (this.error) throw this.error;
      await new Promise<void>((resolve) => (this.waiter = resolve));
    }
  }

  push(frame: Frame): void {
    this.inbox.push(encodeFrame(frame));
    this.wakeReader();
  }

  /** Ends the stream with err (a drop, or a relay refusal such as a GrpcStatusError). */
  kill(err: Error): void {
    if (!this.error) this.error = err;
    this.wakeReader();
  }

  close(): void {
    this.kill(new Error('grpc-web websocket: closed'));
  }

  get closed(): boolean {
    return this.error !== null;
  }

  private wakeReader(): void {
    const w = this.waiter;
    this.waiter = null;
    w?.();
  }
}

/** The daemon side of one route: an E2EE responder whose bytes ride the relay. */
class DaemonRoute {
  private chunks: Uint8Array[] = [];
  private waiter: ((c: Uint8Array) => void) | null = null;
  readonly channel: Channel;
  helloSeen = 0;
  deviceId = '';
  alive = true;

  constructor(
    private readonly relay: FakeRelay,
    private readonly key: string,
  ) {
    const transport: RawTransport = {
      send: (bytes) => relay.toDevice(key, bytes),
      receiveChunk: () => {
        const c = this.chunks.shift();
        if (c) return Promise.resolve(c);
        return new Promise((resolve) => (this.waiter = resolve));
      },
    };
    this.channel = new Channel(transport, relay.daemonKeys, Role.Daemon);
  }

  feed(payload: Uint8Array): void {
    if (!this.alive) return;
    const w = this.waiter;
    if (w) {
      this.waiter = null;
      w(payload);
    } else this.chunks.push(payload);
  }

  async serve(): Promise<void> {
    const decoder = new TextDecoder();
    const reply = (id: string, body: object) => this.channel.send(new TextEncoder().encode(JSON.stringify({ id, ...body })));
    try {
      await this.channel.handshake();
      for (;;) {
        const msg = JSON.parse(decoder.decode(await this.channel.receive()));
        if (msg.method === 'relay.device_hello') {
          this.helloSeen++;
          this.relay.hellos.push(msg.params as DeviceHello);
          if (this.relay.gate) await this.relay.gate;
          if (!this.relay.checkHello(this, msg.id, msg.params as DeviceHello, reply)) return;
          continue;
        }
        this.relay.requests.push({ route: this.key, method: msg.method, params: msg.params });
        if (msg.method === 'workspace.list') reply(msg.id, { result: [] });
        else if (msg.method === 'events.subscribe') reply(msg.id, { result: { topics: msg.params?.topics ?? [] } });
        else if (msg.method === 'hang') {
          // never answered: a request that is in flight when the stream dies
        } else if (msg.id) reply(msg.id, { result: { echo: msg.method } });
      }
    } catch {
      // the route ended
    }
  }
}

export class FakeRelay implements RelayNet {
  readonly daemonKeys = generateKeyPair();
  readonly streams: FakeStream[] = [];
  readonly hellos: DeviceHello[] = [];
  readonly requests: Array<{ route: string; method: string; params: unknown }> = [];
  /** Echo keepalive probes (false = a relay that predates keepalive, or a dead path). */
  echoKeepalive = true;
  /** Streams whose frames are swallowed (a half-open path): nothing is echoed or forwarded. */
  readonly blackholed = new Set<FakeStream>();
  /** What the daemon decides about a device at hello. */
  policy: (deviceId: string) => DevicePolicy = () => 'approved';
  admissions = 0;
  /** When set, the daemon holds every hello answer until it resolves (a slow daemon). */
  gate: Promise<void> | null = null;
  /** When set, admit() fails (the relay is unreachable). */
  admitError: Error | null = null;

  private daemonRoutes = new Map<string, DaemonRoute>();
  private routeStreams = new Map<string, FakeStream>();
  private buffers = new Map<string, Frame[]>();

  // --- RelayNet ---

  async admit(): Promise<Uint8Array> {
    if (this.admitError) throw this.admitError;
    this.admissions++;
    return new Uint8Array([1, 2, 3]);
  }

  openStream(_wsUrl: string, headers: Record<string, string>): DataStream {
    const s = new FakeStream(this, headers);
    this.streams.push(s);
    return s;
  }

  // --- relay behaviour ---

  fromDevice(stream: FakeStream, f: Frame): void {
    if (this.blackholed.has(stream)) return;
    if (!stream.routeKey) {
      stream.routeKey = `${f.deviceId}|${new TextDecoder().decode(f.sessionId)}`;
      this.routeStreams.set(stream.routeKey, stream);
      for (const buffered of this.buffers.get(stream.routeKey) ?? []) stream.push(buffered);
      this.buffers.delete(stream.routeKey);
      this.daemonRoute(stream.routeKey, f.deviceId);
    }
    if (f.keepalive) {
      if (this.echoKeepalive) stream.push({ ...f, direction: Direction.DAEMON_TO_DEVICE });
      return;
    }
    if (f.payload.length > 0) this.daemonRoute(stream.routeKey, f.deviceId).feed(f.payload);
  }

  toDevice(key: string, payload: Uint8Array): void {
    const [deviceId, session] = key.split('|');
    const frame: Frame = {
      workspaceId: 'ws',
      sessionId: new TextEncoder().encode(session),
      deviceId,
      direction: Direction.DAEMON_TO_DEVICE,
      sequence: 0,
      payload,
      keepalive: false,
    };
    const stream = this.routeStreams.get(key);
    if (stream && !stream.closed) stream.push(frame);
    else {
      const b = this.buffers.get(key) ?? [];
      b.push(frame);
      this.buffers.set(key, b);
    }
  }

  private daemonRoute(key: string, deviceId: string): DaemonRoute {
    let r = this.daemonRoutes.get(key);
    if (!r) {
      r = new DaemonRoute(this, key);
      r.deviceId = deviceId;
      this.daemonRoutes.set(key, r);
      void r.serve();
    }
    return r;
  }

  /** The daemon's decision for a hello; false ends the route. */
  checkHello(route: DaemonRoute, id: string, hello: DeviceHello, reply: (id: string, body: object) => void): boolean {
    const peer = route.channel.peerKey!;
    const pub = base64Decode(hello.device_pub);
    const sig = base64Decode(hello.signature);
    const verified =
      deviceIdFor(pub) === route.deviceId && ed25519.verify(sig, helloSignedBytes(this.daemonKeys.publicKey, peer), pub);
    const policy = this.policy(route.deviceId);
    if (policy === 'silent') return true; // what the real daemon does with a method it does not know: no answer
    if (policy === 'legacy-daemon') {
      reply(id, { error: { message: 'method not found: relay.device_hello' } });
      return true;
    }
    if (policy === 'too_many') {
      reply(id, { error: { message: 'too many connected devices', code: 'too_many_devices' } });
      return false;
    }
    if (!verified || policy === 'bad_hello') {
      reply(id, { error: { message: 'device_hello rejected', code: 'bad_hello' } });
      return false;
    }
    if (policy === 'pending') {
      reply(id, { error: { message: 'awaiting approval: run `smind relay devices approve ' + route.deviceId + '` on the daemon', code: 'awaiting_approval' } });
      return false;
    }
    if (policy === 'revoked') {
      reply(id, { error: { message: 'this device was revoked', code: 'revoked' } });
      return false;
    }
    reply(id, { result: { v: 1, device_id: route.deviceId, state: 'approved' } });
    return true;
  }

  // --- test controls ---

  /** The live stream of the newest route (what the client is on now). */
  get lastStream(): FakeStream {
    return this.streams[this.streams.length - 1];
  }

  /** A network drop: the current stream ends, the daemon keeps the session. */
  dropCurrentStream(err: Error = new Error('grpc-web websocket: closed')): void {
    this.lastStream.kill(err);
  }

  /** The daemon loses every session (it restarted): frames for old routes go nowhere. */
  restartDaemon(): void {
    for (const r of this.daemonRoutes.values()) r.alive = false;
    this.daemonRoutes.clear();
    this.buffers.clear();
  }

  /** Distinct route keys the client has opened so far. */
  routeKeys(): string[] {
    return [...new Set(this.streams.map((s) => s.routeKey).filter((k): k is string => !!k))];
  }
}
