// fakeV2Daemon.ts is test-only: a minimal E2EE v2 RESPONDER (the daemon end of
// handshake.go's handshakeDaemon) and in-memory transports, so the client
// Channel can be tested against something that speaks the wire protocol. The
// production module has no daemon role; this lives next to the tests that need
// one (the vector negatives for the daemon endpoint, fakeRelay, the harness).
// It shares the pure primitives with the client (deriveSchedule, dh, ...), so
// the cross-language vectors, not this file, are what pin the schedule.

import { x25519 } from '@noble/curves/ed25519.js';
import {
  Channel,
  checkReady,
  concatBytes,
  confirmTag,
  constantTimeEqual,
  deriveSchedule,
  dh,
  E2EEError,
  encodeFrame,
  FRAME_DATA,
  FRAME_HELLO,
  FRAME_READY,
  FrameReader,
  MAX_FRAME_LEN,
  MAX_HANDSHAKE_FRAME_LEN,
  parseClientHello,
  PROTOCOL_VERSION,
  RawTransport,
  Role,
  Session,
} from '../e2ee';

/** A RawTransport that can be ended, with a record of everything it sent. */
export interface MemTransport extends RawTransport {
  /** Ends the stream: a pending and every later receiveChunk rejects. */
  close(): void;
  /** What this end sent, one entry per send(). */
  readonly sent: Uint8Array[];
}

class MemEnd implements MemTransport {
  readonly sent: Uint8Array[] = [];
  private inbox: Uint8Array[] = [];
  private waiter: { resolve: (c: Uint8Array) => void; reject: (e: Error) => void } | null = null;
  private ended = false;
  peer!: MemEnd;

  send(bytes: Uint8Array): void {
    if (this.ended) throw new Error('mem transport: closed');
    this.sent.push(bytes);
    this.peer.deliver(bytes);
  }

  deliver(bytes: Uint8Array): void {
    const w = this.waiter;
    if (w) {
      this.waiter = null;
      w.resolve(bytes);
    } else this.inbox.push(bytes);
  }

  receiveChunk(): Promise<Uint8Array> {
    const next = this.inbox.shift();
    if (next) return Promise.resolve(next);
    if (this.ended) return Promise.reject(new Error('mem transport: stream ended'));
    return new Promise((resolve, reject) => (this.waiter = { resolve, reject }));
  }

  close(): void {
    this.ended = true;
    const w = this.waiter;
    this.waiter = null;
    w?.reject(new Error('mem transport: stream ended'));
    this.peer.ended = true;
    const pw = this.peer.waiter;
    this.peer.waiter = null;
    pw?.reject(new Error('mem transport: stream ended'));
  }
}

/** Two connected in-memory transports: what one sends the other receives. */
export function memPipe(): [MemTransport, MemTransport] {
  const a = new MemEnd();
  const b = new MemEnd();
  a.peer = b;
  b.peer = a;
  return [a, b];
}

/**
 * The end under test, fed from a script: it receives `frames` as-is, in order,
 * and records what the end wrote. When the script is exhausted the stream ends
 * (closeAfter) or stays silent forever, as a peer that stopped talking would.
 */
export class ScriptedTransport implements MemTransport {
  readonly sent: Uint8Array[] = [];
  private frames: Uint8Array[];
  constructor(
    frames: Uint8Array[],
    private readonly closeAfter: boolean,
  ) {
    this.frames = [...frames];
  }
  send(bytes: Uint8Array): void {
    this.sent.push(bytes);
  }
  receiveChunk(): Promise<Uint8Array> {
    const next = this.frames.shift();
    if (next) return Promise.resolve(next);
    if (this.closeAfter) return Promise.reject(new Error('scripted transport: stream ended'));
    return new Promise(() => {});
  }
  close(): void {}
}

/** The kind of each frame in `sent` ("hello", "ready", "data"), as the vector file's expect_written names them. */
export function frameKinds(sent: Uint8Array[]): string[] {
  return sent.map((f) => ({ [FRAME_HELLO]: 'hello', [FRAME_READY]: 'ready', [FRAME_DATA]: 'data' })[f[4]] ?? `type${f[4]}`);
}

export interface DaemonKeys {
  secretKey: Uint8Array;
  publicKey: Uint8Array;
}

/** A static daemon key from a 32-byte seed (or a random one). */
export function daemonKeysFrom(seed: Uint8Array = x25519.utils.randomSecretKey()): DaemonKeys {
  return { secretKey: seed, publicKey: x25519.getPublicKey(seed) };
}

/**
 * The responder: read M1, send M2 and R_d, wait for a valid R_c, then speak
 * data frames. A fresh ephemeral key per handshake (ephemeralSeed pins it for
 * vectors). A byte-identical repeat of M1 re-sends the stored replies.
 */
