import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderInfo } from "@/lib/types";

/** `provider.setEnv` result: the `provider.updated` payload. */
type SetEnvResult = Pick<ProviderInfo, "id" | "defaultModel" | "env" | "envDirExists" | "envEffective">;

/**
 * Sets (or, with value null, clears) one launch env key of a provider (e.g. CLAUDE_CONFIG_DIR). The value shown
 * always comes from `provider.list`: success only invalidates it, so there is no optimistic state to roll back.
 */
export function useSetProviderEnv(): {
  setEnv: (providerId: string, key: string, value: string | null) => void;
  /** The provider whose call is in flight, else null. */
  pendingId: string | null;
  error: string | null;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<SetEnvResult, Error, { provider: string; key: string; value: string | null }>({
    mutationFn: async (vars) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<SetEnvResult>("provider.setEnv", vars);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
    },
  });
  const { mutate } = mutation;
  return {
    setEnv: (provider, key, value) => mutate({ provider, key, value }),
    pendingId: mutation.isPending ? (mutation.variables?.provider ?? null) : null,
    error: mutation.isError ? mutation.error.message : null,
  };
}
