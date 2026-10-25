// api.ts is the whole plugin client surface of the mobile app: four READ calls and nothing else (ADR-0042: mobile
// lists read-only; plugin.install/link/enable/disable/remove/update/reload are human + local only and the daemon
// refuses them over the relay anyway). A test pins that no other method name is ever sent.

import { RelayConnection } from '../relay/RelayConnection';
import { PluginCommand, PluginInfo, PluginListing, PluginTheme } from './types';

/** Every RPC this module may send. pluginsReadOnly.test.ts compares the source and the calls made against it. */
export const PLUGIN_READ_METHODS = ['plugin.list', 'plugin.get', 'plugin.commands', 'plugin.themes'] as const;

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
