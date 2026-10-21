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
//
// The Host is also the tint boundary (expo-ui: "every @expo/ui tree in a
// Host; the tint comes from the Host"): seedColor is the SwiftUI tint on
// iOS (buttons, switches, sliders), a generated Material 3 palette on
// Android, and the primary colour scale on web. Seeding it with
// theme.primary (black on light, white on dark) plus the resolved
// colorScheme makes every @expo/ui Button neutral instead of iOS system
// blue -- so controls that already carry the design language are used
// as-is, never wrapped.

import { ReactNode } from 'react';
import { StyleProp, ViewStyle } from 'react-native';
import { Host } from '@expo/ui';
import { useAppTheme } from '../theme/ThemeProvider';

export function UiHost({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const theme = useAppTheme();
  return (
    <Host matchContents seedColor={theme.primary} colorScheme={theme.mode} style={style}>
      {children}
    </Host>
  );
}
