// api.ts is the whole plugin client surface of the mobile app: the read RPCs (list/get/commands/themes, the panel
// reads panels/view), and plugin.call for RPC-backed slash commands, nothing else (ADR-0042: mobile
// lists read-only; plugin.install/link/enable/disable/remove/update/reload are human + local only and the daemon
// refuses them over the relay anyway). A test pins that no other method name is ever sent.

import { RelayConnection } from '../relay/RelayConnection';
import { PluginCommand, PluginInfo, PluginListing, PluginPanelsResult, PluginTheme, PluginViewResult, PLUGIN_UI_LEVEL } from './types';

/** Every RPC this module may send. pluginsReadOnly.test.ts compares the source and the calls made against it. */
export const PLUGIN_READ_METHODS = ['plugin.list', 'plugin.get', 'plugin.commands', 'plugin.themes'] as const;

/** The panel reads (ADR-0042 section 5): also data only, guarded like the other reads. */
export const PLUGIN_PANEL_METHODS = ['plugin.panels', 'plugin.view'] as const;

/** The one plugin RPC that is not a read: run an RPC the plugin DECLARED, for an RPC-backed slash command (slashRpc.ts). */
export const PLUGIN_CALL_METHOD = 'plugin.call';

export async function callPluginRpc(conn: RelayConnection, pluginId: string, rpc: string, params: Record<string, unknown>): Promise<unknown> {
  return conn.call(PLUGIN_CALL_METHOD, { pluginId, rpc, params });
}

export async function fetchPluginList(conn: RelayConnection): Promise<PluginListing> {
  const res = (await conn.call('plugin.list')) as Partial<PluginListing> | null;
  return { globalEnabled: res?.globalEnabled === true, plugins: Array.isArray(res?.plugins) ? res.plugins : [] };
}

export async function fetchPlugin(conn: RelayConnection, id: string): Promise<PluginInfo> {
  return (await conn.call('plugin.get', { id })) as PluginInfo;
}

export async function fetchPluginCommands(conn: RelayConnection): Promise<PluginCommand[]> {
  const res = await conn.call('plugin.commands');
  return Array.isArray(res) ? (res as PluginCommand[]) : [];
}

export async function fetchPluginThemes(conn: RelayConnection): Promise<PluginTheme[]> {
  const res = await conn.call('plugin.themes');
  return Array.isArray(res) ? (res as PluginTheme[]) : [];
}

/** The panels and sidebar items of the ACTIVE plugins at this client's ui level (the daemon holds back the rest). Reads only. */
export async function fetchPluginPanels(conn: RelayConnection): Promise<PluginPanelsResult> {
  const res = (await conn.call('plugin.panels', { ui: PLUGIN_UI_LEVEL })) as Partial<PluginPanelsResult> | null;
  return { panels: Array.isArray(res?.panels) ? res.panels : [], sidebar: Array.isArray(res?.sidebar) ? res.sidebar : [] };
}

/** One panel's view document, stated at this client's ui level; `context` (a task id, ...) goes to the panel only when given. */
export async function fetchPluginView(conn: RelayConnection, pluginId: string, panelId: string, context?: unknown): Promise<unknown> {
  const params: Record<string, unknown> = { pluginId, panelId, ui: PLUGIN_UI_LEVEL };
  if (context !== undefined) params.context = context;
  const res = (await conn.call('plugin.view', params)) as Partial<PluginViewResult> | null;
  return res?.view;
}

/** What the detail screen loads: the plugin and the prompt commands (to list the plugin's own). Reads only. */
export async function fetchPluginDetail(conn: RelayConnection, id: string): Promise<{ info: PluginInfo; commands: PluginCommand[] }> {
  const [info, commands] = await Promise.all([fetchPlugin(conn, id), fetchPluginCommands(conn)]);
  return { info, commands };
}
