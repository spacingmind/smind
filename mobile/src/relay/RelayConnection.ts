// RelayConnection.ts is Milestone 2's Item 1 -- the persistent, multi-call
// generalization of Milestone 1's one-shot connectAndFetchWorkspaceList
// (docs/plans/active/mobile-app-milestone-2.md) -- grown into the mobile half
// of ADR-0031 D9's multi-device relay (P13b): the connection has a device
// identity, proves it with relay.device_hello, keeps its stream alive with
// keepalives, and survives drops by resuming (or, when the daemon lost the
// session, re-handshaking) on its own.
//
// One admit + E2EE handshake + device_hello buys any number of sequential (or
// concurrent) JSON-RPC-style calls over the same Channel, plus topic
// subscriptions for the server's unsolicited eventNotification pushes -- the
// TypeScript mirror of internal/wsapi/conn.go's id-keyed request/response
// contract layered with internal/wsapi/events.go's subscribe/push mechanism:
//   out:  {id, method, params}
//   back: {id, result} | {id, error: {message, code?}}
//         {id, event, params}                  (request-scoped emit, e.g. run.attach chunks)
//         {event: {topic, seq, payload}}       (no id: pushed event notification)
//
// Lifecycle (see ConnectionState): connecting -> connected; a drop goes to
// reconnecting and back; a daemon that has not approved this device yet is
// pending (retried until approved); a revoked device is revoked for good.
// Requests are at-most-once across a drop (the relay has no acknowledgements,
// so one in flight when the stream died may be lost): they reject and the
// caller decides whether to repeat them. Subscriptions are the connection's
// business and are re-established after every reconnect.

import { computeProof, PROTOCOL_VERSION, randomNonce } from './admission';
import { buildDeviceHello, ChallengeVersionError, DeviceIdentity, HELLO_METHOD, HELLO_VERSION, parseChallengeMessage } from './deviceIdentity';
import { Channel, generateKeyPair, RawTransport, Role } from './e2ee';
import { DataStream, GrpcCode, GRPCWebSocketStream, GrpcStatusError, grpcWebUnary } from './grpcweb';
import { DEFAULT_KEEPALIVE, Keepalive, KeepaliveTiming } from './keepalive';
import { PairingOffer, parsePairingURL } from './pairing';
import {
  decodeAdmitChallengeResponse,
  decodeAdmitResponse,
  decodeFrame,
  Direction,
  encodeAdmitChallengeRequest,
  encodeAdmitRequest,
  encodeFrame,
} from './proto';

/** One server-pushed eventNotification, already unwrapped from its envelope. */
export interface RelayEvent {
  topic: string;
  seq?: number;
  payload: unknown;
}

/** Options for a single call(): onEvent receives this call's request-scoped interim events ({id, event, params} messages, e.g. run.attach's streamed chunks). */
export interface CallOptions {
  onEvent?: (event: string, params: unknown) => void;
}

/** A call() promise with a detach path: cancel() sends the daemon's task.cancel for this request (conn.go's no-id cancellation envelope) and rejects the promise. */
export interface CancellablePromise<T> extends Promise<T> {
  cancel(): void;
}

interface WireError {
  message: string;
  code?: string;
}

interface WireEnvelope {
  id?: string;
  result?: unknown;
  error?: WireError;
  /** A string here means a request-scoped emit; an object is a pushed eventNotification. */
  event?: string | { topic: string; seq?: number; payload?: unknown };
  params?: unknown;
}

interface PendingCall {
  sentAt: number;
  /** Connection housekeeping (subscriptions, liveness probes): not something the liveness watchdog waits on. */
  background: boolean;
  resolve: (result: unknown) => void;
  reject: (err: Error) => void;
  onEvent?: (event: string, params: unknown) => void;
}

/** Where a connection is in its life; see the file comment. */
export type ConnectionState =
  | { kind: 'connecting' }
  | { kind: 'connected' }
  /** The stream dropped (or the daemon is unreachable) and the connection is working to get back. */
  | { kind: 'reconnecting'; attempt: number; reason: string }
  /** The daemon knows this device but has not approved it: `smind relay devices approve <deviceId>` on the daemon. Retried automatically. */
  | { kind: 'pending'; deviceId: string; label: string }
  /** The daemon (or relay) revoked this device. Terminal: pair again to use a fresh identity. */
  | { kind: 'revoked'; deviceId: string }
  /**
   * Cannot connect and will not retry on its own (bad offer secret, ...).
   * versionMismatch: the daemon is older than this app (no device challenge)
   * or speaks another hello version; update the daemon (or the app).
   */
  | { kind: 'failed'; message: string; versionMismatch?: boolean }
  | { kind: 'closed' };

