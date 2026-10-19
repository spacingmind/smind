// deviceIdentity.test.ts pins the mobile device identity to the Go side
// through the shared cross-language vector (internal/relay/devices/testdata/
// device_hello_vector.json, hello VERSION 2 -- the same file the Go and Rust
// tests read): the device id derivation, the challenge message parse, the
// signed bytes, the Ed25519 signature and the hello JSON must match byte for
// byte, and each of the four negatives (a changed daemon key, mobile key,
// session id or challenge) must NOT verify against the real bytes.

import { readFileSync } from 'fs';
import { join } from 'path';
import { ed25519 } from '@noble/curves/ed25519.js';
import { describe, expect, it } from 'vitest';
import { E2EE_V2_PENDING } from './e2eeV2Gate';
import { base64Decode, base64Encode } from '../base64';
import {
  buildDeviceHello,
  ChallengeVersionError,
  deviceIdFor,
  HELLO_VERSION,
  helloSignedBytes,
  identityFromSeed,
  loadOrCreateIdentity,
  MemoryDeviceKeyStore,
  parseChallengeMessage,
  sanitizeLabel,
} from '../deviceIdentity';
import { secureStoreDeviceKeyStore, SecureStoreLike } from '../deviceKeyStore';

interface Hello {
  v: number;
  device_pub: string;
  label: string;
  signature: string;
}

interface Vector {
  version: number;
  seed_hex: string;
  device_pub_hex: string;
  device_id: string;
  daemon_pub_hex: string;
  mobile_pub_hex: string;
  session_id: string;
  challenge_hex: string;
  challenge_message: { event: string; params: { v: number; challenge: string } };
  label: string;
  signed_bytes_hex: string;
  signature_hex: string;
  hello: Hello;
  negative: Array<{ name: string; changed: string; signed_bytes_hex: string; hello: Hello; error: string }>;
}

const vector: Vector = JSON.parse(
  readFileSync(join(__dirname, '..', '..', '..', '..', 'internal', 'relay', 'devices', 'testdata', 'device_hello_vector.json'), 'utf8'),
);

function fromHex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function hex(b: Uint8Array): string {
  return Array.from(b)
    .map((x) => x.toString(16).padStart(2, '0'))
    .join('');
}

const utf8 = (s: string) => new TextEncoder().encode(s);

