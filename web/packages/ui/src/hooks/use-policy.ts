import { useQuery } from "@tanstack/react-query";

import type { PolicyAuditRow, PolicyStatus } from "@/lib/policy-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * The policy engine's status and rules (`policy.get`, ADR-0047 §2): read-only — the file is
 * human-edited on disk and never written through the UI, so nothing invalidates it live.
 */
export function usePolicyStatus() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.policyStatus(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<PolicyStatus> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<PolicyStatus>("policy.get", {});
    },
  });
}

/** One `policy.audit` page, newest first (`beforeId: next` pages older rows). */
export function usePolicyAudit(limit = 50) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.policyAudit(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<{ rows: PolicyAuditRow[]; next: number }> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<{ rows: PolicyAuditRow[]; next: number }>("policy.audit", { limit });
    },
  });
}
