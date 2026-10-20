// First, before anything can ask for a key or a nonce: installs the native CSPRNG (see src/relay/random.ts).
import './src/relay/platformRandom';

import { registerRootComponent } from 'expo';

import App from './App';

// registerRootComponent calls AppRegistry.registerComponent('main', () => App);
// It also ensures that whether you load the app in Expo Go or in a native build,
// the environment is set up appropriately
registerRootComponent(App);
