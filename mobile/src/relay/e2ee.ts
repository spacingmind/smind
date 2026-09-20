// e2ee.ts is the TypeScript mirror of internal/relay/e2ee's handshake.go +
// session.go: X25519 key agreement, HKDF key derivation, and
// ChaCha20-Poly1305 AEAD framing, byte-for-byte compatible with the Go
// side (see mobile/src/relay/__tests__/e2ee.test.ts, which checks this
// module's output against fixed vectors also computed Go-side in
// internal/relay/e2ee/fixture_test.go -- per the plan's Test Scenarios,
// this is the single highest-risk file in the whole milestone).
//
// Pure JS crypto (@noble/curves, @noble/hashes, @noble/ciphers): no native
// crypto module, so this runs the same way under Hermes as any other JS.

import { x25519 } from '@noble/curves/ed25519.js';
import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';

export const PROTOCOL_VERSION = 1;
const PUBLIC_KEY_SIZE = 32;
const HELLO_PAYLOAD_LEN = 1 + 1 + PUBLIC_KEY_SIZE;

const FRAME_HELLO = 0x01;
const FRAME_READY = 0x02;
const FRAME_DATA = 0x03;

export const Role = {
  Daemon: 1,
  Mobile: 2,
} as const;
export type RoleValue = (typeof Role)[keyof typeof Role];

const HKDF_LABEL = 'smind relay e2ee v1';
const DIR_DAEMON_TO_MOBILE = `${HKDF_LABEL} daemon->mobile`;
const DIR_MOBILE_TO_DAEMON = `${HKDF_LABEL} mobile->daemon`;

const textEncoder = new TextEncoder();

export interface KeyPair {
  publicKey: Uint8Array;
  secretKey: Uint8Array;
}