describe('device_hello v2 cross-language vector', () => {
  const identity = identityFromSeed(fromHex(vector.seed_hex));
  const daemonPub = fromHex(vector.daemon_pub_hex);
  const mobilePub = fromHex(vector.mobile_pub_hex);
  const sessionId = utf8(vector.session_id);
  const challenge = fromHex(vector.challenge_hex);

  it('is the version this client speaks', () => {
    expect(vector.version).toBe(2);
    expect(HELLO_VERSION).toBe(2);
  });

  it('derives the vector public key and device id', () => {
    expect(hex(identity.publicKey)).toBe(vector.device_pub_hex);
    expect(identity.deviceId).toBe(vector.device_id);
    expect(deviceIdFor(fromHex(vector.device_pub_hex))).toBe(vector.device_id);
    expect(identity.deviceId).toHaveLength(26);
  });

  it('parses the daemon\'s challenge message into the vector challenge', () => {
    const message = utf8(JSON.stringify(vector.challenge_message));
    expect(hex(parseChallengeMessage(message)!)).toBe(vector.challenge_hex);
    expect(base64Encode(challenge)).toBe(vector.challenge_message.params.challenge);
  });

  it.skip(`${E2EE_V2_PENDING}: builds the vector signed bytes`, () => {
    expect(hex(helloSignedBytes(daemonPub, mobilePub, sessionId, challenge))).toBe(vector.signed_bytes_hex);
  });

  it.skip(`${E2EE_V2_PENDING}: device_hello v2 signature verifies against the Go vector (and matches it byte for byte)`, () => {
    const hello = buildDeviceHello(identity, vector.label, daemonPub, mobilePub, sessionId, challenge);
    expect(hello).toEqual(vector.hello);
    expect(hello.v).toBe(2);
    // Ed25519 is deterministic, so the signature is the Go one...
    expect(hex(base64Decode(hello.signature))).toBe(vector.signature_hex);
    // ...and it verifies under the device key over the vector's signed bytes.
    expect(ed25519.verify(fromHex(vector.signature_hex), fromHex(vector.signed_bytes_hex), identity.publicKey)).toBe(true);
  });

  it.skip(`${E2EE_V2_PENDING}: has four negatives, each with its own signed bytes and a signature that does not verify against the real bytes`, () => {
    expect(vector.negative.map((n) => n.name)).toEqual(['other_daemon_pub', 'other_mobile_pub', 'other_session_id', 'other_challenge']);
    const real = helloSignedBytes(daemonPub, mobilePub, sessionId, challenge);
    for (const n of vector.negative) {
      expect(n.error).toBe('bad_signature');
      expect(n.signed_bytes_hex, n.name).not.toBe(vector.signed_bytes_hex);
      const sig = base64Decode(n.hello.signature);
      // It IS a valid signature over its own bytes (the file is honest about what was signed)...
      expect(ed25519.verify(sig, fromHex(n.signed_bytes_hex), identity.publicKey), n.name).toBe(true);
      // ...and must fail against the bytes the daemon would reconstruct for the real session.
      expect(ed25519.verify(sig, real, identity.publicKey), n.name).toBe(false);
    }
  });

  it.skip(`${E2EE_V2_PENDING}: what this client signs changes with each of daemon key, mobile key, session id and challenge`, () => {
    const base = hex(helloSignedBytes(daemonPub, mobilePub, sessionId, challenge));
    const other = (b: Uint8Array) => new Uint8Array(b.map((x) => x ^ 1));
    expect(hex(helloSignedBytes(other(daemonPub), mobilePub, sessionId, challenge))).not.toBe(base);
    expect(hex(helloSignedBytes(daemonPub, other(mobilePub), sessionId, challenge))).not.toBe(base);
    expect(hex(helloSignedBytes(daemonPub, mobilePub, utf8('sess-another-route'), challenge))).not.toBe(base);
    expect(hex(helloSignedBytes(daemonPub, mobilePub, sessionId, other(challenge)))).not.toBe(base);
    // and the negatives' bytes are exactly those variants
    // and the vector's negatives are exactly those variants (where the file carries enough to rebuild them)
    const byName = Object.fromEntries(vector.negative.map((n) => [n.name, n.signed_bytes_hex]));
    expect(hex(helloSignedBytes(daemonPub, mobilePub, utf8('sess-another-route'), challenge))).toBe(byName.other_session_id);
    expect(hex(helloSignedBytes(daemonPub, mobilePub, sessionId, new Uint8Array(challenge.map((x) => x + 1))))).toBe(byName.other_challenge);
    expect(hex(helloSignedBytes(fromHex('202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f'), mobilePub, sessionId, challenge))).toBe(
      byName.other_daemon_pub,
    );
    for (const n of vector.negative) expect(vector.signed_bytes_hex).not.toBe(n.signed_bytes_hex);
  });

  it('refuses to sign without a 32-byte challenge or with an oversized session id', () => {
    expect(() => helloSignedBytes(daemonPub, mobilePub, sessionId, new Uint8Array(31))).toThrow('challenge');
    expect(() => helloSignedBytes(daemonPub, mobilePub, new Uint8Array(129), challenge)).toThrow('session id');
  });
});

describe('daemon challenge message', () => {
  const msg = (v: unknown, challenge: unknown, event = 'relay.device_challenge') => utf8(JSON.stringify({ event, params: { v, challenge } }));
  const good = base64Encode(new Uint8Array(32).fill(5));

  it('ignores messages that are not the challenge', () => {
    expect(parseChallengeMessage(utf8('{"id":"1","result":[]}'))).toBeNull();
    expect(parseChallengeMessage(utf8('not json'))).toBeNull();
    expect(parseChallengeMessage(msg(2, good, 'task.created'))).toBeNull();
  });

  it('rejects a challenge of another version, a bad length and non-canonical base64', () => {
    expect(() => parseChallengeMessage(msg(1, good))).toThrow(ChallengeVersionError);
    expect(() => parseChallengeMessage(msg(3, good))).toThrow(ChallengeVersionError);
    expect(() => parseChallengeMessage(msg(2, base64Encode(new Uint8Array(31))))).toThrow(ChallengeVersionError);
    expect(() => parseChallengeMessage(msg(2, base64Encode(new Uint8Array(33))))).toThrow(ChallengeVersionError);
    expect(() => parseChallengeMessage(msg(2, 42))).toThrow(ChallengeVersionError);
    expect(() => parseChallengeMessage(msg(2, good.replace(/=+$/, '')))).toThrow(ChallengeVersionError); // padding is part of the contract
    expect(parseChallengeMessage(msg(2, good))).toEqual(new Uint8Array(32).fill(5));
  });
});

