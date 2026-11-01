// GateNotice is the line under the provider/model fields when the daemon gates the chosen provider or model
// (internal/gate): the existing Badge, the existing note text, and for clear_cooldown the existing TextButton as the
// tap target. No new style; what to show comes from providerGate.ts.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { GateNotice as Notice } from '../../providerGate';
import { Badge } from '../../ui/Badge';
import { TextButton } from '../../ui/TextButton';

export function GateNotice({ notice, busy, onClear }: { notice: Notice; busy: boolean; onClear: (provider: string) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const action = notice.action;
  return (
    <View style={styles.box} testID="gate-notice" accessibilityRole="alert">
      <View style={styles.head}>
        <Badge tone={notice.tone}>{notice.badge}</Badge>
        <Text style={styles.text} testID="gate-notice-text">
          {notice.text}
        </Text>
      </View>
      {action?.kind === 'hint' ? <Text style={styles.text}>{action.text}</Text> : null}
      {action?.kind === 'clear_cooldown' ? (
        <TextButton label={busy ? 'Clearing…' : action.label} onPress={() => onClear(action.provider)} disabled={busy} style={styles.button} />
      ) : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    box: { gap: theme.spacing[2] },
    head: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2], flexWrap: 'wrap' },
    text: { ...theme.type.small, color: theme.foregroundSubtle, flexShrink: 1 },
    button: { alignSelf: 'flex-start' },
  });
}
