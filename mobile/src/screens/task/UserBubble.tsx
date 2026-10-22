// UserBubble: the right-aligned user message (ZCode look): `secondary` fill,
// radius.lg, reading type. Memoised with primitive props (vercel-react-native-
// skills: list items are memo'd and take primitives); selectable text (expo-native-ui).

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

export const UserBubble = memo(function UserBubble({ text }: { text: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.bubble}>
      <Text style={styles.text} selectable>
        {text}
      </Text>
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    bubble: {
      alignSelf: 'flex-end',
      maxWidth: '85%',
      backgroundColor: theme.secondary,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      paddingHorizontal: theme.spacing[4],
      paddingVertical: theme.spacing[3],
    },
    text: { ...theme.type.reading, color: theme.foreground },
  });
}
