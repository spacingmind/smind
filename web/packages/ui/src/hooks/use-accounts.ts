import { skipToken, useMutation, useMutationState, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { useUsageReports } from "@/hooks/use-usage-reports";
import { accountStatus, type AccountStatus } from "@/lib/account-status";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { Account, ProviderTestResult } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";

/**
 * The account list and the account mutations on the shared React Query cache
 * (ADR-0032 AC6): one `account.list` entry per connection, written by
 * `account.updated`/`account.removed` through the event bridge and by the
 * mutations below (upsert-by-id, so the event arriving too is idempotent).
 *
 * `provider.test` results live next to them under
 * `queryKeys.providerTest(key)`: a local diagnostic that is never fetched by
 * a query -- `useProviderTestResult` reads the cache only (skipToken), and
 * `useRunProviderTest` writes into it when the user presses Test.
 */

/** The account list. Loads once per connection; events and mutations keep it live. */
export function useAccounts() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.accountList(),
    enabled: client !== null,
    staleTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
    queryFn: async (): Promise<Account[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<Account[]>("account.list")) ?? [];
    },
  });
}

/** Writes an `account.*` mutation's result into the account-list cache (upsert-by-id). */
export function upsertAccountInCache(queryClient: QueryClient, account: Account): void {
  queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) => {
    if (!held) return [account];
    const index = held.findIndex((existing) => existing.id === account.id);
    if (index === -1) return [...held, account];
    const next = held.slice();
    next[index] = account;
    return next;
  });
}

function useAccountMutation<TVars>(
  method: string,
  apply: (queryClient: QueryClient, result: Account) => void,
  toParams: (vars: TVars) => unknown,
) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<Account, Error, TVars>({
    mutationFn: async (vars) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<Account>(method, toParams(vars));
    },
    onSuccess: (account) => apply(queryClient, account),
  });
}

/** `account.rename` {id, label}. */
export function useRenameAccount() {
  return useAccountMutation<{ id: number; label: string }>(
    "account.rename",
    upsertAccountInCache,
    (vars) => vars,
  );
}

/** `account.updateCredential` {id, credential, baseUrl?} -- the same params shape providers-section's form sends. */
export function useUpdateAccountCredential() {
  return useAccountMutation<{ id: number; credential: string; baseUrl?: string }>(
    "account.updateCredential",
    upsertAccountInCache,
    ({ id, credential, baseUrl }) => ({ id, credential, ...(baseUrl ? { baseUrl } : {}) }),
  );
}

/** `account.remove` {id}; drops the account from the list. */
export function useRemoveAccount() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<null, Error, number>({
    mutationFn: async (id) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<null>("account.remove", { id });
    },
    onSuccess: (_result, id) => {
      queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) =>
        held ? held.filter((a) => a.id !== id) : held,
      );
    },
  });
}

/** The cached `provider.test` result for one provider key, if any. Never fetches. */
export function useProviderTestResult(providerKey: string): ProviderTestResult | undefined {
  return useQuery({
    queryKey: queryKeys.providerTest(providerKey),
    queryFn: skipToken,
    staleTime: Infinity,
    gcTime: Infinity,
  }).data as ProviderTestResult | undefined;
}

/** Runs `provider.test {provider}` on demand (a Test button, never a mount/focus/retry) and stores the answer in the cache. */
export function useRunProviderTest() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<ProviderTestResult, Error, string>({
    mutationKey: queryKeys.providerTestMutation("__all__"),
    mutationFn: async (provider) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ProviderTestResult>("provider.test", { provider });
    },
    onSuccess: (result, provider) => {
      queryClient.setQueryData(queryKeys.providerTest(provider), result);
    },
    onError: (err, provider) => {
      // Same shape the old per-dialog hook stored: an erroring diagnostic is a failed test, not a thrown UI.
      const message = err instanceof RpcError || err instanceof Error ? err.message : String(err);
      queryClient.setQueryData(queryKeys.providerTest(provider), { ok: false, detail: message });
    },
  });
  const pending = useMutationState({
    filters: { mutationKey: queryKeys.providerTestMutation("__all__") },
    select: (m) => ({ status: m.state.status, provider: m.state.variables as string | undefined }),
  });
  const isTesting = (providerKey: string) =>
    pending.some((p) => p.provider === providerKey && p.status === "pending");
  return {
    runTest: mutation.mutate,
    isTesting,
  };
}

/**
 * The one status of an account (AC5): the shared account list, the shared
 * usage cache (matched by accountId, read-only) and the cached local test
 * result folded through `accountStatus`. Undefined for an unknown id.
 */
export function useAccountStatus(accountId: number): AccountStatus | undefined {
  const accounts = useAccounts().data;
  const { snapshot } = useUsageReports();
  const account = accounts?.find((a) => a.id === accountId);
  const usage = snapshot.reports.find((r) => r.accountId === accountId);
  const test = useProviderTestResult(account?.provider ?? "");
  if (!account) return undefined;
  return accountStatus({ account, usage, test });
}
