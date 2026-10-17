// deviceKeyStore.ts keeps the device's Ed25519 seed (ADR-0031 decision 35) in
// the platform secure store: the iOS Keychain or the Android Keystore-backed
// encrypted preferences, via expo-secure-store. The seed is written
// device-bound (never restored onto another device from a backup) and
// readable after the first unlock, so a reconnect after a reboot does not
// need the user to be at the lock screen.
//
// expo-secure-store is injected as an interface so this file stays free of
// React Native imports (and testable under node); platformKeyStore.ts is the
// one place the real module is touched.

import { DeviceKeyStore } from './deviceIdentity';

const STORAGE_KEY = 'smind.relay.deviceKey.v1';
const SEED_LEN = 32;

/** The slice of expo-secure-store this store uses. */
export interface SecureStoreLike {
  getItemAsync(key: string, options?: { keychainAccessible?: number }): Promise<string | null>;
  setItemAsync(key: string, value: string, options?: { keychainAccessible?: number }): Promise<void>;
  deleteItemAsync(key: string, options?: { keychainAccessible?: number }): Promise<void>;
  /** The value of SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY. */
  keychainAccessible?: number;
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function fromHex(s: string): Uint8Array | null {
  if (s.length !== SEED_LEN * 2 || !/^[0-9a-f]+$/.test(s)) return null;
  const out = new Uint8Array(SEED_LEN);
  for (let i = 0; i < SEED_LEN; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

/** A DeviceKeyStore over a secure store; the seed is stored as 64 hex characters. */
export function secureStoreDeviceKeyStore(store: SecureStoreLike): DeviceKeyStore {
  const options = store.keychainAccessible === undefined ? undefined : { keychainAccessible: store.keychainAccessible };
  return {
    async load() {
      const raw = await store.getItemAsync(STORAGE_KEY, options);
      return raw === null ? null : fromHex(raw);
    },
    async save(seed) {
      await store.setItemAsync(STORAGE_KEY, toHex(seed), options);
    },
    async remove() {
      await store.deleteItemAsync(STORAGE_KEY, options);
    },
  };
}
