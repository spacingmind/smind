// platformKeyStore.ts wires deviceKeyStore.ts to the real expo-secure-store.
// Kept apart from deviceKeyStore.ts because importing the native module pulls
// in React Native, which node-side tests cannot load.

import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import { DeviceIdentity, DeviceKeyStore, loadOrCreateIdentity, MemoryDeviceKeyStore } from './deviceIdentity';
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

let identityPromise: Promise<DeviceIdentity> | null = null;
let removal: Promise<void> | null = null;

/** The device identity for this install, created on first use and then kept (in memory for the process, in the secure store across launches). */
export function loadPlatformIdentity(): Promise<DeviceIdentity> {
  if (!identityPromise) {
    // A reset in progress finishes first: loading the key it is deleting, or
    // saving one it then deletes, would split the identity from what is stored.
    const attempt = (removal ?? Promise.resolve())
      .then(platformDeviceKeyStore)
      .then(loadOrCreateIdentity)
      .catch((e) => {
        if (identityPromise === attempt) identityPromise = null; // a failed read is retried, not cached
        throw e;
      });
    identityPromise = attempt;
  }
  return identityPromise;
}

/**
 * Forgets this device's identity: the next connection uses a new key and
 * presents as a new device to the daemon (which has to approve it again). The
 * way back for a device whose identity the daemon revoked.
 */
export async function resetDeviceIdentity(): Promise<void> {
  identityPromise = null;
  const mine = (async () => (await platformDeviceKeyStore()).remove())();
  removal = mine;
  try {
    await mine;
  } finally {
    if (removal === mine) removal = null;
  }
}

/** What the person approving this device on the daemon sees, e.g. "iPhone (smind mobile)". */
export function defaultDeviceLabel(): string {
  const kind = Platform.OS === 'ios' ? 'iPhone' : Platform.OS === 'android' ? 'Android phone' : 'Browser';
  return `${kind} (smind mobile)`;
}
