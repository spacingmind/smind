import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { AccountUsage, AccountUsageParams } from "@/lib/account-usage/types";
import { RpcError, type WsClient } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { usageResult } from "@/test/account-usage-fixtures";
import type { Account, AgentProfile, ProviderListResult, ProviderTestResult, RunSummary } from "@/lib/types";

/**
 * A scripted daemon for the plan-usage tests: answers `account.usage` from a
 * mutable list, records every request (the call-budget tests assert exact
 * counts and modes), and can emit the `account.usage` / `account.removed`
 * events the daemon would push.
 *
 * The accounts/providers surfaces (ADR-0032 AC5-AC9) extend the same fake:
 * `accounts` answers `account.list`, `providers` answers `provider.list`,
 * `testResults` answers `provider.test`, and the account mutations
 * (`account.rename` / `account.updateCredential` / `account.remove`) update
 * `accounts` and record their params. Every RPC records into `calls`.
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

  /** When set, `account.usage` requests (recorded at once) answer only after it settles: holds a request in flight. */
  gate: Promise<void> | null = null;

  /** Answers `account.list`; mutated by the account mutations below and by tests directly. */
  accounts: Account[] = [];
  /** Answers `provider.list`. */
  providers: ProviderListResult = { providers: [] };
  /** Answers `profile.list`; mutated by the profile mutations below and by tests directly. */
  profiles: AgentProfile[] = [];
  /** Answers `run.list` (the remove-account warning's running-run count). */
  runs: RunSummary[] = [];
  /** `provider.test` answers per provider key; a missing key answers `{ ok: true, detail: "ok" }`. */
  testResults: Record<string, ProviderTestResult> = {};
  /** `provider.test` keys whose RPC rejects (the check itself errored, as opposed to answering `{ ok: false }`). */
  testRpcErrors: Record<string, Error> = {};
  /** When set, `provider.setDefaultModel` rejects with it. */
  setDefaultModelError: Error | null = null;
  /** When set, every `account.list` / `provider.list` / `provider.test` / account-mutation request rejects with it. */
  failNonUsageWith: Error | null = null;
  /** Every non-`account.usage` request, as `{ method, params }`, in order. */
  calls: { method: string; params: unknown }[] = [];
  /** Optional shaper for an account mutation's result (the daemon's own answer, e.g. a credential swap that changes credentialType). */
  onAccountMutation: ((method: string, params: unknown, account: Account) => Account) | null = null;

  /**
   * When set, every non-`account.usage` RPC is delegated to this fake
   * (recorded, resolved by hand) instead of auto-answered — for tests that
   * drive calls manually (accounts-dialog's connect/test flows).
   */
  wsClient: FakeWsClient | null = null;

  constructor(reports: AccountUsage[], wsClient: FakeWsClient | null = null) {
    this.reports = reports;
    this.wsClient = wsClient;
  }

  private listeners = new Map<string, Set<(payload: unknown) => void>>();

  private nextId(): number {
    return this.accounts.reduce((max, a) => Math.max(max, a.id), 0) + 1;
  }

  private nextProfileId(): number {
    return this.profiles.reduce((max, p) => Math.max(max, p.ID), 0) + 1;
  }

  readonly client = {
    call: async (method: string, params?: unknown): Promise<unknown> => {
      if (method !== "account.usage" && this.wsClient) return this.wsClient.call(method, params);
      if (method !== "account.usage") {
        this.calls.push({ method, params });
        if (this.failNonUsageWith) throw this.failNonUsageWith;
        switch (method) {
          case "account.list":
            return this.accounts;
          case "provider.list":
            return this.providers;
          case "provider.setDefaultModel": {
            // The daemon's own behaviour: persist into the list (model "" clears), answer {id, defaultModel}.
            const p = params as { provider: string; model: string };
            if (this.setDefaultModelError) throw this.setDefaultModelError;
            this.providers = {
              ...this.providers,
              providers: this.providers.providers.map((info) =>
                info.id === p.provider ? { ...info, defaultModel: p.model || undefined } : info,
              ),
            };
            return { id: p.provider, defaultModel: p.model };
          }
          case "provider.test": {
            const failure = this.testRpcErrors[(params as { provider?: string } | undefined)?.provider ?? ""];
            if (failure) throw failure;
            return this.testResults[(params as { provider?: string } | undefined)?.provider ?? ""] ?? { ok: true, detail: "ok" };
          }
          case "account.rename": {
            const p = params as { id: number; label: string };
            const index = this.accounts.findIndex((a) => a.id === p.id);
            if (index === -1) throw new RpcError(`account ${p.id} not found`);
            this.accounts[index] = { ...this.accounts[index], label: p.label, updatedAt: new Date().toISOString() };
            return this.onAccountMutation ? this.onAccountMutation(method, params, this.accounts[index]) : this.accounts[index];
          }
          case "account.updateCredential": {
            const p = params as { id: number };
            const index = this.accounts.findIndex((a) => a.id === p.id);
            if (index === -1) throw new RpcError(`account ${p.id} not found`);
            this.accounts[index] = { ...this.accounts[index], updatedAt: new Date().toISOString() };
            return this.onAccountMutation ? this.onAccountMutation(method, params, this.accounts[index]) : this.accounts[index];
          }
          case "account.remove": {
            const p = params as { id: number };
            this.accounts = this.accounts.filter((a) => a.id !== p.id);
            return null;
          }
          case "run.list":
            return this.runs;
          case "profile.list":
            return this.profiles;
          case "profile.create": {
            const p = (params ?? {}) as Record<string, unknown>;
            const now = new Date().toISOString();
            const profile: AgentProfile = {
              ID: this.nextProfileId(),
              Name: String(p.name ?? ""),
              Provider: String(p.provider ?? ""),
              PermissionMode: String(p.permissionMode ?? ""),
              AutoAccept: Boolean(p.autoAccept),
              ThinkingLevel: String(p.thinkingLevel ?? ""),
              Model: p.model ? String(p.model) : "",
              Notes: String(p.notes ?? ""),
              CreatedAt: now,
              UpdatedAt: now,
            };
            this.profiles = [...this.profiles, profile];
            return profile;
          }
          case "profile.update": {
            const p = params as { id: number } & Record<string, unknown>;
            const index = this.profiles.findIndex((x) => x.ID === p.id);
            if (index === -1) throw new RpcError(`profile ${p.id} not found`);
            const fields: [keyof AgentProfile, unknown][] = [
              ["Name", p.name],
              ["Provider", p.provider],
              ["PermissionMode", p.permissionMode],
              ["AutoAccept", p.autoAccept],
              ["ThinkingLevel", p.thinkingLevel],
              ["Model", p.model],
              ["Notes", p.notes],
            ];
            const next = { ...this.profiles[index] };
            for (const [field, value] of fields) {
              if (value !== undefined) (next as Record<string, unknown>)[field as string] = value;
            }
            next.UpdatedAt = new Date().toISOString();
            this.profiles[index] = next;
            return next;
          }
          case "profile.delete": {
            const p = params as { id: number };
            this.profiles = this.profiles.filter((x) => x.ID !== p.id);
            return null;
          }
          case "account.add": {
            const p = params as { provider: string; label: string; credentialType?: string };
            const account: Account = {
              id: this.nextId(),
              provider: p.provider,
              label: p.label,
              credentialType: p.credentialType ?? "oauth",
              createdAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            };
            this.accounts = [...this.accounts, account];
            return account;
          }
          default:
            return null;
        }
      }
      const p = (params ?? {}) as AccountUsageParams;
      this.requests.push(p);
      if (this.gate) await this.gate;
      if (this.failWith) throw this.failWith;
      const replaced = this.onRequest?.(p);
      if (replaced) this.reports = replaced;
      const ids = p.accountIds;
      const reports = ids ? this.reports.filter((r) => ids.includes(r.accountId)) : this.reports;
      return usageResult(reports, { enabled: this.enabled, ttlSeconds: this.ttlSeconds });
    },
    /** What the real WsClient offers `useDaemonEvents`: the harness's bridge listens here, as in the app. */
    onNotification: (listener: (n: { topic: string; payload: unknown }) => void): (() => void) => {
      this.notificationListeners.add(listener);
      return () => this.notificationListeners.delete(listener);
    },
    callStream: async (method: string, params?: unknown, onEvent?: unknown, options?: unknown): Promise<unknown> => {
      if (this.wsClient) return this.wsClient.callStream(method, params, onEvent as never, options as never);
      return (this.client as { call: (m: string, p?: unknown) => Promise<unknown> }).call(method, params);
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

  private notificationListeners = new Set<(n: { topic: string; payload: unknown }) => void>();

  emit(topic: string, payload: unknown): void {
    // Like the app: the cache bridge (a notification listener) sees the event before any component listener.
    for (const l of this.notificationListeners) l({ topic, payload });
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

  /** How many non-`account.usage` requests recorded the given method (0 = never called). */
  countCalls(method: string): number {
    return this.calls.filter((c) => c.method === method).length;
  }
}
