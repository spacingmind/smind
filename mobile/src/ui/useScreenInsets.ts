// useScreenInsets.ts re-exports react-native-safe-area-context's
// useSafeAreaInsets under the app's own name so screens never import the
// library directly: the bottom/horizontal insets place fixed bars and pad
// scroll content, while the TOP inset is owned by the native stack header
// (expo-native-ui: the header owns its own inset). Expo Router already
// mounts the SafeAreaProvider, so no provider wiring is needed at call sites.

export { useSafeAreaInsets as useScreenInsets } from 'react-native-safe-area-context';
