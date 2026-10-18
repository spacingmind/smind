import {
  QueryClient,
  QueryClientContext,
  useMutation,
  useMutationState,
  useQuery,
} from "@tanstack/react-query";
import { useCallback, useContext, useEffect, useMemo, useRef } from "react";

import { upsertReport } from "@/lib/account-usage/model";
import {
  DEFAULT_TTL_SECONDS,
  isUnknownMethod,
  isUsageReport,
  usageDataFrom,
  usageMutationKeys,
  usageQueryOptions,
} from "@/lib/account-usage/query";
import type {
  AccountUsage,
  AccountUsageParams,
  AccountUsageResult,
  UsageQueryData,
} from "@/lib/account-usage/types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * Plan-usage reports on the React Query cache (ADR-0032; replaces the module
 * `UsageStore` of ADR-0028 PR-D1). Every surface reads the one
 * `queryKeys.accountUsage()` entry of its connection's QueryClient, so Settings,
 * the modal, the sidebar footer and the chat pill share reports, pending state
 * and the call discipline below. Ported from Paseo's `usage/queries.ts`.
 *
 * ## Call discipline (AC54; the daemon's call budget is the real guarantee,
 * this is the client being polite)
 *
 * - Reading is `useQuery(usageQueryOptions)`: one `mode:"cached"` request per
 *   QueryClient, none when data is cached, never again on mount, focus,
 *   reconnect, retry or an `account.*` event beyond a cached re-read.
 * - `touch` is only ever sent by the cadence controller of a surface that
 *   passes `touch: true` (the modal while open, Settings -> Usage while
 *   mounted, the sidebar footer summary): while the window is visible AND
 *   focused, and never more than once per `ttlSeconds` (the first one included).
 * - `refresh` is only ever sent from `refreshOne` / `refreshAll`, i.e. a button.
 */

/** A pressed Refresh stays disabled this long on the client (AC36); the daemon's own floor is the guarantee. */
export const REFRESH_COOLDOWN_MS = 30_000;

/**
 * How long an unobserved touch / refresh mutation stays in the mutation cache. The touch cadence
 * (`lastTouchAt`) and the refresh cooldown are read from those entries, so they must outlive the
 * default 5 minutes (a daemon TTL can be longer).
 */
const MUTATION_GC_MS = 60 * 60 * 1000;

export interface UsageSnapshot {
  /** `idle` = nothing asked yet, `loading` = first answer pending. */
  phase: "idle" | "loading" | "ready" | "error";
  /** False once the daemon answered "unknown method": it predates `account.usage`; never asked again. */
  supportsUsage: boolean;
  /** `accountUsage.enabled` as the daemon last said. */
  enabled: boolean;
  ttlSeconds: number;
  reports: readonly AccountUsage[];
  /** Why the first answer failed (`phase: "error"`). */
  error: string | null;
  /** Account ids with a refresh request in flight. */
  refreshing: ReadonlySet<number>;
  refreshingAll: boolean;
  /** Account ids whose last refresh request failed; cleared by the next success or a newer report. */
  failed: ReadonlySet<number>;
  /** When a Refresh was last pressed per account (client clock, ms). */
  pressedAt: Readonly<Record<number, number>>;
}

export interface UsageReports {
  snapshot: UsageSnapshot;
  /** The card's Refresh: `refresh` for that account only (AC36). Never call except from a button. */
  refreshOne: (accountId: number) => Promise<void>;
  /** Refresh all: one `refresh` for every account. Never call except from a button. */
  refreshAll: () => Promise<void>;
  /** Error "Try again": one more `cached` read. */
  retry: () => void;
}

interface RefreshVariables {
  /** The request's ids; undefined = every account. */
  accountIds: number[] | undefined;
  /** The accounts the press covers (all of them for Refresh all). */
  targets: number[];
}

/** What a refresh press remembers, to tell later whether a newer report has since cleared its failure. */
interface RefreshContext {
  fetchedAt: Record<number, string | undefined>;
}

