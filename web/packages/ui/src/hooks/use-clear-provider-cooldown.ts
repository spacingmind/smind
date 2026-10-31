import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderInfo } from "@/lib/types";

/** `provider.clearCooldown` result: which provider, whether a cooldown existed, and how many held retries were woken. */
export interface ClearProviderCooldownResult {
  provider: string;
  cleared: boolean;
  woken: number;
}

/** The provider's cooldown end when it is still in the future, else null (absent, unparsable, or already past). */
export function activeCooldownUntil(provider: ProviderInfo | undefined, now: Date): Date | null {
  if (!provider?.cooldownUntil) return null;
  const at = new Date(provider.cooldownUntil);
  return Number.isNaN(at.getTime()) || at.getTime() <= now.getTime() ? null : at;
}

/**
 * Clears a provider's rate-limit cooldown by hand ("I reset my quota, retry now"): the
 * daemon releases the cooldown and any held retries, then publishes `provider.updated`,
 * so success only invalidates `provider.list` -- there is no optimistic state to roll back.
 */
export function useClearProviderCooldown(): {
  clear: (providerId: string) => Promise<void>;
  /** The provider whose call is in flight, else null. */
  pendingId: string | null;
  error: string | null;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<ClearProviderCooldownResult, Error, { provider: string }>({
    mutationFn: async (vars) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ClearProviderCooldownResult>("provider.clearCooldown", vars);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
    },
  });
  const { mutateAsync } = mutation;
  return {
    clear: (provider) => mutateAsync({ provider }).then(() => undefined),
    pendingId: mutation.isPending ? (mutation.variables?.provider ?? null) : null,
    error: mutation.isError ? mutation.error.message : null,
  };
}
