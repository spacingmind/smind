import { queryOptions, useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { AttachmentLimits, DaemonForge } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

interface DaemonInfoAttachments {
  attachments?: AttachmentLimits;
  forge?: DaemonForge;
  /** 'launchd' | 'systemd' | 'docker' when an OS service or container runs the daemon; absent when unmanaged. */
  managedBy?: string;
}

/** `daemon.info` per connection; the attachment caps, the forge capability and who manages the daemon are read here (the desktop banner has its own reader). */
export function daemonInfoQueryOptions(client: WsClientLike | null) {
  return queryOptions({
    queryKey: queryKeys.daemonInfo(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<DaemonInfoAttachments> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<DaemonInfoAttachments>("daemon.info")) ?? {};
    },
  });
}

/**
 * The daemon's attachment caps, or null when it reports none (an older daemon, or the call failed): null means the
 * whole attachment UI stays hidden -- no dead affordances (ADR-0038 §8).
 */
export function useAttachmentLimits(): AttachmentLimits | null {
  const { client } = useDaemonConnection();
  const query = useQuery(daemonInfoQueryOptions(client));
  return query.data?.attachments ?? null;
}

/** The daemon's forge capability, or null when it reports none; the GitHub picker reads `cli` to say why it is disabled. */
export function useDaemonForge(): DaemonForge | null {
  const { client } = useDaemonConnection();
  const query = useQuery(daemonInfoQueryOptions(client));
  return query.data?.forge ?? null;
}

/** Who manages the daemon process (`launchd`, `systemd`, ...), "" when it is unmanaged, undefined while `daemon.info` is unknown (pending or failed). */
export function useDaemonManagedBy(): string | undefined {
  const { client } = useDaemonConnection();
  const query = useQuery(daemonInfoQueryOptions(client));
  return query.data ? (query.data.managedBy ?? "") : undefined;
}
