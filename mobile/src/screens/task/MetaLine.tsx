// MetaLine: a centered status line between hairlines (e.g. `done (end_turn)`).

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

export const MetaLine = memo(function MetaLine({ text }: { text: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.row}>
      <View style={styles.rule} />
      <Text style={styles.text} selectable>
        {text}
      </Text>
      <View style={styles.rule} />
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[3] },
    rule: { flex: 1, height: StyleSheet.hairlineWidth, backgroundColor: theme.border },
    text: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
