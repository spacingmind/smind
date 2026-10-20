// e2ee.ts is the TypeScript mirror of internal/relay/e2ee's handshake.go +
// session.go for E2EE v2 (ADR-0035): a Noise-NK-shaped handshake with a fresh
// ephemeral key on BOTH ends, a transcript hash, and key confirmation, then
// ChaCha20-Poly1305 data frames. It is the initiator (client) end only: the
// daemon is Go, and no v1 code exists here. __tests__/e2ee.test.ts checks this
// module against the Go-generated vectors (internal/relay/e2ee/testdata/
// e2ee_v2_vectors.json) and runs every negative vector.
//
// Pure JS crypto (@noble/curves, @noble/hashes, @noble/ciphers): no native
// crypto module, so this runs the same way under Hermes as any other JS.

import { x25519 } from '@noble/curves/ed25519.js';
import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';

/** The E2EE wire protocol version (handshake.go ProtocolVersion). No negotiation, no older version. */
export const PROTOCOL_VERSION = 2;
const PUBLIC_KEY_SIZE = 32;
const CLIENT_HELLO_LEN = 1 + 1 + PUBLIC_KEY_SIZE;
const DAEMON_HELLO_LEN = 1 + 1 + PUBLIC_KEY_SIZE + PUBLIC_KEY_SIZE;
const READY_LEN = 32;

/** Bound on one frame (type + payload) in an established session (handshake.go maxFrameLen). */
export const MAX_FRAME_LEN = 1 << 20;
/** Bound on one frame while the handshake runs: the largest legitimate one is M2 (1 + 66). */
export const MAX_HANDSHAKE_FRAME_LEN = 128;

export const FRAME_HELLO = 0x01;
export const FRAME_READY = 0x02;
export const FRAME_DATA = 0x03;

export const Role = {
  Daemon: 1,
  Mobile: 2,
} as const;
export type RoleValue = (typeof Role)[keyof typeof Role];

const LABEL = 'smind relay e2ee v2';
const DIR_DAEMON_TO_MOBILE = `${LABEL} daemon->mobile`;
const DIR_MOBILE_TO_DAEMON = `${LABEL} mobile->daemon`;
const CONFIRM_DAEMON = `${LABEL} confirm daemon`;
const CONFIRM_MOBILE = `${LABEL} confirm mobile`;

const textEncoder = new TextEncoder();

/** The error kinds of the vector file's `expect` field (plus the session-level ones). */
export type E2EEErrorKind =
  | 'protocol'
  | 'key_rotation'
  | 'peer_key_mismatch'
  | 'handshake_confirm'
  | 'key_agreement'
  | 'unsupported_version'
  | 'not_established'
  | 'already_established'
  | 'handshake_in_progress'
  | 'closed'
  | 'replay'
  | 'decrypt'
  | 'counter_exhausted';

/** Every error this module throws, tagged with its kind (handshake.go's Err* sentinels). */
export class E2EEError extends Error {
  readonly kind: E2EEErrorKind;
  constructor(kind: E2EEErrorKind, message: string) {
    super(message);
    this.name = 'E2EEError';
    this.kind = kind;
  }
}

/**
 * The peer's hello carried another protocol version. `peer` is its version:
 * below PROTOCOL_VERSION means "update the daemon", above means "update this app".
 */
export class UnsupportedVersionError extends E2EEError {
  readonly peer: number;
  constructor(peer: number) {
    super('unsupported_version', `e2ee: peer speaks protocol version ${peer}, this end speaks ${PROTOCOL_VERSION}`);
    this.name = 'UnsupportedVersionError';
    this.peer = peer;
  }
}

/** The daemon's static key is not the one pinned from the pairing offer. */
export class PeerKeyMismatchError extends E2EEError {
  constructor() {
    super('peer_key_mismatch', 'e2ee: peer public key does not match the pinned key');
    this.name = 'PeerKeyMismatchError';
  }
}

const protocolError = (message: string) => new E2EEError('protocol', `e2ee: protocol violation: ${message}`);

export function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

/** Comparison whose running time does not depend on where the inputs differ (there is no timingSafeEqual under Hermes). Lengths are public. */
export function constantTimeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}

