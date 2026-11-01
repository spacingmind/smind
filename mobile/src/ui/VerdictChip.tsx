// VerdictChip.tsx is the steward verdict as one chip: the registry icon and
// the Badge, side by side in one accessible row. The label and icon come from
// verdictChip (view.ts), so the mapping is unit-tested without rendering; the
// icon's colour follows the tone through the AA text-tier status tokens (the
// Badge already tints its own background and text).

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { useAppTheme } from '../theme/ThemeProvider';
import { verdictChip } from '../steward/view';
import { AppIcon } from './AppIcon';
import { Badge } from './Badge';

export function VerdictChip({ verdict }: { verdict: string | undefined }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const chip = verdictChip(verdict);
  const color =
    chip.tone === 'success'
      ? theme.status.success
      : chip.tone === 'warning'
        ? theme.status.warning
        : chip.tone === 'destructive'
          ? theme.status.danger
          : theme.foregroundSubtle;
  return (
    <View style={styles.row} accessible accessibilityLabel={`Verdict: ${chip.text}`}>
      <AppIcon name={chip.icon} size={14} color={color} />
      <Badge tone={chip.tone}>{chip.text}</Badge>
    </View>
  );
}

function makeStyles(theme: ReturnType<typeof useAppTheme>) {
  return StyleSheet.create({
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1] },
  });
}
