// Card.tsx is the base content-card primitive (expo-design-system: props =
// variant + style, content via children, `style` merged last): the surface a
// list row or panel sits on. Rounded corners use borderCurve 'continuous'
// (expo-native-ui) so the radius renders as a smooth squircle on iOS.

import { ReactNode, useMemo } from 'react';
import { StyleSheet, StyleProp, View, ViewStyle } from 'react-native';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export type CardTone = 'default' | 'subtle';

export function Card({ children, tone = 'default', style }: { children: ReactNode; tone?: CardTone; style?: StyleProp<ViewStyle> }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const backgroundColor = tone === 'subtle' ? theme.surfaceSubtle : theme.card;
  return <View style={[styles.card, { backgroundColor }, style]}>{children}</View>;
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[4],
    },
  });
}
