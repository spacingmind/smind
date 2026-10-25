import type { PluginConsent, PluginInfo, PluginListing, PluginUpdateResult, SkillEntry } from "@/lib/plugin-types";
import type { McpServer } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";

export function consent(over: Partial<PluginConsent> = {}): PluginConsent {
  return {
    id: "acme",
    name: "Acme",
    version: "1.0.0",
    source: "/src/acme",
    sourceKind: "dir",
    hash: "abc123",
    dataOnly: true,
    capabilities: [],
    mcpServers: [{ name: "docs", transport: "http", url: "https://acme.test/mcp" }],
    skills: ["review"],
    profiles: [{ name: "Acme reviewer", provider: "claude-native" }],
    fingerprint: "fp-acme-1",
    ...over,
  };
}

export function plugin(over: Partial<Omit<PluginInfo, "consent">> & { consent?: Partial<PluginConsent> } = {}): PluginInfo {
  const { consent: c, ...rest } = over;
  const id = rest.id ?? c?.id ?? "acme";
  const cons = consent({ id, name: rest.name ?? c?.name ?? "Acme", ...c });
  return {
    id,
    name: cons.name,
    version: cons.version,
    source: cons.source,
    sourceKind: cons.sourceKind,
    hash: cons.hash,
    enabled: false,
    active: false,
    consent: cons,
    consented: false,
    installedAt: "2026-10-25T00:00:00Z",
    updatedAt: "2026-10-25T00:00:00Z",
    ...rest,
  };
}

/** The daemon's own owned-row shape on mcp.list. */
export function ownedMcp(over: Partial<McpServer> = {}): McpServer {
  return {
    id: 1099511627777,
    name: "acme__docs",
    transport: "http",
    command: "",
    args: [],
    env: {},
    url: "https://acme.test/mcp",
    headers: {},
    enabled: true,
    owner: "acme",
    readOnly: true,
    createdAt: "2026-10-25T00:00:00Z",
    updatedAt: "2026-10-25T00:00:00Z",
    ...over,
  };
}

/**
 * A FakeUsageDaemon that also answers the plugin surface: plugin.list / skill.list / mcp.list from mutable fields, and
 * every plugin.* mutation recorded in `calls` and answered by `onMutation` (default: the plugin as it is, enabled when
 * the call was an enable). A mutation can be refused with `refuse`.
 */
export class PluginDaemon extends FakeUsageDaemon {
  listing: PluginListing = { globalEnabled: true, plugins: [] };
  skills: SkillEntry[] = [];
  mcp: McpServer[] = [];
  /** When set, every plugin.* mutation rejects with it. */
  refuse: RpcError | null = null;
  /** Per-method answer override. */
  onMutation: Record<string, (params: Record<string, unknown>) => unknown> = {};

  constructor() {
    super([]);
    const inner = (this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call;
    (this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = async (method, params) => {
      if (method === "plugin.list") {
        this.calls.push({ method, params });
        return this.listing;
      }
      if (method === "skill.list") {
        this.calls.push({ method, params });
        return this.skills;
      }
      if (method === "mcp.list") {
        this.calls.push({ method, params });
        return this.mcp;
      }
      if (method.startsWith("plugin.")) {
        this.calls.push({ method, params });
        if (this.refuse) throw this.refuse;
        const p = (params ?? {}) as Record<string, unknown>;
        const custom = this.onMutation[method];
        if (custom) return custom(p);
        const found = this.listing.plugins.find((x) => x.id === p.id);
        if (method === "plugin.enable" && found) return { ...found, enabled: true, active: this.listing.globalEnabled, consented: true };
        if (method === "plugin.disable" && found) return { ...found, enabled: false, active: false };
        if (method === "plugin.reload" && found) return found;
        if (method === "plugin.remove") return { id: p.id, removed: true, keptData: p.keepData };
        if (method === "plugin.update") return { applied: true, pending: false, plugin: found } satisfies Partial<PluginUpdateResult>;
        return found ?? null;
      }
      return inner.call(this.client, method, params);
    };
  }
}

export const forbidden = (message: string) => new RpcError(message, "forbidden");