/** handshake.go's TranscriptHash: SHA-256(label ‖ daemon_pub ‖ daemon_eph_pub ‖ client_eph_pub). Public data. */
export function transcriptHash(daemonPub: Uint8Array, daemonEphPub: Uint8Array, clientEphPub: Uint8Array): Uint8Array {
  return sha256(concatBytes(textEncoder.encode(LABEL), daemonPub, daemonEphPub, clientEphPub));
}

/** Everything the key schedule derives from one handshake; only th is public. */
export interface Schedule {
  th: Uint8Array;
  d2m: Uint8Array;
  m2d: Uint8Array;
  confirmD: Uint8Array;
  confirmM: Uint8Array;
}

/** ADR-0035 §2: ikm = es ‖ ee, salt = th, one HKDF-SHA256 expansion per purpose. */
export function deriveSchedule(es: Uint8Array, ee: Uint8Array, daemonPub: Uint8Array, daemonEphPub: Uint8Array, clientEphPub: Uint8Array): Schedule {
  const th = transcriptHash(daemonPub, daemonEphPub, clientEphPub);
  const ikm = concatBytes(es, ee);
  try {
    const expand = (info: string) => hkdf(sha256, ikm, th, textEncoder.encode(info), 32);
    return {
      th,
      d2m: expand(DIR_DAEMON_TO_MOBILE),
      m2d: expand(DIR_MOBILE_TO_DAEMON),
      confirmD: expand(CONFIRM_DAEMON),
      confirmM: expand(CONFIRM_MOBILE),
    };
  } finally {
    ikm.fill(0);
  }
}

/** The key-confirmation MAC: HMAC-SHA256(confirmKey, th). */
export function confirmTag(confirmKey: Uint8Array, th: Uint8Array): Uint8Array {
  return hmac(sha256, confirmKey, th);
}

/**
 * X25519 with the RFC 7748 §6.1 all-zero-output check made explicit: whatever
 * the library does with a low-order point, an all-zero result aborts.
 */
export function dh(secretKey: Uint8Array, peerPublicKey: Uint8Array): Uint8Array {
  if (peerPublicKey.length !== PUBLIC_KEY_SIZE) throw protocolError(`public key is ${peerPublicKey.length} bytes, want ${PUBLIC_KEY_SIZE}`);
  let shared: Uint8Array;
  try {
    shared = x25519.getSharedSecret(secretKey, peerPublicKey);
  } catch (e) {
    throw new E2EEError('key_agreement', `e2ee: X25519 key agreement failed: ${e instanceof Error ? e.message : String(e)}`);
  }
  let acc = 0;
  for (const b of shared) acc |= b;
  if (acc === 0) throw new E2EEError('key_agreement', 'e2ee: X25519 key agreement failed: all-zero output');
  return shared;
}

function wipe(schedule: Schedule): void {
  schedule.d2m.fill(0);
  schedule.m2d.fill(0);
  schedule.confirmD.fill(0);
  schedule.confirmM.fill(0);
}

/**
 * Session holds the two directional ChaCha20-Poly1305 keys and per-direction
 * counters -- the TypeScript mirror of session.go's Session. Nonces are 4 zero
 * bytes ‖ u64BE(counter); keys are unique per handshake, so counters restarting
 * at zero never repeats a (key, nonce).
 */
export class Session {
  private readonly sendKey: Uint8Array;
  private readonly recvKey: Uint8Array;
  private sendCounter = 0;
  private recvCounter = 0;

  constructor(sendKey: Uint8Array, recvKey: Uint8Array) {
    this.sendKey = sendKey;
    this.recvKey = recvKey;
  }

  /** The session of `role` over a schedule (session.go newSession). */
  static fromSchedule(role: RoleValue, sched: Schedule): Session {
    const [d2m, m2d] = [sched.d2m.slice(), sched.m2d.slice()];
    return role === Role.Mobile ? new Session(m2d, d2m) : new Session(d2m, m2d);
  }

  /** Renders a frame counter as a 12-byte nonce: 4 zero bytes, then the counter big-endian. Counters stay below 2^32 (see seal), so the high word is zero. */
  private static nonceFor(counter: number): Uint8Array {
    const nonce = new Uint8Array(12);
    new DataView(nonce.buffer).setUint32(8, counter, false);
    return nonce;
  }

