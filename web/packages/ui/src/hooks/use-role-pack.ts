import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { RolePackApplyView, RolePackCheck } from "@/lib/role-pack-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * Role-pack sync (docs/plans/active/role-pack-sync.md, PR 2) on the shared query cache (ADR-0032).
 * `profile.rolePack.check` is a read, but it carries role prompt text, so it is kept off the run-scoped
 * methods and answered only for the Settings -> Profiles human; `profile.rolePack.apply` is human-only
 * and local-only, and the daemon's refusal (an `RpcError`) is the mutation's error message, as-is.
 */

/**
 * The pack-drift check. Runs when the Profiles section opens and never anywhere else: no timer, no
 * polling, no refetch on focus or reconnect (ADR-0058 D4 -- there is no pack-drift daemon event, so the
 * check is read on demand). Retry and the refresh after an apply are the only other calls, both explicit.
 * An answer without a `seats` array (an older or unhealthy daemon) is no data, not an error.
 */
export function useRolePackCheck() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.rolePackCheck(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: false,
    queryFn: async (): Promise<RolePackCheck | null> => {
      if (!client) throw new Error("no daemon connection");
      const answer = await client.call<RolePackCheck | null>("profile.rolePack.check");
      if (answer === null || answer === undefined || answer.seats === undefined) return null;
      return { ...answer, seats: answer.seats ?? [], seatsWithUpdates: answer.seatsWithUpdates };
    },
  });
}

/** Applies the pack: `take: []` is the default scope (stale prompt fields of untouched seats). */
export function useApplyRolePack() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (v: { take?: string[] }): Promise<RolePackApplyView> => {
      if (!client) throw new Error("no daemon connection");
      const view = await client.call<RolePackApplyView>("profile.rolePack.apply", { take: v.take ?? [] });
      return { ...view, results: view?.results ?? [] };
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.rolePackCheck() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.profileList() });
    },
  });
}