interface RefreshState {
  status: "idle" | "pending" | "success" | "error";
  submittedAt: number;
  variables: RefreshVariables | undefined;
  context: RefreshContext | undefined;
}

interface TouchState {
  status: "idle" | "pending" | "success" | "error";
  submittedAt: number;
  error: string | null;
}

const EMPTY_SET: ReadonlySet<number> = new Set();
const NO_REPORTS: readonly AccountUsage[] = [];

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function heldData(queryClient: QueryClient): UsageQueryData | undefined {
  return queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage());
}

/**
 * What a usage hook reads when it renders outside a `DaemonQueryProvider` (a unit test of a token row,
 * an old embedding): an empty cache and no connection, so it reads nothing and sends nothing, exactly
 * as a pill with no usage source did before the cache.
 */
const DETACHED_QUERY_CLIENT = new QueryClient();

/** True while this document is visible and has focus: the only time a surface may send `touch`. */
export function windowIsActive(): boolean {
  return document.visibilityState === "visible" && document.hasFocus();
}

/** When the last `touch` went out on this connection, or null. */
function lastTouchAt(queryClient: QueryClient): number | null {
  let last: number | null = null;
  for (const mutation of queryClient.getMutationCache().findAll({ mutationKey: usageMutationKeys.touch })) {
    const at = mutation.state.submittedAt;
    if (at > 0 && (last === null || at > last)) last = at;
  }
  return last;
}

/**
 * Subscribes a surface to the shared reports. Mounting reads `cached` once per
 * connection when nothing is cached; live `account.usage` / `account.removed`
 * events reach the cache through the event bridge, so nothing refetches.
 *
 * `touch: true` (a surface that is open and showing usage) additionally
 * drives `touch` (AC54): one when the surface opens in an active window (that
 * single request also fills the cache, so no separate `cached` goes out), then
 * at most one per TTL, with the timer paused while the window is hidden or
 * blurred and no catch-up burst on return (on becoming active it touches only
 * if the TTL has elapsed since the last touch, otherwise it waits out the
 * remainder).
 *
 * `enabled: false` reads the cache without ever fetching (a chat pill whose
 * provider has no plan windows).
 */