/** Timings of the reconnect loop; tests shorten them. */
export interface ReconnectTiming {
  /** First backoff step after the first immediate retry; doubles up to maxMs (1 s). */
  baseMs: number;
  /** Backoff ceiling (30 s). */
  maxMs: number;
  /**
   * How often a pending (not yet approved) device asks again (5 s at first,
   * growing by half each time up to pendingPollMaxMs): every ask is a fresh
   * session, and the relay keeps an abandoned route for 5 minutes against its
   * per-host route cap, so a patient phone must not open one every few seconds.
   */
  pendingPollMs: number;
  pendingPollMaxMs: number;
  /** A link that ends sooner than this after connecting counts as a failed attempt (backoff), not a blip (immediate retry) (2 s). */
  minStableMs: number;
  /** How long a resumed session gets to answer a liveness request before it is replaced by a fresh one (10 s). */
  resumeProbeMs: number;
  /** How long a fresh session gets to finish the E2EE handshake and the hello answer (20 s each). */
  handshakeMs: number;
  /**
   * How long after the handshake the daemon's device challenge may take (5 s,
   * as the desktop client). No challenge means a daemon from before hello v2:
   * the client never falls back to the replayable v1 hello.
   */
  challengeMs: number;
  /** Resumes of one session that may fail in a row before it is replaced by a fresh one (3). */
  maxResumeFailures: number;
}

export const DEFAULT_RECONNECT: ReconnectTiming = {
  baseMs: 1_000,
  maxMs: 30_000,
  pendingPollMs: 5_000,
  pendingPollMaxMs: 30_000,
  minStableMs: 2_000,
  resumeProbeMs: 10_000,
  handshakeMs: 20_000,
  challengeMs: 5_000,
  maxResumeFailures: 3,
};

/**
 * Liveness of the end-to-end session (keepalive only proves the RELAY is
 * there): a request nobody answers for pendingStaleMs, or total silence for
 * idleStaleMs, makes the connection ask the daemon a cheap question; no answer
 * within ReconnectTiming.resumeProbeMs means the daemon lost the session (it
 * restarted, or dropped the device) and the connection opens a fresh one.
 */
export interface LivenessTiming {
  pendingStaleMs: number;
  idleStaleMs: number;
  /** How often the watchdog looks. */
  checkMs: number;
}

export const DEFAULT_LIVENESS: LivenessTiming = {
  pendingStaleMs: 15_000,
  idleStaleMs: 90_000,
  checkMs: 2_000,
};

/** The network side of a connection; tests substitute a fake relay. */
export interface RelayNet {
  admit(baseUrl: string, workspaceId: string, secret: Uint8Array, daemonKeyId: string): Promise<Uint8Array>;
  openStream(wsUrl: string, headers: Record<string, string>): DataStream;
}

export interface ConnectOptions {
  /** The persistent device identity (deviceIdentity.ts); proven to the daemon in relay.device_hello. */
  identity: DeviceIdentity;
  /** Shown to the person approving this device on the daemon. */
  label: string;
  keepalive?: Partial<KeepaliveTiming>;
  reconnect?: Partial<ReconnectTiming>;
  liveness?: Partial<LivenessTiming>;
  net?: RelayNet;
  /** Spreads a delay by +-20% (tests pass an identity function). */
  jitter?: (ms: number) => number;
}

/** The hello was refused because the daemon has not approved this device. */
class AwaitingApprovalError extends Error {}
/** The daemon or relay revoked this device. */
class RevokedError extends Error {}
/** A failure that retrying will not fix. */
class FatalError extends Error {}
/** The daemon and this app speak different device-identity versions: nothing to retry, someone has to update. */
class VersionMismatchError extends FatalError {}
/**
 * No challenge arrived in time. Final on a first connect (the daemon is older
 * than this app), but only a stall once a daemon has answered on this
 * connection: one that already served v2 cannot have become outdated.
 */
class ChallengeSilenceError extends VersionMismatchError {}
/** The daemon or relay is busy or full: retry with backoff, even on the first connect. */
class RetryableError extends Error {}

const HELLO_REQUEST_ID = 'hello';
const DAEMON_OUTDATED =
  'The daemon did not send a device challenge: it is older than this app (device hello v2) or runs a different version. Update the daemon and try again.';
const VERSIONS_DIFFER =
  'The daemon and this app use different device-identity versions. Update the daemon, and the app too if it is the older one, to the same release.';
// The liveness probe after a resume: a cheap read every daemon serves. (Not an
// unknown method: the daemon does not answer those at all.)
const PROBE_METHOD = 'workspace.list';

