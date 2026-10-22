// The Connection sheet's "Notifications" row (ADR-0040 section 6): a label and the
// current state like the Status/Device rows, plus an action button and a hint
// line when the state has them. Colours come from tokens only.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useConnectionOrNull } from '../../connection/ConnectionProvider';
import { usePushSettings } from '../../push/native/usePushSettings';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { TextButton } from '../../ui/TextButton';

export function NotificationsRow() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const connection = useConnectionOrNull();
  const { view, enable, disable } = usePushSettings(connection?.conn ?? null);

  return (
    <View>
      <View style={styles.row}>
        <Text style={styles.rowLabel}>Notifications</Text>
        <Text style={styles.rowValue} selectable>
          {view.value}
        </Text>
      </View>
      {view.action !== null && view.actionLabel !== null ? (
        <TextButton
          label={view.actionLabel}
          onPress={() => void (view.action === 'enable' ? enable() : disable())}
          style={styles.action}
        />
      ) : null}
      {view.hint !== null ? <Text style={styles.hint}>{view.hint}</Text> : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      gap: theme.spacing[3],
      minHeight: 44,
      paddingHorizontal: theme.spacing[4],
    },
    rowLabel: { ...theme.type.body, color: theme.foreground },
    rowValue: { ...theme.type.label, color: theme.foregroundSubtle, flexShrink: 1, textAlign: 'right' },
    action: { alignSelf: 'flex-start', paddingHorizontal: theme.spacing[4] },
    hint: { ...theme.type.label, color: theme.foregroundSubtle, paddingHorizontal: theme.spacing[4] },
  });
}
