// statusCardView.ts holds the status card's pure presentation rules (no
// react-native import, so it is unit-testable without a RN transform):
// which Button variant each action gets and which StatusDot tone a status
// maps to. expo-design-system: rules live in code, not per usage site.

import { StatusTone } from '../relay/connectionStatus';
import { StatusDotTone } from '../ui/StatusDot';

/**
 * Button variants for `count` actions: with 2+ the FIRST is the filled
 * forward action and the rest are text; a single action is outlined.
 */
export function actionVariants(count: number): Array<'filled' | 'outlined' | 'text'> {
  if (count <= 0) return [];
  if (count === 1) return ['outlined'];
  return ['filled', ...Array<'text'>(count - 1).fill('text')];
}

/** StatusDot tone for a status tone: info -> running, warning/danger as-is (fill-tier dot, StatusDot.tsx). */
export function statusDotTone(tone: StatusTone): StatusDotTone {
  return tone === 'info' ? 'running' : tone;
}
