// SavedGrantsScreen: every "always allow in this project" decision, with the exact command, class, status,
// expiry and use count, and a Revoke for any that is not already revoked. A grant is created only on a permission
// card by a human; this screen can only read and revoke. The command is agent-authored text: plain <Text> only.

import { useMemo, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useGrants } from '../grants/useGrants';
import { GrantRowView, grantRevokeCopy, grantRows } from '../grants/view';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Badge, BadgeTone } from '../ui/Badge';
import { Card } from '../ui/Card';
import { EmptyState } from '../ui/EmptyState';
import { OutlineButton } from '../ui/OutlineButton';
import { TextButton } from '../ui/TextButton';
import { confirmNative } from '../ui/confirm';

const STATUS_TONE: Record<string, BadgeTone> = { active: 'success', void: 'warning' };

export function SavedGrantsScreen({ conn }: { conn: RelayConnection }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { state, reload, revoke } = useGrants(conn);
  const [error, setError] = useState<string | null>(null);

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading saved grants…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load the saved grants" body={state.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  const rows = grantRows(state.grants, Date.now());

  async function onRevoke(row: GrantRowView) {
    const ok = await confirmNative('Revoke this grant?', grantRevokeCopy(row.command), 'Revoke');
    if (!ok) return;
    setError(null);
    try {
      await revoke(row.id);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
    }
  }

  return <SavedGrantsList rows={rows} error={error} onReload={() => {
    setError(null);
    reload();
  }} onRevoke={(row) => void onRevoke(row)} />;
}

/** The list itself, presentational: the screen feeds it rows; the frames feed it fixtures. */
export function SavedGrantsList({ rows, error, onReload, onRevoke }: { rows: GrantRowView[]; error: string | null; onReload: () => void; onRevoke: (row: GrantRowView) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content} refreshControl={<RefreshControl refreshing={false} onRefresh={onReload} />}>
      {error !== null ? (
        <Text style={styles.error} selectable>
          {`Couldn't revoke: ${error}`}
        </Text>
      ) : null}
      {rows.length === 0 ? (
        <Text style={styles.muted}>Nothing saved. When a permission card offers "Always allow in this project", the commands you save appear here, and you can revoke them.</Text>
      ) : (
        rows.map((row) => (
          <Card key={row.id} style={styles.stack}>
            <View style={styles.wrapRow}>
              <Badge tone={STATUS_TONE[row.status] ?? 'neutral'}>{row.statusLabel}</Badge>
              <Badge tone={row.isRunner ? 'warning' : 'neutral'}>{row.classLabel}</Badge>
              <Text style={styles.muted}>{row.expiry}</Text>
            </View>
            <Text style={styles.mono} selectable>
              {row.command}
            </Text>
            <Text style={styles.muted} selectable>
              {row.folderLine}
            </Text>
            {row.optInLine !== null ? <Text style={styles.muted}>{row.optInLine}</Text> : null}
            <Text style={styles.muted}>{row.usageLine}</Text>
            {row.hint !== '' ? <Text style={styles.muted}>{row.hint}</Text> : null}
            {row.canRevoke ? <OutlineButton label="Revoke" tone="danger" onPress={() => onRevoke(row)} style={styles.revoke} /> : null}
          </Card>
        ))
      )}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    center: { flex: 1, backgroundColor: theme.background, justifyContent: 'center' },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[3] },
    stack: { gap: theme.spacing[2] },
    wrapRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    mono: { ...theme.type.mono, color: theme.foreground },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    error: { ...theme.type.small, color: theme.status.danger },
    revoke: { alignSelf: 'flex-start' },
  });
}