  seal(plaintext: Uint8Array): { counter: number; ciphertext: Uint8Array } {
    // Limit: this client carries the counter in the nonce's low 32-bit word only (JS bit
    // arithmetic), so a session ends at 2^32 - 1 frames per direction, where Go/ADR-0035 allow
    // 2^64 - 1. Hitting it is fatal for the channel (Channel.send closes it) and the supervisor
    // opens a fresh session with a new handshake.
    if (this.sendCounter >= 0xffffffff) throw new E2EEError('counter_exhausted', 'e2ee: frame counter exhausted');
    const counter = this.sendCounter;
    const ciphertext = chacha20poly1305(this.sendKey, Session.nonceFor(counter)).encrypt(plaintext);
    this.sendCounter++;
    return { counter, ciphertext };
  }

  /** Opens a frame whose counter must be exactly the next one expected; a repeat or a skip is refused rather than decrypted. */
  open(counter: number, ciphertext: Uint8Array): Uint8Array {
    if (counter !== this.recvCounter) {
      throw new E2EEError('replay', `e2ee: frame counter replayed or out of order: got ${counter}, want ${this.recvCounter}`);
    }
    let plaintext: Uint8Array;
    try {
      plaintext = chacha20poly1305(this.recvKey, Session.nonceFor(counter)).decrypt(ciphertext);
    } catch (e) {
      throw new E2EEError('decrypt', `e2ee: frame failed to decrypt: counter ${counter}: ${e instanceof Error ? e.message : String(e)}`);
    }
    this.recvCounter++;
    return plaintext;
  }
}

/** One raw wire frame: 4-byte big-endian length (of type+payload), 1-byte type, payload -- writeFrame in handshake.go. */
export function encodeFrame(type: number, payload: Uint8Array): Uint8Array {
  const frame = new Uint8Array(4 + 1 + payload.length);
  new DataView(frame.buffer).setUint32(0, 1 + payload.length, false);
  frame[4] = type;
  frame.set(payload, 5);
  return frame;
}

export interface DecodedFrame {
  type: number;
  payload: Uint8Array;
}

/**
 * FrameReader reassembles e2ee wire frames from a byte stream that may
 * arrive in arbitrary chunk sizes -- the TypeScript equivalent of
 * handshake.go's bufio.Reader-backed readFrame, needed because the
 * underlying transport (a grpc-web OpenData stream's Frame.payload
 * chunks) delivers bytes as discrete messages, not a continuous stream.
 */
export class FrameReader {
  private buffer: Uint8Array = new Uint8Array(0);

  push(chunk: Uint8Array): void {
    this.buffer = concatBytes(this.buffer, chunk);
  }

  /** Drops any partially received frame (the rest of it died with its stream). */
  clear(): void {
    this.buffer = new Uint8Array(0);
  }

  /** True while bytes of an incomplete frame are buffered (a stream that ends now was cut mid-frame). */
  get pending(): boolean {
    return this.buffer.length > 0;
  }

  /**
   * Returns the next complete frame, or null if more bytes are needed. `limit`
   * bounds the length prefix (type + payload) and is checked as soon as the
   * header is in, before the body is waited for or buffered.
   */
  tryRead(limit: number = MAX_FRAME_LEN): DecodedFrame | null {
    if (this.buffer.length < 4) return null;
    const length = new DataView(this.buffer.buffer, this.buffer.byteOffset, 4).getUint32(0, false);
    if (length === 0) throw protocolError('zero-length frame');
    if (length > limit) throw protocolError(`frame length ${length} exceeds the ${limit} byte limit`);
    if (this.buffer.length < 4 + length) return null;
    const type = this.buffer[4];
    const payload = this.buffer.slice(5, 4 + length);
    this.buffer = this.buffer.slice(4 + length);
    return { type, payload };
  }
}

/** Everything a Channel needs from its underlying connection: send raw bytes, receive raw byte chunks. */
export interface RawTransport {
  send(bytes: Uint8Array): void;
  /** Resolves with the next chunk of bytes the peer sent, or rejects if the transport ends. */
  receiveChunk(): Promise<Uint8Array>;
}

