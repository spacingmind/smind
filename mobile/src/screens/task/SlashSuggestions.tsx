// SlashSuggestions: the compact `/` list above the composer (ADR-0042 AC13). It receives rows and one onPick(row);
// it has no way to submit in scope at all, so picking can only insert. Command text is plugin-authored and drawn as
// plain <Text>, never interpreted.

import { useMemo } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { SlashCommand } from '../../plugins/slashCommands';
import { SlashRpcStatus } from '../../plugins/slashRpc';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

const MAX_HEIGHT = 176;

export function SlashSuggestions({ rows, onPick, status = null }: { rows: SlashCommand[]; onPick: (row: SlashCommand) => void; status?: SlashRpcStatus | null }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  if (rows.length === 0) return null;
  return (
    <View style={styles.box} accessibilityRole="menu" accessibilityLabel="Prompt commands">
      <ScrollView keyboardShouldPersistTaps="handled" style={styles.scroll}>
        {rows.map((row, i) => (
          <Pressable
            key={row.key}
            accessibilityRole="menuitem"
            accessibilityLabel={`/${row.label}`}
            onPress={() => onPick(row)}
            style={[styles.row, i > 0 && styles.divider]}
          >
            <Text style={styles.label} numberOfLines={1}>
              /{row.label}
            </Text>
            <Text style={styles.detail} numberOfLines={1}>
              {status?.state === 'running' && status.key === row.key
                ? 'Running…'
                : row.builtin
                  ? row.description
                  : `from ${row.source}${row.description ? ` · ${row.description}` : ''}`}
            </Text>
          </Pressable>
        ))}
      </ScrollView>
      {status?.state === 'error' && (
        <Text style={styles.error} accessibilityRole="alert">
          {status.message}
        </Text>
      )}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    box: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      marginBottom: theme.spacing[2],
      overflow: 'hidden',
    },
    scroll: { maxHeight: MAX_HEIGHT },
    row: { minHeight: 44, paddingHorizontal: theme.spacing[3], paddingVertical: theme.spacing[2], justifyContent: 'center' },
    divider: { borderTopWidth: 1, borderTopColor: theme.border },
    label: { ...theme.type.label, color: theme.foreground },
    error: { ...theme.type.small, color: theme.status.danger, paddingHorizontal: theme.spacing[3], paddingVertical: theme.spacing[2], borderTopWidth: 1, borderTopColor: theme.border },
    detail: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
