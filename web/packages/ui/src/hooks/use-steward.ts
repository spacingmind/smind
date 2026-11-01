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
