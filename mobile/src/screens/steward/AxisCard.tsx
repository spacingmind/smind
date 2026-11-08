// AxisCard is one review pass of a two-axis steward review (ADR-0054): the axis name, its status
// chip, the pass summary, the model and, for the spec axis, the criteria checklist behind a
// >= 44 pt toggle. Read-only: nothing here answers or lands anything. Every string is
// daemon-provided and drawn through <Text>, never interpreted. No cost is ever drawn.

import { useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { StewardAxis } from '../../steward/types';
import { axisLabel, axisStatusLabel, blockedReasonLabel, criteriaCounts } from '../../steward/view';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { AxisChip, toneColor } from '../../ui/AxisChip';
import { Card } from '../../ui/Card';
import { IconName } from '../../ui/icons';

const COLLAPSE_ABOVE = 3;

function criterionView(status: string): { word: string; icon: IconName; tone: 'success' | 'destructive' | 'neutral' } {
  if (status === 'met') return { word: 'Met', icon: 'check', tone: 'success' };
  if (status === 'unmet') return { word: 'Unmet', icon: 'close', tone: 'destructive' };
  return { word: 'Unverifiable', icon: 'info', tone: 'neutral' };
}

export function AxisCard({ axis }: { axis: StewardAxis }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const criteria = Array.isArray(axis.criteria) ? axis.criteria : [];
  const [open, setOpen] = useState(criteria.length <= COLLAPSE_ABOVE);
  const chip = axisStatusLabel(axis);
  const noSpec = axis.status === 'skipped' && axis.blockedReason === 'no_spec';
  const counts = criteriaCounts(criteria);
  const countLine = [`${counts.met} met`, `${counts.unmet} unmet`, ...(counts.unverifiable > 0 ? [`${counts.unverifiable} unverifiable`] : [])].join(', ');
  const showReason = axis.status === 'blocked' && axis.blockedReason !== 'not_run' && !!axis.blockedReason;
  return (
    <Card style={styles.card}>
      <View style={styles.head}>
        <Text style={styles.name}>{axisLabel(axis.axis)}</Text>
        <AxisChip text={chip.text} tone={chip.tone} icon={chip.icon} label={`${axisLabel(axis.axis)}: ${chip.text}`} />
      </View>
      {!noSpec && !!axis.summary && (
        <Text style={styles.body} selectable>
          {axis.summary}
        </Text>
      )}
      {!!axis.model && (
        <Text style={styles.mono} selectable>
          {axis.model}
        </Text>
      )}
      {showReason && <Text style={styles.danger}>{blockedReasonLabel(axis.blockedReason)}</Text>}
      {criteria.length > 0 && (
        <View style={styles.criteria}>
          <Text style={styles.small}>{countLine}</Text>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={open ? 'Hide criteria' : 'Show criteria'}
            accessibilityState={{ expanded: open }}
            onPress={() => setOpen((v) => !v)}
            style={styles.toggle}
          >
            <Text style={styles.toggleText}>{open ? 'Hide criteria' : 'Show criteria'}</Text>
          </Pressable>
          {open &&
            criteria.map((c, i) => {
              const v = criterionView(c.status);
              return (
                <View key={`${c.id}:${i}`} style={styles.criterion}>
                  <AppIcon name={v.icon} size={16} color={toneColor(theme, v.tone)} />
                  <View style={styles.criterionColumn}>
                    <View style={styles.criterionFirst}>
                      <Text style={v.tone === 'destructive' ? styles.danger : styles.small}>{v.word}</Text>
                      <Text style={styles.mono} selectable>
                        {c.id}
                      </Text>
                    </View>
                    {!!c.evidence && (
                      <Text style={styles.small} selectable>
                        {c.evidence}
                      </Text>
                    )}
                  </View>
                </View>
              );
            })}
        </View>
      )}
    </Card>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { gap: theme.spacing[2] },
    head: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    name: { ...theme.type.label, color: theme.foreground },
    body: { ...theme.type.body, color: theme.foreground },
    small: { ...theme.type.small, color: theme.foregroundSubtle },
    mono: { ...theme.type.mono, color: theme.foregroundSubtle },
    danger: { ...theme.type.small, color: theme.status.danger },
    criteria: { gap: theme.spacing[2] },
    toggle: { minHeight: 44, justifyContent: 'center' },
    toggleText: { ...theme.type.label, color: theme.foreground },
    criterion: { flexDirection: 'row', gap: theme.spacing[2] },
    criterionColumn: { flexShrink: 1, flex: 1, gap: 2 },
    criterionFirst: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
  });
}
