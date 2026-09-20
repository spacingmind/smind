// RelayConnection.ts is Milestone 2's Item 1: the persistent, multi-call
// generalization of Milestone 1's one-shot connectAndFetchWorkspaceList
// (docs/plans/active/mobile-app-milestone-2.md). One admit + E2EE
// handshake buys any number of sequential (or concurrent) JSON-RPC-style
// calls over the same Channel, plus topic subscriptions for the server's
// unsolicited eventNotification pushes -- the TypeScript mirror of
// internal/wsapi/conn.go's id-keyed request/response contract layered
// with internal/wsapi/events.go's subscribe/push mechanism:
//   out:  {id, method, params}
//   back: {id, result} | {id, error: {message, code?}}
//         {id, event, params}                  (request-scoped emit, e.g. run.attach chunks)
//         {event: {topic, seq, payload}}       (no id: pushed event notification)

import { computeHMAC, hashSecret, PROTOCOL_VERSION, randomNonce } from './admission';
import { Channel, generateKeyPair, Role } from './e2ee';
import { GRPCWebSocketStream, grpcWebUnary } from './grpcweb';
import { PairingOffer, parsePairingURL } from './pairing';
import {
  decodeAdmitChallengeResponse,
  decodeAdmitResponse,
  decodeFrame,
  Direction,
  encodeAdmitChallengeRequest,
  encodeAdmitRequest,
  encodeFrame,
  Frame,
} from './proto';

/** The daemon-side fixed session identity Milestone 1's bridge bridges (see internal/relay/bridge's DefaultSessionID/DefaultDeviceID doc comment for why). */
const DEFAULT_SESSION_ID = 'm1-default-session';
const DEFAULT_DEVICE_ID = 'm1-default-device';
const MOBILE_DAEMON_KEY_ID = 'mobile-app-milestone-1';

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
  resolve: (result: unknown) => void;
  reject: (err: Error) => void;
  onEvent?: (event: string, params: unknown) => void;
}

