import { useEffect, useMemo, useSyncExternalStore } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { settleReports, upsertReport } from "@/lib/account-usage/model";
import type {
  AccountUsage,
  AccountUsageEventPayload,
  AccountUsageParams,
  AccountUsageResult,
  UsageMode,
} from "@/lib/account-usage/types";
import { RpcError, type WsClient } from "@/lib/ws-client";

/**
 * The plan-usage report store (ADR-0028 PR-D1), the module-store + subscribe
 * replacement for Paseo's react-query `queries.ts`. One store per WsClient, so
 * Settings, the modal (and later the chat hover card and footer item) share
 * reports, pending state and the client-side call discipline.
 *
 * ## Call discipline (AC54; the daemon's call budget is the real guarantee,
 * this is the client being polite)
 *
 * - A plain list is `mode:"cached"`: it never makes the daemon fetch.
 * - `touch` is only ever sent from {@link UsageStore.touch}, which a surface
 *   drives while it is open AND the window is visible and focused, and never
 *   more than once per `ttlSeconds` (the first one included).
 * - `refresh` is only ever sent from {@link UsageStore.refreshOne} /
 *   {@link UsageStore.refreshAll}, i.e. from a button press.
 * - Nothing here runs on mount of an unrelated screen: a store is created and
 *   listed only when a surface that wants usage mounts.
 */

/** A pressed Refresh stays disabled this long on the client (AC36); the daemon's own floor is the guarantee. */
export const REFRESH_COOLDOWN_MS = 30_000;

/** Used until the first answer says what the daemon's TTL is. */
const DEFAULT_TTL_SECONDS = 300;

export interface UsageSnapshot {
  /** `idle` = nothing asked yet, `loading` = first answer pending. */
  phase: "idle" | "loading" | "ready" | "error";
  /** False once the daemon answered "unknown method": it predates `account.usage`; never asked again. */
  supportsUsage: boolean;
  /** `accountUsage.enabled` as the daemon last said. */
  enabled: boolean;
  ttlSeconds: number;
  reports: readonly AccountUsage[];
  error: string | null;
  /** Account ids with a refresh in flight. */
  refreshing: ReadonlySet<number>;
  refreshingAll: boolean;
  /** Account ids whose last refresh request failed; cleared by the next success. */
  failed: ReadonlySet<number>;
  /** When a Refresh was last pressed per account (client clock, ms). */
  pressedAt: Readonly<Record<number, number>>;
}

const EMPTY_SET: ReadonlySet<number> = new Set();

function initialSnapshot(): UsageSnapshot {
  return {
    phase: "idle",
    supportsUsage: true,
    enabled: true,
    ttlSeconds: DEFAULT_TTL_SECONDS,
    reports: [],
    error: null,
    refreshing: EMPTY_SET,
    refreshingAll: false,
    failed: EMPTY_SET,
    pressedAt: {},
  };
}

