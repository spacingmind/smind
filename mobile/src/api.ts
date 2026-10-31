// api.ts is the typed façade over RelayConnection the screens use: plain
// async functions + minimal wire-shaped types, kept out of the components
// so they can be unit-tested against a fake connection (the same fake-
// transport discipline Milestone 1's crypto tests used). Milestone 3's
// startRun joins it for Item 1's follow-up-prompt send.
//
// Field names match the daemon's JSON exactly: internal/store's structs
// carry no json tags, so Go field names are the wire keys verbatim (see
// store/types.go), and internal/runs.RunStatus likewise.

import { RelayConnection } from './relay/RelayConnection';

export interface Workspace {
  ID: number;
  Path: string;
  Title: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Space {
  ID: number;
  WorkspaceID: number;
  Title: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Task {
  ID: number;
  WorkspaceID: number;
  SpaceID: number | null;
  Title: string;
  Status: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface RunSummary {
  ID: string;
  TaskID: number;
  Provider: string;
  Prompt: string;
  Status: 'running' | 'done' | 'error' | 'stopped' | 'interrupted';
  StartedAt: string;
  FinishedAt: string | null;
  /** The run's chat; run.list sends it. */
  ChatID?: number;
  /** Why the run stopped (e.g. max_tokens); derived by the daemon. */
  StopReason?: string;
  /** The daemon's derived disposition (ADR-0027 D13): "needs-continue" when the run ended done at a turn/token/budget cap. */
  Disposition?: string;
  /** The raw error text of a failed run ("<cause> (<transport chain>)"); show it through describeRunError. */
  Err?: string;
  /** ADR-0041: "rate_limited" when the run failed on a provider quota (Status stays "error"); absent/empty otherwise. */
  FailureKind?: string;
  /** The quota window that was hit ("5h", "weekly", ...); absent/empty when unknown. */
  FailureWindow?: string;
  /** When the limit lifts (RFC 3339 UTC); null/absent when unknown. */
  FailureResetAt?: string | null;
  /** True when the daemon assumed the reset's time zone, so the clock is approximate (shown as ≈); absent/false = stated. */
  FailureResetInferred?: boolean;
  /** The zone the reset's text was read in: "UTC+8", an IANA name or "daemon-local:<zone>"; absent = no note. */
  FailureResetZone?: string;
  /** ADR-0041: the id of the run this run continues (a failover successor); absent/empty otherwise. */
  FailoverOf?: string;
  /** The profile the run used; absent on older daemons. */
  ProfileID?: number;
  /** ADR-0038: metadata of the prompt's attachments (never the bytes); lowercase wire name. */
  attachments?: AttachmentMeta[];
}

/** One attachment of a run.start request, as the daemon takes it (data is base64). */
export interface AttachmentPayload {
  kind: 'image' | 'file';
  name: string;
  mimeType: string;
  data: string;
}

/** Metadata run.list carries for a sent prompt's attachments. */
export interface AttachmentMeta {
  id: string;
  kind: 'image' | 'file' | 'file_ref' | 'pr' | 'issue';
  name?: string;
  mimeType?: string;
  size?: number;
  width?: number;
  height?: number;
  path?: string;
  /** pr/issue kinds: the forge number and title. */
  number?: number;
  title?: string;
}

/** daemon.info's attachment limits and accepted kinds (absent on an older daemon). */
export interface AttachmentsInfo {
  maxCount: number;
  maxImageBytes: number;
  maxFileBytes: number;
  maxTotalBytes: number;
  maxMessageBytes: number;
  images: string[];
  kinds?: string[];
}

/** daemon.info: only the attachment limits are read here. */
export async function fetchDaemonInfo(conn: RelayConnection): Promise<{ attachments?: AttachmentsInfo }> {
  return (await conn.call('daemon.info')) as { attachments?: AttachmentsInfo };
}

/** run.attachment: one attachment's bytes (base64), fetched lazily for a timeline thumbnail. */
export async function fetchRunAttachment(
  conn: RelayConnection,
  runId: string,
  id: string,
): Promise<{ name: string; mimeType: string; size: number; data: string }> {
  return (await conn.call('run.attachment', { runId, id })) as { name: string; mimeType: string; size: number; data: string };
}

/** workspace.list: every workspace this daemon tracks. */
export async function listWorkspaces(conn: RelayConnection): Promise<Workspace[]> {
  return (await conn.call('workspace.list')) as Workspace[];
}

/** The spaces and tasks of one workspace, in one call each. */
export async function fetchOverview(
  conn: RelayConnection,
  workspaceId: number,
): Promise<{ spaces: Space[]; tasks: Task[] }> {
  const [spaces, tasks] = await Promise.all([
    conn.call('space.list', { workspaceId }) as Promise<Space[]>,
    conn.call('task.list', { workspaceId }) as Promise<Task[]>,
  ]);
  return { spaces, tasks };
}

/** run.list, filtered to one task; most recent first (the daemon's own ordering). */
export async function listRunsForTask(conn: RelayConnection, taskId: number, chatId?: number): Promise<RunSummary[]> {
  // With a chat the daemon filters (run.list {taskId, chatId}); a task has more than one chat, so "latest" is per chat.
  const all = (await conn.call('run.list', chatId === undefined ? undefined : { taskId, chatId })) as RunSummary[];
  return all.filter((r) => r.TaskID === taskId && (chatId === undefined || r.ChatID === chatId));
}

/**
 * run.start (Milestone 3 Item 1): start a run without implicitly
 * attaching -- unlike task.prompt, whose request-scoped cancellation
 * would stop the run. Caller follows up with its own run.attach, the
 * same detach-not-stop path the screen already uses for viewing a run.
 */
export async function startRun(
  conn: RelayConnection,
  taskId: number,
  provider: string,
  prompt: string,
  attachments?: AttachmentPayload[],
): Promise<string> {
  const params: Record<string, unknown> = { taskId, provider, prompt };
  if (attachments && attachments.length > 0) params.attachments = attachments;
  const result = (await conn.call('run.start', params)) as { runId: string };
  return result.runId;
}

/**
 * run.respondPermission (Milestone 3 Item 2): resolve runId's pending
 * permission request requestId with optionId, from this connection --
 * whatever connection answers first wins server-side.
 */
export async function respondPermission(
  conn: RelayConnection,
  runId: string,
  requestId: string,
  optionId: string,
): Promise<void> {
  await conn.call('run.respondPermission', { runId, requestId, optionId });
}

/** The daemon-side per-chat prompt queue (ADR-0021 §9): one pending item, wire shape as the Go field names. */
export interface ChatQueueItem {
  ID: number;
  ChatID: number;
  Prompt: string;
  RunConfig: string;
  Source: string;
  FromTaskID: number | null;
  FromChatID: number | null;
  Priority: number;
  Status: 'queued' | 'delivered' | 'cancelled';
  RunID: string | null;
  CancelReason: string;
  CancelKind: string;
  /** A human-only hold (ADR-0041): the daemon delivers the item no earlier than this (RFC 3339); null/absent when not held. */
  NotBefore?: string | null;
  CreatedAt: string;
  UpdatedAt: string;
}

/** ADR-0021 §2's busy-chat delivery mode for run.start. */
export type WhenBusy = 'queue' | 'interrupt';

/** run.start's two outcomes with whenBusy set: the run started, or the prompt was parked in the chat's queue. */
export type RunStartResult = { kind: 'started'; runId: string } | { kind: 'queued'; queueItemId: number };

/** run.start with whenBusy: {queued, queueItemId} becomes the queued variant, {runId} the started one. */
export async function startRunWhenBusy(
  conn: RelayConnection,
  p: { taskId: number; chatId?: number; provider: string; prompt: string; whenBusy: WhenBusy; model?: string; permissionMode?: string; attachments?: AttachmentPayload[] },
): Promise<RunStartResult> {
  const params: Record<string, unknown> = { taskId: p.taskId, provider: p.provider, prompt: p.prompt, whenBusy: p.whenBusy };
  if (p.chatId !== undefined) params.chatId = p.chatId;
  if (p.model) params.model = p.model;
  if (p.permissionMode) params.permissionMode = p.permissionMode;
  if (p.attachments && p.attachments.length > 0) params.attachments = p.attachments;
  const result = (await conn.call('run.start', params)) as { runId?: string; queued?: boolean; queueItemId?: number };
  if (result.queued) return { kind: 'queued', queueItemId: result.queueItemId! };
  return { kind: 'started', runId: result.runId! };
}

/** The task's default chat (internal/workspace's DefaultChat): chat.list's first entry. */
export async function defaultChatId(conn: RelayConnection, taskId: number): Promise<number> {
  const chats = (await conn.call('chat.list', { taskId, includeArchived: true })) as Array<{ ID: number }>;
  if (chats.length === 0) throw new Error('task has no chat');
  return chats[0].ID;
}

/** chat.queueList: the chat's queue snapshot (queued, delivered, cancelled). */
export async function listChatQueue(conn: RelayConnection, chatId: number): Promise<ChatQueueItem[]> {
  const result = (await conn.call('chat.queueList', { chatId })) as { items?: ChatQueueItem[] };
  return result.items ?? [];
}

/** chat.queueCancel: cancel one queued item. */
export async function cancelQueuedPrompt(conn: RelayConnection, itemId: number): Promise<void> {
  await conn.call('chat.queueCancel', { itemId });
}

/**
 * run.stop (mobile Stop control): stop the run runId from this -- or any
 * -- connection; the daemon (internal/wsapi handleRunStop) treats stopping
 * an already-finished run as success, so the wrapper never errors for that.
 */
export async function stopRun(conn: RelayConnection, runId: string): Promise<void> {
  await conn.call('run.stop', { runId });
}

// --- Create-a-task-from-the-phone (M6) additions; existing functions above
// are unchanged. The wire shapes follow internal/wsapi handleTaskCreate /
// handleRunStart / handleProviderList and internal/taskrunner ProviderInfo.

export interface ModeInfo {
  id: string;
  label: string;
  description?: string;
  autoApproves?: boolean;
}

export interface ModelInfo {
  id: string;
  label: string;
  description?: string;
  default?: boolean;
}

export interface ProviderInfo {
  /** A string, not a closed set: the four built-ins plus every providers.custom entry (ADR-0042). */
  id: string;
  label?: string;
  /** Which runner executes it; an instance (`extends`) runs through its base's. */
  runner?: 'claude-native' | 'codex-native' | 'acp';
  /** Instances only: the built-in this one is a second login of. */
  extends?: string;
  /** True for a providers.custom entry (an instance or an ACP provider). */
  custom?: boolean;
  modes?: ModeInfo[];
  defaultMode?: string;
  models?: ModelInfo[];
  modelsDiscovered?: boolean;
  /** id of the recipe an instance was made from */
  recipe?: string;
  vendor?: 'zai' | 'anthropic' | 'openai' | 'generic';
  /** the recipe row was never verified against a live endpoint */
  unverified?: boolean;
  thinking?: 'supported' | 'hidden';
  /** fingerprint only, never a value */
  secrets?: { name: string; env: string; set: boolean; fingerprint?: string }[];
}

/** provider.list: {providers: [...]} unwrapped. */
export async function listProviders(conn: RelayConnection): Promise<ProviderInfo[]> {
  const result = (await conn.call('provider.list')) as { providers?: ProviderInfo[] };
  return result.providers ?? [];
}

/** Where a new task starts. HEAD is the daemon default (all three omitted); G4 (ADR-0039) adds variants. */
export type StartMode =
  | { kind: 'head' }
  | { kind: 'baseRef'; ref: string }
  | { kind: 'branch'; name: string }
  | { kind: 'pr'; number: number };

/** HEAD sends nothing; the variants map to task.create's baseRef / branch / prNumber. */
export function startModeParams(start: StartMode): { baseRef?: string; branch?: string; prNumber?: number } {
  switch (start.kind) {
    case 'head':
      return {};
    case 'baseRef':
      return { baseRef: start.ref };
    case 'branch':
      return { branch: start.name };
    case 'pr':
      return { prNumber: start.number };
  }
}

/** task.create; only the documented fields go out. */
export async function createTask(
  conn: RelayConnection,
  p: { workspaceId: number; spaceId?: number | null; title: string; start?: StartMode },
): Promise<Task> {
  const params: Record<string, unknown> = { workspaceId: p.workspaceId, title: p.title, ...startModeParams(p.start ?? { kind: 'head' }) };
  if (p.spaceId !== null && p.spaceId !== undefined) params.spaceId = p.spaceId;
  return (await conn.call('task.create', params)) as Task;
}

/**
 * run.start for a task's first prompt. ADR-0024 §2: while the workspace's
 * setup script runs the task is busy and unset whenBusy errors naming the
 * setup, so the first prompt always goes with whenBusy 'queue' -- it starts
 * at once, or is parked in the daemon queue until setup ends (both fine).
 */
export function startFirstRun(
  conn: RelayConnection,
  p: { taskId: number; provider: string; prompt: string; model?: string; permissionMode?: string },
): Promise<RunStartResult> {
  return startRunWhenBusy(conn, { ...p, whenBusy: 'queue' });
}

/** One usage rollup of a task.tree node; null means unknown, never 0 (ADR-0020). */
export interface TaskTreeUsage {
  runs: number;
  inputTokens: number | null;
  outputTokens: number | null;
  cacheReadTokens: number | null;
  cacheWriteTokens: number | null;
  reasoningTokens: number | null;
  costUsd: number | null;
}

/** One entry of the task.tree flat pre-order list (internal/tasktree Node). */
export interface TaskTreeNode {
  id: number;
  parentId: number | null;
  workspaceId: number;
  title: string;
  depth: number;
  taskStatus: string;
  tier: 'supervisor' | 'leader' | 'coder' | null;
  childCount: number;
  /** ADR-0041 A5 tree budget, on a ROOT node only; omitted for an unbudgeted tree. */
  budgetUsd?: number | null;
  budgetSpentUsd?: number | null;
  budgetPartial?: boolean;
  budgetUnknownRuns?: number;
  overBudget?: boolean;
  /** Stalled-tree alert: both present only while this seat's tree is idle past the threshold (RFC 3339 UTC). */
  stalled?: boolean;
  stalledSince?: string;
  run: {
    state: string;
    runningRunIds: string[];
    pendingPermissions: number;
    disposition?: string;
    failureKind?: string;
    window?: string;
    resetAt?: string;
    resetInferred?: boolean;
  };
  counts: { running: number; waitingPermission: number; failed: number; done: number; idle: number };
  usage: { own: TaskTreeUsage; subtree: TaskTreeUsage; archivedDescendants: number };
}

export interface TaskTreeResult {
  nodes: TaskTreeNode[];
  truncated: boolean;
}

/** task.tree for a task: it and its live descendants, depth-first pre-order. */
export async function fetchTaskTree(conn: RelayConnection, taskId: number): Promise<TaskTreeResult> {
  const result = (await conn.call('task.tree', { taskId })) as Partial<TaskTreeResult> | null;
  return { nodes: result?.nodes ?? [], truncated: result?.truncated === true };
}
