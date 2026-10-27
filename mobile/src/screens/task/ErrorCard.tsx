// ErrorCard: a failed run's error -- the plain sentence, and the stored raw
// text (transport chain, the provider's own wording and its reset time in its
// own zone) behind a "Details" toggle. See runError.ts.

import { memo, useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Card } from '../../ui/Card';
import { StatusDot } from '../../ui/StatusDot';

export const ErrorCard = memo(function ErrorCard({ message, raw, zoneNote }: { message: string; raw?: string; zoneNote?: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [open, setOpen] = useState(false);
  return (
    <Card tone="subtle" style={styles.card}>
      <View style={styles.row}>
        <StatusDot tone="danger" />
        <Text style={styles.message} selectable>
          {message}
        </Text>
      </View>
      {raw ? (
        <>
          <Pressable onPress={() => setOpen((v) => !v)} accessibilityRole="button" accessibilityState={{ expanded: open }} hitSlop={8} style={styles.toggle}>
            <Text style={styles.toggleText}>{open ? 'Hide details' : 'Details'}</Text>
          </Pressable>
          {open ? (
            <View style={styles.raw}>
              <Text style={styles.rawText} selectable>
                {raw}
              </Text>
              {zoneNote ? <Text style={styles.note}>{zoneNote}</Text> : null}
            </View>
          ) : null}
        </>
      ) : null}
    </Card>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { gap: theme.spacing[2] },
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    message: { ...theme.type.body, color: theme.status.danger, flex: 1 },
    toggle: { alignSelf: 'flex-start', minHeight: 32, justifyContent: 'center' },
    toggleText: { ...theme.type.small, color: theme.foregroundSubtle },
    raw: { gap: theme.spacing[2] },
    rawText: { ...theme.type.small, color: theme.foregroundSubtle },
    note: { ...theme.type.small, color: theme.foregroundSubtlest },
  });
}