function hexEncode(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

async function admit(baseUrl: string, workspaceId: string, secret: Uint8Array): Promise<Uint8Array> {
  const clientNonce = randomNonce();
  const challenge = await grpcWebUnary(
    baseUrl,
    'AdmitChallenge',
    encodeAdmitChallengeRequest({
      protocolVersion: PROTOCOL_VERSION,
      workspaceId,
      clientNonce,
      daemonKeyId: MOBILE_DAEMON_KEY_ID,
    }),
    decodeAdmitChallengeResponse
  );

  const hmacValue = computeHMAC(hashSecret(secret), {
    protocolVersion: PROTOCOL_VERSION,
    workspaceId,
    clientNonce,
    serverNonce: challenge.serverNonce,
    daemonKeyId: MOBILE_DAEMON_KEY_ID,
  });

  const admitResp = await grpcWebUnary(
    baseUrl,
    'Admit',
    encodeAdmitRequest({
      protocolVersion: PROTOCOL_VERSION,
      workspaceId,
      clientNonce,
      daemonKeyId: MOBILE_DAEMON_KEY_ID,
      serverNonce: challenge.serverNonce,
      hmac: hmacValue,
    }),
    decodeAdmitResponse
  );
  if (admitResp.admissionId.length === 0) throw new Error('admit: relay returned an empty admission id');
  return admitResp.admissionId;
}

function wsBaseURL(httpsBaseUrl: string): string {
  if (httpsBaseUrl.startsWith('https://')) return 'wss://' + httpsBaseUrl.slice('https://'.length);
  if (httpsBaseUrl.startsWith('http://')) return 'ws://' + httpsBaseUrl.slice('http://'.length);
  throw new Error(`relay URL must be http(s)://, got ${httpsBaseUrl}`);
}

/**
 * Adapts one GRPCWebSocketStream (relay.v1.Frame messages) to e2ee's
 * RawTransport (opaque byte chunks) -- the TypeScript mirror of Go's
 * frameConn (internal/relay/client/client.go), one layer below the E2EE
 * Channel.
 */
function frameTransport(stream: GRPCWebSocketStream, workspaceId: string, sessionId: string, deviceId: string) {
  let seq = 0;
  return {
    send(bytes: Uint8Array): void {
      const frame: Frame = {
        workspaceId,
        sessionId: new TextEncoder().encode(sessionId),
        deviceId,
        direction: Direction.DEVICE_TO_DAEMON,
        sequence: seq++,
        payload: bytes,
      };
      stream.send(encodeFrame(frame));
    },
    receiveChunk(): Promise<Uint8Array> {
      return stream.receiveMessage(decodeFrame).then((f) => f.payload);
    },
  };
}

/**
 * RelayConnection is one admitted, E2EE-handshaken, long-lived relay
 * session. Construct via connect(); then issue any number of call()s
 * (each matched to its own response by a locally-generated incrementing
 * id, so out-of-order or concurrent responses land on the right promise)
 * and subscribe() to topics for pushed eventNotification messages. close()
 * tears the underlying stream down; call()s after close reject instead of
 * hanging, as do in-flight ones.
 */
/** The piece of GRPCWebSocketStream close() needs; an interface so tests can supply a stub. */
export interface CloseableStream {
  close(): void;
}

export class RelayConnection {
  private readonly stream: CloseableStream;
  private readonly channel: Channel;
  private nextId = 0;
  private pending = new Map<string, PendingCall>();
  private topicCallbacks = new Map<string, Set<(event: RelayEvent) => void>>();
  private closed = false;

  /** The pairing offer this connection was admitted under. */
  readonly offer: PairingOffer;

  /** Public for tests (over a pre-handshaken Channel); real callers use connect(). */
  constructor(offer: PairingOffer, stream: CloseableStream, channel: Channel) {
    this.offer = offer;
    this.stream = stream;
    this.channel = channel;
    this.runReceiveLoop();
  }

  /**
   * Runs the full connect sequence (parse offer -> admit -> open the
   * bridged E2EE data session as the mobile role -> verify the daemon's
   * public key against the offer's) and returns the live connection with
   * its receive loop already running.
   */
  static async connect(pairingUrl: string): Promise<RelayConnection> {
    const offer = parsePairingURL(pairingUrl);
    if (!offer.workspaceId) throw new Error('pairing offer has no workspace id');
    if (offer.secret.length === 0) throw new Error('pairing offer has no admission secret');

    const admissionId = await admit(offer.relay, offer.workspaceId, offer.secret);

    const stream = new GRPCWebSocketStream(`${wsBaseURL(offer.relay)}/relay.v1.Relay/OpenData`, {
      'admission-id': hexEncode(admissionId),
    });
    await stream.ready();

    const transport = frameTransport(stream, offer.workspaceId, DEFAULT_SESSION_ID, DEFAULT_DEVICE_ID);
    // The relay derives a stream's route from its first frame, so an empty
    // registration frame is sent immediately -- internal/relay/client's
    // frameConn.register does the same thing on the Go side.
    transport.send(new Uint8Array(0));

    const mobileKeyPair = generateKeyPair();
    const channel = new Channel(transport, mobileKeyPair, Role.Mobile);
    await channel.handshake(offer.publicKey);

    return new RelayConnection(offer, stream, channel);
  }

  /**
   * Sends one {id, method, params} request and resolves with that id's
   * result field. Interim {id, event, params} messages (request-scoped
   * emits like run.attach's streamed chunks) are routed to
   * opts.onEvent if given. A matching {id, error} response (or the
   * connection dying) rejects this call's promise only -- other in-flight
   * calls are unaffected.
   */
  call(method: string, params?: unknown, opts?: CallOptions): CancellablePromise<unknown> {
    if (this.closed) {
      return Object.assign(Promise.reject(new Error('relay: connection is closed')), { cancel: () => {} });
    }
    const id = String(++this.nextId);
    const promise = new Promise<unknown>((resolve, reject) => {
      this.pending.set(id, { resolve, reject, onEvent: opts?.onEvent });
      const envelope: Record<string, unknown> = { id, method };
      if (params !== undefined) envelope.params = params;
      this.channel.send(new TextEncoder().encode(JSON.stringify(envelope)));
    }) as CancellablePromise<unknown>;
    promise.cancel = () => {
      const p = this.pending.get(id);
      if (!p) return;
      this.pending.delete(id);
      // conn.go's cancellation shape: task.cancel with no envelope id,
      // naming the target request's id in params. For run.attach this
      // detaches without stopping the run.
      if (!this.closed) {
        this.channel.send(new TextEncoder().encode(JSON.stringify({ method: 'task.cancel', params: { id } })));
      }
      p.reject(new Error('relay: call cancelled'));
    };
    return promise;
  }

  /**
   * Registers onEvent for the given topics (via events.subscribe) and
   * returns its unsubscribe function, which removes the callbacks and
   * releases the topics no other subscriber wants (via
   * events.unsubscribe). Pushed eventNotification messages are dispatched
   * by topic to every registered callback.
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

  /** Tears down the stream; pending and future call()s reject, and topic callbacks stop. Idempotent. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    const err = new Error('relay: connection is closed');
    for (const p of this.pending.values()) p.reject(err);
    this.pending.clear();
    this.topicCallbacks.clear();
    this.stream.close();
  }

  private fireSubscriptionRPC(method: string, topics: string[]): void {
    if (this.closed) return;
    this.call(method, { topics }).catch(() => {
      // A failed subscribe just means this callback never fires; an
      // unsubscribe that races a closed connection is equally ignorable.
    });
  }

  private runReceiveLoop(): void {
    void (async () => {
      const decoder = new TextDecoder();
      try {
        for (;;) {
          const bytes = await this.channel.receive();
          try {
            this.handleEnvelope(JSON.parse(decoder.decode(bytes)));
          } catch {
            // Malformed JSON: skip the message, mirroring conn.go's serve.
          }
        }
      } catch (err) {
        this.failAllPending(err instanceof Error ? err : new Error(String(err)));
      }
    })();
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

  private failAllPending(err: Error): void {
    this.closed = true;
    for (const p of this.pending.values()) p.reject(err);
    this.pending.clear();
    this.topicCallbacks.clear();
  }
}