function hexEncode(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

async function admit(baseUrl: string, workspaceId: string, secret: Uint8Array, daemonKeyId: string): Promise<Uint8Array> {
  const clientNonce = randomNonce();
  const challenge = await grpcWebUnary(
    baseUrl,
    'AdmitChallenge',
    encodeAdmitChallengeRequest({ protocolVersion: PROTOCOL_VERSION, workspaceId, clientNonce, daemonKeyId }),
    decodeAdmitChallengeResponse,
  );

  const proof = computeProof(secret, {
    protocolVersion: PROTOCOL_VERSION,
    workspaceId,
    clientNonce,
    serverNonce: challenge.serverNonce,
    daemonKeyId,
  });

  const admitResp = await grpcWebUnary(
    baseUrl,
    'Admit',
    encodeAdmitRequest({
      protocolVersion: PROTOCOL_VERSION,
      workspaceId,
      clientNonce,
      daemonKeyId,
      serverNonce: challenge.serverNonce,
      proof,
    }),
    decodeAdmitResponse,
  );
  if (admitResp.admissionId.length === 0) throw new Error('admit: relay returned an empty admission id');
  return admitResp.admissionId;
}

/** The real network: grpc-web unary admission and the grpc-web websocket stream. */
export const grpcWebRelayNet: RelayNet = {
  admit,
  openStream: (wsUrl, headers) => new GRPCWebSocketStream(wsUrl, headers),
};

function wsBaseURL(httpsBaseUrl: string): string {
  if (httpsBaseUrl.startsWith('https://')) return 'wss://' + httpsBaseUrl.slice('https://'.length);
  if (httpsBaseUrl.startsWith('http://')) return 'ws://' + httpsBaseUrl.slice('http://'.length);
  throw new Error(`relay URL must be http(s)://, got ${httpsBaseUrl}`);
}

/** A fresh E2EE session id (Frame.session_id): 128 random bits, hex. */
function randomSessionId(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return 's-' + hexEncode(bytes);
}

function defaultJitter(ms: number): number {
  return ms * (0.8 + Math.random() * 0.4);
}

class TimeoutError extends Error {}

function withTimeout<T>(p: Promise<T>, ms: number, message: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new TimeoutError(message)), ms);
    p.then(
      (v) => {
        clearTimeout(timer);
        resolve(v);
      },
      (e) => {
        clearTimeout(timer);
        reject(e);
      },
    );
  });
}

/**
 * RouteTransport adapts the OpenData streams of ONE relay route (host,
 * session, device) to e2ee's RawTransport -- the TypeScript mirror of Go's
 * frameConn (internal/relay/client/client.go) one layer below the E2EE
 * Channel. The route outlives any one stream: after a drop attach() puts the
 * next stream under the same Channel, and the frame sequence carries on (the
 * relay orders buffered frames by it), so the session resumes in place.
 */
class RouteTransport implements RawTransport {
  private stream: DataStream | null = null;
  private keepalive: Keepalive | null = null;
  private seq = 0;

  constructor(
    private readonly workspaceId: string,
    readonly sessionId: string,
    private readonly deviceId: string,
    private readonly timing: KeepaliveTiming,
  ) {}

  /** Starts using stream: registers the route (the relay derives a stream's route from its first frame) and starts its keepalive. */
  attach(stream: DataStream): void {
    this.stream = stream;
    this.send(new Uint8Array(0));
    this.keepalive?.stop();
    this.keepalive = new Keepalive(
      this.timing,
      (nonce) => this.sendFrame(stream, { sequence: nonce, payload: new Uint8Array(0), keepalive: true }),
      () => stream.close(),
    );
  }

  /** Ends the current stream (idempotent); the session state in the Channel is untouched. */
  detach(): void {
    this.keepalive?.stop();
    this.keepalive = null;
    this.stream?.close();
    this.stream = null;
  }

  private sendFrame(stream: DataStream, f: { sequence: number; payload: Uint8Array; keepalive: boolean }): void {
    stream.send(
      encodeFrame({
        workspaceId: this.workspaceId,
        sessionId: new TextEncoder().encode(this.sessionId),
        deviceId: this.deviceId,
        direction: Direction.DEVICE_TO_DAEMON,
        ...f,
      }),
    );
  }

  send(bytes: Uint8Array): void {
    if (!this.stream) throw new Error('relay: stream is closed');
    this.sendFrame(this.stream, { sequence: this.seq++, payload: bytes, keepalive: false });
  }

  async receiveChunk(): Promise<Uint8Array> {
    const stream = this.stream;
    if (!stream) throw new Error('relay: stream is closed');
    for (;;) {
      const frame = await stream.receiveMessage(decodeFrame);
      this.keepalive?.noteFrame(frame);
      // A liveness frame (an echo of ours, or a probe a relay without
      // keepalive forwarded from the peer): never application data.
      if (frame.keepalive) continue;
      return frame.payload;
    }
  }
}

/** One E2EE session with the daemon: survives stream drops (resume) until it is replaced (fresh). */
interface Session {
  transport: RouteTransport;
  channel: Channel;
}

/** A running receive loop over a session's current stream. */
interface Link {
  session: Session;
  /** Settles when the receive loop ended, i.e. the stream is gone. */
  done: Promise<Error>;
}

/** The piece of GRPCWebSocketStream close() needs; an interface so tests can supply a stub. */
export interface CloseableStream {
  close(): void;
}

/**
 * Maps what a stream or handshake threw to what the supervisor acts on: a
 * relay refusal of a revoked device and a daemon whose key is not the one in
 * the offer are final, everything else is a transport failure.
 */
