// deviceIdentity.test.ts pins the mobile device identity to the Go side
// through the shared cross-language vector (internal/relay/devices/testdata/
// device_hello_vector.json, the same file the Go and Rust tests read): the
// device id derivation, the signed bytes, the Ed25519 signature and the
// hello JSON must match byte for byte, and the negative case must not verify.

import { readFileSync } from 'fs';
import { join } from 'path';
import { ed25519 } from '@noble/curves/ed25519.js';
import { describe, expect, it } from 'vitest';
import { base64Decode, base64Encode } from '../base64';
import {
  buildDeviceHello,
  deviceIdFor,
  helloSignedBytes,
  identityFromSeed,
  loadOrCreateIdentity,
  MemoryDeviceKeyStore,
  sanitizeLabel,
} from '../deviceIdentity';
import { secureStoreDeviceKeyStore, SecureStoreLike } from '../deviceKeyStore';

interface Vector {
  seed_hex: string;
  device_pub_hex: string;
  device_id: string;
  daemon_pub_hex: string;
  mobile_pub_hex: string;
  label: string;
  signed_bytes_hex: string;
  signature_hex: string;
  hello: { v: number; device_pub: string; label: string; signature: string };
  negative: { mobile_pub_hex: string; signed_bytes_hex: string; hello: { signature: string }; error: string };
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

describe('device_hello cross-language vector', () => {
  const identity = identityFromSeed(fromHex(vector.seed_hex));
  const daemonPub = fromHex(vector.daemon_pub_hex);
  const mobilePub = fromHex(vector.mobile_pub_hex);

  it('derives the vector public key and device id', () => {
    expect(hex(identity.publicKey)).toBe(vector.device_pub_hex);
    expect(identity.deviceId).toBe(vector.device_id);
    expect(deviceIdFor(fromHex(vector.device_pub_hex))).toBe(vector.device_id);
    expect(identity.deviceId).toHaveLength(26);
  });

  it('builds the vector signed bytes', () => {
    expect(hex(helloSignedBytes(daemonPub, mobilePub))).toBe(vector.signed_bytes_hex);
  });

  it('device_hello signature verifies against the Go vector (and matches it byte for byte)', () => {
    const hello = buildDeviceHello(identity, vector.label, daemonPub, mobilePub);
    expect(hello).toEqual(vector.hello);
    // Ed25519 is deterministic, so the signature is the Go one...
    expect(hex(base64Decode(hello.signature))).toBe(vector.signature_hex);
    // ...and it verifies under the device key over the vector's signed bytes.
    expect(ed25519.verify(fromHex(vector.signature_hex), fromHex(vector.signed_bytes_hex), identity.publicKey)).toBe(true);
  });

  it('a hello signed for a different session does not verify against this one', () => {
    const wrongSession = fromHex(vector.negative.mobile_pub_hex);
    const hello = buildDeviceHello(identity, vector.label, daemonPub, wrongSession);
    expect(hello.signature).toBe(vector.negative.hello.signature);
    expect(hex(helloSignedBytes(daemonPub, wrongSession))).toBe(vector.negative.signed_bytes_hex);
    // The signature the negative vector carries must fail against the real transcript.
    expect(ed25519.verify(base64Decode(vector.negative.hello.signature), helloSignedBytes(daemonPub, mobilePub), identity.publicKey)).toBe(false);
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
    const hello = buildDeviceHello(identity, 'x', new Uint8Array(32), new Uint8Array(32));
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
