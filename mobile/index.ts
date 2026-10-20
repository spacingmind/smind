// First, before anything can ask for a key or a nonce: installs the native CSPRNG (see src/relay/random.ts).
import './src/relay/platformRandom';

// Expo Router's entry registers the root component (app/_layout.tsx, ADR-0036); it must be imported last
// (docs.expo.dev/router/installation: a custom entry point sets up, then imports 'expo-router/entry').
import 'expo-router/entry';
