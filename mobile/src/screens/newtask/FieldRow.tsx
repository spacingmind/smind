// FieldRow is the form's label+control row (expo-design-system: one
// screen's private composition stays in screens/<name>/): a small label
// above a stretch-aligned child. @expo/ui controls go through UiHost at
// the caller (UiHost needs the stretch style per control).

import { ReactNode, useMemo } from 'react';
import { StyleProp, StyleSheet, Text, View, ViewStyle } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

export function FieldRow({ label, children, style }: { label: string; children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={[styles.row, style]}>
      <Text style={styles.label}>{label}</Text>
      {children}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { gap: theme.spacing[2], alignSelf: 'stretch' },
    label: { ...theme.type.label, color: theme.foregroundSubtle },
  });
}
