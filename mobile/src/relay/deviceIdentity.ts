// deviceIdentity.ts is the mobile half of ADR-0031 D9's device identity
// (decision 35): a persistent Ed25519 key, the self-authenticating device id
// derived from it, and the signed relay.device_hello (version 2) the daemon
// checks inside the encrypted channel. It mirrors internal/relay/devices/
// identity.go and is pinned to it by the shared cross-language vector
// (internal/relay/devices/testdata/device_hello_vector.json, see
// __tests__/deviceIdentity.test.ts).
//
// Version 2: the daemon speaks first on a device route, with a fresh
// 32-byte challenge (relay.device_challenge), and the hello's signature covers
// that challenge and the route's session id as well as the handshake
// transcript th (ADR-0035: it includes the daemon's fresh ephemeral key, and is
// taken from the established E2EE channel, never recomputed here). A client
// never sends a version-1 hello.
//
// Pure JS crypto (@noble) and no React Native imports, so it runs under
// vitest/node; where the key lives is the DeviceKeyStore's business
// (deviceKeyStore.ts wires the platform secure store).

import { ed25519 } from '@noble/curves/ed25519.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { base64Decode, base64Encode } from './base64';

/** Method name of the first application message (devices.HelloMethod). */
export const HELLO_METHOD = 'relay.device_hello';
/** Event name of the daemon's first message on a device route (devices.ChallengeMethod). */
export const CHALLENGE_EVENT = 'relay.device_challenge';
/** The only hello format the daemon accepts (devices.HelloVersion). */
export const HELLO_VERSION = 2;
/** Length of the daemon's challenge (devices.ChallengeLen). */
export const CHALLENGE_LEN = 32;

const HELLO_DOMAIN = 'smind-device-hello-v2';
const MAX_SESSION_ID_BYTES = 128;
const TRANSCRIPT_HASH_LEN = 32;
const DEVICE_ID_LEN = 26;
const MAX_LABEL_BYTES = 64;
const SEED_LEN = 32;
const BASE32 = 'abcdefghijklmnopqrstuvwxyz234567';

const textEncoder = new TextEncoder();

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

/** Lowercase, unpadded RFC 4648 base32. */
function base32Lower(bytes: Uint8Array): string {
  let out = '';
  let buffer = 0;
  let bits = 0;
  for (const b of bytes) {
    buffer = (buffer << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += BASE32[(buffer >> (bits - 5)) & 31];
      bits -= 5;
    }
    buffer &= (1 << bits) - 1;
  }
  if (bits > 0) out += BASE32[(buffer << (5 - bits)) & 31];
  return out;
}

/** devices.DeviceID: base32(SHA-256(pub)) lowercase unpadded, first 26 characters. */
export function deviceIdFor(publicKey: Uint8Array): string {
  return base32Lower(sha256(publicKey)).slice(0, DEVICE_ID_LEN);
}

/**
 * devices.HelloSignedBytes (version 2):
 *   "smind-device-hello-v2" || th || daemonPub
 *   || uint16BE(len(sessionId)) || sessionId || challenge
 * th is the established channel's transcript hash (Channel.transcriptHash),
 * sessionId the exact bytes of the Frame.session_id the data stream was opened
 * with, challenge the 32 bytes the daemon sent on THIS session.
 */
export function helloSignedBytes(th: Uint8Array, daemonPub: Uint8Array, sessionId: Uint8Array, challenge: Uint8Array): Uint8Array {
  if (th.length !== TRANSCRIPT_HASH_LEN) throw new Error(`device hello: transcript hash is ${th.length} bytes, want ${TRANSCRIPT_HASH_LEN}`);
  if (sessionId.length > MAX_SESSION_ID_BYTES) throw new Error('device hello: session id too long to sign');
  if (challenge.length !== CHALLENGE_LEN) throw new Error(`device hello: challenge is ${challenge.length} bytes, want ${CHALLENGE_LEN}`);
  const len = new Uint8Array(2);
  new DataView(len.buffer).setUint16(0, sessionId.length, false);
  return concatBytes(textEncoder.encode(HELLO_DOMAIN), th, daemonPub, len, sessionId, challenge);
}

/** The daemon speaks a different hello version (or sent a malformed challenge): this client cannot identify to it. */
export class ChallengeVersionError extends Error {}

/**
 * Reads the daemon's first message on a device route.
 *   - null: it is not the challenge event (some other message).
 *   - the 32 challenge bytes when it is, with version 2.
 *   - throws ChallengeVersionError for a challenge of another version or one
 *     that is not 32 bytes of standard base64.
 */
export function parseChallengeMessage(message: Uint8Array): Uint8Array | null {
  let env: { event?: unknown; params?: { v?: unknown; challenge?: unknown } };
  try {
    env = JSON.parse(new TextDecoder().decode(message));
  } catch {
    return null;
  }
  if (!env || env.event !== CHALLENGE_EVENT) return null;
  if (env.params?.v !== HELLO_VERSION) {
    throw new ChallengeVersionError(`the daemon sent a device challenge of version ${String(env.params?.v)}, this app speaks ${HELLO_VERSION}`);
  }
  const text = env.params.challenge;
  let challenge: Uint8Array;
  try {
    if (typeof text !== 'string') throw new Error('not a string');
    challenge = base64Decode(text);
    if (base64Encode(challenge) !== text) throw new Error('not canonical standard base64');
  } catch {
    throw new ChallengeVersionError('the daemon sent a malformed device challenge');
  }
  if (challenge.length !== CHALLENGE_LEN) {
    throw new ChallengeVersionError(`the daemon sent a ${challenge.length}-byte device challenge, want ${CHALLENGE_LEN}`);
  }
  return challenge;
}

