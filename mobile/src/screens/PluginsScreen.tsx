// PluginsScreen (ADR-0042 PA.5, AC15): the READ-ONLY plugin list. It shows what the daemon reports and offers no
// enable/disable/install/remove/update control: those are for the computer (the daemon refuses them over the relay
// anyway). It also hosts the plugin theme choice, a local appearance preference. Data: plugin.list and
// plugin.themes, both refetched on plugin.updated.

import { useCallback, useMemo, useSyncExternalStore } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { fetchPluginList, fetchPluginThemes } from '../plugins/api';
import { MANAGE_ON_COMPUTER_TEXT, pluginsListView } from '../plugins/listView';
import { securePluginThemePrefs } from '../plugins/native/securePrefs';
import { themeSupport } from '../plugins/themeMap';
import { pluginThemeStore } from '../plugins/themeStore';
import { usePluginResource } from '../plugins/usePluginResource';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { EmptyState } from '../ui/EmptyState';
import { PluginRow } from './plugins/PluginRow';
import { ThemePicker } from './plugins/ThemePicker';
import { SectionHeader } from './tasks/SectionHeader';
import { TextButton } from '../ui/TextButton';

export function PluginsScreen({ conn, onOpenPlugin }: { conn: RelayConnection; onOpenPlugin: (id: string) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const list = usePluginResource(conn, fetchPluginList);
  const themes = usePluginResource(conn, fetchPluginThemes);
  const selected = useSyncExternalStore(pluginThemeStore.subscribe, () => pluginThemeStore.get().selected);
  const choose = useCallback((id: string | null) => {
    pluginThemeStore.select(id);
    void securePluginThemePrefs.save(id);
  }, []);
  const refresh = useCallback(() => {
    list.reload();
    themes.reload();
  }, [list, themes]);

  if (list.state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.loading}>Loading plugins…</Text>
      </View>
    );
  }
  if (list.state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load plugins" body={list.state.message}>
          <TextButton label="Retry" onPress={list.reload} />
        </EmptyState>
      </View>
    );
  }
  const view = pluginsListView(list.state.value);
  const offered = themes.state.kind === 'ready' ? themes.state.value : [];

  return (
    <ScrollView
      style={styles.screen}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={styles.content}
      refreshControl={<RefreshControl refreshing={false} onRefresh={refresh} />}
    >
      {view.kind === 'off' && (
        <View style={styles.notice} accessibilityRole="alert">
          <Text style={styles.noticeText}>{view.notice}</Text>
        </View>
      )}
      {view.kind === 'empty' && (
        <View style={styles.center}>
          <EmptyState title="No plugins" body={view.text} />
        </View>
      )}
      {(view.kind === 'list' || view.kind === 'off') && view.rows.length > 0 && (
        <View style={styles.list}>
          {view.rows.map((row) => (
            <PluginRow key={row.id} row={row} onPress={onOpenPlugin} />
          ))}
        </View>
      )}
      {offered.length > 0 && (
        <>
          <SectionHeader title="Themes" />
          <ThemePicker themes={offered} selected={selected} support={themeSupport} onSelect={choose} />
        </>
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
