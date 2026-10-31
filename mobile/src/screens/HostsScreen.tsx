// HostsScreen (ADR-0048 F1c): the fleet host list, read-only except for approving or rejecting a pending host.
// A phone sends only the read-only grant (hosts/api.ts) and the human types the six digits the other computer
// shows, so the approval cannot be confirmed blindly. Pairing, grant editing and reverse toggles stay on a
// computer (S13). Rows show alias, label, status and detail only: never a key, address or secret.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { approveHost, fetchHosts, rejectHost } from '../hosts/api';
import { FleetHost, HostList } from '../hosts/types';
import { approveEnabled, formatSas, hostsView, HostRowView } from '../hosts/view';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Badge, BadgeTone } from '../ui/Badge';
import { Card } from '../ui/Card';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';

type Load = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'ready'; list: HostList };

const TONE: Record<HostRowView['tone'], BadgeTone> = { ok: 'success', warning: 'warning', danger: 'destructive', neutral: 'neutral' };

export function HostsScreen({ conn, focus }: { conn: RelayConnection; focus?: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [load, setLoad] = useState<Load>({ kind: 'loading' });
  const [panelHost, setPanelHost] = useState<string | null>(null);
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const focused = useRef(false);

  const reload = useCallback(() => {
    fetchHosts(conn).then(
      (list) => setLoad({ kind: 'ready', list }),
      (e) => setLoad({ kind: 'error', message: e instanceof Error ? e.message : String(e) }),
    );
  }, [conn]);
  useEffect(reload, [reload]);

  const closePanel = useCallback(() => {
    setPanelHost(null);
    setTyped('');
    setError(null);
  }, []);

  // The focused host's panel opens once, on the first load that shows it as approvable.
  useEffect(() => {
    if (focused.current || !focus || load.kind !== 'ready') return;
    const target = load.list.hosts.find((h) => h.hostId === focus);
    if (!target) return;
    focused.current = true;
    if (target.pendingApproval && target.sas) setPanelHost(target.hostId);
  }, [focus, load]);

  const decide = useCallback(
    (op: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      op()
        .then(() => {
          closePanel();
          reload();
        })
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
        .finally(() => setBusy(false));
    },
    [closePanel, reload],
  );

  if (load.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.loading}>Loading hosts…</Text>
      </View>
    );
  }
  if (load.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load hosts" body={load.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  const view = hostsView(load.list);
  const byId = new Map<string, FleetHost>(load.list.hosts.map((h) => [h.hostId, h]));

  return (
    <ScrollView
      style={styles.screen}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={styles.content}
      keyboardShouldPersistTaps="handled"
      refreshControl={<RefreshControl refreshing={false} onRefresh={reload} />}
    >
      {(view.kind === 'off' || view.kind === 'empty') && (
        <View style={styles.center}>
          <EmptyState title={view.title} body={view.text} />
        </View>
      )}
      {view.kind === 'list' &&
        view.rows.map((row) => {
          const host = byId.get(row.key);
          const open = row.canApprove && host !== undefined && panelHost === row.key;
          return (
            <Card key={row.key} style={styles.card}>
              <View style={styles.top}>
                <View style={styles.titles}>
                  <Text style={styles.alias} numberOfLines={1}>
                    {row.alias}
                  </Text>
                  <Text style={styles.label} numberOfLines={1}>
                    {row.label}
                  </Text>
                </View>
                <Badge tone={TONE[row.tone]}>{row.status}</Badge>
              </View>
              {row.detail !== null && <Text style={styles.detail}>{row.detail}</Text>}
              {row.canApprove && !open && <TextButton label="Approve" onPress={() => { setTyped(''); setError(null); setPanelHost(row.key); }} style={styles.action} />}
              {open && host && (
                <View style={styles.panel}>
                  <Text style={styles.body}>{`Check that ${host.alias} shows the same code. Approve only if you started this pairing.`}</Text>
                  <Text style={styles.sas} accessibilityLabel={`Code ${formatSas(host.sas ?? '')}`}>
                    {formatSas(host.sas ?? '')}
                  </Text>
                  <TextInput
                    style={styles.input}
                    value={typed}
                    onChangeText={setTyped}
                    keyboardType="number-pad"
                    autoComplete="off"
                    textContentType="none"
                    autoCorrect={false}
                    maxLength={7}
                    placeholder="Type the 6 digits"
                    placeholderTextColor={theme.foregroundSubtlest}
                    accessibilityLabel="Type the 6 digit code"
                  />
                  <Text style={styles.detail}>This approves read-only access. Widen it on a computer.</Text>
                  {error !== null && (
                    <Text style={styles.error} accessibilityRole="alert">
                      {error}
                    </Text>
                  )}
                  <View style={styles.actions}>
                    <TextButton label="Approve" disabled={busy || !approveEnabled(host, typed)} onPress={() => decide(() => approveHost(conn, host.hostId, typed))} />
                    <TextButton label="Reject" tone="danger" disabled={busy} onPress={() => decide(() => rejectHost(conn, host.hostId))} />
                    <TextButton label="Cancel" disabled={busy} onPress={closePanel} />
                  </View>
                </View>
              )}
            </Card>
          );
        })}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[3] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    loading: { ...theme.type.small, color: theme.foregroundSubtle, padding: theme.spacing[4] },
    card: { padding: theme.spacing[3], gap: theme.spacing[2] },
    top: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    titles: { flex: 1, gap: theme.spacing[0.5] },
    alias: { ...theme.type.label, color: theme.foreground },
    label: { ...theme.type.small, color: theme.foregroundSubtle },
    detail: { ...theme.type.small, color: theme.foregroundSubtle },
    body: { ...theme.type.body, color: theme.foreground },
    action: { alignSelf: 'flex-start' },
    panel: { gap: theme.spacing[3], borderTopWidth: 1, borderTopColor: theme.border, paddingTop: theme.spacing[3] },
    sas: { ...theme.type.mono, fontSize: 28, lineHeight: 36, fontWeight: '600', color: theme.foreground, textAlign: 'center', fontVariant: ['tabular-nums'] },
    input: {
      ...theme.type.body,
      // Web: 16px keeps iOS Safari from zooming on focus (expo-native-ui input floor).
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize,
      color: theme.foreground,
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
      minHeight: 44,
    },
    error: { ...theme.type.small, color: theme.status.danger },
    actions: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
  });
}
