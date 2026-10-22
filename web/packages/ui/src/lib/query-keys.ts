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
  /** The daemon's run registry (`run.list`, all runs): resolves a cited run id to its task and chat. */
  runList: () => ["run.list"] as const,
  /** The workspace -> spaces -> tasks tree the sidebar draws and Settings -> Usage names rows from: one fetch, live via the lifecycle events. */
  workspaceTree: () => ["workspace.tree"] as const,
  /** `daemon.info` (per connection): the attachment limits the composer mirrors. */
  daemonInfo: () => ["daemon.info"] as const,
  /** The bytes of one stored image/file attachment of a run (`run.attachment`); immutable, so cached forever. */
  runAttachment: (runId: string, id: string) => ["run.attachment", { runId, id }] as const,
  /** The task worktree's path index (`task.searchIndex`) the composer's `@` mention ranks; refreshed when a run ends. */
  taskSearchIndex: (taskId: number) => ["task.searchIndex", { taskId }] as const,
  /** One `forge.search` answer (the PR/issue picker). */
  forgeSearch: (taskId: number, kind: "pr" | "issue", query: string) => ["forge.search", { taskId, kind, query }] as const,
  providerList: () => ["provider.list"] as const,
  profileList: () => ["profile.list"] as const,
  /** One `provider.test` answer per account-vocabulary provider key (`accountHealthTestKey`). */
  /** Every cached `provider.test` answer (a prefix, for removal after a reconnect or an account change). */
  providerTestAll: () => ["provider.test"] as const,
  providerTest: (providerKey: string) => ["provider.test", providerKey] as const,
  /** The `useRunProviderTest` mutation for that provider: its pending entries are the row's "Testing…". */
  // Self-learning skills (ADR-0026): the RPC name plus the params that change its answer. The
  // `*All` forms are partial keys: `invalidateQueries` matches every entry that starts with them.
  learningStatus: () => ["learning.status"] as const,
  learnedSkills: (workspaceId: number, statuses?: readonly string[], includeBody = false) =>
    ["learning.skills.list", { workspaceId, statuses, includeBody }] as const,
  learnedSkillsAll: () => ["learning.skills.list"] as const,
  learnedSkillsOf: (workspaceId: number) => ["learning.skills.list", { workspaceId }] as const,
  learnedSkill: (id: number) => ["learning.skills.get", { id }] as const,
  learnedSkillAll: () => ["learning.skills.get"] as const,
  proposals: (workspaceId: number, status?: string) => ["learning.proposals.list", { workspaceId, status }] as const,
  proposalsAll: () => ["learning.proposals.list"] as const,
  proposalsOf: (workspaceId: number) => ["learning.proposals.list", { workspaceId }] as const,
  proposal: (id: number) => ["learning.proposals.get", { id }] as const,
  proposalAll: () => ["learning.proposals.get"] as const,
  proposalPrevalence: (id: number) => ["learning.proposals.prevalence", { id }] as const,
  probeEstimate: (id: number, quick: boolean) => ["learning.probe.estimate", { id, quick }] as const,
  probeEstimateOf: (id: number) => ["learning.probe.estimate", { id }] as const,
  probeEstimateAll: () => ["learning.probe.estimate"] as const,
  probe: (id: number) => ["learning.probe.get", { id }] as const,
  probeAll: () => ["learning.probe.get"] as const,
  probesOf: (workspaceId: number, limit?: number) => ["learning.probe.list", { workspaceId, limit }] as const,
  probesOfWorkspace: (workspaceId: number) => ["learning.probe.list", { workspaceId }] as const,
  probesAll: () => ["learning.probe.list"] as const,
  learningJobs: (workspaceId: number, limit?: number) => ["learning.jobs.list", { workspaceId, limit }] as const,
  learningJobsAll: () => ["learning.jobs.list"] as const,
  learningJobsOf: (workspaceId: number) => ["learning.jobs.list", { workspaceId }] as const,
  providerTestMutation: (providerKey: string) => ["provider.test.run", providerKey] as const,
} as const;
