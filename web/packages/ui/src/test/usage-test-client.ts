import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { AccountUsage, AccountUsageParams } from "@/lib/account-usage/types";
import { RpcError, type WsClient } from "@/lib/ws-client";
import { usageResult } from "@/test/account-usage-fixtures";

/**
 * A scripted daemon for the plan-usage tests: answers `account.usage` from a
 * mutable list, records every request (the call-budget tests assert exact
 * counts and modes), and can emit the `account.usage` / `account.removed`
 * events the daemon would push.
 */
export class FakeUsageDaemon {
  reports: AccountUsage[];
  enabled = true;
  ttlSeconds = 300;
  /** When set, every `account.usage` request rejects with it. */
  failWith: Error | null = null;
  /** `account.usage` requests, in order. */
  requests: AccountUsageParams[] = [];
  /** Per-mode answers override the default (a refresh can return fresher numbers). */
  onRequest: ((params: AccountUsageParams) => AccountUsage[] | void) | null = null;

  private listeners = new Map<string, Set<(payload: unknown) => void>>();

  constructor(reports: AccountUsage[]) {
    this.reports = reports;
  }

  readonly client = {
    call: async (method: string, params?: unknown): Promise<unknown> => {
      if (method !== "account.usage") return null;
      const p = (params ?? {}) as AccountUsageParams;
      this.requests.push(p);
      if (this.failWith) throw this.failWith;
      const replaced = this.onRequest?.(p);
      if (replaced) this.reports = replaced;
      const ids = p.accountIds;
      const reports = ids ? this.reports.filter((r) => ids.includes(r.accountId)) : this.reports;
      return usageResult(reports, { enabled: this.enabled, ttlSeconds: this.ttlSeconds });
    },
  } as unknown as WsClient;

  readonly events: DaemonEvents = {
    subscribe: (topic, listener) => {
      let set = this.listeners.get(topic);
      if (!set) this.listeners.set(topic, (set = new Set()));
      set.add(listener);
      return () => set.delete(listener);
    },
  };

  emit(topic: string, payload: unknown): void {
    for (const l of this.listeners.get(topic) ?? []) l(payload);
  }

  /** Daemon without the RPC: "method not found". */
  makeOld(): void {
    this.failWith = new RpcError("method not found: account.usage");
  }

  modes(): (string | undefined)[] {
    return this.requests.map((r) => r.mode);
  }

  count(mode: "cached" | "touch" | "refresh"): number {
    return this.requests.filter((r) => r.mode === mode).length;
  }
}
