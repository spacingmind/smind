// AutomationsScreen (ADR-0044 section 10, AC21): the READ-ONLY automations list. It shows what the daemon reports and
// offers no create/edit/enable/resume/fire control: those are for the computer (the daemon refuses them over the relay
// anyway). The only action on the phone is the Pause brake, on the detail screen. Data: automation.list, refetched on
// automation.* events.

import { useMemo } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAutomations } from '../automations/useAutomations';
import { automationsListView, MANAGE_ON_COMPUTER_TEXT } from '../automations/listView';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { AutomationRow } from './automations/AutomationRow';

export function AutomationsScreen({ conn, onOpenAutomation }: { conn: RelayConnection; onOpenAutomation: (id: number) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { state, reload } = useAutomations(conn);

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.loading}>Loading automations…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load automations" body={state.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  const view = automationsListView(state.value);

  return (
    <ScrollView
      style={styles.screen}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={styles.content}
      refreshControl={<RefreshControl refreshing={false} onRefresh={reload} />}
    >
      {view.kind === 'off' && (
        <View style={styles.notice} accessibilityRole="alert">
          <Text style={styles.noticeText}>{view.notice}</Text>
        </View>
      )}
      {view.kind !== 'off' && view.notice !== null && (
        <View style={styles.notice} accessibilityRole="alert">
          <Text style={styles.noticeText}>{view.notice}</Text>
        </View>
      )}
      {view.kind === 'empty' && (
        <View style={styles.center}>
          <EmptyState title="No automations" body={view.text} />
        </View>
      )}
      {view.kind === 'list' && (
        <View style={styles.list}>
          {view.rows.map((row) => (
            <AutomationRow key={row.id} row={row} onPress={onOpenAutomation} />
          ))}
        </View>
      )}
      <Text style={styles.hint}>{MANAGE_ON_COMPUTER_TEXT}</Text>
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[3] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    loading: { ...theme.type.small, color: theme.foregroundSubtle, padding: theme.spacing[4] },
    list: { gap: theme.spacing[2] },
    notice: {
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    noticeText: { ...theme.type.body, color: theme.foreground },
    hint: { ...theme.type.small, color: theme.foregroundSubtle, marginTop: theme.spacing[2] },
  });
}
