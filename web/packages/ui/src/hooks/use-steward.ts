import { useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { StewardReview } from "@/lib/steward-types";

/**
 * One steward review (`steward.get`, ADR-0047 §5): commit-bound, so cached forever and re-read
 * by the `steward.updated` event. Idle until a row names its reviewId.
 */
export function useStewardReview(reviewId: number | undefined, enabled = true) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.stewardReview(reviewId ?? 0),
    enabled: client !== null && enabled && reviewId !== undefined,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<StewardReview> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<StewardReview>("steward.get", { reviewId });
    },
  });
}

/**
 * One evidence frame's bytes (`steward.frame`, ADR-0047 §9): immutable, cached forever, fetched
 * only when its thumb scrolls into the review card. The key sits under the steward prefix, so a
 * `steward.updated` event refreshes it with everything else.
 */
export function useStewardFrame(reviewId: number, name: string) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.stewardFrame(reviewId, name),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<{ contentType: string; data: string }> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<{ contentType: string; data: string }>("steward.frame", { reviewId, name });
    },
  });
}