function classify(err: unknown): Error {
  const e = err instanceof Error ? err : new Error(String(err));
  if (
    e instanceof AwaitingApprovalError ||
    e instanceof RevokedError ||
    e instanceof FatalError ||
    e instanceof RetryableError ||
    e instanceof TimeoutError
  ) {
    return e;
  }
  if (e instanceof GrpcStatusError && e.code === GrpcCode.PermissionDenied && /revoked/i.test(e.message)) {
    return new RevokedError(e.message);
  }
  if (e instanceof GrpcStatusError && e.code === GrpcCode.ResourceExhausted) {
    return new RetryableError(e.message); // the relay host is full of devices or routes
  }
  if (e.message.startsWith('e2ee: daemon public key does not match')) {
    return new FatalError('the daemon at the other end is not the one in the pairing offer (its key changed); pair again');
  }
  return e;
}

/**
 * RelayConnection is one admitted, E2EE-handshaken, identified, long-lived
 * relay session. Create it with open() (or connect(), which waits for the
 * connected state); then issue any number of call()s (each matched to its own
 * response by a locally-generated incrementing id, so out-of-order or
 * concurrent responses land on the right promise) and subscribe() to topics
 * for pushed eventNotification messages. It reconnects on its own after a
 * drop (see the file comment); state/onState expose where it is. close()
 * tears it down for good: call()s after close reject instead of hanging, as do
 * in-flight ones.
 */
export class RelayConnection {
  private channel: Channel | null;
  private stream: CloseableStream | null;
  private nextId = 0;
  private pending = new Map<string, PendingCall>();
  private topicCallbacks = new Map<string, Set<(event: RelayEvent) => void>>();
  private closed = false;
  private stateValue: ConnectionState;
  private connectedCount = 0;
  private stateListeners = new Set<(s: ConnectionState) => void>();

  // Supervised mode only (open()); an "established" connection (the
  // constructor's stream + channel) has none of this and does not reconnect.
  private session: Session | null = null;
  private link: Link | null = null;
  private opts: ConnectOptions | null = null;
  private keepalive: KeepaliveTiming = DEFAULT_KEEPALIVE;
  private timing: ReconnectTiming = DEFAULT_RECONNECT;
  private net: RelayNet = grpcWebRelayNet;
  private jitter: (ms: number) => number = defaultJitter;
  private liveness: LivenessTiming = DEFAULT_LIVENESS;
  /** When a message from the daemon last arrived (the liveness watchdog's clock). */
  private lastInbound = 0;
  private probing = false;
  /** The route of a fresh session that is still handshaking, so close() can end it. */
  private connecting: RouteTransport | null = null;
  private wake: (() => void) | null = null;

  /** The pairing offer this connection was admitted under. */
  readonly offer: PairingOffer;

  /**
   * Public for tests: an already-handshaken connection over a pre-made
   * Channel, with no identity and no reconnect (the in-memory harness,
   * relayHarness.ts). Real callers use open() / connect().
   */
  constructor(offer: PairingOffer, stream: CloseableStream | null, channel: Channel | null) {
    this.offer = offer;
    this.stream = stream;
    this.channel = channel;
    this.stateValue = channel ? { kind: 'connected' } : { kind: 'connecting' };
    if (channel) {
      this.connectedCount = 1;
      void this.receiveLoop(channel).then((err) => this.failAllPending(err));
    }
  }

  /**
   * Starts a connection in the background and returns it at once, in the
   * connecting state; watch state/onState. Throws for an offer that cannot be
   * used at all.
   */
  static open(pairingUrl: string, opts: ConnectOptions): RelayConnection {
    const offer = parsePairingURL(pairingUrl);
    if (!offer.workspaceId) throw new Error('pairing offer has no relay host id');
    if (offer.secret.length === 0) throw new Error('pairing offer has no admission secret');
    const conn = new RelayConnection(offer, null, null);
    conn.opts = opts;
    conn.keepalive = { ...DEFAULT_KEEPALIVE, ...opts.keepalive };
    conn.timing = { ...DEFAULT_RECONNECT, ...opts.reconnect };
    conn.net = opts.net ?? grpcWebRelayNet;
    conn.jitter = opts.jitter ?? defaultJitter;
    conn.liveness = { ...DEFAULT_LIVENESS, ...opts.liveness };
    void conn.supervise().catch((e) => conn.setState({ kind: 'failed', message: e instanceof Error ? e.message : String(e) }));
    return conn;
  }

  /**
   * open() that resolves once the connection is connected, and rejects if it
   * ends up revoked, failed or closed. A device that is not approved yet
   * keeps waiting (the pending state): approval arrives out of band.
   */
  static async connect(pairingUrl: string, opts: ConnectOptions): Promise<RelayConnection> {
    const conn = RelayConnection.open(pairingUrl, opts);
    try {
      await conn.whenConnected();
    } catch (e) {
      conn.close();
      throw e;
    }
    return conn;
  }

