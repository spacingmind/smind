// The Themes section of the Plugins screen: "Default" plus every theme the daemon offers. Choosing one is a local
// appearance preference on this phone, not a plugin change; a theme that mobile cannot render says so instead of
// pretending to apply.

import { useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { ThemeSupport, themeCaption } from '../../plugins/themeCaption';
import { PluginTheme } from '../../plugins/types';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';

export function ThemePicker({
  themes,
  selected,
  support,
  onSelect,
}: {
  themes: readonly PluginTheme[];
  selected: string | null;
  support: (t: PluginTheme) => ThemeSupport;
  onSelect: (id: string | null) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const rows: { id: string | null; label: string; caption: string; disabled: boolean }[] = [
    { id: null, label: 'Default', caption: 'The app’s own colours', disabled: false },
    ...themes.map((t) => {
      const caption = themeCaption(t, support(t));
      return { id: t.qualified, label: t.label, caption, disabled: !support(t).light && !support(t).dark };
    }),
  ];
  return (
    <View style={styles.card} accessibilityRole="radiogroup">
      {rows.map((r, i) => {
        const on = r.id === selected;
        return (
          <Pressable
            key={r.id ?? 'default'}
            accessibilityRole="radio"
            accessibilityState={{ checked: on, disabled: r.disabled }}
            accessibilityLabel={r.label}
            disabled={r.disabled}
            onPress={() => onSelect(r.id)}
            style={[styles.row, i > 0 && styles.divider, r.disabled && styles.disabled]}
          >
            <View style={styles.texts}>
              <Text style={styles.label} numberOfLines={1}>
                {r.label}
              </Text>
              <Text style={styles.caption} numberOfLines={2}>
                {r.caption}
              </Text>
            </View>
            {on && <AppIcon name="check" size={18} color={theme.foreground} />}
          </Pressable>
        );
      })}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      overflow: 'hidden',
    },
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[3], minHeight: 52, paddingHorizontal: theme.spacing[3], paddingVertical: theme.spacing[2] },
    divider: { borderTopWidth: 1, borderTopColor: theme.border },
    disabled: { opacity: 0.55 },
    texts: { flex: 1 },
    label: { ...theme.type.label, color: theme.foreground },
    caption: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
