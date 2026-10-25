import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { PluginInfo, PluginListing, PluginUpdateResult, SkillEntry } from "@/lib/plugin-types";

/**
 * The plugin list and the global switch (`plugin.list`, ADR-0042), ONE cache entry per connection: Settings ->
 * Plugins reads it, the event bridge refetches it on `plugin.updated`, and the mutations below write the RPC's own
 * answer into it (upsert by id) so a row changes in place before the event arrives. Reads are open to relay sessions.
 */
export function usePlugins() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.pluginList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<PluginListing> => {
      if (!client) throw new Error("no daemon connection");
      const listing = await client.call<PluginListing>("plugin.list");
      return { globalEnabled: listing?.globalEnabled === true, plugins: listing?.plugins ?? [] };
    },
  });
}

/** `skill.list`: the skills a profile may name (bundled, and `<plugin>:<skill>` of active plugins). Failure -> no entries. */
export function useSkillList() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.skillList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<SkillEntry[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<SkillEntry[]>("skill.list")) ?? [];
    },
  });
}

function upsertPlugin(queryClient: QueryClient, plugin: PluginInfo): void {
  queryClient.setQueryData<PluginListing>(queryKeys.pluginList(), (held) => {
    if (!held) return held;
    const index = held.plugins.findIndex((p) => p.id === plugin.id);
    if (index === -1) return { ...held, plugins: [...held.plugins, plugin] };
    const plugins = held.plugins.slice();
    plugins[index] = plugin;
    return { ...held, plugins };
  });
}

/** The plugin.* mutations. Each resolves with the daemon's answer (or rejects with its own typed, human-readable message). */
export function usePluginActions() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const call = async <T,>(method: string, params: unknown): Promise<T> => {
    if (!client) throw new Error("no daemon connection");
    try {
      return await client.call<T>(method, params);
    } catch (err) {
      // A failed install/reload/update records its error on the plugin row and publishes no event: one re-read
      // shows it. A refusal (`forbidden`) changed nothing, so it costs no round trip.
      if ((err as { code?: string } | null)?.code !== "forbidden") void queryClient.invalidateQueries({ queryKey: queryKeys.pluginList() });
      throw err;
    }
  };
  return {
    install: async (source: string): Promise<PluginInfo> => {
      const info = await call<PluginInfo>("plugin.install", { source });
      upsertPlugin(queryClient, info);
      return info;
    },
    /** `confirm` is the consent fingerprint the Human was shown. */
    enable: async (id: string, confirm: string): Promise<PluginInfo> => {
      const info = await call<PluginInfo>("plugin.enable", { id, confirm });
      upsertPlugin(queryClient, info);
      return info;
    },
    disable: async (id: string): Promise<PluginInfo> => {
      const info = await call<PluginInfo>("plugin.disable", { id });
      upsertPlugin(queryClient, info);
      return info;
    },
    reload: async (id: string): Promise<PluginInfo> => {
      const info = await call<PluginInfo>("plugin.reload", { id });
      upsertPlugin(queryClient, info);
      return info;
    },
    remove: async (id: string, keepData: boolean): Promise<void> => {
      await call("plugin.remove", { id, keepData });
      queryClient.setQueryData<PluginListing>(queryKeys.pluginList(), (held) =>
        held ? { ...held, plugins: held.plugins.filter((p) => p.id !== id) } : held,
      );
    },
    /** No `source` re-reads the recorded directory; `apply` with no source applies the staged pending update. */
    update: async (id: string, opts: { source?: string; apply?: boolean }): Promise<PluginUpdateResult> => {
      const result = await call<PluginUpdateResult>("plugin.update", { id, source: opts.source ?? "", apply: opts.apply === true });
      if (result?.plugin) upsertPlugin(queryClient, result.plugin);
      return result;
    },
  };
}
