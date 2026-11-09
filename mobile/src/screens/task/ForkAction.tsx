// ForkAction: the "Fork from here" control under a finished run's
// transcript (ADR-0057 PR 1b AC19) -- the screen owns the confirms and
// the call, this renders the button (disabled while a fork is in flight)
// and the inline error line a refusal leaves. ErrorCard's danger colour
// token, not a hex literal.

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { TextButton } from '../../ui/TextButton';

export const ForkAction = memo(function ForkAction({ onPress, busy, error }: { onPress: () => void; busy: boolean; error: string | null }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.row}>
      <TextButton label={busy ? 'Forking…' : 'Fork from here'} onPress={onPress} disabled={busy} style={styles.button} />
      {error !== null ? (
        <Text style={styles.error} accessibilityRole="text" selectable>
          {error}
        </Text>
      ) : null}
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { gap: theme.spacing[1], alignSelf: 'flex-start', alignItems: 'flex-start' },
    button: { alignSelf: 'flex-start' },
    error: { ...theme.type.small, color: theme.status.danger },
  });
}
