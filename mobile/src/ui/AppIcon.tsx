// AppIcon.tsx renders a registry icon everywhere: the universal @expo/ui
// Icon (SF Symbol on iOS, a @expo/material-symbols XML vector on Android)
// inside a UiHost (expo-ui: every @expo/ui tree in a Host), and the registry
// glyph in a Text on web, where universal Icon renders nothing. NATIVE maps
// each registry name to an Icon.select literal -- the @expo/ui Babel plugin
// rewrites Icon.select into a Platform.OS ternary, which only works on
// literal call sites (see Icon.select's doc comment in
// @expo/ui/src/universal/Icon/index.tsx), so entries are spelled out rather
// than generated from ICONS. Icons are decorative: the parent control (a
// Pressable row, a Button) carries the accessible label, never the glyph.

import { Platform, Text } from 'react-native';
import { Icon } from '@expo/ui';
import { IconName, ICONS } from './icons';
import { useAppTheme } from '../theme/ThemeProvider';
import { UiHost } from './UiHost';

const NATIVE: Record<IconName, ReturnType<typeof Icon.select>> = {
  chevronRight: Icon.select({ ios: 'chevron.right', android: import('@expo/material-symbols/chevron_right.xml') }),
  more: Icon.select({ ios: 'ellipsis', android: import('@expo/material-symbols/more_horiz.xml') }),
  close: Icon.select({ ios: 'xmark', android: import('@expo/material-symbols/close.xml') }),
  check: Icon.select({ ios: 'checkmark', android: import('@expo/material-symbols/check.xml') }),
  warning: Icon.select({ ios: 'exclamationmark.triangle', android: import('@expo/material-symbols/warning.xml') }),
  info: Icon.select({ ios: 'info.circle', android: import('@expo/material-symbols/info.xml') }),
  refresh: Icon.select({ ios: 'arrow.clockwise', android: import('@expo/material-symbols/refresh.xml') }),
  lock: Icon.select({ ios: 'lock', android: import('@expo/material-symbols/lock.xml') }),
  stop: Icon.select({ ios: 'stop.fill', android: import('@expo/material-symbols/stop.xml') }),
  send: Icon.select({ ios: 'arrow.up', android: import('@expo/material-symbols/arrow_upward.xml') }),
  link: Icon.select({ ios: 'link', android: import('@expo/material-symbols/link.xml') }),
  devices: Icon.select({ ios: 'laptopcomputer.and.iphone', android: import('@expo/material-symbols/devices.xml') }),
};

export function AppIcon({ name, size = 20, color }: { name: IconName; size?: number; color?: string }) {
  const theme = useAppTheme();
  if (Platform.OS === 'web') {
    return (
      <Text accessible={false} style={{ fontSize: size, lineHeight: size, color }}>
        {ICONS[name].glyph}
      </Text>
    );
  }
  return (
    <UiHost>
      <Icon name={NATIVE[name]} size={size} color={color ?? theme.foreground} />
    </UiHost>
  );
}