/**
 * A device's persistent identity. The seed is the secret: it never leaves
 * identityFromSeed's closure except into a DeviceKeyStore, and is never logged.
 */
export interface DeviceIdentity {
  /** The Ed25519 public key (32 bytes). */
  readonly publicKey: Uint8Array;
  /** The key-derived device id used as Frame.device_id. */
  readonly deviceId: string;
  /** Signs msg with the device key. */
  sign(msg: Uint8Array): Uint8Array;
}

/** Builds an identity from a 32-byte Ed25519 seed. */
export function identityFromSeed(seed: Uint8Array): DeviceIdentity {
  if (seed.length !== SEED_LEN) throw new Error(`device key: seed is ${seed.length} bytes, want ${SEED_LEN}`);
  // The seed lives only in this closure: it is not a property of the returned
  // object, so serialising or logging the identity (or the connection and
  // props that hold it) cannot expose it.
  const secret = seed.slice();
  const publicKey = ed25519.getPublicKey(secret);
  return {
    publicKey,
    deviceId: deviceIdFor(publicKey),
    sign: (msg) => ed25519.sign(msg, secret),
  };
}

/** Where the device seed persists. save() must not resolve before the seed is durable. */
export interface DeviceKeyStore {
  /** The stored 32-byte seed, or null when none is stored. */
  load(): Promise<Uint8Array | null>;
  save(seed: Uint8Array): Promise<void>;
  remove(): Promise<void>;
}

/** A store that forgets on restart: tests and platforms without a secure store (a new identity per launch). */
export class MemoryDeviceKeyStore implements DeviceKeyStore {
  private seed: Uint8Array | null = null;
  async load() {
    return this.seed ? this.seed.slice() : null;
  }
  async save(seed: Uint8Array) {
    this.seed = seed.slice();
  }
  async remove() {
    this.seed = null;
  }
}

/**
 * Returns the stored identity, creating and persisting one on first use. A
 * stored value that is not a valid seed is replaced: the key is lost either
 * way, and the device then simply presents as a new device.
 */
export async function loadOrCreateIdentity(store: DeviceKeyStore): Promise<DeviceIdentity> {
  const stored = await store.load();
  if (stored && stored.length === SEED_LEN) return identityFromSeed(stored);
  const seed = ed25519.utils.randomSecretKey();
  await store.save(seed);
  return identityFromSeed(seed);
}

/** The relay.device_hello params (devices.Hello's JSON form: byte fields are standard base64). */
export interface DeviceHello {
  v: number;
  device_pub: string;
  label: string;
  signature: string;
  /** The daemon password (P13c), only when the person typed one this run. Not covered by the signature; only ever sent inside the encrypted channel. */
  password?: string;
}

// Code points devices.ValidateLabel refuses: controls (Cc), the common
// format characters (Cf: soft hyphen, bidi marks and overrides, zero-width,
// word joiners, BOM), line/paragraph separators (Zl/Zp), private use (Co) and
// surrogates. Explicit ranges rather than \p{..} escapes, which Hermes does
// not guarantee.
function isInvisible(cp: number): boolean {
  return (
    cp <= 0x1f ||
    (cp >= 0x7f && cp <= 0x9f) ||
    cp === 0xad ||
    (cp >= 0x600 && cp <= 0x605) ||
    cp === 0x61c ||
    cp === 0x6dd ||
    cp === 0x70f ||
    cp === 0x180e ||
    (cp >= 0x200b && cp <= 0x200f) ||
    (cp >= 0x2028 && cp <= 0x202e) ||
    (cp >= 0x2060 && cp <= 0x206f) ||
    (cp >= 0xd800 && cp <= 0xdfff) ||
    (cp >= 0xe000 && cp <= 0xf8ff) ||
    cp === 0xfeff ||
    (cp >= 0xfff9 && cp <= 0xfffb) ||
    (cp >= 0xe0000 && cp <= 0xe007f) ||
    cp >= 0xf0000
  );
}

/** Makes a label acceptable to devices.ValidateLabel: drops invisible characters and clips to 64 UTF-8 bytes on a code point boundary. */
export function sanitizeLabel(label: string): string {
  let out = '';
  let bytes = 0;
  for (const ch of label) {
    if (isInvisible(ch.codePointAt(0)!)) continue;
    const n = textEncoder.encode(ch).length;
    if (bytes + n > MAX_LABEL_BYTES) break;
    out += ch;
    bytes += n;
  }
  return out.trim();
}

/**
 * Signs a hello for one session: this session's transcript hash (from the
 * established channel), the daemon's public key (from the pairing offer,
 * verified by the handshake), the route's session id and the challenge the
 * daemon sent on this session, so it is valid for exactly one live session.
 */
export function buildDeviceHello(
  identity: DeviceIdentity,
  label: string,
  th: Uint8Array,
  daemonPub: Uint8Array,
  sessionId: Uint8Array,
  challenge: Uint8Array,
  password?: string,
): DeviceHello {
  const hello: DeviceHello = {
    v: HELLO_VERSION,
    device_pub: base64Encode(identity.publicKey),
    label: sanitizeLabel(label),
    signature: base64Encode(identity.sign(helloSignedBytes(th, daemonPub, sessionId, challenge))),
  };
  if (password) hello.password = password;
  return hello;
}
