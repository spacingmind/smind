// PolicyScreen: the permission policy view, READ-ONLY. It draws policy.get (the validity banner, the file's
// path, the rule stack) and policy.audit (the recent decision stack, a row tappable to reveal the full target).
// No control here writes the policy: the file is edited on disk. Every daemon string is drawn as plain <Text>,
// never interpreted.

import { useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { usePolicy } from '../policy/usePolicy';
import { POLICY_EDIT_NOTE, auditRows, policyBanner, policyPath, ruleRows } from '../policy/view';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { AppIcon } from '../ui/AppIcon';
import { Badge, BadgeTone } from '../ui/Badge';
import { Card } from '../ui/Card';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { SectionHeader } from './tasks/SectionHeader';

export function PolicyScreen({ conn }: { conn: RelayConnection }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { state, reload } = usePolicy(conn);
  const [expanded, setExpanded] = useState<string | null>(null);

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading policy…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load the policy" body={state.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }

  const banner = policyBanner(state.status);
  const rules = ruleRows(state.status.rules);
  const decisions = auditRows(state.rows, Date.now());

  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <View accessibilityRole={banner.tone === 'danger' ? 'alert' : undefined}>
        <Card>
          <View style={styles.bannerRow}>
            <AppIcon
              name={banner.tone === 'danger' ? 'warning' : banner.tone === 'success' ? 'check' : 'info'}
              color={banner.tone === 'danger' ? theme.status.danger : banner.tone === 'success' ? theme.status.success : theme.foregroundSubtle}
            />
            <View style={styles.bannerColumn}>
              <Text style={styles.bannerTitle}>{banner.title}</Text>
              {banner.detail !== null ? <Text style={styles.muted}>{banner.detail}</Text> : null}
            </View>
          </View>
        </Card>
      </View>
      <SectionHeader title="Policy file" />
      <Card style={styles.stack}>
        <Text style={styles.monoWrapped} selectable>
          {policyPath(state.status)}
        </Text>
        <Text style={styles.muted}>{POLICY_EDIT_NOTE}</Text>
      </Card>
      <SectionHeader title="Rules" />
      {rules.length === 0 ? (
        <Text style={styles.muted}>No rules.</Text>
      ) : (
        rules.map((rule) => (
          <Card key={rule.id} style={styles.stack}>
            <View style={styles.wrapRow}>
              <Text style={styles.monoId} numberOfLines={1}>
                {rule.id}
              </Text>
              <Badge tone={actionTone(rule.action)}>{rule.action}</Badge>
            </View>
            <Text style={styles.line}>{rule.why}</Text>
            <Text style={styles.muted}>{`Scope: ${rule.scope}`}</Text>
          </Card>
        ))
      )}
      <SectionHeader title="Recent decisions" />
      {decisions.length === 0 ? (
        <Text style={styles.muted}>No decisions recorded yet.</Text>
      ) : (
        decisions.map((row) => {
          const open = expanded === row.key;
          return (
            <Pressable
              key={row.key}
              accessibilityRole="button"
              accessibilityLabel={`${row.decision} ${row.target}. Tap for the full text`}
              style={styles.press}
              onPress={() => setExpanded(open ? null : row.key)}
            >
              <Card style={styles.stack}>
                <View style={styles.wrapRow}>
                  <Badge tone={decisionTone(row.decision)}>{row.decision}</Badge>
                  <Text style={styles.monoId} numberOfLines={1}>
                    {row.ruleId}
                  </Text>
                  <Text style={styles.muted}>{row.time}</Text>
                </View>
                <Text style={styles.monoWrapped} selectable={open} numberOfLines={open ? undefined : 2}>
                  {open ? row.fullTarget : row.target}
                </Text>
                <Text style={styles.muted}>{`Seat: ${row.seat}`}</Text>
              </Card>
            </Pressable>
          );
        })
      )}
    </ScrollView>
  );
}

function actionTone(action: string): BadgeTone {
  if (action === 'allow') return 'success';
  if (action === 'deny') return 'destructive';
  if (action === 'ask') return 'waiting';
  return 'neutral';
}

function decisionTone(decision: string): BadgeTone {
  if (decision.startsWith('allow') || decision === 'ask_human_allow') return 'success';
  if (decision.includes('deny')) return 'destructive';
  if (decision === 'ask') return 'waiting';
  return 'neutral';
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[4] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    stack: { gap: theme.spacing[2] },
    bannerRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[3] },
    bannerColumn: { flexShrink: 1, gap: theme.spacing[1] },
    bannerTitle: { ...theme.type.label, color: theme.foreground },
    wrapRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    monoId: { ...theme.type.mono, color: theme.foreground, flexShrink: 0 },
    monoWrapped: { ...theme.type.mono, color: theme.foreground, flexShrink: 1 },
    line: { ...theme.type.body, color: theme.foreground },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    press: { minHeight: 44 },
  });
}
