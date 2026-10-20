// platformRandom.ts installs expo-crypto's native CSPRNG as the relay
// client's random source. Imported for its side effect, first thing in
// index.ts. Kept apart from random.ts because importing the native module
// pulls in React Native, which node-side tests cannot load.
//
// expo-crypto's `getRandomValues` calls the platform's secure generator
// directly (SecRandomCopyBytes / SecureRandom, `crypto.getRandomValues` on
// web). Its `getRandomBytes` is not used: that one falls back to Math.random
// in development builds.

import { getRandomValues } from 'expo-crypto';
import { installRandomSource } from './random';

installRandomSource((bytes) => {
  getRandomValues(bytes);
});
