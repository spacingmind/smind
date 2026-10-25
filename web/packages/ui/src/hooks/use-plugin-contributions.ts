import { useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { PluginCommand, PluginTheme } from "@/lib/plugin-contrib";

/**
 * The prompt commands and themes of the ACTIVE plugins, one cache entry each per connection (ADR-0032), refetched
 * by the event bridge on `plugin.updated`. A daemon without the RPC (or with plugins off) answers with an error or
 * an empty list; both read as "no contributions", never as a failure the UI shows.
 */
const NONE_COMMANDS: PluginCommand[] = [];
const NONE_THEMES: PluginTheme[] = [];

export function usePluginCommandsQuery() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.pluginCommands(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<PluginCommand[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<PluginCommand[]>("plugin.commands")) ?? [];
    },
  });
}

export function usePluginThemesQuery() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.pluginThemes(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<PluginTheme[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<PluginTheme[]>("plugin.themes")) ?? [];
    },
  });
}

/** The cached plugin commands, `[]` (one stable array) until they load or if the daemon has none. */
export function usePluginCommands(): PluginCommand[] {
  return usePluginCommandsQuery().data ?? NONE_COMMANDS;
}

/** The cached plugin themes, `[]` (one stable array) until they load or if the daemon has none. */
export function usePluginThemes(): PluginTheme[] {
  return usePluginThemesQuery().data ?? NONE_THEMES;
}