  /** Where the connection is now. */
  get state(): ConnectionState {
    return this.stateValue;
  }

  /** Counts how many times the connection became connected (first connect and every reconnect); screens reload their data when it changes. */
  get epoch(): number {
    return this.connectedCount;
  }

  /** Calls cb on every state change; returns its unsubscribe. */
  onState(cb: (state: ConnectionState) => void): () => void {
    this.stateListeners.add(cb);
    return () => this.stateListeners.delete(cb);
  }

  /** Resolves when connected; rejects when revoked, failed or closed. */
  whenConnected(): Promise<void> {
    return new Promise((resolve, reject) => {
      const check = (s: ConnectionState): boolean => {
        if (s.kind === 'connected') resolve();
        else if (s.kind === 'revoked') reject(new Error('this device was revoked by the daemon'));
        else if (s.kind === 'failed') reject(new Error(s.message));
        else if (s.kind === 'closed') reject(new Error('relay: connection is closed'));
        else return false;
        return true;
      };
      if (check(this.stateValue)) return;
      const off = this.onState((s) => {
        if (check(s)) off();
      });
    });
  }

  /**
   * Sends one {id, method, params} request and resolves with that id's
   * result field. Interim {id, event, params} messages (request-scoped
   * emits like run.attach's streamed chunks) are routed to
   * opts.onEvent if given. A matching {id, error} response (or the
   * connection dying) rejects this call's promise only -- other in-flight
   * calls are unaffected. While the connection is not connected the call
   * rejects at once.
   */
  call(method: string, params?: unknown, opts?: CallOptions): CancellablePromise<unknown> {
    if (this.closed) return rejected(new Error('relay: connection is closed'));
    if (this.stateValue.kind !== 'connected' || !this.channel) {
      return rejected(new Error(`relay: not connected (${this.stateValue.kind === 'connected' ? 'reconnecting' : this.stateValue.kind})`));
    }
    return this.request(method, params, opts);
  }

  /**
   * Registers onEvent for the given topics (via events.subscribe) and
   * returns its unsubscribe function, which removes the callbacks and
   * releases the topics no other subscriber wants (via
   * events.unsubscribe). Pushed eventNotification messages are dispatched
   * by topic to every registered callback. The connection re-subscribes to
   * every wanted topic after each reconnect.
   */
  subscribe(topics: string[], onEvent: (event: RelayEvent) => void): () => void {
    const newlySubscribed: string[] = [];
    for (const topic of topics) {
      let set = this.topicCallbacks.get(topic);
      if (!set) {
        set = new Set();
        this.topicCallbacks.set(topic, set);
      }
      if (set.size === 0) newlySubscribed.push(topic);
      set.add(onEvent);
    }
    if (newlySubscribed.length > 0) this.fireSubscriptionRPC('events.subscribe', newlySubscribed);

    return () => {
      const released: string[] = [];
      for (const topic of topics) {
        const set = this.topicCallbacks.get(topic);
        if (!set) continue;
        set.delete(onEvent);
        if (set.size === 0) {
          this.topicCallbacks.delete(topic);
          released.push(topic);
        }
      }
      if (released.length > 0) this.fireSubscriptionRPC('events.unsubscribe', released);
    };
  }

  /** Tears down the stream for good; pending and future call()s reject, topic callbacks stop and no reconnect follows. Idempotent. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.rejectPending(new Error('relay: connection is closed'));
    this.topicCallbacks.clear();
    this.session?.transport.detach();
    this.connecting?.detach();
    this.stream?.close();
    this.setState({ kind: 'closed' });
    this.wake?.();
  }

  // --- requests ---

  private request(method: string, params: unknown, opts?: CallOptions, background = false): CancellablePromise<unknown> {
    const channel = this.channel;
    if (!channel) return rejected(new Error('relay: not connected'));
    const id = String(++this.nextId);
    const promise = new Promise<unknown>((resolve, reject) => {
      this.pending.set(id, { sentAt: Date.now(), background, resolve, reject, onEvent: opts?.onEvent });
      const envelope: Record<string, unknown> = { id, method };
      if (params !== undefined) envelope.params = params;
      try {
        channel.send(new TextEncoder().encode(JSON.stringify(envelope)));
      } catch (e) {
        this.pending.delete(id);
        reject(e instanceof Error ? e : new Error(String(e)));
      }
    }) as CancellablePromise<unknown>;
    promise.cancel = () => {
      const p = this.pending.get(id);
      if (!p) return;
      this.pending.delete(id);
      // conn.go's cancellation shape: task.cancel with no envelope id,
      // naming the target request's id in params. For run.attach this
      // detaches without stopping the run.
      if (!this.closed && this.channel === channel) {
        try {
          channel.send(new TextEncoder().encode(JSON.stringify({ method: 'task.cancel', params: { id } })));
        } catch {
          // The stream is going down; the daemon drops the request with it.
        }
      }
      p.reject(new Error('relay: call cancelled'));
    };
    return promise;
  }

  private fireSubscriptionRPC(method: string, topics: string[]): void {
    if (this.closed || this.stateValue.kind !== 'connected') return;
    this.request(method, { topics }, undefined, true).catch(() => {
      // A failed subscribe just means this callback never fires; an
      // unsubscribe that races a closed connection is equally ignorable.
    });
  }

  private resubscribe(): void {
    const topics = [...this.topicCallbacks.keys()];
    if (topics.length > 0) this.fireSubscriptionRPC('events.subscribe', topics);
  }

  // --- receive path ---

  /** Dispatches decrypted messages from channel until its stream ends; resolves with why. */
  private async receiveLoop(channel: Channel): Promise<Error> {
    const decoder = new TextDecoder();
    try {
      for (;;) {
        const bytes = await channel.receive();
        this.lastInbound = Date.now();
        try {
          this.handleEnvelope(JSON.parse(decoder.decode(bytes)));
        } catch {
          // Malformed JSON: skip the message, mirroring conn.go's serve.
        }
      }
    } catch (err) {
      return err instanceof Error ? err : new Error(String(err));
    }
  }

