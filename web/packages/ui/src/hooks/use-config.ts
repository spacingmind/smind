import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { ConfigGetResult, ConfigKeyView, WritableConfigKey } from "@/lib/config-keys";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * The global config keys (ADR-0062) on the shared query cache: ONE `config.get` answers every switch row.
 * `config.updated` (event bridge) and every successful `config.set` re-read it. `writable` is the daemon's
 * per-connection verdict; the UI never decides it.
 */
export function useConfigGet() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.configGet(),
    enabled: client !== null,
    retry: false,
    staleTime: 30_000,
    queryFn: () => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ConfigGetResult>("config.get", {});
    },
  });
}

/** One key's row out of the shared `config.get` query. */
export function useConfigKey(key: WritableConfigKey): {
  view: ConfigKeyView | undefined;
  isPending: boolean;
  error: Error | null;
} {
  const query = useConfigGet();
  return {
    view: query.data?.keys.find((k) => k.key === key),
    isPending: query.isPending,
    error: query.error,
  };
}

/** True once the daemon has said this connection may change the key (never true while loading, over relay, or env-locked). */
export function useConfigWritable(key: WritableConfigKey): boolean {
  return useConfigKey(key).view?.writable === true;
}

/** `config.set` {key, value}: value is exactly the string "true" or "false". Re-reads config.get and what the key governs. */
export function useConfigSet() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const invalidate = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.configGet() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.automationList() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.pluginList() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.fleetHosts() }),
    ]);
  return useMutation({
    mutationFn: async ({ key, value }: { key: WritableConfigKey; value: boolean }) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<{ key: string; value: string; restart: boolean }>("config.set", { key, value: value ? "true" : "false" });
    },
    onSuccess: invalidate,
    // A refusal means the daemon's real value may differ from what the switch showed: re-read, never keep a guess.
    onError: () => void queryClient.invalidateQueries({ queryKey: queryKeys.configGet() }),
  });
}