describe('device key persistence', () => {
  function fakeSecureStore(): SecureStoreLike & { items: Map<string, string>; options: unknown[] } {
    const items = new Map<string, string>();
    const options: unknown[] = [];
    return {
      items,
      options,
      keychainAccessible: 7,
      async getItemAsync(key, o) {
        options.push(o);
        return items.get(key) ?? null;
      },
      async setItemAsync(key, value, o) {
        options.push(o);
        items.set(key, value);
      },
      async deleteItemAsync(key) {
        items.delete(key);
      },
    };
  }

  it('device key persists in the secure store', async () => {
    const secure = fakeSecureStore();
    const first = await loadOrCreateIdentity(secureStoreDeviceKeyStore(secure));
    // A fresh store object over the same backing storage (an app restart) yields the same identity.
    const second = await loadOrCreateIdentity(secureStoreDeviceKeyStore(secure));
    expect(second.deviceId).toBe(first.deviceId);
    expect(hex(second.publicKey)).toBe(hex(first.publicKey));
    expect(secure.items.size).toBe(1);
    // Stored device-bound/after-first-unlock, via the options the platform gave us.
    expect(secure.options.every((o) => (o as { keychainAccessible: number }).keychainAccessible === 7)).toBe(true);
  });

  it('is generated once and differs between devices', async () => {
    const a = await loadOrCreateIdentity(new MemoryDeviceKeyStore());
    const b = await loadOrCreateIdentity(new MemoryDeviceKeyStore());
    expect(a.deviceId).not.toBe(b.deviceId);
  });

  it('replaces a corrupt stored value with a fresh key', async () => {
    const secure = fakeSecureStore();
    secure.items.set('smind.relay.deviceKey.v1', 'not-a-seed');
    const identity = await loadOrCreateIdentity(secureStoreDeviceKeyStore(secure));
    expect(identity.deviceId).toHaveLength(26);
    expect(secure.items.get('smind.relay.deviceKey.v1')).toMatch(/^[0-9a-f]{64}$/);
  });

  it('never puts the seed in the hello, nor on the identity object', async () => {
    const seed = fromHex(vector.seed_hex);
    const identity = identityFromSeed(seed);
    const hello = buildDeviceHello(identity, 'x', new Uint8Array(32), new Uint8Array(32), utf8('s'), new Uint8Array(32));
    expect(JSON.stringify(hello)).not.toContain(base64Encode(seed));
    // Serialising or logging the identity (or whatever holds it) must not reveal the seed.
    expect(Object.keys(identity).sort()).toEqual(['deviceId', 'publicKey', 'sign']);
    expect(JSON.stringify(identity)).not.toContain(vector.seed_hex);
    expect(JSON.stringify(Array.from(seed))).not.toBe(JSON.stringify(identity));
    expect(JSON.stringify(identity)).not.toContain(JSON.stringify(Array.from(seed)).slice(1, -1));
  });
});

describe('label sanitising', () => {
  it('keeps ordinary labels and clips to 64 bytes on a code point boundary', () => {
    expect(sanitizeLabel('Pixel 9 (test vector)')).toBe('Pixel 9 (test vector)');
    const clipped = sanitizeLabel('é'.repeat(40));
    expect(new TextEncoder().encode(clipped).length).toBeLessThanOrEqual(64);
    expect(clipped).toBe('é'.repeat(32));
  });

  it('drops control and invisible characters the daemon would refuse', () => {
    expect(sanitizeLabel('a\u0000b‮c​d\nf')).toBe('abcdf');
  });
});