/** The version byte is read first, before length or role, so a v1 hello is reported as such whatever its shape. */
function parseVersion(payload: Uint8Array): void {
  if (payload.length === 0) throw protocolError('empty hello');
  if (payload[0] !== PROTOCOL_VERSION) throw new UnsupportedVersionError(payload[0]);
}

/** Validates M2 (`02 01 ‖ daemon_pub ‖ daemon_eph`) and returns the two public keys. */
export function parseDaemonHello(payload: Uint8Array): { daemonPub: Uint8Array; daemonEphPub: Uint8Array } {
  parseVersion(payload);
  if (payload.length !== DAEMON_HELLO_LEN) throw protocolError(`daemon hello is ${payload.length} bytes, want ${DAEMON_HELLO_LEN}`);
  if (payload[1] !== Role.Daemon) throw protocolError(`daemon hello carries role ${payload[1]}, want ${Role.Daemon}`);
  return { daemonPub: payload.slice(2, 2 + PUBLIC_KEY_SIZE), daemonEphPub: payload.slice(2 + PUBLIC_KEY_SIZE) };
}

/** Validates M1 (`02 02 ‖ client_eph`) and returns the client ephemeral public key (the daemon's check, used by the test responder). */
export function parseClientHello(payload: Uint8Array): Uint8Array {
  parseVersion(payload);
  if (payload.length !== CLIENT_HELLO_LEN) throw protocolError(`client hello is ${payload.length} bytes, want ${CLIENT_HELLO_LEN}`);
  if (payload[1] !== Role.Mobile) throw protocolError(`client hello carries role ${payload[1]}, want ${Role.Mobile}`);
  return payload.slice(2);
}

/** M1 for a client ephemeral public key. */
export function clientHelloPayload(clientEphPub: Uint8Array): Uint8Array {
  return concatBytes(new Uint8Array([PROTOCOL_VERSION, Role.Mobile]), clientEphPub);
}

/** A ready payload must be exactly 32 bytes and equal the expected MAC (compared in constant time); an empty ready (the retired v1 shape) is a protocol error. */
export function checkReady(payload: Uint8Array, want: Uint8Array): void {
  if (payload.length !== READY_LEN) throw protocolError(`ready is ${payload.length} bytes, want ${READY_LEN}`);
  if (!constantTimeEqual(payload, want)) throw new E2EEError('handshake_confirm', 'e2ee: handshake key confirmation failed');
}

/**
 * Channel is the initiator end of an E2EE v2 session over a RawTransport --
 * the TypeScript mirror of handshake.go's client Channel.
 *
 * Its ephemeral X25519 key is generated INSIDE handshake(): no API takes a
 * client key, so one can never be reused across sessions (ADR-0035 §3). A
 * Channel runs one handshake; a retry after a failure uses a new Channel (and
 * so a new ephemeral). Every failure is terminal: the channel is closed and
 * refuses further use.
 */
export class Channel {
  private readonly transport: RawTransport;
  private readonly reader = new FrameReader();
  // Production: a fresh random key per handshake. Tests that must reproduce the
  // vectors replace this field (it is private, not part of the API).
  private newEphemeral: () => Uint8Array = () => x25519.utils.randomSecretKey();
  private session: Session | null = null;
  private established = false;
  private closed = false;
  private handshaking = false;
  private daemonPub: Uint8Array | null = null;
  private daemonEphPub: Uint8Array | null = null;
  private clientEphPub: Uint8Array | null = null;
  private th: Uint8Array | null = null;
  // Retained for repeats on an established session (ADR-0035 §4): the daemon's
  // M2 and R_d, of which a byte-identical repeat is tolerated.
  private peerHello: Uint8Array | null = null;
  private peerReady: Uint8Array | null = null;

  constructor(transport: RawTransport) {
    this.transport = transport;
  }

  /** The handshake transcript hash th (public) once established, else null. */
  get transcriptHash(): Uint8Array | null {
    return this.ifEstablished(this.th);
  }

  /** The daemon's static public key once established. */
  get daemonPublicKey(): Uint8Array | null {
    return this.ifEstablished(this.daemonPub);
  }

