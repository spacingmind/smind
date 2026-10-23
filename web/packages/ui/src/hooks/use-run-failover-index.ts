import { useEffect, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { buildFailoverIndex, EMPTY_FAILOVER_INDEX, type FailoverIndex, type FailoverRow } from "@/lib/run-failover";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * The task's failover index (see lib/run-failover.ts), from one task-wide
 * run.list, refetched when a run of the task changes status (a successor
 * starting is a run.status in another chat) or events were dropped.
 */
export function useRunFailoverIndex(client: WsClientLike | null, taskId: number | null, events: DaemonEvents | null = null): FailoverIndex {
  const [index, setIndex] = useState<FailoverIndex>(EMPTY_FAILOVER_INDEX);

  useEffect(() => {
    setIndex(EMPTY_FAILOVER_INDEX);
    if (!client || taskId === null) return;
    let cancelled = false;
    const load = () => {
      client
        .call<FailoverRow[]>("run.list", { taskId })
        .then((rows) => {
          if (!cancelled) setIndex(buildFailoverIndex(rows ?? []));
        })
        .catch(() => {});
    };
    load();
    const offStatus = events?.subscribe("run.status", (payload) => {
      if ((payload as { taskId?: number } | null)?.taskId === taskId) load();
    });
    const offDropped = events?.subscribe("event.dropped", load);
    return () => {
      cancelled = true;
      offStatus?.();
      offDropped?.();
    };
  }, [client, taskId, events]);

  return index;
}
