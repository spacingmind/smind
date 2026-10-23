import { useCallback, useMemo, useRef, useSyncExternalStore } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import type { ProviderHealth } from "@/lib/model-browser";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderTestResult } from "@/lib/types";
import { ConnectionClosedError, type WsClient } from "@/lib/ws-client";

/**
 * Lazy per-provider health over the shared cache (ADR-0032 AC12):
 * `provider.test` is a local PATH / stored-credential check -- no network --
 * but it is still only run when something calls `ensure()` (the composer
 * picker's first open, the sidebar footer's once-per-connection count) or
 * when the user presses Test (`useRunProviderTest`) -- never on render,
 * focus or a retry loop.
 *
 * Results live under `queryKeys.providerTest(id)` with the provider's own
 * taskrunner id (claude-native, codex-native, glm, kimi) -- "can a run on it
 * start?". The daemon answers a PATH check of the agent CLI for the CLI-login
 * providers (their runs use the CLI's own login, not an smind account) and a
 * stored-credential check for kimi, whose id is also its account id. This used
 * to ask by account id ("anthropic"), which reported "no account configured"
 * beside a Claude Code that runs fine: those accounts only route the daemon's
 * /v1 proxy, and the Accounts dialog still tests them under the account id.
 * The sidebar's "N healthy" footer and the Agents dots read the SAME answers. `health[id]` never triggers a fetch
 * (skipToken cache read); `ensure()` fills the missing keys with
 * `fetchQuery` -- single-flight per key (a key already fetching or cached is
 * skipped), so two opens while the first round is in flight test each
 * provider exactly once. A failed RPC leaves the key untested (undefined =
 * enabled, never cached as failed) so a later open can try again. A new
 * daemon connection is a new QueryClient, so all of it is per-connection.
 */
export function useProviderHealth(): {
  health: Record<string, ProviderHealth | undefined>;
  ensure: () => void;
  /** Provider ids whose last `provider.test` RPC itself failed (shown as "Connection failed", never used to disable a picker row). */
  rpcFailed: ReadonlySet<string>;
  /**
   * `tested`: providers (of the daemon's own list) with an answer, a failed RPC included -- it is "tested
   * and not healthy", like the old footer, so one bad provider never hides the count; the footer shows
   * its count once `tested === total`.
   */
  summary: { healthy: number; total: number; tested: number };
  /**
   * True while some provider's account-id check (the Providers rows' credential dot) has no answer: after
   * an `account.updated` drops it, a consumer that re-runs `ensure()` when this flips refills it.
   */
  accountChecksPending: boolean;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  // The daemon's own list once it has answered; the fallback only after the list failed or timed out
  // (never while it is still loading: the fallback ids are not the vocabulary provider.test expects).
  const { providers: listed, providersOrFallback, settled } = useProviderCatalog();
  const providers = listed.length > 0 ? listed : settled ? providersOrFallback : listed;

  // Read straight from the query cache through useSyncExternalStore with a snapshot that only changes
  // when an answer does (compared by value): `useQueries` hands back a new array/object per render, and a
  // consumer effect keyed on `health` then re-renders forever. Never fetches.
  const cache = queryClient.getQueryCache();
  const subscribe = useCallback((onChange: () => void) => cache.subscribe(onChange), [cache]);
  type Snapshot = { map: Record<string, ProviderHealth | undefined>; failed: Set<string>; accountPending: boolean };
  const last = useRef<Snapshot & { signature: string }>({
    signature: "",
    map: {},
    failed: new Set(),
    accountPending: false,
  });
  const getSnapshot = useCallback((): Snapshot => {
    const map: Record<string, ProviderHealth | undefined> = {};
    const failed = new Set<string>();
    let accountPending = false;
    for (const info of providers) {
      if (info.accountProvider && info.accountProvider !== info.id && !queryClient.getQueryData(queryKeys.providerTest(info.accountProvider))) {
        accountPending = true;
      }
      const result = queryClient.getQueryData<HealthEntry>(queryKeys.providerTest(info.id));
      if (!result) continue;
      // A failed RPC is "tested, not healthy" for a count, but stays untested (enabled) for a picker.
      if (result.rpcError) failed.add(info.id);
      else map[info.id] = { ok: result.ok, detail: result.detail };
    }
    const signature = JSON.stringify([map, [...failed], accountPending]);
    if (signature !== last.current.signature) last.current = { signature, map, failed, accountPending };
    return last.current;
  }, [providers, queryClient]);
  const { map: health, failed, accountPending } = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);

  const summary = useMemo(() => {
    let healthy = 0;
    let tested = 0;
    for (const info of listed) {
      if (health[info.id] || failed.has(info.id)) tested++;
      if (health[info.id]?.ok) healthy++;
    }
    return { healthy, total: listed.length, tested };
  }, [listed, health, failed]);

  // Stable identity (a consumer's effect may be keyed on it); reads the latest providers through a ref.
  const providersRef = useRef(providers);
  providersRef.current = providers;
  const ensure = useCallback((): void => {
    if (!client) return;
    for (const info of providersRef.current) {
      runProviderTest(queryClient, client, info.id);
      // The Providers page's account rows show the credential check under the account id; nothing else
      // fills it, so it is asked here too (a local, network-free check, cached like the one above).
      if (info.accountProvider && info.accountProvider !== info.id) runProviderTest(queryClient, client, info.accountProvider);
    }
  }, [client, queryClient]);

  return { health, ensure, summary, rpcFailed: failed, accountChecksPending: accountPending };
}

/** A cached `provider.test` answer; `rpcError` marks "the RPC itself failed" (not a verdict on the provider). */
type HealthEntry = ProviderTestResult & { rpcError?: boolean };

/**
 * Runs `provider.test` for one key (a provider id, or an account id) into the shared cache unless it already has a real answer or
 * an in-flight request. Never throws. An entry that was invalidated is re-run. A failed RPC is cached as
 * `rpcError` (counted as "not healthy" by a footer count, still untested for a picker) and retried by the
 * next `ensure()`; a dropped socket caches nothing.
 */
export function runProviderTest(queryClient: QueryClient, client: WsClient, key: string): void {
  const queryKey = queryKeys.providerTest(key);
  const held = queryClient.getQueryData<HealthEntry>(queryKey);
  const state = queryClient.getQueryState(queryKey);
  if (state?.fetchStatus === "fetching") return;
  if (held !== undefined && !held.rpcError && !state?.isInvalidated) return;
  void queryClient
    .fetchQuery({
      queryKey,
      queryFn: async (): Promise<HealthEntry> => client.call<ProviderTestResult>("provider.test", { provider: key }),
      staleTime: 0,
      retry: false,
    })
    .catch((err: unknown) => {
      if (err instanceof ConnectionClosedError) return;
      queryClient.setQueryData<HealthEntry>(queryKey, {
        ok: false,
        detail: err instanceof Error ? err.message : String(err),
        rpcError: true,
      });
    });
}
