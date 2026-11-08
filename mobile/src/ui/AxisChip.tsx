// AxisChip.tsx is one review-axis state as a chip: the registry icon and the Badge side by side in
// one accessible row (never colour alone), the same pattern as VerdictChip. Text, tone and icon
// come from axisStatusLabel (steward/view.ts). PartialChip is the neutral "Partial" marker a
// one-axis re-run wears next to its verdict.

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChipTone } from '../steward/view';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { AppIcon } from './AppIcon';
import { IconName } from './icons';
import { Badge } from './Badge';

export function toneColor(theme: AppTheme, tone: ChipTone): string {
  return tone === 'success' ? theme.status.success : tone === 'warning' ? theme.status.warning : tone === 'destructive' ? theme.status.danger : theme.foregroundSubtle;
}

export function AxisChip({ text, tone, icon, label }: { text: string; tone: ChipTone; icon: IconName; label?: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.row} accessible accessibilityLabel={label ?? text}>
      <AppIcon name={icon} size={14} color={toneColor(theme, tone)} />
      <Badge tone={tone}>{text}</Badge>
    </View>
  );
}

export function PartialChip() {
  return <AxisChip text="Partial" tone="neutral" icon="info" label="Partial review" />;
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1], flexShrink: 1 },
  });
}
