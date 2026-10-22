// The push key store on the real expo-secure-store. AFTER_FIRST_UNLOCK so a push
// that arrives after a reboot, before the app was opened, can still be read by
// the headless task. Kept under native/ because it imports expo-secure-store.

import * as SecureStore from 'expo-secure-store';
import { MemoryPushKeyStore, secureStorePushKeyStore, type PushKeyStore } from '../keys';

export function platformPushKeyStore(): PushKeyStore {
  let inner: Promise<PushKeyStore> | null = null;
  const get = (): Promise<PushKeyStore> => {
    inner ??= SecureStore.isAvailableAsync().then((ok) =>
      ok
        ? secureStorePushKeyStore({
            getItemAsync: SecureStore.getItemAsync,
            setItemAsync: SecureStore.setItemAsync,
            deleteItemAsync: SecureStore.deleteItemAsync,
            keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY,
          })
        : new MemoryPushKeyStore(),
    );
    return inner;
  };
  return {
    put: async (deviceId, key) => (await get()).put(deviceId, key),
    get: async (deviceId) => (await get()).get(deviceId),
    delete: async (deviceId) => (await get()).delete(deviceId),
    all: async () => (await get()).all(),
  };
}