function isUnknownMethod(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  return err instanceof RpcError && /unknown method|method not found|not implemented/i.test(message);
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function isReport(value: unknown): value is AccountUsage {
  return typeof value === "object" && value !== null && typeof (value as AccountUsage).accountId === "number";
}

function without(set: ReadonlySet<number>, id: number): ReadonlySet<number> {
  if (!set.has(id)) return set;
  const next = new Set(set);
  next.delete(id);
  return next;
}

function withId(set: ReadonlySet<number>, id: number): ReadonlySet<number> {
  if (set.has(id)) return set;
  return new Set(set).add(id);
}

export class UsageStore {
  private snapshot = initialSnapshot();
  private readonly listeners = new Set<() => void>();
  /** Surfaces currently mounted on this store (event subscription lives while > 0). */
  private consumers = 0;
  private offEvents: (() => void)[] = [];
  /** When the last `touch` (or the first list-by-touch) went out; the TTL cadence counts from it. */
  private lastTouchAt: number | null = null;
  private listing: Promise<void> | null = null;

  constructor(
    private readonly client: WsClient,
    private readonly now: () => number = Date.now,
  ) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): UsageSnapshot => this.snapshot;

  private set(patch: Partial<UsageSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const l of this.listeners) l();
  }

  /** A surface mounted: wire live events and make sure there is data (one cached list when empty). */
  attach(events: DaemonEvents | null | undefined): () => void {
    this.consumers++;
    if (this.consumers === 1 && events) {
      this.offEvents = [
        events.subscribe("account.usage", (payload) => this.onUsageEvent(payload)),
        events.subscribe("account.removed", (payload) => this.onRemoved(payload)),
      ];
    }
    return () => {
      this.consumers--;
      if (this.consumers === 0) {
        for (const off of this.offEvents) off();
        this.offEvents = [];
      }
    };
  }

  /**
   * Makes sure the store has been listed once, always as `cached`. A no-op
   * when it already has an answer (or is waiting for one).
   */
  ensureListed(): void {
    if (this.snapshot.phase !== "idle" || !this.snapshot.supportsUsage) return;
    void this.list("cached");
  }

  /** Reconnect re-list (AC54): one `cached` request, never a fetch. */
  relist(): void {
    if (!this.snapshot.supportsUsage) return;
    void this.list("cached");
  }

  /**
   * One `touch` for every account, at most once per TTL: returns false (and
   * sends nothing) inside the TTL, when polling is off, or when the daemon
   * lacks the RPC. The caller decides *when* (visible + focused + open).
   */
  touch(): boolean {
    const { supportsUsage, enabled, ttlSeconds } = this.snapshot;
    if (!supportsUsage || (!enabled && this.snapshot.phase === "ready")) return false;
    const at = this.now();
    if (this.lastTouchAt !== null && at - this.lastTouchAt < ttlSeconds * 1000) return false;
    this.lastTouchAt = at;
    void this.list("touch");
    return true;
  }

  /** Milliseconds until the next touch is allowed (0 = now). */
  msUntilTouch(): number {
    if (this.lastTouchAt === null) return 0;
    return Math.max(0, this.lastTouchAt + this.snapshot.ttlSeconds * 1000 - this.now());
  }

  private async list(mode: UsageMode): Promise<void> {
    if (this.listing && mode === "cached") return this.listing;
    if (this.snapshot.phase === "idle") this.set({ phase: "loading" });
    const run = (async () => {
      try {
        const params: AccountUsageParams = { mode };
        const res = await this.client.call<AccountUsageResult>("account.usage", params);
        const finished = (res?.reports ?? []).filter(isReport);
        this.set({
          phase: "ready",
          error: null,
          enabled: res?.enabled !== false,
          ttlSeconds: res?.ttlSeconds && res.ttlSeconds > 0 ? res.ttlSeconds : this.snapshot.ttlSeconds,
          reports: settleReports(this.snapshot.reports, finished) as AccountUsage[],
        });
      } catch (err) {
        if (isUnknownMethod(err)) {
          this.set({ phase: "ready", supportsUsage: false, error: null });
        } else if (this.snapshot.phase !== "ready") {
          this.set({ phase: "error", error: errorMessage(err) });
        }
        // A failed re-list of a ready store keeps what is on screen.
      }
    })();
    const tracked = run.finally(() => {
      if (this.listing === tracked) this.listing = null;
    });
    if (mode === "cached") this.listing = tracked;
    return tracked;
  }

  /** The card's Refresh: `refresh` for that account only (AC36). */
  refreshOne(accountId: number): Promise<void> {
    return this.refresh([accountId]);
  }

  /** Refresh all: one `refresh` for every account. */
  refreshAll(): Promise<void> {
    return this.refresh(undefined);
  }

  private async refresh(ids: number[] | undefined): Promise<void> {
    if (!this.snapshot.supportsUsage) return;
    const targets = ids ?? this.snapshot.reports.map((r) => r.accountId);
    const at = this.now();
    const pressedAt = { ...this.snapshot.pressedAt };
    let refreshing = this.snapshot.refreshing;
    let failed = this.snapshot.failed;
    for (const id of targets) {
      pressedAt[id] = at;
      refreshing = withId(refreshing, id);
      failed = without(failed, id);
    }
    this.set({ pressedAt, refreshing, failed, refreshingAll: ids === undefined ? true : this.snapshot.refreshingAll });
    try {
      const params: AccountUsageParams = ids ? { accountIds: ids, mode: "refresh" } : { mode: "refresh" };
      const res = await this.client.call<AccountUsageResult>("account.usage", params);
      let reports: readonly AccountUsage[] = this.snapshot.reports;
      for (const report of (res?.reports ?? []).filter(isReport)) reports = upsertReport(reports, report);
      this.set({ reports, enabled: res?.enabled !== false });
    } catch {
      let nextFailed = this.snapshot.failed;
      for (const id of targets) nextFailed = withId(nextFailed, id);
      this.set({ failed: nextFailed });
    } finally {
      let nextRefreshing = this.snapshot.refreshing;
      for (const id of targets) nextRefreshing = without(nextRefreshing, id);
      this.set({ refreshing: nextRefreshing, refreshingAll: ids === undefined ? false : this.snapshot.refreshingAll });
    }
  }

  private onUsageEvent(payload: unknown): void {
    const usage = (payload as AccountUsageEventPayload | null)?.usage;
    if (!isReport(usage)) return;
    // PR-C: an event for an account the UI does not know is ignored; the
    // next list (a touch answers with every account) is what adds it.
    if (!this.snapshot.reports.some((r) => r.accountId === usage.accountId)) return;
    this.set({
      reports: upsertReport(this.snapshot.reports, usage) as AccountUsage[],
      failed: without(this.snapshot.failed, usage.accountId),
    });
  }

  private onRemoved(payload: unknown): void {
    const id = (payload as { id?: unknown } | null)?.id;
    if (typeof id !== "number") return;
    if (!this.snapshot.reports.some((r) => r.accountId === id)) return;
    this.set({ reports: this.snapshot.reports.filter((r) => r.accountId !== id) });
  }
}

