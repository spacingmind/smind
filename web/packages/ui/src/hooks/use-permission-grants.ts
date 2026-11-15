import { useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { SavedGrant } from "@/lib/types";

/**
 * The saved "always allow in this project" grants (`permission.grants.list`): every grant with its
 * computed status, newest first. Read fresh whenever the page opens: no daemon topic announces a grant.
 */
export function usePermissionGrants() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.permissionGrants(),
    enabled: client !== null,
    staleTime: 0,
    retry: false,
    queryFn: async (): Promise<SavedGrant[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<{ grants: SavedGrant[] }>("permission.grants.list", {})).grants ?? [];
    },
  });
}

/** Revokes one grant (`permission.grants.revoke`, human-only); the next request for that command asks again. */
export function useRevokeGrant() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return async (grantId: number): Promise<void> => {
    if (!client) throw new Error("no daemon connection");
    try {
      await client.call("permission.grants.revoke", { grantId });
    } finally {
      void queryClient.invalidateQueries({ queryKey: queryKeys.permissionGrants() });
    }
  };
}
