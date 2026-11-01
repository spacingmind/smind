// PermissionPolicyLine: the policy footprint inside a permission card -- the matched rule (or the miss), its
// why, and the FULL command the engine judged, wrapped and vertically scrollable inside a bordered box so
// nothing is clipped or ellipsised. Display only: no button, no response action; every daemon string is drawn
// as plain <Text>, never interpreted.

import { useMemo } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import type { PermissionDetail } from '../../permissionRequests';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { permissionCommand, permissionPolicyLine } from '../../transcript/permissionPolicyLine';

export function PermissionPolicyLine({ detail }: { detail: PermissionDetail | undefined }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const line = permissionPolicyLine(detail);
  const cmd = permissionCommand(detail);
  if (line === null && cmd === null) return null;
  return (
    <View style={styles.column}>
      {line !== null ? (
        <View style={styles.titleRow}>
          <AppIcon name="lock" size={14} color={theme.foregroundSubtle} />
          <Text style={styles.muted}>
            {line.ruleId !== null ? (
              <>
                Matched rule <Text style={theme.type.mono}>{line.ruleId}</Text>
              </>
            ) : (
              line.title
            )}
          </Text>
        </View>
      ) : null}
      {line !== null && line.why !== null ? <Text style={styles.muted}>{line.why}</Text> : null}
      {cmd !== null ? (
        <View accessibilityLabel="Command to run" style={styles.box}>
          <ScrollView nestedScrollEnabled>
            <Text style={styles.mono} selectable>
              {cmd}
            </Text>
          </ScrollView>
        </View>
      ) : null}
      {detail?.truncated ? <Text style={styles.muted}>Truncated</Text> : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    column: { gap: theme.spacing[2] },
    titleRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[1.5] },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    box: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      backgroundColor: theme.surfaceSubtle,
      padding: theme.spacing[2],
      maxHeight: 240,
    },
    mono: { ...theme.type.mono, color: theme.foreground, flexShrink: 1 },
  });
}