const stores = new WeakMap<WsClient, UsageStore>();

/** The shared store for a client (created on first use; a reconnect swaps the client, so it gets a fresh one). */
export function usageStoreFor(client: WsClient): UsageStore {
  let store = stores.get(client);
  if (!store) {
    store = new UsageStore(client);
    stores.set(client, store);
  }
  return store;
}

const NO_STORE_SNAPSHOT: UsageSnapshot = initialSnapshot();
const noop = () => () => {};

/** True while this document is visible and has focus: the only time a surface may send `touch`. */
export function windowIsActive(): boolean {
  return document.visibilityState === "visible" && document.hasFocus();
}

/**
 * Subscribes a surface to the shared report store. Mounting lists once as
 * `cached` when the store is empty (an unrelated screen never mounts this);
 * live `account.usage` / `account.removed` events upsert without a refetch.
 *
 * `touch: true` (a surface that is open and showing usage: the modal,
 * Settings -> Usage) additionally drives `touch` (AC54): one when the
 * surface opens in an active window (that single request also lists, so no
 * separate `cached` goes out), then at most one per TTL, with the timer
 * paused while the window is hidden or blurred and no catch-up burst on
 * return (on becoming active it touches only if the TTL has elapsed since
 * the last touch, otherwise it waits out the remainder).
 */
export function useAccountUsage(
  client: WsClient | null,
  events: DaemonEvents | null | undefined,
  { touch = false }: { touch?: boolean } = {},
): { store: UsageStore | null; snapshot: UsageSnapshot } {
  const store = useMemo(() => (client ? usageStoreFor(client) : null), [client]);
  const snapshot = useSyncExternalStore(
    store ? store.subscribe : noop,
    store ? store.getSnapshot : () => NO_STORE_SNAPSHOT,
  );

  useEffect(() => {
    if (!store) return;
    const detach = store.attach(events);
    // A touching surface in an active window lists through its first touch.
    if (!(touch && windowIsActive())) store.ensureListed();
    return detach;
  }, [store, events, touch]);

  useEffect(() => {
    if (!store || !touch) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const clear = () => {
      if (timer !== null) clearTimeout(timer);
      timer = null;
    };
    const tick = () => {
      clear();
      if (!windowIsActive()) return;
      store.touch();
      timer = setTimeout(tick, Math.max(1_000, store.msUntilTouch()));
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
      document.removeEventListener("visibilitychange", onChange);
      window.removeEventListener("focus", onChange);
      window.removeEventListener("blur", onChange);
    };
  }, [store, touch]);

  return { store, snapshot };
}
