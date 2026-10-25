// The plugin.* wire (internal/plugins Info/Consent/Listing, internal/wsapi/plugins.go; ADR-0042). Reads are open to
// every connection; every mutation is human + local only and may answer `forbidden`.

export type PluginSourceKind = "dir" | "link" | "git" | "archive";

export interface PluginHookEvent {
  event: string;
  onError?: string;
}

export interface PluginMcpEntry {
  name: string;
  transport: string;
  command?: string[];
  url?: string;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

export interface PluginAcpEntry {
  id: string;
  command: string[];
}

export interface PluginProfileEntry {
  name: string;
  provider: string;
  permissionMode?: string;
  autoAccept?: boolean;
  orchestrationTools?: boolean;
  childPolicy?: string;
  /** Why this profile needs the Human's attention; empty/absent for a calm one. */
  escalations?: string[];
}

/** What the Human is shown before enabling a plugin. `fingerprint` is the confirmation `plugin.enable` takes. */
export interface PluginConsent {
  id: string;
  name: string;
  version: string;
  description?: string;
  source: string;
  sourceKind: PluginSourceKind;
  hash: string;
  /** Nothing the plugin declares runs code. */
  dataOnly: boolean;
  process?: string[];
  capabilities?: string[];
  hookEvents?: PluginHookEvent[];
  mcpServers?: PluginMcpEntry[];
  acpProviders?: PluginAcpEntry[];
  skills?: string[];
  profiles?: PluginProfileEntry[];
  /** Set for a linked source: its files are read live, so changes after consent are not re-reviewed. */
  linkWarning?: string;
  fingerprint: string;
}

export interface PluginConflict {
  plugin: string;
  kind: "mcpServer" | "skill" | "profile" | string;
  name: string;
  reason: string;
}

export interface PluginInfo {
  id: string;
  name: string;
  version: string;
  source: string;
  sourceKind: PluginSourceKind;
  hash: string;
  enabled: boolean;
  /** Enabled AND the global switch is on: the only state in which a contribution loads. */
  active: boolean;
  consent: PluginConsent;
  consented: boolean;
  pending?: { consent: PluginConsent };
  lastError?: string;
  conflicts?: PluginConflict[];
  installedAt: string;
  updatedAt: string;
}

export interface PluginListing {
  globalEnabled: boolean;
  plugins: PluginInfo[];
}

export interface PluginUpdateResult {
  applied: boolean;
  pending: boolean;
  plugin: PluginInfo;
}

/** `plugin.updated` payload: `id` is "" for a flip of the global switch. */
export interface PluginUpdatedPayload {
  id: string;
  globalEnabled: boolean;
}

/** One row of `skill.list`: a bundled skill, or an active plugin's as "<plugin>:<skill>". */
export interface SkillEntry {
  name: string;
  source: "bundled" | "plugin";
  owner: string | null;
  readOnly: boolean;
  description?: string;
}
