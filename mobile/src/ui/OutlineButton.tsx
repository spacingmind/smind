// OutlineButton.tsx is the quiet secondary action: a bordered pill-less rounded button with a 44pt minimum
// target, the mobile twin of the web's `variant="outline"` button (Decline, Always allow in this project, Revoke).
// Plain Pressable: the label is the accessible name, the disabled state is also stated to assistive tech.

import { useMemo } from 'react';
import { Pressable, StyleProp, StyleSheet, Text, ViewStyle } from 'react-native';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export function OutlineButton({
  label,
  onPress,
  disabled,
  tone = 'default',
  style,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  tone?: 'default' | 'danger';
  style?: StyleProp<ViewStyle>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: disabled === true }}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [styles.button, tone === 'danger' && styles.danger, disabled && styles.disabled, pressed && styles.pressed, style]}
    >
      <Text style={[styles.label, tone === 'danger' && styles.dangerLabel]}>{label}</Text>
    </Pressable>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    button: {
      minHeight: 44,
      paddingHorizontal: theme.spacing[4],
      alignItems: 'center',
      justifyContent: 'center',
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      borderWidth: 1,
      borderColor: theme.border,
      backgroundColor: theme.card,
    },
    danger: { borderColor: theme.status.danger },
    disabled: { opacity: 0.45 },
    pressed: { opacity: 0.7 },
    label: { ...theme.type.label, color: theme.foreground },
    dangerLabel: { color: theme.status.danger },
  });
}
