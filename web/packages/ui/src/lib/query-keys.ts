/**
 * The only place React Query keys are built (ADR-0032). A key is the RPC name
 * plus the params that change its answer, so the daemon-event bridge
 * (`lib/query/event-bridge.ts`) and every consumer address the same entry.
 * `query-keys.test.ts` fails when a key array is written inline elsewhere.
 */
export const queryKeys = {
  accountList: () => ["account.list"] as const,
  /**
   * The daemon's cached plan-usage reports (`account.usage`, `mode: "cached"`).
   * Reading it never makes the daemon call a provider; see `usageQueryOptions`.
   */
  accountUsage: () => ["account.usage", { cached: true }] as const,
  providerList: () => ["provider.list"] as const,
  profileList: () => ["profile.list"] as const,
  /** One `provider.test` answer per account-vocabulary provider key (`accountHealthTestKey`). */
  providerTest: (providerKey: string) => ["provider.test", providerKey] as const,
  /** The `useRunProviderTest` mutation for that provider: its pending entries are the row's "Testing…". */
  providerTestMutation: (providerKey: string) => ["provider.test.run", providerKey] as const,
} as const;