  /** The daemon's per-handshake ephemeral public key once established. */
  get daemonEphemeralPublicKey(): Uint8Array | null {
    return this.ifEstablished(this.daemonEphPub);
  }

  /** This client's per-handshake ephemeral public key once established. */
  get clientEphemeralPublicKey(): Uint8Array | null {
    return this.ifEstablished(this.clientEphPub);
  }

  get isEstablished(): boolean {
    return this.established;
  }

  get isClosed(): boolean {
    return this.closed;
  }

  private ifEstablished(b: Uint8Array | null): Uint8Array | null {
    return this.established && b ? b.slice() : null;
  }

  private fail(err: unknown): never {
    this.closed = true;
    throw err;
  }

  private async readFrame(limit: number): Promise<DecodedFrame> {
    for (;;) {
      const frame = this.reader.tryRead(limit);
      if (frame) return frame;
      let chunk: Uint8Array;
      try {
        chunk = await this.transport.receiveChunk();
      } catch (e) {
        // A stream that ends in the middle of a frame is a protocol violation, not a clean end.
        if (this.reader.pending) throw protocolError('truncated frame');
        throw e;
      }
      this.reader.push(chunk);
    }
  }

  /**
   * Runs the handshake: send M1, read M2 then R_d, verify, send R_c. Resolves
   * only once key confirmation succeeded, so no application frame is sent or
   * accepted before the keys are confirmed.
   *
   * `expectedDaemonKey` pins the daemon's static public key (from the pairing
   * offer): any other key in M2 fails with PeerKeyMismatchError before any key
   * agreement. The pin fails closed: pinning an empty or malformed key can
   * never match. Omit it only to run unpinned (tests).
   *
   * A second call while one is running, after success, or after close rejects.
   */
  async handshake(expectedDaemonKey?: Uint8Array): Promise<void> {
    if (this.closed) throw new E2EEError('closed', 'e2ee: channel closed');
    if (this.established) throw new E2EEError('already_established', 'e2ee: handshake already completed');
    if (this.handshaking) throw new E2EEError('handshake_in_progress', 'e2ee: handshake already in progress');
    this.handshaking = true;
    try {
      await this.handshakeClient(expectedDaemonKey);
    } catch (e) {
      this.fail(e);
    } finally {
      this.handshaking = false;
    }
  }

  private async handshakeClient(expectedDaemonKey: Uint8Array | undefined): Promise<void> {
    const eph = this.newEphemeral();
    const clientEphPub = x25519.getPublicKey(eph);
    let es: Uint8Array | null = null;
    let ee: Uint8Array | null = null;
    let sched: Schedule | null = null;
    try {
      this.transport.send(encodeFrame(FRAME_HELLO, clientHelloPayload(clientEphPub)));

      const m2Frame = await this.readFrame(MAX_HANDSHAKE_FRAME_LEN);
      if (m2Frame.type !== FRAME_HELLO) throw protocolError(`expected hello, got frame type 0x${m2Frame.type.toString(16)}`);
      const m2 = m2Frame.payload;
      const { daemonPub, daemonEphPub } = parseDaemonHello(m2);
      // The pin is checked before any key agreement.
      if (expectedDaemonKey !== undefined && !constantTimeEqual(daemonPub, expectedDaemonKey)) throw new PeerKeyMismatchError();

      es = dh(eph, daemonPub);
      ee = dh(eph, daemonEphPub);
      sched = deriveSchedule(es, ee, daemonPub, daemonEphPub, clientEphPub);
      const wantReady = confirmTag(sched.confirmD, sched.th);
      const replyReady = confirmTag(sched.confirmM, sched.th);

      for (;;) {
        const frame = await this.readFrame(MAX_HANDSHAKE_FRAME_LEN);
        if (frame.type === FRAME_READY) {
          checkReady(frame.payload, wantReady);
          break;
        }
        if (frame.type === FRAME_HELLO) {
          this.checkRepeatedHello(frame.payload, m2);
          continue;
        }
        throw protocolError(`expected ready, got frame type 0x${frame.type.toString(16)}`);
      }

      this.transport.send(encodeFrame(FRAME_READY, replyReady));
      this.session = Session.fromSchedule(Role.Mobile, sched);
      this.daemonPub = daemonPub;
      this.daemonEphPub = daemonEphPub;
      this.clientEphPub = clientEphPub;
      this.th = sched.th;
      this.peerHello = m2;
      this.peerReady = wantReady;
      this.established = true;
    } finally {
      eph.fill(0);
      es?.fill(0);
      ee?.fill(0);
      if (sched) wipe(sched);
    }
  }

