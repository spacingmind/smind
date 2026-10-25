// listView.ts turns the daemon's plugin.list / plugin.get into what the Plugins screens draw, as plain data, so the
// states are unit-testable without rendering. Everything shown is a string rendered through <Text>: plugin names,
// descriptions and errors are plugin-authored and are never interpreted.

import { PluginCommand, PluginConflict, PluginInfo, PluginListing, PluginProfileEntry, PluginSourceKind } from './types';

export const PLUGINS_OFF_TEXT = 'Plugins are off on this daemon. Turn them on in config.yaml on the computer.';
export const PLUGINS_EMPTY_TEXT = 'No plugins are installed on this daemon.';
export const MANAGE_ON_COMPUTER_TEXT = 'Manage plugins on the computer: Settings -> Plugins';
export const LINK_WARNING_TEXT = 'Live directory: changes after consent are not re-reviewed';

export type PluginStatus = 'Enabled' | 'Disabled' | 'Pending update' | 'Error';
export type StatusTone = 'success' | 'neutral' | 'warning' | 'destructive';

/** Error beats a pending update beats the enabled flag: one chip, the most important fact. */
export function pluginStatus(p: PluginInfo): { label: PluginStatus; tone: StatusTone } {
  if (p.lastError) return { label: 'Error', tone: 'destructive' };
  if (p.pending) return { label: 'Pending update', tone: 'warning' };
  return p.enabled ? { label: 'Enabled', tone: 'success' } : { label: 'Disabled', tone: 'neutral' };
}

const SOURCE_LABEL: Record<PluginSourceKind, string> = { dir: 'Folder', link: 'Link', git: 'Git', archive: 'Archive' };

export function sourceLabel(kind: string): string {
  return (SOURCE_LABEL as Record<string, string>)[kind] ?? kind;
}

export interface PluginRowView {
  id: string;
  name: string;
  version: string;
  source: string;
  status: PluginStatus;
  tone: StatusTone;
  lastError: string | null;
  linkWarning: string | null;
}

export type PluginsListView =
  | { kind: 'off'; notice: string; rows: PluginRowView[] }
  | { kind: 'empty'; text: string }
  | { kind: 'list'; rows: PluginRowView[] };

function rowOf(p: PluginInfo): PluginRowView {
  const s = pluginStatus(p);
  return {
    id: p.id,
    name: p.name || p.id,
    version: p.version,
    source: sourceLabel(p.sourceKind),
    status: s.label,
    tone: s.tone,
    lastError: p.lastError ? p.lastError : null,
    linkWarning: p.sourceKind === 'link' ? LINK_WARNING_TEXT : null,
  };
}

export function pluginsListView(listing: PluginListing): PluginsListView {
  const rows = listing.plugins.map(rowOf);
  if (!listing.globalEnabled) return { kind: 'off', notice: PLUGINS_OFF_TEXT, rows };
  if (rows.length === 0) return { kind: 'empty', text: PLUGINS_EMPTY_TEXT };
  return { kind: 'list', rows };
}

export interface DetailProfile {
  name: string;
  provider: string;
  escalations: string[];
}

export interface PluginDetailView {
  header: PluginRowView;
  description: string | null;
  source: string;
  dataOnly: boolean;
  process: string[];
  capabilities: string[];
  mcpServers: { name: string; transport: string; owner: string }[];
  skills: { name: string; owner: string }[];
  profiles: (DetailProfile & { owner: string })[];
  commands: { label: string; owner: string }[];
  conflicts: { kind: string; name: string; reason: string }[];
  pending: boolean;
  /** Set when an update waits: the summary below is the pending version's, not the running one's. */
  pendingNotice: string | null;
  /** The only mutation hint: where to do it. The screen offers no control. */
  manageHint: string;
}

function ownerLabel(plugin: string): string {
  return `from ${plugin}`;
}

function profileView(p: PluginProfileEntry, owner: string): DetailProfile & { owner: string } {
  return { name: p.name, provider: p.provider, escalations: p.escalations?.filter((e) => e !== '') ?? [], owner };
}

/** The consent summary as the daemon returned it (the pending one when an update waits), plus owner labels. */
export function pluginDetailView(p: PluginInfo, commands: readonly PluginCommand[]): PluginDetailView {
  const c = p.pending?.consent ?? p.consent;
  const owner = ownerLabel(p.id);
  const conflicts: PluginConflict[] = p.conflicts ?? [];
  return {
    header: rowOf(p),
    description: c.description ? c.description : null,
    source: p.source,
    dataOnly: c.dataOnly,
    process: c.process ?? [],
    capabilities: c.capabilities ?? [],
    mcpServers: (c.mcpServers ?? []).map((m) => ({ name: m.name, transport: m.transport, owner })),
    skills: (c.skills ?? []).map((name) => ({ name, owner })),
    profiles: (c.profiles ?? []).map((pr) => profileView(pr, owner)),
    commands: commands.filter((cmd) => cmd.plugin === p.id).map((cmd) => ({ label: `/${cmd.qualified}`, owner })),
    conflicts: conflicts.map((x) => ({ kind: x.kind, name: x.name, reason: x.reason })),
    pending: p.pending !== undefined,
    pendingNotice: p.pending ? `Pending update ${p.pending.consent.version}: this is what it would declare. It is not applied yet.` : null,
    manageHint: MANAGE_ON_COMPUTER_TEXT,
  };
}
