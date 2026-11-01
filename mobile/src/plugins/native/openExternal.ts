// openExternal.ts: the one place a plugin panel's confirmed openUrl reaches the OS. The url is
// already validated (view/actions.ts openUrlHost) and confirmed by the Human (ui/confirm.ts) before
// it gets here; a rejection (no handler, airplane mode) is allowed to propagate so runViewAction
// turns it into a typed error outcome instead of a silent no-op. Kept under native/ because it
// imports react-native.

import { Linking } from 'react-native';

export function openExternal(url: string): Promise<void> {
  return Linking.openURL(url);
}
