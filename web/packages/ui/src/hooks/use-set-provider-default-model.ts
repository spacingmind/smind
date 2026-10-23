import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/** `provider.setDefaultModel` result: the provider and the default it now has ("" / absent = cleared). */
interface SetDefaultModelResult {
  id: string;
  defaultModel?: string;
}

/**
 * Sets (or, with model "", clears) a provider's configured default model (U15). The value shown always comes
 * from `provider.list`: success only invalidates it (and the `provider.updated` event does the same for other
 * windows), so there is no optimistic state to roll back.
 */
export function useSetProviderDefaultModel(): {
  setDefaultModel: (providerId: string, model: string) => void;
  /** The provider whose call is in flight, else null. */
  pendingId: string | null;
  error: string | null;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<SetDefaultModelResult, Error, { provider: string; model: string }>({
    mutationFn: async (vars) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<SetDefaultModelResult>("provider.setDefaultModel", vars);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
    },
  });
  const { mutate } = mutation;
  return {
    setDefaultModel: (provider, model) => mutate({ provider, model }),
    pendingId: mutation.isPending ? (mutation.variables?.provider ?? null) : null,
    error: mutation.isError ? mutation.error.message : null,
  };
}
