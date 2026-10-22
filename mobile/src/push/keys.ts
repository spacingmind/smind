// Per-device push keys (ADR-0040 section 1) in the platform secure store.
// expo-secure-store cannot list keys and the headless background task needs
// every key to trial-decrypt, so an index item lists the device ids. The
// secure store is injected (see relay/deviceKeyStore.ts) to keep this file
// free of React Native imports.

import type { SecureStoreLike } from '../relay/deviceKeyStore';
import type { PushKeyEntry } from './open';

export interface PushKeyStore {
  put(deviceId: string, key: Uint8Array): Promise<void>;
  get(deviceId: string): Promise<Uint8Array | null>;
  delete(deviceId: string): Promise<void>;
  all(): Promise<PushKeyEntry[]>;
}

const KEY_PREFIX = 'smind.push.key.v1.';
const INDEX_KEY = 'smind.push.keys.v1';
const KEY_LEN = 32;
const DEVICE_ID = /^[a-z0-9._-]{1,64}$/;

function checkDeviceId(deviceId: string): void {
  if (!DEVICE_ID.test(deviceId)) throw new RangeError('invalid push device id');
}

function checkKey(key: Uint8Array): void {
  if (key.length !== KEY_LEN) throw new RangeError(`push key must be ${KEY_LEN} bytes`);
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function fromHex(s: string): Uint8Array | null {
  if (s.length !== KEY_LEN * 2 || !/^[0-9a-f]+$/.test(s)) return null;
  const out = new Uint8Array(KEY_LEN);
  for (let i = 0; i < KEY_LEN; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

/** A PushKeyStore over a secure store: one hex item per key plus a JSON index of device ids. */
export function secureStorePushKeyStore(store: SecureStoreLike): PushKeyStore {
  const options = store.keychainAccessible === undefined ? undefined : { keychainAccessible: store.keychainAccessible };

  async function readIndex(): Promise<string[]> {
    const raw = await store.getItemAsync(INDEX_KEY, options);
    if (raw === null) return [];
    try {
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return [];
      return parsed.filter((v): v is string => typeof v === 'string');
    } catch {
      return [];
    }
  }

  async function get(deviceId: string): Promise<Uint8Array | null> {
    checkDeviceId(deviceId);
    const raw = await store.getItemAsync(KEY_PREFIX + deviceId, options);
    return raw === null ? null : fromHex(raw);
  }

  return {
    async put(deviceId, key) {
      checkDeviceId(deviceId);
      checkKey(key);
      await store.setItemAsync(KEY_PREFIX + deviceId, toHex(key), options);
      const index = await readIndex();
      if (!index.includes(deviceId)) {
        await store.setItemAsync(INDEX_KEY, JSON.stringify([...index, deviceId]), options);
      }
    },
    get,
    async delete(deviceId) {
      checkDeviceId(deviceId);
      await store.deleteItemAsync(KEY_PREFIX + deviceId, options);
      const index = await readIndex();
      if (index.includes(deviceId)) {
        await store.setItemAsync(INDEX_KEY, JSON.stringify(index.filter((id) => id !== deviceId)), options);
      }
    },
    async all() {
      const out: PushKeyEntry[] = [];
      for (const deviceId of await readIndex()) {
        if (!DEVICE_ID.test(deviceId)) continue;
        const key = await get(deviceId);
        if (key !== null) out.push({ deviceId, key });
      }
      return out;
    },
  };
}

export class MemoryPushKeyStore implements PushKeyStore {
  private readonly keys = new Map<string, Uint8Array>();
  async put(deviceId: string, key: Uint8Array) {
    checkDeviceId(deviceId);
    checkKey(key);
    this.keys.set(deviceId, key.slice());
  }
  async get(deviceId: string) {
    checkDeviceId(deviceId);
    return this.keys.get(deviceId)?.slice() ?? null;
  }
  async delete(deviceId: string) {
    checkDeviceId(deviceId);
    this.keys.delete(deviceId);
  }
  async all() {
    return Array.from(this.keys, ([deviceId, key]) => ({ deviceId, key: key.slice() }));
  }
}
