// The expo-notifications foreground behavior for the ADR-0040 rule. Pure: the
// native handler in native/PushController.tsx calls this.

import { shouldPresentInForeground, type ForegroundInput } from './foreground';

export function foregroundBehavior(input: ForegroundInput): {
  shouldShowBanner: boolean;
  shouldShowList: boolean;
  shouldPlaySound: false;
  shouldSetBadge: false;
} {
  const show = shouldPresentInForeground(input);
  return { shouldShowBanner: show, shouldShowList: show, shouldPlaySound: false, shouldSetBadge: false };
}