export class FakeV2Daemon {
  private readonly reader = new FrameReader();
  private session: Session | null = null;
  private replyHello: Uint8Array | null = null;
  private replyReady: Uint8Array | null = null;
  private firstHello: Uint8Array | null = null;
  /** The client ephemeral and daemon ephemeral of the last handshake. */
  clientEphPub: Uint8Array | null = null;
  daemonEphPub: Uint8Array | null = null;
  th: Uint8Array | null = null;

  constructor(
    private readonly transport: RawTransport,
    readonly keys: DaemonKeys,
    private readonly ephemeralSeed?: Uint8Array,
  ) {}

  private async readFrame(limit: number): Promise<{ type: number; payload: Uint8Array }> {
    for (;;) {
      const f = this.reader.tryRead(limit);
      if (f) return f;
      let chunk: Uint8Array;
      try {
        chunk = await this.transport.receiveChunk();
      } catch (e) {
        if (this.reader.pending) throw new E2EEError('protocol', 'e2ee: protocol violation: truncated frame');
        throw e;
      }
      this.reader.push(chunk);
    }
  }

  async handshake(): Promise<void> {
    const m1 = await this.readFrame(MAX_HANDSHAKE_FRAME_LEN);
    if (m1.type !== FRAME_HELLO) throw new E2EEError('protocol', `e2ee: protocol violation: expected hello, got frame type 0x${m1.type.toString(16)}`);
    const clientEphPub = parseClientHello(m1.payload);

    const eph = this.ephemeralSeed ? this.ephemeralSeed.slice() : x25519.utils.randomSecretKey();
    const daemonEphPub = x25519.getPublicKey(eph);
    const es = dh(this.keys.secretKey, clientEphPub);
    const ee = dh(eph, clientEphPub);
    const sched = deriveSchedule(es, ee, this.keys.publicKey, daemonEphPub, clientEphPub);
    const replyHello = concatBytes(new Uint8Array([PROTOCOL_VERSION, Role.Daemon]), this.keys.publicKey, daemonEphPub);
    const replyReady = confirmTag(sched.confirmD, sched.th);
    const wantReady = confirmTag(sched.confirmM, sched.th);

    this.transport.send(encodeFrame(FRAME_HELLO, replyHello));
    this.transport.send(encodeFrame(FRAME_READY, replyReady));

    for (;;) {
      const f = await this.readFrame(MAX_HANDSHAKE_FRAME_LEN);
      if (f.type === FRAME_READY) {
        checkReady(f.payload, wantReady);
        break;
      }
      if (f.type === FRAME_HELLO) {
        parseClientHello(f.payload);
        if (!constantTimeEqual(f.payload, m1.payload)) throw new E2EEError('key_rotation', 'e2ee: peer re-handshaked with a different key');
        this.transport.send(encodeFrame(FRAME_HELLO, replyHello));
        this.transport.send(encodeFrame(FRAME_READY, replyReady));
        continue;
      }
      throw new E2EEError('protocol', `e2ee: protocol violation: expected ready, got frame type 0x${f.type.toString(16)}`);
    }
    this.session = Session.fromSchedule(Role.Daemon, sched);
    this.clientEphPub = clientEphPub;
    this.daemonEphPub = daemonEphPub;
    this.th = sched.th;
    this.firstHello = m1.payload;
    this.replyHello = replyHello;
    this.replyReady = replyReady;
  }

  send(message: Uint8Array): void {
    if (!this.session) throw new Error('fake daemon: handshake not completed');
    const { counter, ciphertext } = this.session.seal(message);
    const payload = new Uint8Array(8 + ciphertext.length);
    new DataView(payload.buffer).setUint32(4, counter, false);
    payload.set(ciphertext, 8);
    this.transport.send(encodeFrame(FRAME_DATA, payload));
  }

  async receive(): Promise<Uint8Array> {
    if (!this.session) throw new Error('fake daemon: handshake not completed');
    for (;;) {
      const f = await this.readFrame(MAX_FRAME_LEN);
      if (f.type === FRAME_DATA) {
        const view = new DataView(f.payload.buffer, f.payload.byteOffset, 8);
        return this.session.open(view.getUint32(4, false), f.payload.slice(8));
      }
      if (f.type === FRAME_HELLO) {
        // The client never saw our reply: send the stored M2 and R_d again.
        if (!constantTimeEqual(f.payload, this.firstHello!)) throw new E2EEError('key_rotation', 'e2ee: peer re-handshaked with a different key');
        this.transport.send(encodeFrame(FRAME_HELLO, this.replyHello!));
        this.transport.send(encodeFrame(FRAME_READY, this.replyReady!));
      }
    }
  }
}

/** A client Channel and a FakeV2Daemon connected back to back (nothing handshaked yet). */
export function channelPair(daemonKeys: DaemonKeys = daemonKeysFrom()): { channel: Channel; daemon: FakeV2Daemon; client: MemTransport; server: MemTransport } {
  const [client, server] = memPipe();
  return { channel: new Channel(client), daemon: new FakeV2Daemon(server, daemonKeys), client, server };
}
