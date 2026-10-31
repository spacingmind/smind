import { QueryClient, QueryClientContext, useQuery } from "@tanstack/react-query";
import { useCallback, useContext, useEffect, useRef } from "react";

import { windowIsActive } from "@/hooks/use-usage-reports";
import { DEFAULT_TTL_SECONDS, isUnknownMethod } from "@/lib/account-usage/query";
import type { ProviderUsageParams, ProviderUsageResult, ProviderUsageRow } from "@/lib/account-usage/provider-usage-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/** A pressed Refresh stays ignored this long on the client; the daemon's own floor is the guarantee. */
export const PROVIDER_REFRESH_COOLDOWN_MS = 30_000;

const TOUCHED_AT_KEY = ["provider.usage.touchedAt"] as const;
const REFRESH_STATE_KEY = ["provider.usage.refreshState"] as const;

interface ProviderUsageData {
  supports: boolean;
  enabled: boolean;
  ttlSeconds: number;
  reports: ProviderUsageRow[];
}

interface RefreshState {
  refreshing: string[];
  failed: string[];
  pressedAt: Record<string, number>;
}

const EMPTY_REFRESH: RefreshState = { refreshing: [], failed: [], pressedAt: {} };
const NO_ROWS: readonly ProviderUsageRow[] = [];
const DETACHED_QUERY_CLIENT = new QueryClient();

export interface ProviderUsageSnapshot {
  phase: "idle" | "loading" | "ready" | "error";
  /** False once the daemon answered "unknown method"; never asked again. */
  supports: boolean;
  enabled: boolean;
  ttlSeconds: number;
  reports: readonly ProviderUsageRow[];
  error: string | null;
  refreshing: ReadonlySet<string>;
  failed: ReadonlySet<string>;
  pressedAt: Readonly<Record<string, number>>;
  /** One `refresh` for that instance only. Never call except from a button. */
  refresh: (instanceId: string) => Promise<void>;
}

function isRow(value: unknown): value is ProviderUsageRow {
  return typeof value === "object" && value !== null && typeof (value as ProviderUsageRow).instanceId === "string";
}

function upsertRows(held: readonly ProviderUsageRow[], incoming: readonly ProviderUsageRow[]): ProviderUsageRow[] {
  const next = [...held];
  for (const row of incoming) {
    const at = next.findIndex((r) => r.instanceId === row.instanceId);
    if (at === -1) next.push(row);
    else next[at] = row;
  }
  return next;
}

function dataFrom(result: ProviderUsageResult | null | undefined, held: ProviderUsageData | undefined): ProviderUsageData {
  return {
    supports: true,
    enabled: result?.enabled !== false,
    ttlSeconds: result?.ttlSeconds && result.ttlSeconds > 0 ? result.ttlSeconds : (held?.ttlSeconds ?? DEFAULT_TTL_SECONDS),
    reports: upsertRows(held?.reports ?? [], (result?.reports ?? []).filter(isRow)),
  };
}

const errorMessage = (err: unknown): string => (err instanceof Error ? err.message : String(err));

/**
 * Per-instance plan usage (`provider.usage`), with the call discipline of `useUsageReports`: one
 * `cached` read per QueryClient; `touch` only when `touch: true`, the window is visible and focused,
 * and at most once per TTL per QueryClient; `refresh` only from `refresh()`.
 */
