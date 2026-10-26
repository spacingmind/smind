// confirm.ts: a yes/no question through the platform's own dialog -- Alert.alert on a device, window.confirm on web
// (react-native-web's Alert is a no-op, so the web smoke target needs its own path). Resolves true only on the
// destructive confirm button; dismissing or cancelling resolves false.

import { Alert, Platform } from 'react-native';

export function confirmNative(title: string, message: string, confirmLabel = 'Pause'): Promise<boolean> {
  if (Platform.OS === 'web') {
    const ask = (globalThis as { confirm?: (m: string) => boolean }).confirm;
    return Promise.resolve(typeof ask === 'function' ? ask(`${title}\n${message}`) : false);
  }
  return new Promise((resolve) => {
    Alert.alert(
      title,
      message,
      [
        { text: 'Cancel', style: 'cancel', onPress: () => resolve(false) },
        { text: confirmLabel, style: 'destructive', onPress: () => resolve(true) },
      ],
      { cancelable: true, onDismiss: () => resolve(false) },
    );
  });
}