  /** ADR-0035 §4: a byte-identical repeat of the daemon's hello is a transport retry; any other valid M2 is a key rotation. A v1 or malformed repeat is reported as such. */
  private checkRepeatedHello(payload: Uint8Array, first: Uint8Array): void {
    parseDaemonHello(payload);
    if (!constantTimeEqual(payload, first)) {
      throw new E2EEError('key_rotation', 'e2ee: peer re-handshaked with a different key: rotation requires a new session, not an in-session rekey');
    }
  }

  /**
   * Prepares the channel for a new transport after a drop, keeping the
   * session (key and both counters): bytes of a frame the dead stream only
   * half delivered are discarded, as client.DataConn.Resume does on the Go
   * side. Call it only when no receive() is in flight.
   */
  resumeTransport(): void {
    if (!this.session) throw new E2EEError('not_established', 'e2ee: no established session to resume');
    this.reader.clear();
  }

  private activeSession(): Session {
    if (this.closed) throw new E2EEError('closed', 'e2ee: channel closed');
    if (!this.established || !this.session) throw new E2EEError('not_established', 'e2ee: handshake not completed');
    return this.session;
  }

  send(message: Uint8Array): void {
    const session = this.activeSession();
    let sealed: { counter: number; ciphertext: Uint8Array };
    try {
      sealed = session.seal(message);
    } catch (e) {
      if (e instanceof E2EEError && e.kind === 'counter_exhausted') this.closed = true;
      throw e;
    }
    const { counter, ciphertext } = sealed;
    const payload = new Uint8Array(8 + ciphertext.length);
    // u64BE(counter): the high word is always zero (see Session.seal).
    new DataView(payload.buffer).setUint32(4, counter, false);
    payload.set(ciphertext, 8);
    this.transport.send(encodeFrame(FRAME_DATA, payload));
  }

  /**
   * Reads the next application message. A replayed or out-of-order frame (or
   * one that fails to decrypt) throws and leaves the channel usable, as in Go;
   * a protocol violation closes it. Handshake frames on an established session
   * follow ADR-0035 §4: a byte-identical repeat of M2 or R_d is ignored, a
   * different M2 is a key rotation, a different ready fails confirmation.
   */
  async receive(): Promise<Uint8Array> {
    const session = this.activeSession();
    for (;;) {
      // A read failure does not close the channel (as in Go): the stream may just have
      // dropped, and resumeTransport() then discards the half-read frame.
      const frame = await this.readFrame(MAX_FRAME_LEN);
      if (frame.type === FRAME_DATA) {
        if (frame.payload.length < 8) this.fail(protocolError(`data frame is ${frame.payload.length} bytes, want at least 8`));
        const view = new DataView(frame.payload.buffer, frame.payload.byteOffset, 8);
        const high = view.getUint32(0, false);
        const low = view.getUint32(4, false);
        // A counter above 2^32 can never be the next one expected.
        // Beyond this client's 2^32 - 1 limit the session can only stall: fatal, so a fresh session replaces it.
        if (high !== 0) this.fail(new E2EEError('counter_exhausted', `e2ee: peer frame counter ${high} * 2^32 + ${low} is beyond this client's 2^32 - 1 limit`));
        return session.open(low, frame.payload.slice(8));
      }
      try {
        if (frame.type === FRAME_HELLO) {
          this.checkRepeatedHello(frame.payload, this.peerHello!);
        } else if (frame.type === FRAME_READY) {
          checkReady(frame.payload, this.peerReady!);
        } else {
          throw protocolError(`unknown frame type 0x${frame.type.toString(16)}`);
        }
      } catch (e) {
        this.fail(e);
      }
    }
  }
}