/** A fresh, random per-session X25519 keypair -- what a mobile device uses (see e2ee/keypair.go's doc comment: the daemon persists one, mobile never does). */
export function generateKeyPair(): KeyPair {
  const secretKey = x25519.utils.randomSecretKey();
  return { secretKey, publicKey: x25519.getPublicKey(secretKey) };
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

/**
 * Session holds the two directional ChaCha20-Poly1305 keys and per-
 * direction counters -- the exact TypeScript mirror of session.go's
 * Session/newSession/deriveKeys/Seal/Open/counterNonce.
 */
export class Session {
  private sendKey: Uint8Array;
  private recvKey: Uint8Array;
  private sendCounter = 0;
  private recvCounter = 0;

  private constructor(sendKey: Uint8Array, recvKey: Uint8Array) {
    this.sendKey = sendKey;
    this.recvKey = recvKey;
  }

  static derive(privateKey: Uint8Array, peerPublicKey: Uint8Array, role: RoleValue, daemonPub: Uint8Array, mobilePub: Uint8Array): Session {
    const shared = x25519.getSharedSecret(privateKey, peerPublicKey);

    // salt = SHA256(label || daemonPub || mobilePub), always daemon-then-
    // mobile regardless of which end is deriving -- session.go's newSession.
    const salt = sha256(concatBytes(textEncoder.encode(HKDF_LABEL), daemonPub, mobilePub));

    const daemonToMobile = hkdf(sha256, shared, salt, textEncoder.encode(DIR_DAEMON_TO_MOBILE), 32);
    const mobileToDaemon = hkdf(sha256, shared, salt, textEncoder.encode(DIR_MOBILE_TO_DAEMON), 32);

    return role === Role.Mobile ? new Session(mobileToDaemon, daemonToMobile) : new Session(daemonToMobile, mobileToDaemon);
  }

  /** Renders a frame counter as a 12-byte nonce: 4 zero bytes, then the counter big-endian -- counterNonce in session.go. */
  private static nonceFor(counter: number): Uint8Array {
    const nonce = new Uint8Array(12);
    const view = new DataView(nonce.buffer);
    // Counters here never exceed 2^32 in this milestone's single-request
    // proof; session.go's real limit is 2^64, tracked as a known gap if
    // this module is ever reused for a long-lived connection.
    view.setUint32(8, counter, false);
    return nonce;
  }

  seal(plaintext: Uint8Array): { counter: number; ciphertext: Uint8Array } {
    const counter = this.sendCounter;
    const cipher = chacha20poly1305(this.sendKey, Session.nonceFor(counter));
    const ciphertext = cipher.encrypt(plaintext);
    this.sendCounter++;
    return { counter, ciphertext };
  }

  open(counter: number, ciphertext: Uint8Array): Uint8Array {
    if (counter !== this.recvCounter) {
      throw new Error(`e2ee: frame counter replayed or out of order: got ${counter}, want ${this.recvCounter}`);
    }
    const cipher = chacha20poly1305(this.recvKey, Session.nonceFor(counter));
    const plaintext = cipher.decrypt(ciphertext);
    this.recvCounter++;
    return plaintext;
  }
}

/** One raw wire frame: 4-byte big-endian length (of type+payload), 1-byte type, payload -- writeFrame/readFrame in handshake.go. */
export function encodeFrame(type: number, payload: Uint8Array): Uint8Array {
  const frame = new Uint8Array(4 + 1 + payload.length);
  new DataView(frame.buffer).setUint32(0, 1 + payload.length, false);
  frame[4] = type;
  frame.set(payload, 5);
  return frame;
}

interface DecodedFrame {
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

  /** Returns the next complete frame, or null if more bytes are needed. */
  tryRead(): DecodedFrame | null {
    if (this.buffer.length < 4) return null;
    const length = new DataView(this.buffer.buffer, this.buffer.byteOffset, 4).getUint32(0, false);
    if (length === 0) throw new Error('e2ee: zero-length frame');
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

function parseHello(payload: Uint8Array, ownRole: RoleValue): { peerPublicKey: Uint8Array } {
  if (payload.length !== HELLO_PAYLOAD_LEN) {
    throw new Error(`e2ee: hello is ${payload.length} bytes, want ${HELLO_PAYLOAD_LEN}`);
  }
  const version = payload[0];
  if (version !== PROTOCOL_VERSION) {
    throw new Error(`e2ee: peer protocol version ${version}, want ${PROTOCOL_VERSION}`);
  }
  const peerRole = payload[1];
  if (peerRole !== Role.Daemon && peerRole !== Role.Mobile) {
    throw new Error(`e2ee: peer sent unknown role ${peerRole}`);
  }
  if (peerRole === ownRole) {
    throw new Error(`e2ee: peer claims the same role (${peerRole}) as this end`);
  }
  return { peerPublicKey: payload.slice(2) };
}

/**
 * Channel is one end of an E2EE session over a RawTransport -- the
 * TypeScript mirror of handshake.go's Channel. It supports exactly the
 * operations Item 3's mobile client needs: Handshake, then one Send and
 * one Receive (there is no long-lived Resume/reconnect story here, unlike
 * the Go daemon-side client -- Milestone 1's proof-of-life is a single
 * request/response).
 */
export class Channel {
  private readonly transport: RawTransport;
  private readonly keyPair: KeyPair;
  private readonly role: RoleValue;
  private readonly reader = new FrameReader();
  private session: Session | null = null;
  private peerPublicKey: Uint8Array | null = null;

  constructor(transport: RawTransport, keyPair: KeyPair, role: RoleValue) {
    this.transport = transport;
    this.keyPair = keyPair;
    this.role = role;
  }

  private writeHello(): void {
    const payload = concatBytes(new Uint8Array([PROTOCOL_VERSION, this.role]), this.keyPair.publicKey);
    this.transport.send(encodeFrame(FRAME_HELLO, payload));
  }

  private async readFrame(): Promise<DecodedFrame> {
    for (;;) {
      const frame = this.reader.tryRead();
      if (frame) return frame;
      this.reader.push(await this.transport.receiveChunk());
    }
  }

  /** Runs the X25519 handshake: send hello, read peer hello, derive keys, exchange ready frames. */
  async handshake(daemonPublicKeyIfMobile?: Uint8Array): Promise<void> {
    this.writeHello();

    const helloFrame = await this.readFrame();
    if (helloFrame.type !== FRAME_HELLO) {
      throw new Error(`e2ee: expected hello, got frame type 0x${helloFrame.type.toString(16)}`);
    }
    const { peerPublicKey } = parseHello(helloFrame.payload, this.role);
    this.peerPublicKey = peerPublicKey;

    // A mobile client that already knows the daemon's public key (from
    // the pairing offer) should refuse to complete a handshake with a
    // different one -- this is the mobile-side analogue of ADR-0007 (e)'s
    // "peer changed keys" rejection, applied to the very first handshake
    // rather than a re-handshake.
    if (daemonPublicKeyIfMobile && !bytesEqual(peerPublicKey, daemonPublicKeyIfMobile)) {
      throw new Error('e2ee: daemon public key does not match the pairing offer');
    }

    const daemonPub = this.role === Role.Mobile ? peerPublicKey : this.keyPair.publicKey;
    const mobilePub = this.role === Role.Mobile ? this.keyPair.publicKey : peerPublicKey;
    this.session = Session.derive(this.keyPair.secretKey, peerPublicKey, this.role, daemonPub, mobilePub);

    this.transport.send(encodeFrame(FRAME_READY, new Uint8Array(0)));

    for (;;) {
      const frame = await this.readFrame();
      if (frame.type === FRAME_READY) return;
      if (frame.type === FRAME_HELLO) {
        const retry = parseHello(frame.payload, this.role);
        if (!bytesEqual(retry.peerPublicKey, peerPublicKey)) {
          throw new Error('e2ee: peer changed keys mid-handshake');
        }
        this.transport.send(encodeFrame(FRAME_READY, new Uint8Array(0)));
        continue;
      }
      throw new Error(`e2ee: expected ready, got frame type 0x${frame.type.toString(16)}`);
    }
  }

  send(message: Uint8Array): void {
    if (!this.session) throw new Error('e2ee: handshake not completed');
    const { counter, ciphertext } = this.session.seal(message);
    const payload = new Uint8Array(8 + ciphertext.length);
    new DataView(payload.buffer).setUint32(0, 0, false); // high 32 bits of the counter, always 0 in this milestone's single-request use.
    new DataView(payload.buffer).setUint32(4, counter, false);
    payload.set(ciphertext, 8);
    this.transport.send(encodeFrame(FRAME_DATA, payload));
  }

  async receive(): Promise<Uint8Array> {
    if (!this.session) throw new Error('e2ee: handshake not completed');
    for (;;) {
      const frame = await this.readFrame();
      if (frame.type === FRAME_DATA) {
        if (frame.payload.length < 8) {
          throw new Error(`e2ee: data frame is ${frame.payload.length} bytes, want at least 8`);
        }
        const counter = new DataView(frame.payload.buffer, frame.payload.byteOffset, 8).getUint32(4, false);
        return this.session.open(counter, frame.payload.slice(8));
      }
      if (frame.type === FRAME_READY) continue; // a duplicate ready is harmless.
      if (frame.type === FRAME_HELLO) {
        const retry = parseHello(frame.payload, this.role);
        if (!this.peerPublicKey || !bytesEqual(retry.peerPublicKey, this.peerPublicKey)) {
          throw new Error('e2ee: peer re-handshaked with a different key');
        }
        this.transport.send(encodeFrame(FRAME_READY, new Uint8Array(0)));
        continue;
      }
      throw new Error(`e2ee: unknown frame type 0x${frame.type.toString(16)}`);
    }
  }
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