export function useUsageReports({
  touch = false,
  enabled = true,
}: { touch?: boolean; enabled?: boolean } = {}): UsageReports {
  const { client } = useDaemonConnection();
  const queryClient = useContext(QueryClientContext) ?? DETACHED_QUERY_CLIENT;

  // The TTL clock must survive a reconnect (a new QueryClient has no touch mutations), or the new
  // connection's first touch would go out at once instead of the one `cached` read a reconnect gets.
  const carriedTouch = useRef<number | null>(null);
  const touchedAt = useCallback((): number | null => {
    const held = lastTouchAt(queryClient);
    const carried = carriedTouch.current;
    return held === null ? carried : carried === null ? held : Math.max(held, carried);
  }, [queryClient]);

  // A touching surface in an active window lists through its first touch, so it must not also send
  // `cached`; decided once per connection, at mount (and not while another surface's touch is already
  // filling the cache).
  const listing = useRef<{ queryClient: QueryClient; viaTouch: boolean } | null>(null);
  if (listing.current === null || listing.current.queryClient !== queryClient) {
    const ttlMs = (heldData(queryClient)?.ttlSeconds ?? DEFAULT_TTL_SECONDS) * 1000;
    const last = touchedAt();
    const ttlElapsed = last === null || Date.now() - last >= ttlMs;
    const touching = queryClient.isMutating({ mutationKey: usageMutationKeys.touch }) > 0;
    listing.current = { queryClient, viaTouch: touch && windowIsActive() && (ttlElapsed || touching) };
  }

  const query = useQuery(
    {
      ...usageQueryOptions(client),
      enabled: enabled && client !== null && !listing.current.viaTouch,
    },
    queryClient,
  );
  const data = query.data;

  const touchMutation = useMutation<AccountUsageResult, Error, void>({
    mutationKey: usageMutationKeys.touch,
    gcTime: MUTATION_GC_MS,
    mutationFn: () => {
      if (!client) throw new Error("no daemon connection");
      const params: AccountUsageParams = { mode: "touch" };
      return client.call<AccountUsageResult>("account.usage", params);
    },
    onSuccess: (result) => {
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (held) => usageDataFrom(result, held));
    },
    onError: (err) => {
      if (isUnknownMethod(err)) {
        queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), {
          supportsUsage: false,
          enabled: true,
          ttlSeconds: DEFAULT_TTL_SECONDS,
          reports: [],
        });
      }
      // Any other failure leaves what is on screen; a first answer that failed shows as `phase: "error"`.
    },
  }, queryClient);

  const refreshMutation = useMutation<AccountUsageResult, Error, RefreshVariables, RefreshContext>({
    mutationKey: usageMutationKeys.refresh,
    gcTime: MUTATION_GC_MS,
    mutationFn: ({ accountIds }) => {
      if (!client) throw new Error("no daemon connection");
      const params: AccountUsageParams = accountIds ? { accountIds, mode: "refresh" } : { mode: "refresh" };
      return client.call<AccountUsageResult>("account.usage", params);
    },
    onMutate: ({ targets }) => {
      const fetchedAt: Record<number, string | undefined> = {};
      for (const id of targets) {
        fetchedAt[id] = heldData(queryClient)?.reports.find((r) => r.accountId === id)?.report?.fetchedAt;
      }
      return { fetchedAt };
    },
    onSuccess: (result) => {
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (held) => {
        if (!held) return usageDataFrom(result, undefined);
        let reports: readonly AccountUsage[] = held.reports;
        for (const report of (result?.reports ?? []).filter(isUsageReport)) reports = upsertReport(reports, report);
        return { ...held, enabled: result?.enabled !== false, reports: [...reports] };
      });
    },
  }, queryClient);

  const touchStates = useMutationState<TouchState>({
    filters: { mutationKey: usageMutationKeys.touch },
    select: (m) => ({
      status: m.state.status,
      submittedAt: m.state.submittedAt,
      error: m.state.error ? errorMessage(m.state.error) : null,
    }),
  }, queryClient);
  const refreshStates = useMutationState<RefreshState>({
    filters: { mutationKey: usageMutationKeys.refresh },
    select: (m) => ({
      status: m.state.status,
      submittedAt: m.state.submittedAt,
      variables: m.state.variables as RefreshVariables | undefined,
      context: m.state.context as RefreshContext | undefined,
    }),
  }, queryClient);

  const queryStatus = query.status;
  const queryFetching = query.fetchStatus === "fetching";
  const queryError = query.error;
  const snapshot = useMemo<UsageSnapshot>(() => {
    const reports = data?.reports ?? NO_REPORTS;
    const latestTouch = touchStates.reduce<TouchState | null>(
      (latest, state) => (latest === null || state.submittedAt >= latest.submittedAt ? state : latest),
      null,
    );
    let phase: UsageSnapshot["phase"] = "idle";
    let error: string | null = null;
    if (data !== undefined) {
      phase = "ready";
    } else if (queryStatus === "error") {
      phase = "error";
      error = errorMessage(queryError);
    } else if (latestTouch?.status === "pending" || queryFetching) {
      phase = "loading";
    } else if (latestTouch?.status === "error") {
      phase = "error";
      error = latestTouch.error;
    }

    const pressedAt: Record<number, number> = {};
    const refreshing = new Set<number>();
    const latestPerAccount = new Map<number, RefreshState>();
    let refreshingAll = false;
    for (const state of [...refreshStates].sort((a, b) => a.submittedAt - b.submittedAt)) {
      if (!state.variables) continue;
      for (const id of state.variables.targets) {
        pressedAt[id] = state.submittedAt;
        latestPerAccount.set(id, state);
        if (state.status === "pending") refreshing.add(id);
      }
      if (state.status === "pending" && state.variables.accountIds === undefined) refreshingAll = true;
    }
    const failed = new Set<number>();
    for (const [id, state] of latestPerAccount) {
      if (state.status !== "error") continue;
      // A report that arrived after the press (an event, a touch) clears the failure.
      const current = reports.find((r) => r.accountId === id)?.report?.fetchedAt;
      if (current === state.context?.fetchedAt[id]) failed.add(id);
    }

    return {
      phase,
      supportsUsage: data?.supportsUsage ?? true,
      enabled: data?.enabled ?? true,
      ttlSeconds: data?.ttlSeconds ?? DEFAULT_TTL_SECONDS,
      reports,
      error,
      refreshing: refreshing.size === 0 ? EMPTY_SET : refreshing,
      refreshingAll,
      failed: failed.size === 0 ? EMPTY_SET : failed,
      pressedAt,
    };
  }, [data, queryStatus, queryFetching, queryError, touchStates, refreshStates]);

  // The cadence controller reads through refs so its timer is not reset by every render.
  const latest = useRef({ snapshot, touchedAt, touchMutate: touchMutation.mutate, hasClient: client !== null });
  useEffect(() => {
    latest.current = { snapshot, touchedAt, touchMutate: touchMutation.mutate, hasClient: client !== null };
  });

  const msUntilTouch = useCallback((): number => {
    const last = latest.current.touchedAt();
    if (last === null) return 0;
    return Math.max(0, last + latest.current.snapshot.ttlSeconds * 1000 - Date.now());
  }, []);

  /**
   * One `touch` for every account, at most once per TTL: sends nothing inside the TTL, when polling is
   * off, or when the daemon lacks the RPC. The caller decides *when* (visible + focused + open).
   */
  const touchNow = useCallback((): boolean => {
    const { snapshot: s, touchMutate, hasClient } = latest.current;
    if (!hasClient || !s.supportsUsage || (!s.enabled && s.phase === "ready")) return false;
    if (msUntilTouch() > 0) return false;
    carriedTouch.current = Date.now();
    touchMutate();
    return true;
  }, [msUntilTouch]);

  useEffect(() => {
    if (!touch || !enabled || client === null) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const clear = () => {
      if (timer !== null) clearTimeout(timer);
      timer = null;
    };
    const tick = () => {
      clear();
      if (!windowIsActive()) return;
      touchNow();
      // Nothing is pending when polling is off or the daemon lacks the RPC (no touch sent, TTL clock
      // never started): check again once per TTL instead of every second, since a permanently
      // mounted footer would otherwise run a no-op timer for as long as the app is open.
      const wait = msUntilTouch();
      timer = setTimeout(tick, wait > 0 ? Math.max(1_000, wait) : latest.current.snapshot.ttlSeconds * 1000);
    };
    const onChange = () => {
      if (windowIsActive()) tick();
      else clear();
    };
    onChange();
    document.addEventListener("visibilitychange", onChange);
    window.addEventListener("focus", onChange);
    window.addEventListener("blur", onChange);
    return () => {
      clear();
      carriedTouch.current = touchedAt();
      document.removeEventListener("visibilitychange", onChange);
      window.removeEventListener("focus", onChange);
      window.removeEventListener("blur", onChange);
    };
    // `client` is a dependency so a reconnect restarts the cadence on the new connection.
  }, [touch, enabled, client, touchNow, msUntilTouch, touchedAt]);

  const { mutateAsync: refreshAsync } = refreshMutation;
  const refresh = useCallback(
    async (accountIds: number[] | undefined): Promise<void> => {
      const held = heldData(queryClient);
      if (held && !held.supportsUsage) return;
      const targets = accountIds ?? held?.reports.map((r) => r.accountId) ?? [];
      // A failure is state (`failed`), not an exception for the button handler.
      await refreshAsync({ accountIds, targets }).then(
        () => undefined,
        () => undefined,
      );
    },
    [queryClient, refreshAsync],
  );
  const refreshOne = useCallback((accountId: number) => refresh([accountId]), [refresh]);
  const refreshAll = useCallback(() => refresh(undefined), [refresh]);

  const { refetch } = query;
  const retry = useCallback(() => void refetch(), [refetch]);

  return { snapshot, refreshOne, refreshAll, retry };
}