  private handleEnvelope(env: WireEnvelope): void {
    if (env.error && env.id !== undefined) {
      const p = this.pending.get(env.id);
      if (p) {
        this.pending.delete(env.id);
        p.reject(new Error(env.error.message));
      }
      return;
    }
    if (typeof env.event === 'object' && env.event !== null) {
      const relayEvent: RelayEvent = {
        topic: env.event.topic,
        seq: env.event.seq,
        payload: env.event.payload,
      };
      const set = this.topicCallbacks.get(relayEvent.topic);
      if (set) for (const cb of set) cb(relayEvent);
      return;
    }
    if (typeof env.event === 'string' && env.id !== undefined) {
      const p = this.pending.get(env.id);
      if (p?.onEvent) p.onEvent(env.event, env.params);
      return;
    }
    if (env.id !== undefined) {
      const p = this.pending.get(env.id);
      if (p) {
        this.pending.delete(env.id);
        p.resolve(env.result);
      }
    }
  }

  private rejectPending(err: Error): void {
    for (const p of this.pending.values()) p.reject(err);
    this.pending.clear();
  }

  /** An established (non-reconnecting) connection lost its stream: same as the old behaviour, closed for good. */
  private failAllPending(err: Error): void {
    this.closed = true;
    this.rejectPending(err);
    this.topicCallbacks.clear();
    this.setState({ kind: 'closed' });
  }

  private setState(next: ConnectionState): void {
    if (this.stateValue.kind === 'closed' && next.kind !== 'closed') return;
    this.stateValue = next;
    if (next.kind === 'connected') this.connectedCount++;
    for (const cb of [...this.stateListeners]) cb(next);
  }

