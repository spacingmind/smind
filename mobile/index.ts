// First, before anything can ask for a key or a nonce: installs the native CSPRNG (see src/relay/random.ts).
import './src/relay/platformRandom';

// Android push (ADR-0040): the background task must be defined when the headless JS context starts, which only
// runs module-scope code from the entry, so this has to be an early side-effect import, not a lazy one.
import './src/push/native/backgroundTask';

// Expo Router's entry registers the root component (app/_layout.tsx, ADR-0036); it must be imported last
// (docs.expo.dev/router/installation: a custom entry point sets up, then imports 'expo-router/entry').
import 'expo-router/entry';
