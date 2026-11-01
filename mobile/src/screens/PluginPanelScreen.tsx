// PluginPanelScreen (ADR-0042 section 5): one plugin panel, drawn read-only. The panel's view
// document comes from plugin.view and is rendered by PluginView (the pure model plus this app's
// components); it is refetched on plugin.updated, on every reconnect and on a plugin.event that
// touches this panel -- no per-plugin store, the last good view simply stays on screen. A panel's
// interactive surface is runViewAction with this screen supplying the effects (the rpc, the
// composer inbox, panel navigation, the url confirm); an outcome that is refused or failed is one
// inline line, plugin text is only ever drawn through Text.

import { useCallback, useMemo, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { callPluginRpc, fetchPluginCommands, fetchPluginPanels, fetchPluginView } from '../plugins/api';
import { setPendingInsert } from '../plugins/composerInbox';
import { pluginPanelHref } from '../navigation/routes';
import { openExternal } from '../plugins/native/openExternal';
import { eventTouchesPanel, panelErrorView, panelsOfPlugin } from '../plugins/panelView';
import { watchPluginEvents } from '../plugins/subscription';
import { usePluginResource } from '../plugins/usePluginResource';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { confirmNative } from '../ui/confirm';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { buildView } from '../plugins/view/model';
import { OnAction, PluginView } from '../plugins/view/PluginView';
import { ActionDeps, runViewAction } from '../plugins/view/runAction';

/** The panel resource: a built view, or the words of why the daemon's answer cannot be drawn. */
type PanelResource = { kind: 'view'; view: ReturnType<typeof buildView> } | { kind: 'failed'; title: string; body: string };

export function PluginPanelScreen({ conn, pluginId, panelId }: { conn: RelayConnection; pluginId: string; panelId: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const router = useRouter();
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  // The failure is a ready value (not a throw) so the daemon's error code keeps its words; a
  // refused panel looks exactly like a load that did not work, with a Retry.
  const load = useCallback(
    async (c: RelayConnection): Promise<PanelResource> => {
      try {
        return { kind: 'view', view: buildView(await fetchPluginView(c, pluginId, panelId)) };
      } catch (err) {
        return { kind: 'failed', ...panelErrorView(err) };
      }
    },
    [pluginId, panelId],
  );
  const watchExtra = useCallback(
    (c: RelayConnection, reload: () => void) =>
      watchPluginEvents(c, (p) => {
        if (eventTouchesPanel(p, pluginId, panelId)) reload();
      }),
    [pluginId, panelId],
  );
  const { state, reload } = usePluginResource(conn, load, watchExtra);
  const commandsRes = usePluginResource(conn, fetchPluginCommands); // action deps only
  const panelsRes = usePluginResource(conn, fetchPluginPanels); // action deps only

  const onAction = useCallback<OnAction>(
    (action, extra) => {
      const deps: ActionDeps = {
        pluginId,
        panelIds: panelsRes.state.kind === 'ready' ? panelsOfPlugin(panelsRes.state.value, pluginId).map((p) => p.id) : [],
        commands: commandsRes.state.kind === 'ready' ? commandsRes.state.value : [],
        callRpc: (req) => callPluginRpc(conn, req.pluginId, req.rpc, req.params),
        insertPrompt: (t) => {
          const ok = setPendingInsert(t);
          if (ok) setNotice('Prompt ready. Open a task: it is in the composer, not sent.');
          return ok;
        },
        // Mobile passes no context yet (there is no task id at hand on this screen).
        openPanel: (panel) => router.push(pluginPanelHref(pluginId, panel)),
        confirmOpenUrl: (host, url) => confirmNative('Open link?', `This opens ${host} in your browser.\n${url}`, 'Open'),
        openUrl: openExternal,
        refresh: reload,
      };
      void runViewAction(action, deps, extra).then((outcome) => {
        if (outcome.kind === 'refused') setActionError(outcome.reason);
        else if (outcome.kind === 'error') setActionError(outcome.message);
        else setActionError(null);
      });
    },
    [conn, pluginId, router, reload, panelsRes.state, commandsRes.state],
  );

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load this panel" body={state.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  if (state.value.kind === 'failed') {
    return (
      <View style={styles.center}>
        <EmptyState title={state.value.title} body={state.value.body}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <PluginView view={state.value.view} onAction={onAction} />
      {notice !== null && <Text style={styles.notice}>{notice}</Text>}
      {actionError !== null && <Text style={styles.actionError}>{actionError}</Text>}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[2] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    notice: { ...theme.type.caption, color: theme.foregroundSubtle },
    actionError: { ...theme.type.caption, color: theme.status.danger },
  });
}
