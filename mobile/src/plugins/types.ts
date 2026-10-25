// The plugin.* wire the mobile app reads (internal/plugins Info/Consent/Listing, internal/wsapi/plugins.go;
// ADR-0042). Mobile is read-only: nothing here describes a mutation. Field names are the daemon's JSON.

export type PluginSourceKind = 'dir' | 'link' | 'git' | 'archive';

export interface PluginMcpEntry {
  name: string;
  transport: string;
  command?: string[];
  url?: string;
}

export interface PluginProfileEntry {
  name: string;
  provider: string;
  /** Why this profile needs the Human's attention; empty/absent for a calm one. */
  escalations?: string[];
}

/** What the Human is shown before enabling a plugin (the daemon's consent summary). */
export interface PluginConsent {
  id: string;
  name: string;
  version: string;
  description?: string;
  source: string;
  sourceKind: PluginSourceKind;
  dataOnly: boolean;
  process?: string[];
  capabilities?: string[];
  mcpServers?: PluginMcpEntry[];
  skills?: string[];
  profiles?: PluginProfileEntry[];
  /** Set for a linked source: its files are read live, so changes after consent are not re-reviewed. */
  linkWarning?: string;
}

export interface PluginConflict {
  plugin: string;
  kind: string;
  name: string;
  reason: string;
}

export interface PluginInfo {
  id: string;
  name: string;
  version: string;
  source: string;
  sourceKind: PluginSourceKind;
  enabled: boolean;
  /** Enabled AND the global switch is on. */
  active: boolean;
  consent: PluginConsent;
  pending?: { consent: PluginConsent };
  lastError?: string;
  conflicts?: PluginConflict[];
}

export interface PluginListing {
  globalEnabled: boolean;
  plugins: PluginInfo[];
}

export interface PluginCommand {
  plugin: string;
  name: string;
  /** `<plugin>:<name>`, always unique. */
  qualified: string;
  description?: string;
  /** The text the command inserts into the composer. It is never sent by the command itself. */
  prompt: string;
  alias?: string;
}

export type ThemeScheme = 'light' | 'dark';

export interface PluginTheme {
  plugin: string;
  id: string;
  /** `<plugin>:<id>`: what the preference stores. */
  qualified: string;
  label: string;
  /** Per declared scheme, a token map keyed by the web's CSS custom property names. */
  schemes: Partial<Record<ThemeScheme, Record<string, string>>>;
}

/** `plugin.updated` payload: `id` is "" for a flip of the global switch. */
export interface PluginUpdatedPayload {
  id: string;
  globalEnabled: boolean;
}

/** The only topic the plugin screens subscribe to. */
export const PLUGIN_UPDATED_TOPIC = 'plugin.updated';
