/**
 * The only place React Query keys are built (ADR-0032). A key is the RPC name
 * plus the params that change its answer, so the daemon-event bridge
 * (`lib/query/event-bridge.ts`) and every consumer address the same entry.
 * `query-keys.test.ts` fails when a key array is written inline elsewhere.
 */
/** What a `forge.search` runs against: exactly one of these goes on the wire. */
export type ForgeScope = { taskId: number } | { workspaceId: number };

export const queryKeys = {
  accountList: () => ["account.list"] as const,
  /**
   * The daemon's cached plan-usage reports (`account.usage`, `mode: "cached"`).
   * Reading it never makes the daemon call a provider; see `usageQueryOptions`.
   */
  accountUsage: () => ["account.usage", { cached: true }] as const,
  /** The daemon's cached per-instance plan usage (`provider.usage`); same call discipline as `accountUsage`. */
  providerUsage: () => ["provider.usage", { cached: true }] as const,
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
  /** One `forge.search` answer (the PR/issue picker), scoped to a task (the composer) or a workspace (the New Task dialog). */
  forgeSearch: (scope: ForgeScope, kind: "pr" | "issue", query: string) =>
    ["forge.search", { ...scope, kind, query }] as const,
  /** `forge.status` (ADR-0039 §8): fresh forever, replaced by the `forge.status` event. */
  forgeStatus: () => ["forge.status"] as const,
  /** One `workspace.branches` page (the New Task "Start from → Branch…" combobox). */
  workspaceBranches: (workspaceId: number, query: string, limit: number) =>
    ["workspace.branches", { workspaceId, query, limit }] as const,
  /** One root task's payload (`task.get`) carrying its tree-budget spend (ADR-0041 A5); refetched when a run ends and whenever the popover opens. */
  taskBudget: (taskId: number) => ["task.budget", { taskId }] as const,
  taskBudgetAll: () => ["task.budget"] as const,
  /** One task's git/PR state (`task.git`); live via the `task.git` event. */
  taskGit: (taskId: number) => ["task.git", { taskId }] as const,
  /** Every task's git/PR state in one workspace (`task.gitList`); the sidebar/header chips read it. */
  workspaceTaskGit: (workspaceId: number) => ["task.gitList", { workspaceId }] as const,
  /** Partial keys: every cached `task.git` / `task.gitList` entry (the event bridge and a reconnect address them). */
  taskGitAll: () => ["task.git"] as const,
  workspaceTaskGitAll: () => ["task.gitList"] as const,
  /** `task.prDetails` for one task (the PR panel); read only while the PR tab is focused. */
  taskPRDetails: (taskId: number) => ["task.prDetails", { taskId }] as const,
  /** `task.prPreview` for one task and base branch ("" = the task's base_ref); the Create PR dialog's prefill. */
  prPreview: (taskId: number, base: string) => ["task.prPreview", { taskId, base }] as const,
  prPreviewAll: () => ["task.prPreview"] as const,
  /** `task.archivePreview` for one task: the archive dialog's risk rows; refetched on every open. */
  archivePreview: (taskId: number) => ["task.archivePreview", { taskId }] as const,
  /** A workspace's archived tasks (`task.list` with `archived: true`): the sidebar's Archived view. */
  archivedTasks: (workspaceId: number) => ["task.list", { workspaceId, archived: true }] as const,
  archivedTasksAll: () => ["task.list", { archived: true }] as const,
  /** `workspace.branches` (base picker options; the RPC may not exist on an older daemon). */
  providerList: () => ["provider.list"] as const,
  /** `provider.dirSuggestions`: the directories Settings -> Providers offers as one-click instances. */
  providerDirSuggestions: () => ["provider.dirSuggestions"] as const,
  /** `provider.recipes`: the catalog rows the add-provider flow offers as templates. */
  providerRecipes: () => ["provider.recipes"] as const,
  profileList: () => ["profile.list"] as const,
  /** `plugin.list` (ADR-0042): the global switch and every plugin; live via the `plugin.updated` event. */
  pluginList: () => ["plugin.list"] as const,
  /** `fleet.host.list` (ADR-0048): every connected host and its link state; live via the `fleet.hosts.changed` event. */
  fleetHosts: () => ["fleet.host.list"] as const,
  /** One host's fleet audit rows (`fleet.host.audit`); read-only, fetched when its Activity row is opened. */
  fleetHostAudit: (hostId: string) => ["fleet.host.audit", hostId] as const,
  /** `automation.list` (ADR-0044): the daemon switches and every automation; live via `automation.fired/paused/updated`. */
  automationList: () => ["automation.list"] as const,
  /** Every cached audit page / memory read of every automation (a prefix, for the event bridge). */
  automationAuditAll: () => ["automation.audit"] as const,
  /** `automation.audit` pages of one automation (infinite query, newest first). */
  automationAudit: (id: number) => ["automation.audit", id] as const,
  automationMemoryAll: () => ["automation.memory"] as const,
  automationMemory: (id: number) => ["automation.memory", id] as const,
  /** `inbox.*` (ADR-0045): every cached list / count / pending-request read (a prefix, for the event bridge). */
  inboxAll: () => ["inbox"] as const,
  inboxList: () => ["inbox", "list"] as const,
  inboxCount: () => ["inbox", "count"] as const,
  /**
   * The permission request an inbox row expands to (from `run.logs`). Deliberately NOT under the "inbox" prefix: a
   * pending request's options never change, and refetching it when `inbox.changed` fires (right after it is answered)
   * would flash "already answered" over the card before its row leaves the list.
   */
  inboxRequest: (runId: string, requestId: string) => ["inbox.request", runId, requestId] as const,
  /** `skill.list`: bundled skills and the active plugins' `<plugin>:<skill>` entries. */
  skillList: () => ["skill.list"] as const,
  /** `plugin.commands`: the prompt slash commands of the active plugins; refetched on `plugin.updated`. */
  pluginCommands: () => ["plugin.commands"] as const,
  /** `plugin.themes`: the themes of the active plugins; refetched on `plugin.updated`. */
  pluginThemes: () => ["plugin.themes"] as const,
  /** `plugin.panels`: the panels and sidebar items of the active plugins; refetched on `plugin.updated`. */
  pluginPanels: () => ["plugin.panels"] as const,
  /** One `plugin.view` answer for a plugin's panel (context null when the caller had none). */
  pluginView: (pluginId: string, panelId: string, context?: unknown) =>
    ["plugin.view", pluginId, panelId, context ?? null] as const,
  /** Partial key: every cached view of one panel, across every context (a prefix, for `plugin.event`). */
  pluginViewOfPanel: (pluginId: string, panelId: string) => ["plugin.view", pluginId, panelId] as const,
  /** Partial key: every cached view of one plugin's panels (a prefix, for `plugin.event`). */
  pluginViewOfPlugin: (pluginId: string) => ["plugin.view", pluginId] as const,
  /** Every cached `plugin.view` answer (a prefix, for the event bridge). */
  pluginViewAll: () => ["plugin.view"] as const,
  /** One `provider.test` answer per key: a provider's own id for run readiness (use-provider-health), an account id for an account's credential (accounts). */
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
