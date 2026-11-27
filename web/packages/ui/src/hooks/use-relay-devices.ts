import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { RelayDevice, RelayDeviceList, RelayDeviceWindow } from "@/lib/relay-devices";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * The relay-paired devices and the open pairing window (`relay.device.list`, B6 D1): ONE read on mount, one after
 * every mutation, none on a timer. `enabled` is false until the caller knows the connection is not a relay session --
 * the call is local-only and would only be refused there.
 */
export function useRelayDevices(enabled: boolean) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.relayDevices(),
    enabled: enabled && client !== null,
    retry: false,
    staleTime: Infinity,
    queryFn: (): Promise<RelayDeviceList> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<RelayDeviceList>("relay.device.list", {});
    },
  });
}

/**
 * The `relay.device.*` mutations, each answering the device (or, for the window, the window). Every one re-reads the
 * list afterwards, whether it succeeded or was refused: the daemon's state may differ from what the row showed.
 */
export function useRelayDeviceActions() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: queryKeys.relayDevices() });
  const call = <T,>(method: string, params: unknown): Promise<T> => {
    if (!client) return Promise.reject(new Error("no daemon connection"));
    return client.call<T>(method, params);
  };

  const approve = useMutation({
    mutationFn: (id: string) => call<RelayDevice>("relay.device.approve", { id }),
    onSuccess: invalidate,
    onError: invalidate,
  });
  const revoke = useMutation({
    mutationFn: (id: string) => call<RelayDevice>("relay.device.revoke", { id }),
    onSuccess: invalidate,
    onError: invalidate,
  });
  const rename = useMutation({
    mutationFn: ({ id, label }: { id: string; label: string }) => call<RelayDevice>("relay.device.rename", { id, label }),
    onSuccess: invalidate,
    onError: invalidate,
  });
  /** Opens the pairing window with the daemon's own default duration (no params). */
  const openWindow = useMutation({
    mutationFn: () => call<RelayDeviceWindow | null>("relay.device.window", {}),
    onSuccess: invalidate,
    onError: invalidate,
  });
  return { approve, revoke, rename, openWindow };
}
