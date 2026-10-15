// platformKeyStore.ts wires deviceKeyStore.ts to the real expo-secure-store.
// Kept apart from deviceKeyStore.ts because importing the native module pulls
// in React Native, which node-side tests cannot load.

import * as SecureStore from 'expo-secure-store';
import { DeviceKeyStore, MemoryDeviceKeyStore } from './deviceIdentity';
import { secureStoreDeviceKeyStore } from './deviceKeyStore';

/**
 * The device key store for this platform: the secure store where the
 * platform has one. Where it does not (Expo web, which is a development and
 * smoke-test target, not a shipped one) the key lives only in memory — a new
 * identity per page load — rather than in localStorage, which any script on
 * the page could read.
 */
export async function platformDeviceKeyStore(): Promise<DeviceKeyStore> {
  if (!(await SecureStore.isAvailableAsync())) return new MemoryDeviceKeyStore();
  return secureStoreDeviceKeyStore({
    getItemAsync: SecureStore.getItemAsync,
    setItemAsync: SecureStore.setItemAsync,
    deleteItemAsync: SecureStore.deleteItemAsync,
    keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY,
  });
}
