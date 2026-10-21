// TextButton.tsx is the one text-variant action: a native @expo/ui Button
// (variant "text", expo-ui: use the native control) whose label colour is set
// explicitly. The Host's seedColor tints iOS/Android labels correctly, but
// the web renderer (the smoke target) paints text buttons in a mid-grey step
// of the generated scale -- poor contrast in dark mode -- so the colour is
// stated once here instead of being repeated by every screen. 44pt target
// (height); the accessible name is the label. A Host is part of the unit, so
// callers pass `style` only to place it (e.g. alignSelf in a row).

import { StyleProp, ViewStyle } from 'react-native';
import { Button, Text } from '@expo/ui';
import { useAppTheme } from '../theme/ThemeProvider';
import { UiHost } from './UiHost';

export type TextButtonTone = 'default' | 'danger';

export function TextButton({
  label,
  onPress,
  tone = 'default',
  disabled,
  style,
}: {
  label: string;
  onPress: () => void;
  tone?: TextButtonTone;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const theme = useAppTheme();
  const color = tone === 'danger' ? theme.status.danger : theme.foreground;
  return (
    <UiHost style={style}>
      <Button variant="text" onPress={onPress} disabled={disabled} style={{ height: 44 }}>
        <Text textStyle={{ color, fontSize: theme.type.label.fontSize, fontWeight: theme.type.label.fontWeight }}>{label}</Text>
      </Button>
    </UiHost>
  );
}
