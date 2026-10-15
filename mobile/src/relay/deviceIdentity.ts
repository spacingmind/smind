// deviceIdentity.ts is the mobile half of ADR-0031 D9's device identity
// (decision 35): a persistent Ed25519 key, the self-authenticating device id
// derived from it, and the signed relay.device_hello the daemon checks
// inside the encrypted channel. It mirrors internal/relay/devices/
// identity.go and is pinned to it by the shared cross-language vector
// (internal/relay/devices/testdata/device_hello_vector.json, see
// __tests__/deviceIdentity.test.ts).
//
// Pure JS crypto (@noble) and no React Native imports, so it runs under
// vitest/node; where the key lives is the DeviceKeyStore's business
// (deviceKeyStore.ts wires the platform secure store).

import { ed25519 } from '@noble/curves/ed25519.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { base64Encode } from './base64';

/** Method name of the first application message (devices.HelloMethod). */
export const HELLO_METHOD = 'relay.device_hello';
/** The only hello format the daemon accepts (devices.HelloVersion). */
export const HELLO_VERSION = 1;

const HELLO_DOMAIN = 'smind-device-hello';
const E2EE_LABEL = 'smind relay e2ee v1';
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

/** e2ee.TranscriptHash: SHA-256("smind relay e2ee v1" || daemonPub || mobilePub), also the session's HKDF salt. */
export function transcriptHash(daemonPub: Uint8Array, mobilePub: Uint8Array): Uint8Array {
  return sha256(concatBytes(textEncoder.encode(E2EE_LABEL), daemonPub, mobilePub));
}

/** devices.HelloSignedBytes: "smind-device-hello" || TranscriptHash || daemonPub. */
export function helloSignedBytes(daemonPub: Uint8Array, mobilePub: Uint8Array): Uint8Array {
  return concatBytes(textEncoder.encode(HELLO_DOMAIN), transcriptHash(daemonPub, mobilePub), daemonPub);
}

/**
 * A device's persistent identity. The seed is the secret: it never leaves
 * this object except into a DeviceKeyStore, and is never logged.
 */
export interface DeviceIdentity {
  /** The Ed25519 public key (32 bytes). */
  readonly publicKey: Uint8Array;
  /** The key-derived device id used as Frame.device_id. */
  readonly deviceId: string;
  /** Signs msg with the device key. */
  sign(msg: Uint8Array): Uint8Array;
}

class SeedIdentity implements DeviceIdentity {
  readonly publicKey: Uint8Array;
  readonly deviceId: string;
  private readonly seed: Uint8Array;

  constructor(seed: Uint8Array) {
    this.seed = seed;
    this.publicKey = ed25519.getPublicKey(seed);
    this.deviceId = deviceIdFor(this.publicKey);
  }

  sign(msg: Uint8Array): Uint8Array {
    return ed25519.sign(msg, this.seed);
  }
}

/** Builds an identity from a 32-byte Ed25519 seed. */
export function identityFromSeed(seed: Uint8Array): DeviceIdentity {
  if (seed.length !== SEED_LEN) throw new Error(`device key: seed is ${seed.length} bytes, want ${SEED_LEN}`);
  return new SeedIdentity(seed.slice());
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
 * Signs a hello for the session identified by the daemon's public key (from
 * the pairing offer, verified by the handshake) and this session's ephemeral
 * X25519 public key, so it is valid for exactly one session.
 */
export function buildDeviceHello(
  identity: DeviceIdentity,
  label: string,
  daemonPub: Uint8Array,
  mobilePub: Uint8Array,
): DeviceHello {
  return {
    v: HELLO_VERSION,
    device_pub: base64Encode(identity.publicKey),
    label: sanitizeLabel(label),
    signature: base64Encode(identity.sign(helloSignedBytes(daemonPub, mobilePub))),
  };
}