  // --- supervisor ---

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const timer = setTimeout(done, ms);
      const self = this;
      function done() {
        clearTimeout(timer);
        self.wake = null;
        resolve();
      }
      this.wake = done;
    });
  }

  private backoff(attempt: number): number {
    return this.jitter(Math.min(this.timing.baseMs * 2 ** (attempt - 1), this.timing.maxMs));
  }

  /**
   * The connection's whole life: establish a session (resume the previous one
   * after a drop, otherwise a fresh one), serve until its stream ends, then
   * establish again -- until closed, revoked or failed for good.
   */
  private async supervise(): Promise<void> {
    const opts = this.opts!;
    let everConnected = false;
    // Set once something has actually answered (a daemon that wants approval, a
    // relay that is full): from then on a failure is retried, not reported as final.
    let reachedDaemon = false;
    let attempt = 0;
    let resumeFailures = 0;
    let pendingPolls = 0;

    while (!this.closed) {
      let link: Link;
      try {
        link = this.session ? await this.resumeSession(this.session) : await this.freshSession();
      } catch (raw) {
        if (this.closed) return;
        const e = classify(raw);
        if (e instanceof RevokedError) {
          this.dropSession();
          this.setState({ kind: 'revoked', deviceId: opts.identity.deviceId });
          return;
        }
        const stalled = e instanceof ChallengeSilenceError && (everConnected || reachedDaemon);
        if (e instanceof FatalError && !stalled) {
          this.dropSession();
          this.setState({ kind: 'failed', message: e.message, versionMismatch: e instanceof VersionMismatchError || undefined });
          return;
        }
        if (e instanceof AwaitingApprovalError) {
          this.dropSession();
          reachedDaemon = true;
          this.setState({ kind: 'pending', deviceId: opts.identity.deviceId, label: opts.label });
          const ask = Math.min(this.timing.pendingPollMs * 1.5 ** pendingPolls++, this.timing.pendingPollMaxMs);
          await this.sleep(this.jitter(ask));
          continue;
        }
        if (e instanceof TimeoutError && this.session) {
          // The resumed session was not answered: the daemon lost it (it
          // restarted, or dropped the device). Start over with a fresh one.
          this.dropSession();
          continue;
        }
        if (e instanceof RetryableError) reachedDaemon = true;
        if (!everConnected && !reachedDaemon) {
          // Nothing has worked yet: an unreachable relay or a bad secret is
          // the user's to fix; report it rather than retrying silently.
          this.dropSession();
          this.setState({ kind: 'failed', message: e.message });
          return;
        }
        if (this.session && ++resumeFailures >= this.timing.maxResumeFailures) {
          this.dropSession();
          resumeFailures = 0;
        }
        attempt++;
        this.setState({ kind: 'reconnecting', attempt, reason: e.message });
        await this.sleep(this.backoff(attempt));
        continue;
      }

      everConnected = true;
      resumeFailures = 0;
      pendingPolls = 0;
      this.link = link;
      const connectedAt = Date.now();
      this.setState({ kind: 'connected' });
      this.resubscribe();

      const stopWatching = this.watchLiveness(link);
      const err = classify(await link.done);
      stopWatching();
      link.session.transport.detach();
      this.link = null;
      if (this.closed) return;
      if (err instanceof RevokedError) {
        this.dropSession();
        this.setState({ kind: 'revoked', deviceId: opts.identity.deviceId });
        return;
      }
      // A drop after a stable stretch is a blip and is retried at once; one
      // right after connecting is a flapping path and backs off like any
      // failed attempt (the hub's minStableConnection rule).
      attempt = Date.now() - connectedAt >= this.timing.minStableMs ? 0 : attempt + 1;
      this.setState({ kind: 'reconnecting', attempt, reason: err.message });
      if (attempt > 0) await this.sleep(this.backoff(attempt));
    }
  }

  /**
   * The end-to-end liveness watchdog (see LivenessTiming). Returns its stop.
   * A probe that goes unanswered ends the link AND forgets the session, so
   * the supervisor opens a fresh one instead of resuming a dead one.
   */
  private watchLiveness(link: Link): () => void {
    const startedAt = Date.now();
    const timer = setInterval(() => {
      if (this.probing || this.closed || this.channel !== link.session.channel) return;
      // The OLDEST unanswered request starts the clock: a user tapping again on
      // a frozen screen must not keep pushing the probe back.
      let waitingSince = 0;
      for (const p of this.pending.values()) if (!p.background) waitingSince = waitingSince === 0 ? p.sentAt : Math.min(waitingSince, p.sentAt);
      const quietSince = Math.max(this.lastInbound, startedAt, waitingSince);
      const staleAfter = waitingSince > 0 ? this.liveness.pendingStaleMs : this.liveness.idleStaleMs;
      if (Date.now() - quietSince < staleAfter) return;
      this.probing = true;
      void withTimeout(
        this.request(PROBE_METHOD, undefined, undefined, true).catch(answeredOrThrow),
        this.timing.resumeProbeMs,
        'the daemon stopped answering on this session',
      )
        .catch((e) => {
          if (!(e instanceof TimeoutError) || this.closed || this.channel !== link.session.channel) return;
          this.dropSession();
          link.session.transport.detach();
        })
        .finally(() => {
          this.probing = false;
        });
    }, this.liveness.checkMs);
    return () => clearInterval(timer);
  }

  private dropSession(): void {
    this.session?.transport.detach();
    this.session = null;
    this.channel = null;
  }

  /** Admits and opens one OpenData stream for the route; failures here are transport failures (the session, if any, is intact). */
  private async openRoute(): Promise<DataStream> {
    const { offer } = this;
    const admissionId = await this.net.admit(offer.relay, offer.workspaceId, offer.secret, `device-${this.opts!.identity.deviceId}`);
    if (this.closed) throw new Error('relay: connection is closed');
    const stream = this.net.openStream(`${wsBaseURL(offer.relay)}/relay.v1.Relay/OpenData`, {
      'admission-id': hexEncode(admissionId),
    });
    try {
      await stream.ready();
    } catch (e) {
      stream.close();
      throw e;
    }
    if (this.closed) {
      stream.close();
      throw new Error('relay: connection is closed');
    }
    return stream;
  }

  /** Starts the receive loop over the session's current stream; done fails every pending request when the stream ends. */
  private startLink(session: Session): Link {
    this.channel = session.channel;
    this.lastInbound = Date.now();
    const done = this.receiveLoop(session.channel).then((err) => {
      // From here no call may be sent on this session: it would never be
      // answered (and never rejected) if issued before the supervisor reacts.
      if (this.channel === session.channel) this.channel = null;
      this.rejectPending(new Error('relay: connection lost'));
      return err;
    });
    return { session, done };
  }

  /** A brand new session: new route id, new E2EE handshake, device_hello. */
  private async freshSession(): Promise<Link> {
    const { identity, label } = this.opts!;
    this.dropSession();
    const transport = new RouteTransport(this.offer.workspaceId, randomSessionId(), identity.deviceId, this.keepalive);
    const keyPair = generateKeyPair();
    const channel = new Channel(transport, keyPair, Role.Mobile);
    this.connecting = transport;
    try {
      transport.attach(await this.openRoute());
      if (this.closed) throw new Error('relay: connection is closed');
      await withTimeout(
        channel.handshake(this.offer.publicKey),
        this.timing.handshakeMs,
        'the daemon did not complete the relay handshake (is it running and connected to this relay?)',
      );
      // The daemon speaks first on a device route: its challenge, fresh for
      // this session, goes into the signature. Never fall back to a hello
      // without it (version 1 was replayable).
      const challenge = await this.readChallenge(channel);
      channel.send(
        new TextEncoder().encode(
          JSON.stringify({
            id: HELLO_REQUEST_ID,
            method: HELLO_METHOD,
            params: buildDeviceHello(
              identity,
              label,
              this.offer.publicKey,
              keyPair.publicKey,
              new TextEncoder().encode(transport.sessionId),
              challenge,
            ),
          }),
        ),
      );
      await withTimeout(
        this.readHelloAnswer(channel),
        this.timing.handshakeMs,
        'the daemon did not answer device_hello (it may be older than this app: update it)',
      );
      // close() ended the route while the handshake was in flight: do not
      // turn the finished handshake into a live, admitted session.
      if (this.closed) throw new Error('relay: connection is closed');
    } catch (e) {
      transport.detach();
      throw classify(e);
    } finally {
      this.connecting = null;
    }
    const session: Session = { transport, channel };
    this.session = session;
    return this.startLink(session);
  }

  /** Waits for the daemon's device challenge (the first message after the handshake). */
  private async readChallenge(channel: Channel): Promise<Uint8Array> {
    const read = (async () => {
      for (;;) {
        const challenge = parseChallengeMessage(await channel.receive());
        if (challenge) return challenge;
      }
    })();
    try {
      return await withTimeout(read, this.timing.challengeMs, DAEMON_OUTDATED);
    } catch (e) {
      if (e instanceof TimeoutError) throw new ChallengeSilenceError(DAEMON_OUTDATED);
      if (e instanceof ChallengeVersionError) throw new VersionMismatchError(`${VERSIONS_DIFFER} (${e.message})`);
      throw e;
    }
  }

  /** Reads the hello answer (the first message after the handshake) and turns a refusal into the error the supervisor acts on. */
  private async readHelloAnswer(channel: Channel): Promise<void> {
    const decoder = new TextDecoder();
    for (;;) {
      let env: WireEnvelope;
      try {
        env = JSON.parse(decoder.decode(await channel.receive()));
      } catch (e) {
        if (e instanceof SyntaxError) continue;
        throw e;
      }
      if (env.id !== HELLO_REQUEST_ID) continue;
      if (env.error) {
        const { code, message } = env.error;
        if (code === 'awaiting_approval') throw new AwaitingApprovalError(message);
        if (code === 'revoked') throw new RevokedError(message);
        if (code === 'hello_version_unsupported') throw new VersionMismatchError(VERSIONS_DIFFER);
        if (code === 'bad_hello' || code === 'device_hello_unsupported') throw new FatalError(message);
        if (!code && /method not found/i.test(message)) throw new VersionMismatchError(DAEMON_OUTDATED);
        throw new RetryableError(message); // too_many_devices, too_many_pending, internal: worth retrying
      }
      const result = env.result as { v?: number; device_id?: string } | undefined;
      if (result?.v !== HELLO_VERSION || result.device_id !== this.opts!.identity.deviceId) {
        throw new FatalError('the daemon answered device_hello with an unexpected identity');
      }
      return;
    }
  }

  /** Puts a new stream under the existing session (same route, same keys and counters) and checks that the daemon still has it. */
  private async resumeSession(session: Session): Promise<Link> {
    const stream = await this.openRoute();
    session.transport.attach(stream);
    session.channel.resumeTransport();
    const link = this.startLink(session);
    try {
      // The relay has no acknowledgements, so the only proof the daemon still
      // holds this session is an answer. Any answer will do, even an error.
      await withTimeout(this.request(PROBE_METHOD, undefined, undefined, true).catch(answeredOrThrow), this.timing.resumeProbeMs, 'the daemon did not answer on the resumed session');
    } catch (e) {
      session.transport.detach();
      const why = classify(await link.done);
      if (why instanceof RevokedError) throw why;
      throw e instanceof TimeoutError ? e : classify(e);
    }
    return link;
  }
}

/** A daemon's error answer is still an answer; only a lost connection or a failed send is not. */
function answeredOrThrow(e: Error): void {
  if (e.message === 'relay: connection lost' || e.message === 'relay: not connected' || e.message === 'relay: connection is closed') throw e;
}

function rejected(err: Error): CancellablePromise<unknown> {
  const p = Promise.reject(err) as CancellablePromise<unknown>;
  p.cancel = () => {};
  return p;
}