export function useProviderUsage({ touch = false }: { touch?: boolean } = {}): ProviderUsageSnapshot {
  const { client } = useDaemonConnection();
  const queryClient = useContext(QueryClientContext) ?? DETACHED_QUERY_CLIENT;

  const query = useQuery(
    {
      queryKey: queryKeys.providerUsage(),
      enabled: client !== null,
      staleTime: Infinity,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      retry: false,
      networkMode: "always",
      queryFn: async ({ client: qc }): Promise<ProviderUsageData> => {
        if (!client) throw new Error("no daemon connection");
        const held = qc.getQueryData<ProviderUsageData>(queryKeys.providerUsage());
        if (held && !held.supports) return held;
        const params: ProviderUsageParams = { mode: "cached" };
        try {
          const result = await client.call<ProviderUsageResult>("provider.usage", params);
          return dataFrom(result, qc.getQueryData<ProviderUsageData>(queryKeys.providerUsage()));
        } catch (err) {
          if (isUnknownMethod(err)) return { supports: false, enabled: true, ttlSeconds: DEFAULT_TTL_SECONDS, reports: [] };
          throw err;
        }
      },
    },
    queryClient,
  );
  const data = query.data;

  const refreshQuery = useQuery<RefreshState>(
    {
      queryKey: REFRESH_STATE_KEY,
      queryFn: () => EMPTY_REFRESH,
      enabled: false,
      initialData: EMPTY_REFRESH,
      staleTime: Infinity,
      gcTime: Infinity,
    },
    queryClient,
  );
  const refreshState = refreshQuery.data;

  const supports = data?.supports ?? true;
  const ttlSeconds = data?.ttlSeconds ?? DEFAULT_TTL_SECONDS;
  const ready = data !== undefined;

  // Touch cadence: one per TTL per QueryClient, only while visible and focused; re-checked when the window returns.
  const latest = useRef({ client, ttlSeconds });
  useEffect(() => {
    latest.current = { client, ttlSeconds };
  });
  useEffect(() => {
    if (!touch || !ready || !supports || client === null) return;
    const maybeTouch = () => {
      const { client: c, ttlSeconds: ttl } = latest.current;
      if (!c || !windowIsActive()) return;
      const last = queryClient.getQueryData<number>(TOUCHED_AT_KEY);
      if (last !== undefined && Date.now() - last < ttl * 1000) return;
      queryClient.setQueryData<number>(TOUCHED_AT_KEY, Date.now());
      const params: ProviderUsageParams = { mode: "touch" };
      c.call<ProviderUsageResult>("provider.usage", params).then(
        (result) =>
          queryClient.setQueryData<ProviderUsageData>(queryKeys.providerUsage(), (held) => dataFrom(result, held)),
        () => {
          // A failed touch leaves what is on screen and still burns the TTL: polite over eager.
        },
      );
    };
    maybeTouch();
    document.addEventListener("visibilitychange", maybeTouch);
    window.addEventListener("focus", maybeTouch);
    return () => {
      document.removeEventListener("visibilitychange", maybeTouch);
      window.removeEventListener("focus", maybeTouch);
    };
  }, [touch, ready, supports, client, queryClient]);

  const refresh = useCallback(
    async (instanceId: string): Promise<void> => {
      if (!client) return;
      const state = queryClient.getQueryData<RefreshState>(REFRESH_STATE_KEY) ?? EMPTY_REFRESH;
      const pressed = state.pressedAt[instanceId];
      if (state.refreshing.includes(instanceId)) return;
      if (pressed !== undefined && Date.now() - pressed < PROVIDER_REFRESH_COOLDOWN_MS) return;
      const update = (fn: (s: RefreshState) => RefreshState) =>
        queryClient.setQueryData<RefreshState>(REFRESH_STATE_KEY, (s) => fn(s ?? EMPTY_REFRESH));
      update((s) => ({
        ...s,
        refreshing: [...s.refreshing, instanceId],
        pressedAt: { ...s.pressedAt, [instanceId]: Date.now() },
      }));
      const params: ProviderUsageParams = { instanceIds: [instanceId], mode: "refresh" };
      try {
        const result = await client.call<ProviderUsageResult>("provider.usage", params);
        queryClient.setQueryData<ProviderUsageData>(queryKeys.providerUsage(), (held) => dataFrom(result, held));
        update((s) => ({
          ...s,
          refreshing: s.refreshing.filter((id) => id !== instanceId),
          failed: s.failed.filter((id) => id !== instanceId),
        }));
      } catch {
        update((s) => ({
          ...s,
          refreshing: s.refreshing.filter((id) => id !== instanceId),
          failed: s.failed.includes(instanceId) ? s.failed : [...s.failed, instanceId],
        }));
      }
    },
    [client, queryClient],
  );

  let phase: ProviderUsageSnapshot["phase"] = "idle";
  let error: string | null = null;
  if (data !== undefined) phase = "ready";
  else if (query.status === "error") {
    phase = "error";
    error = errorMessage(query.error);
  } else if (query.fetchStatus === "fetching") phase = "loading";

  return {
    phase,
    supports,
    enabled: data?.enabled ?? true,
    ttlSeconds,
    reports: data?.reports ?? NO_ROWS,
    error,
    refreshing: new Set(refreshState.refreshing),
    failed: new Set(refreshState.failed),
    pressedAt: refreshState.pressedAt,
    refresh,
  };
}
