import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { ForgeStatus, ForgeStatusResult } from "@/lib/types";

/**
 * `forge.status` (ADR-0039 §8): the daemon host's `gh` and its signed-in hosts. Fresh forever; the `forge.status`
 * event (event bridge) replaces it when sign-in changes, so a second tab follows.
 */
export function useForgeStatus() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.forgeStatus(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<ForgeStatus> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ForgeStatus>("forge.status", {});
    },
  });
}

/**
 * `forge.setToken`. The pasted token lives only in a ref for the length of the call: it is NOT a mutation variable
 * (the mutation cache would hold it), `gcTime` is 0, and the ref is emptied the moment the request has been built,
 * on settle and on unmount. Nothing here logs, saves or re-throws the token.
 */
export function useForgeSetToken() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const tokenRef = useRef<string | null>(null);
  const mutation = useMutation<ForgeStatusResult, Error, { host?: string }>({
    gcTime: 0,
    mutationFn: async ({ host }) => {
      const token = tokenRef.current;
      tokenRef.current = null;
      if (!client) throw new Error("no daemon connection");
      if (!token) throw new Error("no token");
      return client.call<ForgeStatusResult>("forge.setToken", host ? { host, token } : { token });
    },
    onSuccess: (result) => queryClient.setQueryData<ForgeStatus>(queryKeys.forgeStatus(), result.status),
    onSettled: () => {
      tokenRef.current = null;
    },
  });
  useEffect(
    () => () => {
      tokenRef.current = null;
    },
    [],
  );
  const { mutateAsync } = mutation;
  const submit = useCallback(
    (token: string, host?: string) => {
      tokenRef.current = token;
      return mutateAsync({ host });
    },
    [mutateAsync],
  );
  return { submit, isPending: mutation.isPending };
}

/** `forge.logout {host}`: `gh auth logout` on the daemon host; the answer replaces the cached status. */
export function useForgeLogout() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<ForgeStatusResult, Error, { host: string }>({
    gcTime: 0,
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ForgeStatusResult>("forge.logout", params);
    },
    onSuccess: (result) => queryClient.setQueryData<ForgeStatus>(queryKeys.forgeStatus(), result.status),
  });
}
