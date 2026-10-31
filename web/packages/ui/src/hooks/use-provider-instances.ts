import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type {
  AddProviderInstanceParams,
  ProviderDirSuggestion,
  ProviderDirSuggestionsResult,
  ProviderInfo,
  ProviderListResult,
  UpdateProviderInstanceParams,
} from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

const NO_SUGGESTIONS: ProviderDirSuggestion[] = [];

/** `provider.dirSuggestions` (human-only, names only): read when the Add-instance area opens and after a change, never on a timer. */
function dirSuggestionsQueryOptions(client: WsClientLike | null, enabled: boolean) {
  return queryOptions({
    queryKey: queryKeys.providerDirSuggestions(),
    enabled: enabled && client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<ProviderDirSuggestionsResult> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<ProviderDirSuggestionsResult>("provider.dirSuggestions", {})) ?? { suggestions: [] };
    },
  });
}

/** The suggested instance directories; `enabled` false (the area is closed) never asks the daemon. */
export function useProviderDirSuggestions(enabled: boolean): { suggestions: ProviderDirSuggestion[]; loaded: boolean } {
  const { client } = useDaemonConnection();
  const query = useQuery(dirSuggestionsQueryOptions(client, enabled));
  return { suggestions: query.data?.suggestions ?? NO_SUGGESTIONS, loaded: query.data !== undefined };
}

/**
 * `provider.addInstance`: writes one providers.custom entry. The new instance shows up through the daemon's
 * `provider.updated` event (the event bridge re-reads provider.list), with the mutation's own invalidation as the
 * fallback when the event is missed; the daemon's refusal text is returned as `error` for an inline message.
 */
export function useAddProviderInstance(): {
  add: (params: AddProviderInstanceParams, onAdded?: () => void) => void;
  pending: boolean;
  error: string | null;
  clearError: () => void;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<unknown, Error, AddProviderInstanceParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call("provider.addInstance", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerDirSuggestions() });
    },
  });
  const { mutate, reset } = mutation;
  return {
    add: (params, onAdded) => mutate(params, { onSuccess: onAdded }),
    pending: mutation.isPending,
    error: mutation.isError ? mutation.error.message : null,
    clearError: reset,
  };
}

/**
 * `provider.updateInstance`: edits one instance's own overlay (models, disallowedTools, inheritClaudeConfig) in
 * config.yaml. Same event shape as `provider.addInstance`: the daemon publishes `provider.updated` (the event
 * bridge re-reads provider.list), with the mutation's own invalidation as the fallback; the daemon's refusal
 * text is returned as `error` for an inline message.
 */
export function useUpdateProviderInstance(): {
  update: (params: UpdateProviderInstanceParams, onDone?: () => void) => void;
  pending: boolean;
  error: string | null;
  clearError: () => void;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<unknown, Error, UpdateProviderInstanceParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call("provider.updateInstance", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
    },
  });
  const { mutate, reset } = mutation;
  return {
    update: (params, onDone) => mutate(params, { onSuccess: () => onDone?.() }),
    pending: mutation.isPending,
    error: mutation.isError ? mutation.error.message : null,
    clearError: reset,
  };
}

/** `provider.reload`: re-reads providers.custom from config.yaml (after a hand edit); answers the whole catalog. */
export function useReloadProviders(): { reload: () => void; pending: boolean; error: string | null } {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<ProviderListResult, Error, void>({
    mutationFn: async () => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ProviderListResult>("provider.reload", {});
    },
    onSuccess: (result) => {
      if (result?.providers) queryClient.setQueryData<ProviderListResult>(queryKeys.providerList(), { providers: result.providers as ProviderInfo[] });
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerDirSuggestions() });
    },
  });
  return { reload: () => mutation.mutate(), pending: mutation.isPending, error: mutation.isError ? mutation.error.message : null };
}
