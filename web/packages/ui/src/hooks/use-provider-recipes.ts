import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type {
  AddFromRecipeParams,
  AddFromRecipeResult,
  ProviderRecipe,
  ProviderRecipesResult,
  ProviderSecretResult,
} from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

const NO_RECIPES: ProviderRecipe[] = [];

/** `provider.recipes` (read-only, no params): the catalog rows the add-provider flow offers as templates. */
function recipesQueryOptions(client: WsClientLike | null, enabled: boolean) {
  return queryOptions({
    queryKey: queryKeys.providerRecipes(),
    enabled: enabled && client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<ProviderRecipesResult> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<ProviderRecipesResult>("provider.recipes", {})) ?? { recipes: [] };
    },
  });
}

/** The recipe catalog; `enabled` false (the add-provider area is closed) never asks the daemon. The daemon's refusal text is returned as `error`. */
export function useProviderRecipes(enabled: boolean): { recipes: ProviderRecipe[]; loaded: boolean; error: string | null } {
  const { client } = useDaemonConnection();
  const query = useQuery(recipesQueryOptions(client, enabled));
  return { recipes: query.data?.recipes ?? NO_RECIPES, loaded: query.data !== undefined, error: query.isError ? query.error.message : null };
}

/**
 * `provider.addFromRecipe`: instantiate one catalog row as a providers.custom entry. The pasted key in
 * `params.secrets` is write-only: it exists only in the request, is never kept in state or a ref, never
 * logged, and the daemon's answer carries only fingerprints. The new instance shows up through the
 * daemon's `provider.updated` event, with the mutation's own invalidation as the fallback when the
 * event is missed; the daemon's refusal text is returned as `error` for an inline message.
 */
export function useAddFromRecipe(): {
  add: (params: AddFromRecipeParams, onAdded?: (r: AddFromRecipeResult) => void) => void;
  pending: boolean;
  error: string | null;
  clearError: () => void;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<AddFromRecipeResult, Error, AddFromRecipeParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<AddFromRecipeResult>("provider.addFromRecipe", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
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
 * `provider.setSecret` / `provider.clearSecret`: store or remove one named secret of a custom instance.
 * The `value` is write-only exactly like an addFromRecipe key: it exists only in the request, is never
 * kept anywhere, and only the store's fingerprint ever comes back. A success invalidates `provider.list`
 * so the instance's secret state re-reads (the daemon's `provider.updated` event is the primary path).
 */
export function useProviderSecret(): {
  set: (args: { instanceId: string; name: string; value: string }, onDone?: () => void) => void;
  clear: (args: { instanceId: string; name: string }, onDone?: () => void) => void;
  pending: boolean;
  error: string | null;
  clearError: () => void;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
  };
  const setMutation = useMutation<ProviderSecretResult, Error, { instanceId: string; name: string; value: string }>({
    mutationFn: async (args) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ProviderSecretResult>("provider.setSecret", { instanceId: args.instanceId, name: args.name, value: args.value });
    },
    onSuccess: invalidate,
  });
  const clearMutation = useMutation<ProviderSecretResult, Error, { instanceId: string; name: string }>({
    mutationFn: async (args) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ProviderSecretResult>("provider.clearSecret", { instanceId: args.instanceId, name: args.name });
    },
    onSuccess: invalidate,
  });
  const failure = setMutation.error ?? clearMutation.error;
  return {
    set: (args, onDone) => setMutation.mutate(args, { onSuccess: () => onDone?.() }),
    clear: (args, onDone) => clearMutation.mutate(args, { onSuccess: () => onDone?.() }),
    pending: setMutation.isPending || clearMutation.isPending,
    error: failure ? failure.message : null,
    clearError: () => {
      setMutation.reset();
      clearMutation.reset();
    },
  };
}
