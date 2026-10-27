// The Tasks header's Inbox entry: a 44 pt button with the unseen count (inbox.count). The count is announced politely
// when it changes (AccessibilityInfo.announceForAccessibility never moves focus); the first load is silent.

import { useEffect, useMemo, useRef } from 'react';
import { AccessibilityInfo, Pressable, StyleSheet, Text, View } from 'react-native';
import { useInbox } from '../../inbox/useInbox';
import { inboxAnnouncement, inboxBadgeLabel } from '../../inbox/view';
import { RelayConnection } from '../../relay/RelayConnection';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Badge } from '../../ui/Badge';

export function InboxEntry({ conn, onPress }: { conn: RelayConnection; onPress: () => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { count } = useInbox(conn, false);
  const unseen = count?.unseen ?? 0;
  const prev = useRef<number | null>(null);

  useEffect(() => {
    if (count === null) return;
    const text = inboxAnnouncement(prev.current, count.unseen);
    prev.current = count.unseen;
    if (text) AccessibilityInfo.announceForAccessibility(text);
  }, [count]);

  return (
    <Pressable accessibilityRole="button" accessibilityLabel={inboxBadgeLabel(unseen)} onPress={onPress} style={styles.button}>
      <Text style={styles.label}>Inbox</Text>
      {unseen > 0 && (
        <View importantForAccessibility="no-hide-descendants" accessibilityElementsHidden>
          <Badge tone="waiting">{unseen > 99 ? '99+' : String(unseen)}</Badge>
        </View>
      )}
    </Pressable>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    button: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1], minHeight: 44, paddingHorizontal: theme.spacing[2] },
    label: { ...theme.type.label, color: theme.foreground },
  });
}
