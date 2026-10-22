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
export async function startRun(conn: RelayConnection, taskId: number, provider: string, prompt: string): Promise<string> {
  const result = (await conn.call('run.start', { taskId, provider, prompt })) as { runId: string };
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
  p: { taskId: number; chatId?: number; provider: string; prompt: string; whenBusy: WhenBusy; model?: string; permissionMode?: string },
): Promise<RunStartResult> {
  const params: Record<string, unknown> = { taskId: p.taskId, provider: p.provider, prompt: p.prompt, whenBusy: p.whenBusy };
  if (p.chatId !== undefined) params.chatId = p.chatId;
  if (p.model) params.model = p.model;
  if (p.permissionMode) params.permissionMode = p.permissionMode;
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
  id: string;
  label?: string;
  modes?: ModeInfo[];
  defaultMode?: string;
  models?: ModelInfo[];
  modelsDiscovered?: boolean;
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
