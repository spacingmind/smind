import { useMutation } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";

/**
 * `doctor.sessions` (read-only, local human only): what the daemon can still read of the provider sessions
 * behind its finished runs. Counts only -- no crash report, no role-pack line, no path -- and a relay
 * session is refused outright with `forbidden`, which the Diagnostics group turns into read-only copy.
 */
export interface DoctorSessions {
  runs: number;
  withRef: number;
  readable: number;
  tooLarge: number;
  gone: number;
  checked: number;
}

/**
 * One `doctor.sessions` call per `mutate()`: a probe the person asks for, so a mutation and never a query
 * (nothing changes on its own, so there is nothing to poll or refetch), and never called over a known
 * relay connection, where the daemon would answer `forbidden`.
 */
export function useDoctorSessions() {
  const { client } = useDaemonConnection();
  return useMutation({
    mutationFn: async () => {
      if (!client) throw new Error("no daemon connection");
      return client.call<DoctorSessions>("doctor.sessions", {});
    },
  });
}
