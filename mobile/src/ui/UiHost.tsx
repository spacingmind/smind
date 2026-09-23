// UiHost.tsx is the per-subtree @expo/ui Host wrapper (mobile-web-smoke-
// test plan): PR #183 wrapped the whole RN app in one root <Host>, which
// is the wrong shape -- on iOS Host is a SwiftUI hosting view whose RN
// children must go through RNHostView (.agents/skills/expo-ui/references/
// swift-ui.md), and on web Host renders a plain View with no flex, which
// squeezed the app into a ~347px corner panel. The documented pattern is
// one Host per @expo/ui subtree, sized to its content via matchContents.
// matchContents implies alignSelf: 'flex-start' (web Host applies it
// before `style`), so callers pass style to restore stretch/center/
// flex-end alignment inside rows -- see each screen's *Host styles.

import { ReactNode } from 'react';
import { StyleProp, ViewStyle } from 'react-native';
import { Host } from '@expo/ui';

export function UiHost({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  return (
    <Host matchContents style={style}>
      {children}
    </Host>
  );
}
