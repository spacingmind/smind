import { useQuery, useQueryClient } from "@tanstack/react-query";

import { isFleetOff, normalizeSas, type FleetGrant, type FleetHost, type FleetHostList } from "@/lib/fleet-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * The connected hosts (`fleet.host.list`, ADR-0048), ONE cache entry per connection; the event bridge refetches it on
 * `fleet.hosts.changed`. A daemon without the fleet methods answers "method not found": that reads as `off`, not an error.
 */
export function useFleetHosts() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.fleetHosts(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<FleetHostList> => {
      if (!client) throw new Error("no daemon connection");
      try {
        const res = await client.call<{ hosts?: FleetHost[] }>("fleet.host.list", {});
        return { off: false, hosts: res?.hosts ?? [] };
      } catch (err) {
        if (isFleetOff(err)) return { off: true, hosts: [] };
        throw err;
      }
    },
  });
}

/** The fleet.host.* mutations. Each re-reads the host list afterwards, whether it succeeded or not. */
export function useFleetHostActions() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const call = async <T,>(method: string, params: unknown): Promise<T> => {
    if (!client) throw new Error("no daemon connection");
    try {
      return await client.call<T>(method, params);
    } finally {
      void queryClient.invalidateQueries({ queryKey: queryKeys.fleetHosts() });
    }
  };
  return {
    invite: (windowMinutes?: number): Promise<{ invite: string; expiresAt: string }> =>
      call("fleet.host.invite", windowMinutes === undefined ? {} : { window: `${windowMinutes}m` }),
    add: async (invite: string, alias?: string, allowCleartext?: boolean): Promise<FleetHost> => {
      const params: Record<string, unknown> = { invite };
      if (alias) params.alias = alias;
      if (allowCleartext) params.allowCleartext = true;
      return (await call<{ host: FleetHost }>("fleet.host.add", params)).host;
    },
    approve: (hostId: string, sas: string, grant: FleetGrant): Promise<unknown> =>
      call("fleet.host.approve", { hostId, sas: normalizeSas(sas), grant }),
    reject: (hostId: string): Promise<unknown> => call("fleet.host.reject", { hostId }),
    remove: (hostId: string): Promise<unknown> => call("fleet.host.rm", { hostId }),
    revoke: (hostId: string): Promise<unknown> => call("fleet.host.revoke", { hostId }),
    /** Merge `set` into and drop `remove` from this host's labels; the daemon validates and answers the resulting set. */
    setLabels: (hostId: string, change: { set?: Record<string, string>; remove?: string[] }): Promise<{ labels: Record<string, string> }> =>
      call("fleet.host.labels", { hostId, ...change }),
    rename: (hostId: string, alias: string): Promise<unknown> => call("fleet.host.rename", { hostId, alias }),
    setGrant: (hostId: string, grant: FleetGrant): Promise<unknown> => call("fleet.host.grant", { hostId, grant }),
    enableReverse: (hostId: string): Promise<unknown> => call("fleet.host.enable_reverse", { hostId }),
    disableReverse: (hostId: string): Promise<unknown> => call("fleet.host.disable_reverse", { hostId }),
  };
}
